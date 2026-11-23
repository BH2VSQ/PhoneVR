#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#ifndef NOMINMAX
#define NOMINMAX
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <d3d11.h>
#include <d3d11_4.h> // ID3D11Multithread is declared by the Windows 10 D3D11.4 header.
#include <dxgi1_2.h>
#include <d3dcompiler.h>
#include <openvr_driver.h>
#include "daydream_pose_model.h"

#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <condition_variable>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <cstdio>
#include <cstdlib>
#include <string>
#include <mutex>
#include <thread>
#include <vector>
#include <deque>
#include <fstream>

namespace phonevr {
constexpr uint16_t kPoseBridgePort = 48103;
constexpr uint16_t kControllerBridgePort = 48108;
constexpr uint32_t kControllerPacketSize = 44;
constexpr uint32_t kPosePacketSize = 36;
constexpr uint16_t kVideoCapturePort = 48106;
constexpr uint32_t kVideoFrameRate = 60;

struct VideoStreamProfile {
    uint16_t width;
    uint16_t height;
    const char* id;
};

const VideoStreamProfile& ActiveVideoStreamProfile() {
    static const VideoStreamProfile profile = []() -> VideoStreamProfile {
        wchar_t appData[MAX_PATH]{};
        const DWORD length = GetEnvironmentVariableW(L"APPDATA", appData, MAX_PATH);
        if (length == 0 || length >= MAX_PATH) return {1600, 720, "balanced"};
        std::wstring path(appData);
        path += L"\\PhoneVRStream\\video-profile-active.json";
        HANDLE file = CreateFileW(path.c_str(), GENERIC_READ, FILE_SHARE_READ | FILE_SHARE_WRITE,
                                  nullptr, OPEN_EXISTING, FILE_ATTRIBUTE_NORMAL, nullptr);
        if (file == INVALID_HANDLE_VALUE) return {1600, 720, "balanced"};
        char buffer[1024]{};
        DWORD bytesRead = 0;
        const BOOL readOk = ReadFile(file, buffer, sizeof(buffer) - 1, &bytesRead, nullptr);
        CloseHandle(file);
        if (!readOk || bytesRead == 0) return {1600, 720, "balanced"};
        const std::string json(buffer, bytesRead);
        const auto isProfile = [&json](const char* id) {
            const size_t key = json.find("\"profileId\"");
            if (key == std::string::npos) return false;
            const size_t colon = json.find(':', key);
            if (colon == std::string::npos) return false;
            const size_t quote = json.find('\"', colon + 1);
            if (quote == std::string::npos) return false;
            const size_t end = json.find('\"', quote + 1);
            if (end == std::string::npos) return false;
            return json.compare(quote + 1, end - quote - 1, id) == 0;
        };
        // Host publishes this active marker only after loading the profile and
        // completing the matching hardware encoder preflight. The pending UI
        // selection is intentionally ignored until Host is restarted.
        if (isProfile("performance")) return {1280, 576, "performance"};
        if (isProfile("high")) return {1920, 864, "high"};
        if (isProfile("maximum")) return {2400, 1080, "maximum"};
        return {1600, 720, "balanced"};
    }();
    return profile;
}
constexpr size_t kVideoCaptureSlots = 3;
constexpr double gDisplayRefreshRateHz = 72.0;
constexpr double DisplayFrameIntervalSeconds() { return 1.0 / gDisplayRefreshRateHz; }

// Tracking can briefly pause under Android scheduling or Wi-Fi jitter. A stale
// pose becomes untracked, but it must never disconnect the HMD device itself.
constexpr uint64_t kTrackingPoseTimeoutNs = 1000000000ULL;

// PhoneVR currently provides 3DoF orientation only. Place the virtual HMD at
// a realistic standing eye height above the SteamVR floor instead of leaving
// vecPosition at the origin (which puts the headset at floor level).
constexpr double kDefaultHeadHeightMeters = 1.65;

// Stable four-character PhoneVR tag (PHVR in ASCII).
// SteamVR requires the JSON universeID (uint32) to match the HMD's
// Prop_CurrentUniverseId_Uint64 property.
constexpr uint32_t kPhoneVRUniverseId = 0x50485652u;
constexpr char kDriverProvidedChaperoneJson[] = R"PVCHAP(
{
  "json_id": "chaperone_info",
  "version": 5,
  "time": "2026-11-21T00:00:00Z",
  "universes": [
    {
      "collision_bounds": [],
      "play_area": [1.0, 1.0],
      "seated": {
        "translation": [0.0, 0.0, 0.0],
        "yaw": 0.0
      },
      "standing": {
        "translation": [0.0, 0.0, 0.0],
        "yaw": 0.0
      },
      "universeID": 1346917970
    }
  ]
}
)PVCHAP";

uint32_t ReadBE32(const uint8_t* p) {
    return (static_cast<uint32_t>(p[0]) << 24) | (static_cast<uint32_t>(p[1]) << 16) |
           (static_cast<uint32_t>(p[2]) << 8) | static_cast<uint32_t>(p[3]);
}
uint64_t ReadBE64(const uint8_t* p) {
    return (static_cast<uint64_t>(ReadBE32(p)) << 32) | static_cast<uint64_t>(ReadBE32(p + 4));
}
float ReadBEFloat(const uint8_t* p) {
    const uint32_t bits = ReadBE32(p);
    float value = 0.0f;
    std::memcpy(&value, &bits, sizeof(value));
#if defined(_MSC_VER)
    // ReadBE32 returns the host integer value represented by network bytes;
    // memcpy therefore yields the correctly ordered IEEE-754 bit pattern.
#endif
    return value;
}

uint64_t MonotonicNowNs() {
    return static_cast<uint64_t>(std::chrono::duration_cast<std::chrono::nanoseconds>(
        std::chrono::steady_clock::now().time_since_epoch()).count());
}

void AtomicMax(std::atomic<uint64_t>& target, uint64_t value) {
    uint64_t current = target.load(std::memory_order_relaxed);
    while (current < value &&
           !target.compare_exchange_weak(current, value,
                                         std::memory_order_relaxed,
                                         std::memory_order_relaxed)) {
    }
}

class ScopedCallbackTiming {
public:
    ScopedCallbackTiming(std::atomic<uint64_t>& calls, std::atomic<uint64_t>& maxDurationNs)
        : m_calls(calls), m_maxDurationNs(maxDurationNs), m_startedNs(MonotonicNowNs()) {}
    ~ScopedCallbackTiming() {
        const uint64_t now = MonotonicNowNs();
        m_calls.fetch_add(1, std::memory_order_relaxed);
        AtomicMax(m_maxDurationNs, now >= m_startedNs ? now - m_startedNs : 0);
    }
    ScopedCallbackTiming(const ScopedCallbackTiming&) = delete;
    ScopedCallbackTiming& operator=(const ScopedCallbackTiming&) = delete;
private:
    std::atomic<uint64_t>& m_calls;
    std::atomic<uint64_t>& m_maxDurationNs;
    uint64_t m_startedNs;
};

struct VirtualDisplayTimingSnapshot {
    uint64_t presentCalls = 0;
    uint64_t waitCalls = 0;
    uint64_t completedFrames = 0;
    uint64_t waitCallsWithoutPresent = 0;
    uint64_t vsyncNoneCalls = 0;
    uint64_t vsyncWaitRenderCalls = 0;
    uint64_t vsyncNoWaitRenderCalls = 0;
    uint64_t maxPresentNs = 0;
    uint64_t maxWaitNs = 0;
    uint64_t maxPresentIntervalNs = 0;
    uint64_t presentGapsOverOneAndHalfFrames = 0;
    uint64_t presentGapsOverThreeFrames = 0;
    uint64_t pendingPresentOverwrites = 0;
    uint64_t maxWaitOvershootNs = 0;
    uint64_t waitOvershootsOverOneMs = 0;
    uint64_t waitOvershootsOverFiveMs = 0;
    uint64_t missedVirtualVsyncs = 0;
    uint64_t keyedMutexAcquireFailures = 0;
};

struct Quaternion {
    double x = 0.0, y = 0.0, z = 0.0, w = 1.0;
    double angularVelocityX = 0.0, angularVelocityY = 0.0, angularVelocityZ = 0.0;
    uint32_t sequence = 0;
    uint64_t sensorTimestampNs = 0;
    uint64_t receivedAtNs = 0;
    bool valid = false;
};

class PoseReceiver {
public:
    bool Start() {
        if (m_running.exchange(true)) return true;
        WSADATA data{};
        if (WSAStartup(MAKEWORD(2, 2), &data) != 0) { m_running = false; return false; }
        m_socket = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
        if (m_socket == INVALID_SOCKET) { WSACleanup(); m_running = false; return false; }
        // Keep the driver's loopback queue small. The receiver drains each burst
        // and records sequence gaps, while the pose publisher wakes on each new
        // generation instead of waiting for the vrserver RunFrame callback.
        int receiveBufferBytes = 32 * 1024;
        (void)setsockopt(m_socket, SOL_SOCKET, SO_RCVBUF,
                         reinterpret_cast<const char*>(&receiveBufferBytes), sizeof(receiveBufferBytes));
        sockaddr_in address{};
        address.sin_family = AF_INET;
        address.sin_port = htons(kPoseBridgePort);
        address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        if (bind(m_socket, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == SOCKET_ERROR) {
            closesocket(m_socket); m_socket = INVALID_SOCKET; WSACleanup(); m_running = false; return false;
        }
        m_thread = std::thread(&PoseReceiver::Run, this);
        return true;
    }

    void Stop() {
        if (!m_running.exchange(false)) return;
        m_updateCv.notify_all();
        if (m_socket != INVALID_SOCKET) {
            shutdown(m_socket, SD_BOTH);
            closesocket(m_socket);
            m_socket = INVALID_SOCKET;
        }
        if (m_thread.joinable()) m_thread.join();
        WSACleanup();
    }

    Quaternion Snapshot() const {
        std::lock_guard<std::mutex> lock(m_mutex);
        return m_pose;
    }

    // Wait until a new sensor generation arrives or the keepalive interval
    // expires. The generation check prevents lost wakeups if a packet arrives
    // between Snapshot() and the next wait.
    bool WaitForUpdate(uint64_t* observedGeneration, std::chrono::milliseconds timeout) {
        if (!observedGeneration) return false;
        std::unique_lock<std::mutex> lock(m_mutex);
        m_updateCv.wait_for(lock, timeout, [this, observedGeneration] {
            return !m_running.load(std::memory_order_acquire) || m_generation != *observedGeneration;
        });
        if (!m_running.load(std::memory_order_acquire)) return false;
        *observedGeneration = m_generation;
        return true; // Also true on timeout: the publisher emits a bounded keepalive.
    }

    uint64_t TakeReceivedPacketCount() {
        std::lock_guard<std::mutex> lock(m_mutex);
        const uint64_t count = m_receivedPacketCount;
        m_receivedPacketCount = 0;
        return count;
    }
    uint64_t TakeSequenceGapCount() {
        std::lock_guard<std::mutex> lock(m_mutex);
        const uint64_t count = m_sequenceGapCount;
        m_sequenceGapCount = 0;
        return count;
    }
    uint64_t TakeDrainedPacketCount() {
        std::lock_guard<std::mutex> lock(m_mutex);
        const uint64_t count = m_drainedPacketCount;
        m_drainedPacketCount = 0;
        return count;
    }

    ~PoseReceiver() { Stop(); }

private:
    static bool DecodePacket(const uint8_t* data, int length, Quaternion* output) {
        if (!data || !output || length != static_cast<int>(kPosePacketSize) ||
            std::memcmp(data, "PVSP", 4) != 0 || data[4] != 1) return false;
        Quaternion next{};
        next.x = ReadBEFloat(data + 20);
        next.y = ReadBEFloat(data + 24);
        next.z = ReadBEFloat(data + 28);
        next.w = ReadBEFloat(data + 32);
        if (!std::isfinite(next.x) || !std::isfinite(next.y) ||
            !std::isfinite(next.z) || !std::isfinite(next.w)) return false;
        const double norm = std::sqrt(next.x * next.x + next.y * next.y +
                                      next.z * next.z + next.w * next.w);
        if (norm < 0.5 || norm > 1.5) return false;
        next.x /= norm; next.y /= norm; next.z /= norm; next.w /= norm;
        next.sequence = ReadBE32(data + 8);
        next.sensorTimestampNs = ReadBE64(data + 12);
        next.receivedAtNs = MonotonicNowNs();
        next.valid = true;
        *output = next;
        return true;
    }

    static void EstimateAngularVelocity(const Quaternion& previous, Quaternion& current) {
        current.angularVelocityX = 0.0;
        current.angularVelocityY = 0.0;
        current.angularVelocityZ = 0.0;
        if (!previous.valid || previous.sensorTimestampNs == 0 ||
            current.sensorTimestampNs <= previous.sensorTimestampNs) return;
        const double dot = previous.x * current.x + previous.y * current.y +
                           previous.z * current.z + previous.w * current.w;
        if (dot < 0.0) {
            current.x = -current.x; current.y = -current.y;
            current.z = -current.z; current.w = -current.w;
        }
        const double dt = static_cast<double>(current.sensorTimestampNs - previous.sensorTimestampNs) / 1.0e9;
        if (dt < 0.001 || dt > 0.100) return;
        const double bx = -previous.x, by = -previous.y, bz = -previous.z, bw = previous.w;
        double dx = current.w * bx + current.x * bw + current.y * bz - current.z * by;
        double dy = current.w * by - current.x * bz + current.y * bw + current.z * bx;
        double dz = current.w * bz + current.x * by - current.y * bx + current.z * bw;
        double dw = current.w * bw - current.x * bx - current.y * by - current.z * bz;
        const double deltaNorm = std::sqrt(dx * dx + dy * dy + dz * dz + dw * dw);
        if (deltaNorm < 1.0e-9) return;
        dx /= deltaNorm; dy /= deltaNorm; dz /= deltaNorm; dw /= deltaNorm;
        if (dw < 0.0) { dx = -dx; dy = -dy; dz = -dz; dw = -dw; }
        const double sinHalf = std::sqrt(dx * dx + dy * dy + dz * dz);
        if (sinHalf < 1.0e-7) return;
        const double angle = 2.0 * std::atan2(sinHalf, std::clamp(dw, -1.0, 1.0));
        const double scale = angle / (sinHalf * dt);
        double vx = dx * scale, vy = dy * scale, vz = dz * scale;
        const double speed = std::sqrt(vx * vx + vy * vy + vz * vz);
        constexpr double maxAngularSpeed = 20.0;
        if (speed > maxAngularSpeed) {
            const double clampScale = maxAngularSpeed / speed;
            vx *= clampScale; vy *= clampScale; vz *= clampScale;
        }
        constexpr double alpha = 0.80;
        current.angularVelocityX = previous.angularVelocityX * (1.0 - alpha) + vx * alpha;
        current.angularVelocityY = previous.angularVelocityY * (1.0 - alpha) + vy * alpha;
        current.angularVelocityZ = previous.angularVelocityZ * (1.0 - alpha) + vz * alpha;
    }

    void Run() {
        const SOCKET socket = m_socket;
        std::array<uint8_t, 1500> buffer{};
        bool haveSequence = false;
        uint32_t lastSequence = 0;
        while (m_running.load(std::memory_order_acquire)) {
            int received = recv(socket, reinterpret_cast<char*>(buffer.data()),
                                static_cast<int>(buffer.size()), 0);
            if (received == SOCKET_ERROR) {
                if (!m_running.load(std::memory_order_acquire)) break;
                continue;
            }
            Quaternion velocityBase = Snapshot();
            Quaternion latest{};
            uint64_t validInBurst = 0;
            uint64_t drainedInBurst = 0;
            auto consume = [&](int length) {
                Quaternion candidate{};
                if (!DecodePacket(buffer.data(), length, &candidate)) return;
                if (haveSequence) {
                    const uint32_t delta = candidate.sequence - lastSequence;
                    if (delta > 1 && delta < 0x80000000u) {
                        std::lock_guard<std::mutex> lock(m_mutex);
                        m_sequenceGapCount += static_cast<uint64_t>(delta - 1);
                    }
                }
                haveSequence = true;
                lastSequence = candidate.sequence;
                EstimateAngularVelocity(velocityBase, candidate);
                velocityBase = candidate;
                latest = candidate;
                ++validInBurst;
            };
            consume(received);
            while (m_running.load(std::memory_order_acquire)) {
                u_long pendingBytes = 0;
                if (ioctlsocket(socket, FIONREAD, &pendingBytes) == SOCKET_ERROR || pendingBytes == 0) break;
                received = recv(socket, reinterpret_cast<char*>(buffer.data()),
                                static_cast<int>(buffer.size()), 0);
                if (received == SOCKET_ERROR) break;
                consume(received);
                ++drainedInBurst;
            }
            if (validInBurst == 0) continue;
            // The receiver differentiates every sample in the burst, but stores
            // only its newest orientation. The publisher wakes once per burst
            // and sends the freshest state instead of replaying stale history.
            {
                std::lock_guard<std::mutex> lock(m_mutex);
                m_pose = latest;
                ++m_generation;
                m_receivedPacketCount += validInBurst;
                m_drainedPacketCount += drainedInBurst;
            }
            m_updateCv.notify_one();
        }
    }

    std::atomic<bool> m_running{false};
    SOCKET m_socket = INVALID_SOCKET;
    std::thread m_thread;
    mutable std::mutex m_mutex;
    std::condition_variable m_updateCv;
    Quaternion m_pose{};
    uint64_t m_generation = 0;
    uint64_t m_receivedPacketCount = 0;
    uint64_t m_sequenceGapCount = 0;
    uint64_t m_drainedPacketCount = 0;
};

constexpr uint16_t kStatusPort = 48105;
constexpr uint8_t kStatusDriverLoaded = 1 << 0;
constexpr uint8_t kStatusHmdActive = 1 << 1;
constexpr uint8_t kStatusPoseValid = 1 << 2;
constexpr uint8_t kStatusDisplayActive = 1 << 3;
constexpr uint8_t kStatusPresentedFrame = 1 << 4;

class SteamVRStatusReporter {
public:
    bool Start() {
        if (m_socket != INVALID_SOCKET) return true;
        WSADATA data{};
        if (WSAStartup(MAKEWORD(2, 2), &data) != 0) return false;
        m_socket = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
        if (m_socket == INVALID_SOCKET) {
            WSACleanup();
            return false;
        }
        m_target = {};
        m_target.sin_family = AF_INET;
        m_target.sin_port = htons(kStatusPort);
        m_target.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        return true;
    }

    void Send(uint8_t flags, uint64_t frameCounter) {
        if (m_socket == INVALID_SOCKET) return;
        const uint64_t now = MonotonicNowNs();
        if (m_lastSentNs && now - m_lastSentNs < 250000000ULL) return;
        m_lastSentNs = now;
        std::array<uint8_t, 16> packet{};
        std::memcpy(packet.data(), "PVST", 4);
        packet[4] = 1;
        packet[5] = flags;
        for (int i = 0; i < 8; ++i)
            packet[8 + i] = static_cast<uint8_t>(frameCounter >> (56 - i * 8));
        (void)sendto(m_socket, reinterpret_cast<const char*>(packet.data()),
                     static_cast<int>(packet.size()), 0,
                     reinterpret_cast<const sockaddr*>(&m_target), sizeof(m_target));
    }

    void Stop() {
        if (m_socket == INVALID_SOCKET) return;
        closesocket(m_socket);
        m_socket = INVALID_SOCKET;
        WSACleanup();
        m_lastSentNs = 0;
    }

    ~SteamVRStatusReporter() { Stop(); }

private:
    SOCKET m_socket = INVALID_SOCKET;
    sockaddr_in m_target{};
    uint64_t m_lastSentNs = 0;
};

class HmdDisplay final : public vr::IVRDisplayComponent {
public:
    void GetWindowBounds(int32_t* x, int32_t* y, uint32_t* width, uint32_t* height) override {
        *x = 0; *y = 0; *width = ActiveVideoStreamProfile().width; *height = ActiveVideoStreamProfile().height;
    }
    bool IsDisplayOnDesktop() override { return false; }
    bool IsDisplayRealDisplay() override { return false; }
    void GetRecommendedRenderTargetSize(uint32_t* width, uint32_t* height) override {
        *width = ActiveVideoStreamProfile().width; *height = ActiveVideoStreamProfile().height;
    }
    void GetEyeOutputViewport(vr::EVREye eye, uint32_t* x, uint32_t* y,
                              uint32_t* width, uint32_t* height) override {
        const uint32_t eyeWidth = ActiveVideoStreamProfile().width / 2;
        *x = eye == vr::Eye_Left ? 0 : eyeWidth;
        *y = 0; *width = eyeWidth; *height = ActiveVideoStreamProfile().height;
    }
    void GetProjectionRaw(vr::EVREye, float* left, float* right, float* top, float* bottom) override {
        *left = -1.0f; *right = 1.0f; *top = -1.0f; *bottom = 1.0f;
    }
    vr::DistortionCoordinates_t ComputeDistortion(vr::EVREye, float u, float v) override {
        vr::DistortionCoordinates_t result{};
        result.rfRed[0] = result.rfGreen[0] = result.rfBlue[0] = u;
        result.rfRed[1] = result.rfGreen[1] = result.rfBlue[1] = v;
        return result;
    }
    bool ComputeInverseDistortion(vr::HmdVector2_t* result, vr::EVREye, uint32_t, float u, float v) override {
        if (!result) return false;
        result->v[0] = u; result->v[1] = v; return true;
    }
};

class PhoneVRHmd final : public vr::ITrackedDeviceServerDriver {
public:
    ~PhoneVRHmd() { Deactivate(); }

    vr::EVRInitError Activate(uint32_t objectId) override {
        m_objectId = objectId;
        auto* properties = vr::VRProperties();
        const auto container = properties->TrackedDeviceToPropertyContainer(objectId);
        if (vr::VRDriverLog()) {
            char line[160]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: HMD Activate entered objectId=%u; checking DisplayRedirect override properties",
                          objectId);
            vr::VRDriverLog()->Log(line);
        }
        properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "PhoneVR Wireless HMD");
        properties->SetStringProperty(container, vr::Prop_SerialNumber_String, "PHONEVR-HMD-001");
        properties->SetStringProperty(container, vr::Prop_ManufacturerName_String, "BH2VSQ");
        properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_hmd");
        properties->SetBoolProperty(container, vr::Prop_HasDisplayComponent_Bool, true);
        properties->SetBoolProperty(container, vr::Prop_DeviceIsWireless_Bool, true);
        properties->SetFloatProperty(container, vr::Prop_UserIpdMeters_Float, 0.064f);
        properties->SetFloatProperty(container, vr::Prop_DisplayFrequency_Float,
                                     static_cast<float>(gDisplayRefreshRateHz));

        // SteamVR resolves these through k_ulDisplayRedirectContainer before
        // reading the HMD's effective properties. Log the effective values so
        // startup ordering and the virtual-output route can be verified.
        vr::ETrackedPropertyError virtualDisplayPropertyError = vr::TrackedProp_UnknownProperty;
        vr::ETrackedPropertyError adapterPropertyError = vr::TrackedProp_UnknownProperty;
        const bool hasVirtualDisplay = properties->GetBoolProperty(
            container, vr::Prop_HasVirtualDisplayComponent_Bool, &virtualDisplayPropertyError);
        const uint64_t effectiveAdapterLuid = properties->GetUint64Property(
            container, vr::Prop_GraphicsAdapterLuid_Uint64, &adapterPropertyError);
        if (vr::VRDriverLog()) {
            char line[256]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: HMD effective display properties objectId=%u virtualDisplay=%s(error=%d) adapterLuid=0x%016llX(error=%d)",
                          objectId, hasVirtualDisplay ? "true" : "false",
                          static_cast<int>(virtualDisplayPropertyError),
                          static_cast<unsigned long long>(effectiveAdapterLuid),
                          static_cast<int>(adapterPropertyError));
            vr::VRDriverLog()->Log(line);
        }

        const auto universeResult = properties->SetUint64Property(
            container, vr::Prop_CurrentUniverseId_Uint64,
            static_cast<uint64_t>(kPhoneVRUniverseId));
        const auto chaperoneResult = properties->SetStringProperty(
            container, vr::Prop_DriverProvidedChaperoneJson_String,
            kDriverProvidedChaperoneJson);
        if (universeResult != vr::TrackedProp_Success || chaperoneResult != vr::TrackedProp_Success) {
            char message[224]{};
            std::snprintf(message, sizeof(message),
                          "PhoneVR: failed to publish driver Chaperone (universe property=%d, JSON property=%d)",
                          static_cast<int>(universeResult), static_cast<int>(chaperoneResult));
            if (vr::VRDriverLog()) {
                vr::VRDriverLog()->Log(message);
                vr::VRDriverLog()->Log(
                    "PhoneVR: continuing HMD activation without driver Chaperone; SteamVR may request Room Setup");
            }
        } else if (vr::VRDriverLog()) {
            char message[160]{};
            std::snprintf(message, sizeof(message),
                          "PhoneVR: driver-provided Chaperone published (universe 0x%08X; 3DoF; no room-scale bounds)",
                          kPhoneVRUniverseId);
            vr::VRDriverLog()->Log(message);
        }

        // Initial state is published before the worker starts: connected, but
        // untracked until a fresh phone sensor sample is received.
        const vr::DriverPose_t initialPose = MakeUntrackedPose();
        {
            std::lock_guard<std::mutex> lock(m_poseMutex);
            m_lastPose = initialPose;
        }
        m_lastTrackingValid.store(false, std::memory_order_release);
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(objectId, initialPose, sizeof(initialPose));

        if (!m_receiver.Start()) {
            if (vr::VRDriverLog())
                vr::VRDriverLog()->Log("PhoneVR: failed to bind local pose bridge on UDP 48103; HMD remains connected but untracked");
            return vr::VRInitError_None;
        }

        m_posePublisherRunning.store(true, std::memory_order_release);
        try {
            m_posePublisherThread = std::thread(&PhoneVRHmd::PosePublisherLoop, this);
        } catch (...) {
            m_posePublisherRunning.store(false, std::memory_order_release);
            m_receiver.Stop();
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: failed to create pose publisher thread");
            return vr::VRInitError_Driver_Unknown;
        }
        if (vr::VRDriverLog()) {
            vr::VRDriverLog()->Log("PhoneVR: HMD activation published; 3DoF virtual head height=1.65m above SteamVR floor");
            vr::VRDriverLog()->Log("PhoneVR: pose updates use dedicated event-driven worker");
            char refreshLine[160]{};
            std::snprintf(refreshLine, sizeof(refreshLine),
                          "PhoneVR: pose publisher priority=ABOVE_NORMAL, keepalive=10ms; virtual display refresh=%.1fHz",
                          gDisplayRefreshRateHz);
            vr::VRDriverLog()->Log(refreshLine);
            vr::VRDriverLog()->Log("PhoneVR: HMD reports IsDisplayOnDesktop=false; enable SteamVR Settings > Developer > Enable Direct Mode for headset display operation");
        }
        return vr::VRInitError_None;
    }

    void Deactivate() override {
        m_posePublisherRunning.store(false, std::memory_order_release);
        m_receiver.Stop(); // Also wakes WaitForUpdate() in the publisher.
        if (m_posePublisherThread.joinable()) m_posePublisherThread.join();
        m_objectId.store(vr::k_unTrackedDeviceIndexInvalid, std::memory_order_release);
        m_lastTrackingValid.store(false, std::memory_order_release);
    }
    void EnterStandby() override {}
    void* GetComponent(const char* nameAndVersion) override {
        const bool displayMatch = nameAndVersion &&
            _stricmp(nameAndVersion, vr::IVRDisplayComponent_Version) == 0;
        if (vr::VRDriverLog() && !m_loggedGetComponent.exchange(true, std::memory_order_relaxed)) {
            char line[224]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: HMD GetComponent first query='%s' display-component-match=%s",
                          nameAndVersion ? nameAndVersion : "<null>", displayMatch ? "yes" : "no");
            vr::VRDriverLog()->Log(line);
        }
        if (displayMatch) return static_cast<void*>(static_cast<vr::IVRDisplayComponent*>(&m_display));
        return nullptr;
    }
    void DebugRequest(const char*, char* response, uint32_t size) override {
        if (response && size) response[0] = '\0';
    }
    vr::DriverPose_t GetPose() override {
        std::lock_guard<std::mutex> lock(m_poseMutex);
        return m_lastPose;
    }

    bool IsActivated() const {
        return m_objectId.load(std::memory_order_acquire) != vr::k_unTrackedDeviceIndexInvalid;
    }
    bool HasValidPose() const { return m_lastTrackingValid.load(std::memory_order_acquire); }

    static vr::DriverPose_t MakeUntrackedPose() {
        vr::DriverPose_t pose{};
        pose.poseTimeOffset = 0.0;
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {1.0, 0.0, 0.0, 0.0};
        pose.vecPosition[0] = 0.0;
        pose.vecPosition[1] = kDefaultHeadHeightMeters;
        pose.vecPosition[2] = 0.0;
        pose.result = vr::TrackingResult_Uninitialized;
        pose.deviceIsConnected = true;
        pose.poseIsValid = false;
        pose.willDriftInYaw = true;
        pose.shouldApplyHeadModel = false;
        return pose;
    }

    void RunFrame() {
        // Intentionally do not submit pose here. OpenVR's RunFrame cadence is
        // controlled by vrserver. The dedicated publisher submits every fresh
        // UDP generation immediately and records this callback cadence only as
        // a diagnostic so it can be compared with the independent pose rate.
        const uint64_t now = MonotonicNowNs();
        if (m_lastRuntimeFrameNs != 0) AtomicMax(m_maxRuntimeRunFrameGapNs, now - m_lastRuntimeFrameNs);
        m_lastRuntimeFrameNs = now;
        m_runtimeRunFrameCount.fetch_add(1, std::memory_order_relaxed);
        if (now - m_lastDiagnosticsNs < 5000000000ULL) return;
        const double intervalSeconds = m_lastDiagnosticsNs == 0
            ? 5.0 : static_cast<double>(now - m_lastDiagnosticsNs) / 1.0e9;
        m_lastDiagnosticsNs = now;

        const uint64_t rx = m_receiver.TakeReceivedPacketCount();
        const uint64_t gaps = m_receiver.TakeSequenceGapCount();
        const uint64_t drained = m_receiver.TakeDrainedPacketCount();
        const uint64_t published = m_publishedPoseCount.exchange(0, std::memory_order_relaxed);
        const uint64_t unique = m_uniquePoseCount.exchange(0, std::memory_order_relaxed);
        const uint64_t repeated = m_repeatedPoseCount.exchange(0, std::memory_order_relaxed);
        const uint64_t maxPublishGap = m_maxPublishGapNs.exchange(0, std::memory_order_relaxed);
        const uint64_t maxSampleAge = m_maxSampleAgeNs.exchange(0, std::memory_order_relaxed);
        const uint64_t runtimeCalls = m_runtimeRunFrameCount.exchange(0, std::memory_order_relaxed);
        const uint64_t maxRuntimeGap = m_maxRuntimeRunFrameGapNs.exchange(0, std::memory_order_relaxed);
        char message[512]{};
        std::snprintf(message, sizeof(message),
                      "PhoneVR: pose rates driver-rx=%.1fHz published=%.1fHz unique=%.1fHz repeated=%.1fHz sequence-gaps=%llu drained=%llu max-publish-gap=%.2fms max-sample-age=%.2fms | runtime RunFrame=%.1fHz max-gap=%.2fms | display-refresh=%.1fHz",
                      rx / intervalSeconds, published / intervalSeconds,
                      unique / intervalSeconds, repeated / intervalSeconds,
                      static_cast<unsigned long long>(gaps), static_cast<unsigned long long>(drained),
                      maxPublishGap / 1.0e6, maxSampleAge / 1.0e6,
                      runtimeCalls / intervalSeconds, maxRuntimeGap / 1.0e6,
                      gDisplayRefreshRateHz);
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log(message);
    }

private:
    void PosePublisherLoop() {
        (void)SetThreadPriority(GetCurrentThread(), THREAD_PRIORITY_ABOVE_NORMAL);
        uint64_t observedGeneration = 0;
        uint32_t lastPublishedSequence = 0;
        bool havePublishedSequence = false;
        uint64_t lastPublishNs = 0;
        while (m_posePublisherRunning.load(std::memory_order_acquire)) {
            if (!m_receiver.WaitForUpdate(&observedGeneration, std::chrono::milliseconds(10))) break;
            if (!m_posePublisherRunning.load(std::memory_order_acquire)) break;

            const Quaternion sample = m_receiver.Snapshot();
            const uint64_t now = MonotonicNowNs();
            vr::DriverPose_t pose = MakeUntrackedPose();
            uint64_t ageNs = 0;
            bool trackingValid = false;
            if (sample.valid && sample.receivedAtNs != 0) {
                ageNs = now >= sample.receivedAtNs ? now - sample.receivedAtNs : 0;
                trackingValid = ageNs <= kTrackingPoseTimeoutNs;
                const uint64_t predictionAgeNs = std::min<uint64_t>(ageNs, 50000000ULL);
                pose.poseTimeOffset = -static_cast<double>(predictionAgeNs) / 1.0e9;
                pose.qRotation = {sample.w, sample.x, sample.y, sample.z};
                pose.vecAngularVelocity[0] = sample.angularVelocityX;
                pose.vecAngularVelocity[1] = sample.angularVelocityY;
                pose.vecAngularVelocity[2] = sample.angularVelocityZ;
                pose.result = trackingValid ? vr::TrackingResult_Running_OK : vr::TrackingResult_Uninitialized;
                pose.poseIsValid = trackingValid;
                pose.deviceIsConnected = true;
                pose.willDriftInYaw = true;
                pose.shouldApplyHeadModel = false;
            }
            AtomicMax(m_maxSampleAgeNs, ageNs);

            const bool previousTrackingValid = m_lastTrackingValid.exchange(trackingValid, std::memory_order_acq_rel);
            if (previousTrackingValid != trackingValid && vr::VRDriverLog()) {
                char stateMessage[160]{};
                std::snprintf(stateMessage, sizeof(stateMessage),
                              "PhoneVR: IMU tracking %s (sample age %.1f ms); HMD device remains connected",
                              trackingValid ? "resumed" : "stale",
                              static_cast<double>(ageNs) / 1.0e6);
                vr::VRDriverLog()->Log(stateMessage);
            }

            const bool uniquePose = sample.valid &&
                (!havePublishedSequence || sample.sequence != lastPublishedSequence);
            if (uniquePose) {
                m_uniquePoseCount.fetch_add(1, std::memory_order_relaxed);
                lastPublishedSequence = sample.sequence;
                havePublishedSequence = true;
            } else {
                m_repeatedPoseCount.fetch_add(1, std::memory_order_relaxed);
            }

            const uint64_t publishNow = MonotonicNowNs();
            if (lastPublishNs != 0 && publishNow >= lastPublishNs)
                AtomicMax(m_maxPublishGapNs, publishNow - lastPublishNs);
            lastPublishNs = publishNow;
            {
                std::lock_guard<std::mutex> lock(m_poseMutex);
                m_lastPose = pose;
            }
            const uint32_t objectId = m_objectId.load(std::memory_order_acquire);
            if (objectId != vr::k_unTrackedDeviceIndexInvalid) {
                if (auto* host = vr::VRServerDriverHost())
                    host->TrackedDevicePoseUpdated(objectId, pose, sizeof(pose));
            }
            m_publishedPoseCount.fetch_add(1, std::memory_order_relaxed);
        }
    }

    std::atomic<uint32_t> m_objectId{vr::k_unTrackedDeviceIndexInvalid};
    std::atomic<bool> m_loggedGetComponent{false};
    PoseReceiver m_receiver;
    HmdDisplay m_display;
    std::thread m_posePublisherThread;
    std::atomic<bool> m_posePublisherRunning{false};
    mutable std::mutex m_poseMutex;
    vr::DriverPose_t m_lastPose{};
    std::atomic<bool> m_lastTrackingValid{false};
    std::atomic<uint64_t> m_publishedPoseCount{0};
    std::atomic<uint64_t> m_uniquePoseCount{0};
    std::atomic<uint64_t> m_repeatedPoseCount{0};
    std::atomic<uint64_t> m_maxPublishGapNs{0};
    std::atomic<uint64_t> m_maxSampleAgeNs{0};
    std::atomic<uint64_t> m_runtimeRunFrameCount{0};
    std::atomic<uint64_t> m_maxRuntimeRunFrameGapNs{0};
    uint64_t m_lastRuntimeFrameNs = 0;
    uint64_t m_lastDiagnosticsNs = 0;
};

class VirtualDisplayComponent final : public vr::IVRVirtualDisplay {
public:
    VirtualDisplayComponent() = default;
    ~VirtualDisplayComponent() { Shutdown(); }

    bool Initialize(uint64_t* adapterLuid) {
        if (!adapterLuid) return false;
        std::lock_guard<std::mutex> lock(m_mutex);
        if (m_device && m_context) {
            *adapterLuid = m_adapterLuid;
            return true;
        }

        IDXGIFactory1* factory = nullptr;
        HRESULT hr = CreateDXGIFactory1(__uuidof(IDXGIFactory1), reinterpret_cast<void**>(&factory));
        if (FAILED(hr) || !factory) {
            LogD3DFailure("CreateDXGIFactory1", hr);
            return false;
        }

        IDXGIAdapter1* adapter = nullptr;
        for (UINT index = 0;; ++index) {
            IDXGIAdapter1* candidate = nullptr;
            const HRESULT enumResult = factory->EnumAdapters1(index, &candidate);
            if (enumResult == DXGI_ERROR_NOT_FOUND) break;
            if (FAILED(enumResult) || !candidate) {
                LogD3DFailure("IDXGIFactory1::EnumAdapters1", enumResult);
                break;
            }
            DXGI_ADAPTER_DESC1 desc{};
            if (SUCCEEDED(candidate->GetDesc1(&desc))) {
                char adapterName[256]{};
                WideCharToMultiByte(CP_UTF8, 0, desc.Description, -1, adapterName,
                                    static_cast<int>(sizeof(adapterName)), nullptr, nullptr);
                if (vr::VRDriverLog()) {
                    char line[384]{};
                    std::snprintf(line, sizeof(line),
                                  "PhoneVR: DXGI adapter %u: %s (vendor=0x%04X device=0x%04X flags=0x%X)",
                                  index, adapterName, desc.VendorId, desc.DeviceId, desc.Flags);
                    vr::VRDriverLog()->Log(line);
                }
                if (!(desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE) && !adapter) {
                    m_adapterLuid = (static_cast<uint64_t>(static_cast<uint32_t>(desc.AdapterLuid.HighPart)) << 32) |
                                    static_cast<uint32_t>(desc.AdapterLuid.LowPart);
                    adapter = candidate; // Retain the first hardware adapter as the current default.
                    continue;
                }
            }
            candidate->Release();
        }
        factory->Release();
        if (!adapter) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: no hardware DXGI adapter was found");
            return false;
        }

        D3D_FEATURE_LEVEL featureLevel{};
        constexpr UINT flags = D3D11_CREATE_DEVICE_BGRA_SUPPORT;
        hr = D3D11CreateDevice(adapter, D3D_DRIVER_TYPE_UNKNOWN, nullptr, flags,
                               nullptr, 0, D3D11_SDK_VERSION,
                               &m_device, &featureLevel, &m_context);
        adapter->Release();
        if (SUCCEEDED(hr) && m_device && m_context && vr::VRDriverLog()) {
            char line[192]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: D3D11 capture device selected (adapter LUID 0x%016llX)",
                          static_cast<unsigned long long>(m_adapterLuid));
            vr::VRDriverLog()->Log(line);
        }
        if (FAILED(hr) || !m_device || !m_context) {
            LogD3DFailure("D3D11CreateDevice", hr);
            ShutdownUnlocked();
            return false;
        }
        *adapterLuid = m_adapterLuid;
        ID3D11Multithread* multithread = nullptr;
        if (SUCCEEDED(m_context->QueryInterface(__uuidof(ID3D11Multithread), reinterpret_cast<void**>(&multithread))) && multithread) {
            multithread->SetMultithreadProtected(TRUE);
            multithread->Release();
        }
        m_captureRunning.store(true, std::memory_order_release);
        m_captureThread = std::thread(&VirtualDisplayComponent::VideoCaptureLoop, this);
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: D3D11 virtual display capture initialized; H.264 bridge thread started");
        return true;
    }

    void Shutdown() {
        m_captureRunning.store(false, std::memory_order_release);
        m_captureCv.notify_all();
        if (m_captureThread.joinable() && m_captureThread.get_id() != std::this_thread::get_id()) m_captureThread.join();
        std::lock_guard<std::mutex> lock(m_mutex);
        ShutdownUnlocked();
    }

    void Present(const vr::PresentInfo_t* info, uint32_t infoSize) override {
        ScopedCallbackTiming timing(m_presentCalls, m_maxPresentDurationNs);
        std::lock_guard<std::mutex> lock(m_mutex);
        if (!info || infoSize < sizeof(vr::PresentInfo_t)) return;

        const uint64_t presentNowNs = MonotonicNowNs();
        if (m_lastPresentCallbackNs != 0 && presentNowNs >= m_lastPresentCallbackNs) {
            const uint64_t intervalNs = presentNowNs - m_lastPresentCallbackNs;
            const uint64_t nominalIntervalNs = static_cast<uint64_t>(1.0e9 / gDisplayRefreshRateHz);
            AtomicMax(m_maxPresentIntervalNs, intervalNs);
            if (intervalNs > nominalIntervalNs + nominalIntervalNs / 2)
                m_presentGapsOverOneAndHalfFrames.fetch_add(1, std::memory_order_relaxed);
            if (intervalNs > nominalIntervalNs * 3)
                m_presentGapsOverThreeFrames.fetch_add(1, std::memory_order_relaxed);
        }
        m_lastPresentCallbackNs = presentNowNs;

        const double requestedVsyncSeconds =
            (std::isfinite(info->flVSyncTimeInSeconds) && info->flVSyncTimeInSeconds > 0.0)
                ? info->flVSyncTimeInSeconds : NowSeconds();
        m_pendingVsyncTimeNs = static_cast<uint64_t>(requestedVsyncSeconds * 1.0e9);
        switch (info->vsync) {
            case vr::VSync_None: m_vsyncNoneCalls.fetch_add(1, std::memory_order_relaxed); break;
            case vr::VSync_WaitRender: m_vsyncWaitRenderCalls.fetch_add(1, std::memory_order_relaxed); break;
            case vr::VSync_NoWaitRender: m_vsyncNoWaitRenderCalls.fetch_add(1, std::memory_order_relaxed); break;
            default: break;
        }
        if (m_pendingPresent)
            m_pendingPresentOverwrites.fetch_add(1, std::memory_order_relaxed);
        m_pendingPresent = true;
        m_lastFrameId = info->nFrameId;

        if (!info->backbufferTextureHandle || !m_device || !m_context) return;

        HANDLE sharedHandle = reinterpret_cast<HANDLE>(info->backbufferTextureHandle);
        if (sharedHandle != m_sharedHandle) {
            ReleaseTexture(m_sharedTexture);
            m_sharedHandle = sharedHandle;
            HRESULT openResult = m_device->OpenSharedResource(
                sharedHandle, __uuidof(ID3D11Texture2D), reinterpret_cast<void**>(&m_sharedTexture));
            if (FAILED(openResult) || !m_sharedTexture) {
                m_sharedHandle = nullptr;
                LogD3DFailureOnce("OpenSharedResource", openResult, m_loggedOpenFailure);
                return;
            }
            m_loggedOpenFailure = false;

            D3D11_TEXTURE2D_DESC source{};
            m_sharedTexture->GetDesc(&source);
            if (vr::VRDriverLog()) {
                char line[256]{};
                std::snprintf(line, sizeof(line),
                              "PhoneVR: opened compositor backbuffer %ux%u format=%u samples=%u array=%u mips=%u misc=0x%X",
                              source.Width, source.Height, static_cast<unsigned>(source.Format),
                              source.SampleDesc.Count, source.ArraySize, source.MipLevels, source.MiscFlags);
                vr::VRDriverLog()->Log(line);
            }
        }
        if (!m_sharedTexture) return;

        // The compositor has released the shared texture before Present is
        // called. Acquire without waiting on the compositor thread: a busy
        // frame is skipped and the next frame will be tried instead.
        IDXGIKeyedMutex* keyedMutex = nullptr;
        const HRESULT mutexQuery = m_sharedTexture->QueryInterface(
            __uuidof(IDXGIKeyedMutex), reinterpret_cast<void**>(&keyedMutex));
        bool mutexAcquired = false;
        if (SUCCEEDED(mutexQuery) && keyedMutex) {
            const HRESULT acquire = keyedMutex->AcquireSync(0, 0);
            if (acquire != S_OK) {
                ++m_mutexAcquireFailures;
                m_keyedMutexAcquireFailures.fetch_add(1, std::memory_order_relaxed);
                LogMutexAcquireFailure(acquire);
                keyedMutex->Release();
                return;
            }
            mutexAcquired = true;
        } else if (FAILED(mutexQuery)) {
            LogD3DFailureOnce("QueryInterface(IDXGIKeyedMutex)", mutexQuery, m_loggedMutexQueryFailure);
        }

        // AcquireSync above is the compositor-to-driver handoff. Do not insert
        // a blocking GPU readback/fence into the compositor callback path.

        if (m_captureBridgeReady.load(std::memory_order_acquire) && !m_captureResourcesReady) {
            D3D11_TEXTURE2D_DESC captureSource{};
            m_sharedTexture->GetDesc(&captureSource);
            EnsureVideoCaptureResources(captureSource);
        }
        if (m_captureBridgeReady.load(std::memory_order_acquire) &&
            (!keyedMutex || mutexAcquired) && m_captureResourcesReady) {
            CaptureVideoFrameUnlocked(info->nFrameId);
            // Submit the capture work without waiting for it. The readback
            // thread polls staging textures with DO_NOT_WAIT.
            m_context->Flush();
        }

        if (mutexAcquired) keyedMutex->ReleaseSync(0);
        if (keyedMutex) keyedMutex->Release();
    }

    void WaitForPresent() override {
        ScopedCallbackTiming timing(m_waitCalls, m_maxWaitDurationNs);
        // The virtual display has one scan-out clock. Serialize waiters so
        // overlapping runtime callbacks cannot reserve the same clock tick.
        std::lock_guard<std::mutex> waitLock(m_waitMutex);
        uint64_t requestedVsyncNs = 0;
        {
            std::lock_guard<std::mutex> lock(m_mutex);
            if (!m_pendingPresent) {
                m_waitCallsWithoutPresent.fetch_add(1, std::memory_order_relaxed);
                return;
            }
            requestedVsyncNs = m_pendingVsyncTimeNs;
            m_pendingPresent = false;
        }

        // There is no physical scan-out device on a wireless HMD. AcquireSync
        // in Present confirms the compositor has released its shared backbuffer;
        // WaitForPresent then advances the emulated display clock at 72 Hz.
        const VsyncSchedule schedule = ScheduleVirtualVsync(requestedVsyncNs);
        WaitUntilMonotonic(schedule.targetNs);
        const uint64_t actualWakeNs = MonotonicNowNs();
        if (actualWakeNs > schedule.targetNs) {
            const uint64_t overshootNs = actualWakeNs - schedule.targetNs;
            AtomicMax(m_maxWaitOvershootNs, overshootNs);
            if (overshootNs > 1000000ULL)
                m_waitOvershootsOverOneMs.fetch_add(1, std::memory_order_relaxed);
            if (overshootNs > 5000000ULL)
                m_waitOvershootsOverFiveMs.fetch_add(1, std::memory_order_relaxed);
        }

        // Publish the virtual-vsync origin only after the scheduled event has
        // actually been reached. Publishing a future timestamp before waiting
        // makes GetTimeSinceLastVsync report an event that has not happened yet;
        // SteamVR can then run its compositor clock ahead of the display.
        {
            std::lock_guard<std::mutex> timingLock(m_timingMutex);
            if (schedule.targetNs >= m_vsyncOriginNs) {
                m_vsyncOriginNs = schedule.targetNs;
                m_vsyncOriginFrameCounter += schedule.frameAdvance;
            }
        }

        const uint64_t completed = m_completedFrameCounter.fetch_add(1, std::memory_order_relaxed) + 1;
        m_timingCompletedFrames.fetch_add(1, std::memory_order_relaxed);
        if (completed == 1 && vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: first virtual display frame synchronized to the 72 Hz clock");
    }

    bool GetTimeSinceLastVsync(float* seconds, uint64_t* frameCounter) override {
        if (!seconds || !frameCounter) return false;
        std::lock_guard<std::mutex> lock(m_timingMutex);
        if (m_vsyncOriginNs == 0) return false;
        const uint64_t now = MonotonicNowNs();
        const uint64_t elapsed = now >= m_vsyncOriginNs ? now - m_vsyncOriginNs : 0;
        const uint64_t intervalNs = static_cast<uint64_t>(1.0e9 / gDisplayRefreshRateHz);
        const uint64_t elapsedVsyncs = elapsed / intervalNs;
        const uint64_t lastVsyncOffset = elapsed - elapsedVsyncs * intervalNs;
        *seconds = static_cast<float>(lastVsyncOffset / 1.0e9);
        *frameCounter = m_vsyncOriginFrameCounter + elapsedVsyncs;
        return true;
    }

    uint64_t PresentCounter() const { return m_completedFrameCounter.load(std::memory_order_relaxed); }
    VirtualDisplayTimingSnapshot TakeTimingSnapshot() {
        VirtualDisplayTimingSnapshot snapshot{};
        snapshot.presentCalls = m_presentCalls.exchange(0, std::memory_order_relaxed);
        snapshot.waitCalls = m_waitCalls.exchange(0, std::memory_order_relaxed);
        snapshot.completedFrames = m_timingCompletedFrames.exchange(0, std::memory_order_relaxed);
        snapshot.waitCallsWithoutPresent = m_waitCallsWithoutPresent.exchange(0, std::memory_order_relaxed);
        snapshot.vsyncNoneCalls = m_vsyncNoneCalls.exchange(0, std::memory_order_relaxed);
        snapshot.vsyncWaitRenderCalls = m_vsyncWaitRenderCalls.exchange(0, std::memory_order_relaxed);
        snapshot.vsyncNoWaitRenderCalls = m_vsyncNoWaitRenderCalls.exchange(0, std::memory_order_relaxed);
        snapshot.maxPresentNs = m_maxPresentDurationNs.exchange(0, std::memory_order_relaxed);
        snapshot.maxWaitNs = m_maxWaitDurationNs.exchange(0, std::memory_order_relaxed);
        snapshot.maxPresentIntervalNs = m_maxPresentIntervalNs.exchange(0, std::memory_order_relaxed);
        snapshot.presentGapsOverOneAndHalfFrames = m_presentGapsOverOneAndHalfFrames.exchange(0, std::memory_order_relaxed);
        snapshot.presentGapsOverThreeFrames = m_presentGapsOverThreeFrames.exchange(0, std::memory_order_relaxed);
        snapshot.pendingPresentOverwrites = m_pendingPresentOverwrites.exchange(0, std::memory_order_relaxed);
        snapshot.maxWaitOvershootNs = m_maxWaitOvershootNs.exchange(0, std::memory_order_relaxed);
        snapshot.waitOvershootsOverOneMs = m_waitOvershootsOverOneMs.exchange(0, std::memory_order_relaxed);
        snapshot.waitOvershootsOverFiveMs = m_waitOvershootsOverFiveMs.exchange(0, std::memory_order_relaxed);
        snapshot.missedVirtualVsyncs = m_missedVirtualVsyncs.exchange(0, std::memory_order_relaxed);
        snapshot.keyedMutexAcquireFailures = m_keyedMutexAcquireFailures.exchange(0, std::memory_order_relaxed);
        return snapshot;
    }

private:
    static double NowSeconds() {
        return static_cast<double>(MonotonicNowNs()) / 1.0e9;
    }
    static void ReleaseTexture(ID3D11Texture2D*& texture) {
        if (texture) { texture->Release(); texture = nullptr; }
    }
    static void LogD3DFailure(const char* operation, HRESULT hr) {
        if (vr::VRDriverLog()) {
            char line[192]{};
            std::snprintf(line, sizeof(line), "PhoneVR: %s failed (HRESULT 0x%08X)", operation, static_cast<unsigned>(hr));
            vr::VRDriverLog()->Log(line);
        }
    }
    static void LogD3DFailureOnce(const char* operation, HRESULT hr, bool& logged) {
        if (logged) return;
        logged = true;
        LogD3DFailure(operation, hr);
    }
    struct VsyncSchedule {
        uint64_t targetNs = 0;
        uint64_t frameAdvance = 1;
    };

    static HANDLE HighResolutionTimer() {
        struct ThreadTimer {
            HANDLE handle = CreateWaitableTimerExW(
                nullptr, nullptr, 0x00000002UL, TIMER_ALL_ACCESS); // CREATE_WAITABLE_TIMER_HIGH_RESOLUTION
            ThreadTimer() {
                if (!handle) handle = CreateWaitableTimerExW(nullptr, nullptr, 0, TIMER_ALL_ACCESS);
            }
            ~ThreadTimer() { if (handle) CloseHandle(handle); }
        };
        static thread_local ThreadTimer timer;
        return timer.handle;
    }

    static void WaitUntilMonotonic(uint64_t targetNs) {
        HANDLE timer = HighResolutionTimer();
        for (;;) {
            const uint64_t now = MonotonicNowNs();
            if (now >= targetNs) return;
            const uint64_t remaining = targetNs - now;

            // Use a high-resolution waitable timer for the coarse part of the
            // wait, then yield-spin over the final 0.5 ms. Sleep(1) can round
            // up to the system timer quantum (often 15.6 ms), which is longer
            // than the 13.89 ms frame budget at 72 Hz.
            if (remaining > 1500000ULL && timer) {
                const uint64_t sleepNs = remaining - 500000ULL;
                LARGE_INTEGER due{};
                due.QuadPart = -static_cast<LONGLONG>((sleepNs + 99ULL) / 100ULL);
                if (SetWaitableTimer(timer, &due, 0, nullptr, nullptr, FALSE)) {
                    if (WaitForSingleObject(timer, INFINITE) == WAIT_OBJECT_0) continue;
                }
            } else if (remaining > 5000000ULL) {
                Sleep(1);
            } else {
                SwitchToThread();
            }
        }
    }

    VsyncSchedule ScheduleVirtualVsync(uint64_t requestedNs) {
        const uint64_t now = MonotonicNowNs();
        const uint64_t intervalNs = static_cast<uint64_t>(1.0e9 / gDisplayRefreshRateHz);
        constexpr uint64_t kMaximumAcceptedClockSkewNs = 1000000000ULL;
        std::lock_guard<std::mutex> lock(m_timingMutex);

        uint64_t target = 0;
        uint64_t frameAdvance = 1;
        if (m_lastScheduledVsyncNs == 0) {
            // PresentInfo reports the compositor's VSync event for this frame.
            // The wireless display can scan out at the next virtual interval.
            // Ignore timestamps from a different clock domain.
            const bool timestampIsComparable = requestedNs > 0 &&
                (requestedNs > now
                    ? requestedNs - now <= kMaximumAcceptedClockSkewNs
                    : now - requestedNs <= kMaximumAcceptedClockSkewNs);
            const uint64_t base = timestampIsComparable ? requestedNs : now;
            target = base + intervalNs;
            if (target <= now) {
                const uint64_t missed = (now - target) / intervalNs + 1;
                target += missed * intervalNs;
                frameAdvance += missed;
            }
        } else {
            target = m_lastScheduledVsyncNs + intervalNs;
            if (target <= now) {
                const uint64_t missed = (now - target) / intervalNs + 1;
                target += missed * intervalNs;
                frameAdvance += missed;
            }
        }

        // A stale reservation must never make the compositor wait several
        // display periods. Rebase to one interval from now if it drifted too far
        // into the future. The published frame counter remains monotonic.
        if (target > now + intervalNs * 2) {
            target = now + intervalNs;
            frameAdvance = 1;
        }

        if (frameAdvance > 1)
            m_missedVirtualVsyncs.fetch_add(frameAdvance - 1, std::memory_order_relaxed);

        // Reserve the target for the next WaitForPresent call, but do not expose
        // it as the last completed VSync until WaitForPresent has reached it.
        m_lastScheduledVsyncNs = target;
        return VsyncSchedule{target, frameAdvance};
    }

    void LogMutexAcquireFailure(HRESULT hr) {
        const uint64_t now = MonotonicNowNs();
        if (now - m_lastMutexFailureLogNs < 2000000000ULL) return;
        m_lastMutexFailureLogNs = now;
        char line[192]{};
        std::snprintf(line, sizeof(line),
                      "PhoneVR: compositor keyed-mutex acquisition failed %llu times (last HRESULT 0x%08X); frames were dropped",
                      static_cast<unsigned long long>(m_mutexAcquireFailures), static_cast<unsigned>(hr));
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log(line);
        m_mutexAcquireFailures = 0;
    }

    struct CaptureSlot {
        ID3D11Texture2D* staging = nullptr;
        std::atomic<bool> busy{false};
        uint32_t frameId = 0;
        uint64_t timestampNs = 0;
    };

    static constexpr const char* kCaptureVertexShader = R"HLSL(
struct VOut { float4 position : SV_POSITION; float2 uv : TEXCOORD0; };
VOut main(uint id : SV_VertexID) {
    float2 p = id == 0 ? float2(-1.0, -1.0) : (id == 1 ? float2(-1.0, 3.0) : float2(3.0, -1.0));
    VOut o; o.position = float4(p, 0.0, 1.0); o.uv = float2((p.x + 1.0) * 0.5, (1.0 - p.y) * 0.5); return o;
}
)HLSL";
    static constexpr const char* kCapturePixelShader = R"HLSL(
Texture2D sourceTexture : register(t0);
SamplerState linearSampler : register(s0);
float4 main(float4 position : SV_POSITION, float2 uv : TEXCOORD0) : SV_TARGET {
    return sourceTexture.Sample(linearSampler, saturate(uv));
}
)HLSL";

    bool EnsureVideoCaptureResources(const D3D11_TEXTURE2D_DESC& source) {
        if (!m_device || !m_context || source.SampleDesc.Count != 1 ||
            (source.Format != DXGI_FORMAT_B8G8R8A8_UNORM && source.Format != DXGI_FORMAT_R8G8B8A8_UNORM)) {
            if (!m_loggedVideoFormatFailure && vr::VRDriverLog()) {
                vr::VRDriverLog()->Log("PhoneVR: video capture requires single-sample BGRA8/RGBA8 compositor texture; video stream disabled for this backbuffer");
                m_loggedVideoFormatFailure = true;
            }
            return false;
        }
        std::lock_guard<std::mutex> lock(m_captureResourcesMutex);
        if (m_captureResourcesReady) return true;
        ReleaseVideoCaptureResourcesUnlocked();

        // The HMD render target now matches the encoded SBS profile. In that
        // common path, copy directly into the CPU-readable staging ring instead
        // of doing a full-size input copy, shader draw, and output copy.
        if (source.Width == ActiveVideoStreamProfile().width && source.Height == ActiveVideoStreamProfile().height &&
            source.Format == DXGI_FORMAT_B8G8R8A8_UNORM) {
            D3D11_TEXTURE2D_DESC staging = source;
            staging.MipLevels = 1;
            staging.ArraySize = 1;
            staging.SampleDesc.Count = 1;
            staging.Usage = D3D11_USAGE_STAGING;
            staging.BindFlags = 0;
            staging.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
            staging.MiscFlags = 0;
            for (auto& slot : m_captureSlots) {
                const HRESULT createResult = m_device->CreateTexture2D(&staging, nullptr, &slot.staging);
                if (FAILED(createResult) || !slot.staging) {
                    LogD3DFailure("CreateTexture2D(direct video staging)", createResult);
                    ReleaseVideoCaptureResourcesUnlocked();
                    return false;
                }
            }
            m_captureDirectCopy = true;
            m_captureResourcesReady = true;
            m_captureWidth = source.Width;
            m_captureHeight = source.Height;
            if (vr::VRDriverLog())
                vr::VRDriverLog()->Log("PhoneVR: direct BGRA capture path enabled; render target matches active stream profile");
            return true;
        }

        D3D11_TEXTURE2D_DESC input = source;
        input.MipLevels = 1; input.ArraySize = 1; input.Usage = D3D11_USAGE_DEFAULT;
        input.BindFlags = D3D11_BIND_SHADER_RESOURCE; input.CPUAccessFlags = 0; input.MiscFlags = 0;
        HRESULT hr = m_device->CreateTexture2D(&input, nullptr, &m_captureInputTexture);
        if (FAILED(hr)) { LogD3DFailure("CreateTexture2D(video input)", hr); return false; }
        hr = m_device->CreateShaderResourceView(m_captureInputTexture, nullptr, &m_captureSrv);
        if (FAILED(hr)) { LogD3DFailure("CreateShaderResourceView(video)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }

        D3D11_TEXTURE2D_DESC output{};
        output.Width = ActiveVideoStreamProfile().width; output.Height = ActiveVideoStreamProfile().height; output.MipLevels = 1; output.ArraySize = 1;
        output.Format = DXGI_FORMAT_B8G8R8A8_UNORM; output.SampleDesc.Count = 1;
        output.Usage = D3D11_USAGE_DEFAULT; output.BindFlags = D3D11_BIND_RENDER_TARGET;
        hr = m_device->CreateTexture2D(&output, nullptr, &m_captureOutputTexture);
        if (FAILED(hr)) { LogD3DFailure("CreateTexture2D(video output)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        hr = m_device->CreateRenderTargetView(m_captureOutputTexture, nullptr, &m_captureRtv);
        if (FAILED(hr)) { LogD3DFailure("CreateRenderTargetView(video)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }

        ID3DBlob* vsBlob = nullptr; ID3DBlob* errorBlob = nullptr;
        hr = D3DCompile(kCaptureVertexShader, std::strlen(kCaptureVertexShader), "PhoneVRVideoVS", nullptr, nullptr, "main", "vs_4_0", D3DCOMPILE_OPTIMIZATION_LEVEL3, 0, &vsBlob, &errorBlob);
        if (FAILED(hr)) { if (errorBlob) errorBlob->Release(); LogD3DFailure("D3DCompile(video VS)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        hr = m_device->CreateVertexShader(vsBlob->GetBufferPointer(), vsBlob->GetBufferSize(), nullptr, &m_captureVS);
        vsBlob->Release();
        if (FAILED(hr)) { LogD3DFailure("CreateVertexShader(video)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        errorBlob = nullptr; ID3DBlob* psBlob = nullptr;
        hr = D3DCompile(kCapturePixelShader, std::strlen(kCapturePixelShader), "PhoneVRVideoPS", nullptr, nullptr, "main", "ps_4_0", D3DCOMPILE_OPTIMIZATION_LEVEL3, 0, &psBlob, &errorBlob);
        if (FAILED(hr)) { if (errorBlob) errorBlob->Release(); LogD3DFailure("D3DCompile(video PS)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        hr = m_device->CreatePixelShader(psBlob->GetBufferPointer(), psBlob->GetBufferSize(), nullptr, &m_capturePS);
        psBlob->Release();
        if (FAILED(hr)) { LogD3DFailure("CreatePixelShader(video)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }

        D3D11_SAMPLER_DESC sampler{}; sampler.Filter = D3D11_FILTER_MIN_MAG_MIP_LINEAR;
        sampler.AddressU = sampler.AddressV = sampler.AddressW = D3D11_TEXTURE_ADDRESS_CLAMP;
        sampler.MaxLOD = D3D11_FLOAT32_MAX;
        hr = m_device->CreateSamplerState(&sampler, &m_captureSampler);
        if (FAILED(hr)) { LogD3DFailure("CreateSamplerState(video)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        for (auto& slot : m_captureSlots) {
            D3D11_TEXTURE2D_DESC staging = output;
            staging.Usage = D3D11_USAGE_STAGING; staging.BindFlags = 0; staging.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
            hr = m_device->CreateTexture2D(&staging, nullptr, &slot.staging);
            if (FAILED(hr)) { LogD3DFailure("CreateTexture2D(video staging)", hr); ReleaseVideoCaptureResourcesUnlocked(); return false; }
        }
        m_captureWidth = source.Width; m_captureHeight = source.Height; m_loggedVideoFormatFailure = false;
        m_captureDirectCopy = false;
        m_captureResourcesReady = true;
        if (vr::VRDriverLog()) {
            char line[192]{}; std::snprintf(line, sizeof(line), "PhoneVR: H.264 capture resources ready: source %ux%u -> SBS %ux%u @ %u FPS", source.Width, source.Height, ActiveVideoStreamProfile().width, ActiveVideoStreamProfile().height, kVideoFrameRate);
            vr::VRDriverLog()->Log(line);
        }
        return true;
    }

    void CaptureVideoFrameUnlocked(uint64_t frameId) {
        if (!m_captureRunning.load(std::memory_order_acquire) || !m_captureResourcesReady) return;
        const uint64_t now = MonotonicNowNs();
        const uint64_t minimumInterval = 1000000000ULL / kVideoFrameRate;
        if (m_lastVideoCaptureNs && now - m_lastVideoCaptureNs < minimumInterval) return;
        m_lastVideoCaptureNs = now;
        std::unique_lock<std::mutex> resourceLock(m_captureResourcesMutex, std::try_to_lock);
        if (!resourceLock.owns_lock()) { m_videoFramesSkipped.fetch_add(1, std::memory_order_relaxed); return; }
        size_t slotIndex = kVideoCaptureSlots;
        for (size_t i = 0; i < kVideoCaptureSlots; ++i) {
            bool expected = false;
            if (m_captureSlots[i].busy.compare_exchange_strong(expected, true, std::memory_order_acq_rel)) { slotIndex = i; break; }
        }
        if (slotIndex == kVideoCaptureSlots) { m_videoFramesSkipped.fetch_add(1, std::memory_order_relaxed); return; }
        auto& slot = m_captureSlots[slotIndex];
        if (m_captureDirectCopy) {
            m_context->CopyResource(slot.staging, m_sharedTexture);
        } else {
            m_context->CopyResource(m_captureInputTexture, m_sharedTexture);
            ID3D11RenderTargetView* target = m_captureRtv;
            m_context->OMSetRenderTargets(1, &target, nullptr);
            D3D11_VIEWPORT viewport{}; viewport.Width = static_cast<float>(ActiveVideoStreamProfile().width); viewport.Height = static_cast<float>(ActiveVideoStreamProfile().height); viewport.MaxDepth = 1.0f;
            m_context->RSSetViewports(1, &viewport);
            m_context->IASetInputLayout(nullptr); m_context->IASetPrimitiveTopology(D3D11_PRIMITIVE_TOPOLOGY_TRIANGLELIST);
            m_context->VSSetShader(m_captureVS, nullptr, 0); m_context->PSSetShader(m_capturePS, nullptr, 0);
            m_context->PSSetShaderResources(0, 1, &m_captureSrv); m_context->PSSetSamplers(0, 1, &m_captureSampler);
            m_context->Draw(3, 0);
            ID3D11ShaderResourceView* nullSrv = nullptr; m_context->PSSetShaderResources(0, 1, &nullSrv);
            ID3D11RenderTargetView* nullRtv = nullptr; m_context->OMSetRenderTargets(1, &nullRtv, nullptr);
            m_context->CopyResource(slot.staging, m_captureOutputTexture);
        }
        slot.frameId = static_cast<uint32_t>(frameId); slot.timestampNs = now;
        {
            std::lock_guard<std::mutex> queueLock(m_captureQueueMutex);
            if (m_captureQueue.size() >= kVideoCaptureSlots) {
                auto dropped = m_captureQueue.front(); m_captureQueue.pop_front();
                m_captureSlots[dropped].busy.store(false, std::memory_order_release);
                m_videoFramesSkipped.fetch_add(1, std::memory_order_relaxed);
            }
            m_captureQueue.push_back(slotIndex);
        }
        m_videoFramesQueued.fetch_add(1, std::memory_order_relaxed);
        m_captureCv.notify_one();
    }

    static bool SendAll(SOCKET socketValue, const char* data, size_t length, int* socketError = nullptr) {
        while (length) {
            const int chunk = static_cast<int>(std::min<size_t>(length, 1024 * 1024));
            const int sent = send(socketValue, data, chunk, 0);
            if (sent == SOCKET_ERROR) {
                if (socketError) *socketError = WSAGetLastError();
                return false;
            }
            if (sent == 0) {
                if (socketError) *socketError = WSAECONNRESET;
                return false;
            }
            data += sent; length -= static_cast<size_t>(sent);
        }
        return true;
    }
    static void PutBE16(uint8_t* p, uint16_t value) { p[0] = static_cast<uint8_t>(value >> 8); p[1] = static_cast<uint8_t>(value); }
    static void PutBE32(uint8_t* p, uint32_t value) { p[0]=static_cast<uint8_t>(value>>24); p[1]=static_cast<uint8_t>(value>>16); p[2]=static_cast<uint8_t>(value>>8); p[3]=static_cast<uint8_t>(value); }
    static void PutBE64(uint8_t* p, uint64_t value) { PutBE32(p, static_cast<uint32_t>(value>>32)); PutBE32(p+4, static_cast<uint32_t>(value)); }

    void MaybeLogVideoCaptureStats(bool bridgeActive, uint64_t& lastStatsNs) {
        const uint64_t now = MonotonicNowNs();
        if (lastStatsNs != 0 && now - lastStatsNs < 5000000000ULL) return;
        lastStatsNs = now;
        const uint64_t queued = m_videoFramesQueued.exchange(0, std::memory_order_relaxed);
        const uint64_t readback = m_videoFramesReadback.exchange(0, std::memory_order_relaxed);
        const uint64_t sent = m_videoFramesSent.exchange(0, std::memory_order_relaxed);
        const uint64_t mapFailures = m_videoReadbackFailures.exchange(0, std::memory_order_relaxed);
        const uint64_t mapPending = m_videoReadbackPending.exchange(0, std::memory_order_relaxed);
        const uint64_t bridgeFailures = m_videoBridgeFailures.exchange(0, std::memory_order_relaxed);
        const uint64_t skipped = m_videoFramesSkipped.exchange(0, std::memory_order_relaxed);
        char line[320]{};
        std::snprintf(line, sizeof(line),
                      "PhoneVR: video capture pipeline bridge=%s queued=%llu readback=%llu sent=%llu gpu-map-pending=%llu gpu-map-fail=%llu bridge-fail=%llu skipped=%llu",
                      bridgeActive ? "connected" : "waiting",
                      static_cast<unsigned long long>(queued),
                      static_cast<unsigned long long>(readback),
                      static_cast<unsigned long long>(sent),
                      static_cast<unsigned long long>(mapPending),
                      static_cast<unsigned long long>(mapFailures),
                      static_cast<unsigned long long>(bridgeFailures),
                      static_cast<unsigned long long>(skipped));
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log(line);
    }

    void VideoCaptureLoop() {
        WSADATA wsa{};
        if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: WSAStartup failed for video capture bridge");
            m_captureRunning.store(false, std::memory_order_release);
            return;
        }
        SOCKET bridge = INVALID_SOCKET;
        uint64_t lastStatsNs = 0;
        std::vector<uint8_t> pixels(static_cast<size_t>(ActiveVideoStreamProfile().width) * ActiveVideoStreamProfile().height * 4);
        while (m_captureRunning.load(std::memory_order_acquire)) {
            if (bridge == INVALID_SOCKET) {
                bridge = socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
                if (bridge == INVALID_SOCKET) {
                    m_videoBridgeFailures.fetch_add(1, std::memory_order_relaxed);
                }
                if (bridge != INVALID_SOCKET) {
                    sockaddr_in address{}; address.sin_family = AF_INET; address.sin_port = htons(kVideoCapturePort); address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
                    if (connect(bridge, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == SOCKET_ERROR) {
                        closesocket(bridge); bridge = INVALID_SOCKET;
                        m_videoBridgeFailures.fetch_add(1, std::memory_order_relaxed);
                    } else {
                        BOOL noDelay = TRUE; setsockopt(bridge, IPPROTO_TCP, TCP_NODELAY, reinterpret_cast<const char*>(&noDelay), sizeof(noDelay));
                        // A raw 1600x720 BGRA frame is 4.6 MiB. A 250 ms send
                        // timeout is too aggressive during brief Host/encoder stalls
                        // and causes a reconnect loop that used to respawn FFmpeg.
                        DWORD timeout = 5000;
                        setsockopt(bridge, SOL_SOCKET, SO_SNDTIMEO, reinterpret_cast<const char*>(&timeout), sizeof(timeout));
                        int sendBufferBytes = 8 * 1024 * 1024;
                        setsockopt(bridge, SOL_SOCKET, SO_SNDBUF, reinterpret_cast<const char*>(&sendBufferBytes), sizeof(sendBufferBytes));
                        char activation = 0;
                        if (recv(bridge, &activation, 1, MSG_WAITALL) != 1 || activation != 1) {
                            closesocket(bridge); bridge = INVALID_SOCKET;
                            m_videoBridgeFailures.fetch_add(1, std::memory_order_relaxed);
                        } else {
                            m_captureBridgeReady.store(true, std::memory_order_release);
                            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: Host accepted the H.264 capture bridge");
                        }
                    }
                }
                if (bridge == INVALID_SOCKET) {
                    MaybeLogVideoCaptureStats(false, lastStatsNs);
                    std::this_thread::sleep_for(std::chrono::milliseconds(500));
                    continue;
                }
            }
            size_t slotIndex = kVideoCaptureSlots;
            {
                std::unique_lock<std::mutex> lock(m_captureQueueMutex);
                m_captureCv.wait_for(lock, std::chrono::milliseconds(100), [this] { return !m_captureQueue.empty() || !m_captureRunning.load(); });
                if (!m_captureRunning.load()) break;
                if (!m_captureQueue.empty()) { slotIndex = m_captureQueue.front(); m_captureQueue.pop_front(); }
            }
            if (slotIndex == kVideoCaptureSlots) {
                MaybeLogVideoCaptureStats(true, lastStatsNs);
                continue;
            }

            // Only hold the D3D resource lock while mapping and copying into the
            // CPU-owned staging buffer. TCP writes can block, so they must run
            // after the capture slot has been released to the Present callback.
            bool readbackReady = false;
            bool mapPending = false;
            uint32_t frameId = 0;
            uint64_t timestampNs = 0;
            {
                std::lock_guard<std::mutex> resourceLock(m_captureResourcesMutex);
                auto& slot = m_captureSlots[slotIndex];
                frameId = slot.frameId;
                timestampNs = slot.timestampNs;
                D3D11_MAPPED_SUBRESOURCE mapped{};
                // Never let the video worker block inside Map while holding
                // the protected immediate-context lock. A blocking readback
                // can stall SteamVR Present/WaitForPresent on this D3D11
                // device. Poll with DO_NOT_WAIT and requeue the slot until the
                // GPU has completed the staging copy.
                const HRESULT hr = m_context->Map(slot.staging, 0, D3D11_MAP_READ,
                                                   D3D11_MAP_FLAG_DO_NOT_WAIT, &mapped);
                if (SUCCEEDED(hr)) {
                    for (uint32_t row = 0; row < ActiveVideoStreamProfile().height; ++row)
                        std::memcpy(pixels.data() + static_cast<size_t>(row) * ActiveVideoStreamProfile().width * 4,
                                    static_cast<const uint8_t*>(mapped.pData) + static_cast<size_t>(row) * mapped.RowPitch,
                                    static_cast<size_t>(ActiveVideoStreamProfile().width) * 4);
                    m_context->Unmap(slot.staging, 0);
                    readbackReady = true;
                    m_videoFramesReadback.fetch_add(1, std::memory_order_relaxed);
                    slot.busy.store(false, std::memory_order_release);
                } else if (hr == DXGI_ERROR_WAS_STILL_DRAWING) {
                    mapPending = true;
                    m_videoReadbackPending.fetch_add(1, std::memory_order_relaxed);
                    // Keep the resource lock until the slot is back in the
                    // queue; otherwise Present could reuse it in the gap.
                    std::lock_guard<std::mutex> queueLock(m_captureQueueMutex);
                    m_captureQueue.push_front(slotIndex);
                } else {
                    m_videoReadbackFailures.fetch_add(1, std::memory_order_relaxed);
                    slot.busy.store(false, std::memory_order_release);
                }
            }

            if (mapPending) {
                MaybeLogVideoCaptureStats(true, lastStatsNs);
                std::this_thread::sleep_for(std::chrono::milliseconds(1));
                continue;
            }

            // A transient GPU readback failure drops this frame only. It must
            // not tear down an otherwise healthy TCP bridge.
            if (readbackReady) {
                uint8_t header[32]{}; std::memcpy(header, "PVRC", 4); header[4] = 1; header[5] = 1;
                PutBE16(header + 6, 32); PutBE32(header + 8, frameId); PutBE64(header + 12, timestampNs);
                PutBE16(header + 20, ActiveVideoStreamProfile().width); PutBE16(header + 22, ActiveVideoStreamProfile().height);
                PutBE32(header + 24, ActiveVideoStreamProfile().width * 4); PutBE32(header + 28, static_cast<uint32_t>(pixels.size()));
                int sendError = 0;
                const bool sentFrame = SendAll(bridge, reinterpret_cast<const char*>(header), sizeof(header), &sendError) &&
                                       SendAll(bridge, reinterpret_cast<const char*>(pixels.data()), pixels.size(), &sendError);
                if (sentFrame) {
                    m_videoFramesSent.fetch_add(1, std::memory_order_relaxed);
                } else {
                    m_captureBridgeReady.store(false, std::memory_order_release);
                    const uint64_t errorNow = MonotonicNowNs();
                    if (vr::VRDriverLog() && (m_lastBridgeErrorLogNs == 0 || errorNow - m_lastBridgeErrorLogNs >= 2000000000ULL)) {
                        char line[192]{};
                        std::snprintf(line, sizeof(line),
                                      "PhoneVR: raw video TCP send failed WSA error=%d; reconnecting capture bridge",
                                      sendError);
                        vr::VRDriverLog()->Log(line);
                        m_lastBridgeErrorLogNs = errorNow;
                    }
                    closesocket(bridge); bridge = INVALID_SOCKET;
                    m_videoBridgeFailures.fetch_add(1, std::memory_order_relaxed);
                    m_videoFramesSkipped.fetch_add(1, std::memory_order_relaxed);
                }
            } else {
                m_videoFramesSkipped.fetch_add(1, std::memory_order_relaxed);
            }
            MaybeLogVideoCaptureStats(bridge != INVALID_SOCKET, lastStatsNs);
        }
        m_captureBridgeReady.store(false, std::memory_order_release);
        if (bridge != INVALID_SOCKET) closesocket(bridge);
        WSACleanup();
    }

    void ReleaseVideoCaptureResourcesUnlocked() {
        m_captureResourcesReady = false;
        m_captureDirectCopy = false;
        for (auto& slot : m_captureSlots) { if (slot.staging) { slot.staging->Release(); slot.staging = nullptr; } slot.busy.store(false); }
        if (m_captureSampler) { m_captureSampler->Release(); m_captureSampler = nullptr; }
        if (m_capturePS) { m_capturePS->Release(); m_capturePS = nullptr; }
        if (m_captureVS) { m_captureVS->Release(); m_captureVS = nullptr; }
        if (m_captureRtv) { m_captureRtv->Release(); m_captureRtv = nullptr; }
        if (m_captureSrv) { m_captureSrv->Release(); m_captureSrv = nullptr; }
        ReleaseTexture(m_captureOutputTexture); ReleaseTexture(m_captureInputTexture);
        std::lock_guard<std::mutex> queueLock(m_captureQueueMutex); m_captureQueue.clear();
    }
    void ReleaseVideoCaptureResources() {
        std::lock_guard<std::mutex> lock(m_captureResourcesMutex);
        ReleaseVideoCaptureResourcesUnlocked();
    }

    void ShutdownUnlocked() {
        ReleaseVideoCaptureResources();
        m_pendingPresent = false;
        m_lastPresentCallbackNs = 0;
        m_sharedHandle = nullptr;
        ReleaseTexture(m_sharedTexture);
        if (m_context) { m_context->ClearState(); m_context->Flush(); m_context->Release(); m_context = nullptr; }
        if (m_device) { m_device->Release(); m_device = nullptr; }
        m_adapterLuid = 0;
        m_pendingVsyncTimeNs = 0;
        m_lastFrameId = 0;
        {
            std::lock_guard<std::mutex> timingLock(m_timingMutex);
            m_vsyncOriginNs = 0;
            m_vsyncOriginFrameCounter = 0;
            m_lastScheduledVsyncNs = 0;
        }
        m_completedFrameCounter.store(0, std::memory_order_relaxed);
    }

    mutable std::mutex m_mutex;
    std::mutex m_waitMutex;
    ID3D11Device* m_device = nullptr;
    ID3D11DeviceContext* m_context = nullptr;
    ID3D11Texture2D* m_sharedTexture = nullptr;
    HANDLE m_sharedHandle = nullptr;
    uint64_t m_adapterLuid = 0;
    uint64_t m_lastFrameId = 0;
    uint64_t m_pendingVsyncTimeNs = 0;
    uint64_t m_lastPresentCallbackNs = 0;
    bool m_pendingPresent = false;
    std::mutex m_timingMutex;
    uint64_t m_vsyncOriginNs = 0;
    uint64_t m_vsyncOriginFrameCounter = 0;
    uint64_t m_lastScheduledVsyncNs = 0;
    bool m_loggedOpenFailure = false;
    bool m_loggedMutexQueryFailure = false;
    uint64_t m_mutexAcquireFailures = 0;
    uint64_t m_lastMutexFailureLogNs = 0;
    std::atomic<uint64_t> m_completedFrameCounter{0};
    std::atomic<uint64_t> m_presentCalls{0};
    std::atomic<uint64_t> m_waitCalls{0};
    std::atomic<uint64_t> m_timingCompletedFrames{0};
    std::atomic<uint64_t> m_waitCallsWithoutPresent{0};
    std::atomic<uint64_t> m_vsyncNoneCalls{0};
    std::atomic<uint64_t> m_vsyncWaitRenderCalls{0};
    std::atomic<uint64_t> m_vsyncNoWaitRenderCalls{0};
    std::atomic<uint64_t> m_maxPresentDurationNs{0};
    std::atomic<uint64_t> m_maxWaitDurationNs{0};
    std::atomic<uint64_t> m_maxPresentIntervalNs{0};
    std::atomic<uint64_t> m_presentGapsOverOneAndHalfFrames{0};
    std::atomic<uint64_t> m_presentGapsOverThreeFrames{0};
    std::atomic<uint64_t> m_pendingPresentOverwrites{0};
    std::atomic<uint64_t> m_maxWaitOvershootNs{0};
    std::atomic<uint64_t> m_waitOvershootsOverOneMs{0};
    std::atomic<uint64_t> m_waitOvershootsOverFiveMs{0};
    std::atomic<uint64_t> m_missedVirtualVsyncs{0};
    std::atomic<uint64_t> m_keyedMutexAcquireFailures{0};
    std::mutex m_captureResourcesMutex;
    std::mutex m_captureQueueMutex;
    std::condition_variable m_captureCv;
    std::deque<size_t> m_captureQueue;
    std::array<CaptureSlot, kVideoCaptureSlots> m_captureSlots{};
    ID3D11Texture2D* m_captureInputTexture = nullptr;
    ID3D11Texture2D* m_captureOutputTexture = nullptr;
    bool m_captureDirectCopy = false;
    bool m_captureResourcesReady = false;
    ID3D11ShaderResourceView* m_captureSrv = nullptr;
    ID3D11RenderTargetView* m_captureRtv = nullptr;
    ID3D11VertexShader* m_captureVS = nullptr;
    ID3D11PixelShader* m_capturePS = nullptr;
    ID3D11SamplerState* m_captureSampler = nullptr;
    std::thread m_captureThread;
    std::atomic<bool> m_captureRunning{false};
    std::atomic<bool> m_captureBridgeReady{false};
    std::atomic<uint64_t> m_videoFramesSkipped{0};
    std::atomic<uint64_t> m_videoFramesQueued{0};
    std::atomic<uint64_t> m_videoFramesReadback{0};
    std::atomic<uint64_t> m_videoFramesSent{0};
    std::atomic<uint64_t> m_videoReadbackFailures{0};
    std::atomic<uint64_t> m_videoReadbackPending{0};
    std::atomic<uint64_t> m_videoBridgeFailures{0};
    uint64_t m_lastVideoCaptureNs = 0;
    uint64_t m_lastBridgeErrorLogNs = 0;
    uint32_t m_captureWidth = 0;
    uint32_t m_captureHeight = 0;
    bool m_loggedVideoFormatFailure = false;
};

class PhoneVRDisplayRedirect final : public vr::ITrackedDeviceServerDriver {
public:
    // Initialize the D3D11 device before registering the HMD. Activate() then
    // only needs to publish the redirect properties and return the component.
    bool Prepare() {
        uint64_t adapterLuid = 0;
        if (!m_display.Initialize(&adapterLuid)) {
            if (vr::VRDriverLog())
                vr::VRDriverLog()->Log("PhoneVR: DisplayRedirect D3D11 pre-initialization failed");
            return false;
        }
        m_adapterLuid.store(adapterLuid, std::memory_order_release);
        if (vr::VRDriverLog()) {
            char line[192]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: DisplayRedirect D3D11 pre-initialized adapterLuid=0x%016llX",
                          static_cast<unsigned long long>(adapterLuid));
            vr::VRDriverLog()->Log(line);
        }
        // The reserved override container is guaranteed to exist before the
        // DisplayRedirect device activates. Try an early write as well, then
        // retry immediately after TrackedDeviceAdded has registered the device.
        PublishHmdOverrides("pre-registration");
        return true;
    }

    bool PublishHmdOverrides(const char* phase) {
        const uint64_t adapterLuid = m_adapterLuid.load(std::memory_order_acquire);
        if (adapterLuid == 0) return false;
        auto* properties = vr::VRProperties();
        const auto virtualDisplayResult = properties->SetBoolProperty(
            vr::k_ulDisplayRedirectContainer, vr::Prop_HasVirtualDisplayComponent_Bool, true);
        const auto adapterResult = properties->SetUint64Property(
            vr::k_ulDisplayRedirectContainer, vr::Prop_GraphicsAdapterLuid_Uint64, adapterLuid);
        const bool success = virtualDisplayResult == vr::TrackedProp_Success &&
                             adapterResult == vr::TrackedProp_Success;
        if (vr::VRDriverLog()) {
            char line[288]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: DisplayRedirect HMD override publish phase=%s adapterLuid=0x%016llX results hasVirtualDisplay=%d adapterLuid=%d success=%s",
                          phase ? phase : "unknown",
                          static_cast<unsigned long long>(adapterLuid),
                          static_cast<int>(virtualDisplayResult),
                          static_cast<int>(adapterResult), success ? "yes" : "no");
            vr::VRDriverLog()->Log(line);
        }
        return success;
    }

    vr::EVRInitError Activate(uint32_t objectId) override {
        uint64_t adapterLuid = 0;
        if (!m_display.Initialize(&adapterLuid)) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: virtual display capture initialization failed; display redirect is inactive");
            return vr::VRInitError_Driver_Unknown;
        }
        const auto container = vr::VRProperties()->TrackedDeviceToPropertyContainer(objectId);
        m_adapterLuid.store(adapterLuid, std::memory_order_release);
        auto* properties = vr::VRProperties();
        const auto trackingSystemResult = properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        const auto modelResult = properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "PhoneVR Wireless Display Redirect");
        const auto serialResult = properties->SetStringProperty(container, vr::Prop_SerialNumber_String, "PHONEVR-DISPLAY-001");
        const auto typeResult = properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_display_redirect");
        const auto virtualDisplayResult = properties->SetBoolProperty(container, vr::Prop_HasVirtualDisplayComponent_Bool, true);
        const auto adapterResult = properties->SetUint64Property(container, vr::Prop_GraphicsAdapterLuid_Uint64, adapterLuid);

        // Publish the HMD-facing route again from Activate as a final fallback.
        // The provider also attempts this after TrackedDeviceAdded and before
        // registering the HMD, so the override can be visible to early HMD probes.
        PublishHmdOverrides("displayredirect-activate");

        m_active.store(true, std::memory_order_release);
        if (vr::VRDriverLog()) {
            char line[640]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: DisplayRedirect Activate succeeded objectId=%u adapterLuid=0x%016llX device-property-results tracking=%d model=%d serial=%d type=%d hasVirtualDisplay=%d adapterLuid=%d",
                          objectId, static_cast<unsigned long long>(adapterLuid),
                          static_cast<int>(trackingSystemResult), static_cast<int>(modelResult),
                          static_cast<int>(serialResult), static_cast<int>(typeResult),
                          static_cast<int>(virtualDisplayResult), static_cast<int>(adapterResult));
            vr::VRDriverLog()->Log(line);
        }
        return vr::VRInitError_None;
    }
    void Deactivate() override { m_active.store(false, std::memory_order_release); m_display.Shutdown(); }
    void EnterStandby() override {}
    bool IsActive() const { return m_active.load(std::memory_order_acquire); }
    uint64_t PresentCounter() const { return m_display.PresentCounter(); }
    uint64_t ComponentQueryCount() const { return m_componentQueries.load(std::memory_order_relaxed); }
    uint64_t ComponentMatchCount() const { return m_componentMatches.load(std::memory_order_relaxed); }
    VirtualDisplayTimingSnapshot TakeTimingSnapshot() { return m_display.TakeTimingSnapshot(); }
    void* GetComponent(const char* nameAndVersion) override {
        m_componentQueries.fetch_add(1, std::memory_order_relaxed);
        const bool virtualDisplayMatch = nameAndVersion &&
            _stricmp(nameAndVersion, vr::IVRVirtualDisplay_Version) == 0;
        if (virtualDisplayMatch) m_componentMatches.fetch_add(1, std::memory_order_relaxed);
        if (vr::VRDriverLog() && !m_loggedGetComponent.exchange(true, std::memory_order_relaxed)) {
            char line[256]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: DisplayRedirect GetComponent first query='%s' expected='%s' match=%s",
                          nameAndVersion ? nameAndVersion : "<null>",
                          vr::IVRVirtualDisplay_Version, virtualDisplayMatch ? "yes" : "no");
            vr::VRDriverLog()->Log(line);
        }
        if (virtualDisplayMatch) {
            return static_cast<void*>(static_cast<vr::IVRVirtualDisplay*>(&m_display));
        }
        return nullptr;
    }
    void DebugRequest(const char*, char* response, uint32_t size) override { if (response && size) response[0] = '\0'; }
    vr::DriverPose_t GetPose() override {
        vr::DriverPose_t pose{};
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {1.0, 0.0, 0.0, 0.0};
        pose.result = vr::TrackingResult_Running_OK;
        pose.deviceIsConnected = true;
        pose.poseIsValid = true;
        return pose;
    }
private:
    VirtualDisplayComponent m_display;
    std::atomic<uint64_t> m_adapterLuid{0};
    std::atomic<bool> m_active{false};
    std::atomic<bool> m_loggedGetComponent{false};
    std::atomic<uint64_t> m_componentQueries{0};
    std::atomic<uint64_t> m_componentMatches{0};
};

struct DaydreamControllerSnapshot {
    bool connected = false;
    bool touching = false;
    bool hasOrientation = false;
    bool click = false;
    bool app = false;
    bool home = false;
    bool volumeUp = false;
    bool volumeDown = false;
    uint32_t sequence = 0;
    uint64_t timestampNs = 0;
    uint64_t receivedAtNs = 0;
    double x = 0.0, y = 0.0, z = 0.0, w = 1.0;
    double touchX = 0.0, touchY = 0.0;
};

class DaydreamControllerReceiver {
public:
    bool Start() {
        if (m_running.exchange(true)) return true;
        WSADATA data{};
        if (WSAStartup(MAKEWORD(2, 2), &data) != 0) {
            m_running = false;
            return false;
        }
        m_socket = socket(AF_INET, SOCK_DGRAM, IPPROTO_UDP);
        if (m_socket == INVALID_SOCKET) {
            WSACleanup();
            m_running = false;
            return false;
        }
        int receiveBufferBytes = 16 * 1024;
        (void)setsockopt(m_socket, SOL_SOCKET, SO_RCVBUF,
                         reinterpret_cast<const char*>(&receiveBufferBytes), sizeof(receiveBufferBytes));
        DWORD timeoutMs = 100;
        (void)setsockopt(m_socket, SOL_SOCKET, SO_RCVTIMEO,
                         reinterpret_cast<const char*>(&timeoutMs), sizeof(timeoutMs));
        sockaddr_in address{};
        address.sin_family = AF_INET;
        address.sin_port = htons(kControllerBridgePort);
        address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        if (bind(m_socket, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == SOCKET_ERROR) {
            closesocket(m_socket);
            m_socket = INVALID_SOCKET;
            WSACleanup();
            m_running = false;
            return false;
        }
        m_thread = std::thread(&DaydreamControllerReceiver::Run, this);
        return true;
    }

    void Stop() {
        if (!m_running.exchange(false)) return;
        const SOCKET socket = m_socket;
        m_socket = INVALID_SOCKET;
        if (socket != INVALID_SOCKET) {
            shutdown(socket, SD_BOTH);
            closesocket(socket);
        }
        if (m_thread.joinable()) m_thread.join();
        WSACleanup();
    }

    DaydreamControllerSnapshot Snapshot(uint8_t hand) const {
        if (hand > 1) return {};
        std::lock_guard<std::mutex> lock(m_mutex);
        return m_states[hand];
    }

    ~DaydreamControllerReceiver() { Stop(); }

private:
    static bool Decode(const uint8_t* data, int length, DaydreamControllerSnapshot* output) {
        if (!data || !output || length != static_cast<int>(kControllerPacketSize) ||
            std::memcmp(data, "PVSC", 4) != 0 || data[4] != 1 || data[5] > 1) return false;
        DaydreamControllerSnapshot next{};
        const uint8_t flags = data[6];
        const uint8_t buttons = data[7];
        next.connected = (flags & (1 << 0)) != 0;
        next.touching = (flags & (1 << 1)) != 0;
        next.hasOrientation = (flags & (1 << 2)) != 0;
        next.click = (buttons & (1 << 0)) != 0;
        next.app = (buttons & (1 << 1)) != 0;
        next.home = (buttons & (1 << 2)) != 0;
        next.volumeUp = (buttons & (1 << 3)) != 0;
        next.volumeDown = (buttons & (1 << 4)) != 0;
        next.sequence = ReadBE32(data + 8);
        next.timestampNs = ReadBE64(data + 12);
        next.x = ReadBEFloat(data + 20);
        next.y = ReadBEFloat(data + 24);
        next.z = ReadBEFloat(data + 28);
        next.w = ReadBEFloat(data + 32);
        next.touchX = ReadBEFloat(data + 36);
        next.touchY = ReadBEFloat(data + 40);
        const double values[] = {next.x, next.y, next.z, next.w, next.touchX, next.touchY};
        for (double value : values) if (!std::isfinite(value)) return false;
        next.touchX = std::clamp(next.touchX, 0.0, 1.0);
        next.touchY = std::clamp(next.touchY, 0.0, 1.0);
        if (next.hasOrientation) {
            const double norm = std::sqrt(next.x * next.x + next.y * next.y + next.z * next.z + next.w * next.w);
            if (norm < 0.5 || norm > 1.5) return false;
            next.x /= norm; next.y /= norm; next.z /= norm; next.w /= norm;
        } else {
            next.x = next.y = next.z = 0.0;
            next.w = 1.0;
        }
        next.receivedAtNs = MonotonicNowNs();
        *output = next;
        return true;
    }

    void Run() {
        const SOCKET socket = m_socket;
        std::array<uint8_t, 256> buffer{};
        while (m_running.load(std::memory_order_acquire)) {
            const int received = recv(socket, reinterpret_cast<char*>(buffer.data()),
                                      static_cast<int>(buffer.size()), 0);
            if (received == SOCKET_ERROR) {
                if (!m_running.load(std::memory_order_acquire)) break;
                const int error = WSAGetLastError();
                if (error == WSAETIMEDOUT || error == WSAEWOULDBLOCK) continue;
                continue;
            }
            DaydreamControllerSnapshot state{};
            if (!Decode(buffer.data(), received, &state)) {
                m_invalidPackets.fetch_add(1, std::memory_order_relaxed);
                continue;
            }
            const uint8_t hand = buffer[5];
            {
                std::lock_guard<std::mutex> lock(m_mutex);
                m_states[hand] = state;
            }
            m_receivedPackets.fetch_add(1, std::memory_order_relaxed);
        }
    }

    std::atomic<bool> m_running{false};
    SOCKET m_socket = INVALID_SOCKET;
    std::thread m_thread;
    mutable std::mutex m_mutex;
    DaydreamControllerSnapshot m_states[2]{};
    std::atomic<uint64_t> m_receivedPackets{0};
    std::atomic<uint64_t> m_invalidPackets{0};
};

class PhoneVRDaydreamController final : public vr::ITrackedDeviceServerDriver {
public:
    explicit PhoneVRDaydreamController(bool isLeft) : m_isLeft(isLeft) {}
    ~PhoneVRDaydreamController() { Deactivate(); }

    vr::EVRInitError Activate(uint32_t objectId) override {
        m_objectId.store(objectId, std::memory_order_release);
        auto* properties = vr::VRProperties();
        const auto container = properties->TrackedDeviceToPropertyContainer(objectId);
        const char* serial = m_isLeft ? "PHONEVR-DAYDREAM-LEFT" : "PHONEVR-DAYDREAM-RIGHT";
        properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "Google Daydream Controller");
        properties->SetStringProperty(container, vr::Prop_SerialNumber_String, serial);
        properties->SetStringProperty(container, vr::Prop_ManufacturerName_String, "Google");
        properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_daydream_controller");
        properties->SetStringProperty(container, vr::Prop_ControllerType_String, "phonevr_daydream_controller");
        properties->SetStringProperty(container, vr::Prop_ResourceRoot_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_InputProfilePath_String,
                                       "{phonevr}/input/phonevr_daydream_controller_profile.json");
        properties->SetInt32Property(container, vr::Prop_ControllerRoleHint_Int32,
                                     m_isLeft ? vr::TrackedControllerRole_LeftHand : vr::TrackedControllerRole_RightHand);
        properties->SetBoolProperty(container, vr::Prop_DeviceIsWireless_Bool, true);
        properties->SetBoolProperty(container, vr::Prop_HasControllerComponent_Bool, true);
        properties->SetBoolProperty(container, vr::Prop_HasDisplayComponent_Bool, false);
        properties->SetBoolProperty(container, vr::Prop_HasCameraComponent_Bool, false);
        properties->SetBoolProperty(container, vr::Prop_HasDriverDirectModeComponent_Bool, false);
        auto* input = vr::VRDriverInput();
        if (!input) return vr::VRInitError_Driver_Unknown;
        auto createBoolean = [&](const char* path, vr::VRInputComponentHandle_t* handle) {
            return input->CreateBooleanComponent(container, path, handle) == vr::VRInputError_None;
        };
        auto createScalar = [&](const char* path, vr::VRInputComponentHandle_t* handle) {
            return input->CreateScalarComponent(container, path, handle,
                                                vr::VRScalarType_Absolute,
                                                vr::VRScalarUnits_NormalizedTwoSided) == vr::VRInputError_None;
        };
        if (!createScalar("/input/joystick/x", &m_joystickX) ||
            !createScalar("/input/joystick/y", &m_joystickY) ||
            input->CreateScalarComponent(container, "/input/trigger/value", &m_triggerValue,
                                         vr::VRScalarType_Absolute,
                                         vr::VRScalarUnits_NormalizedOneSided) != vr::VRInputError_None ||
            !createBoolean("/input/joystick/touch", &m_joystickTouch) ||
            !createBoolean("/input/joystick/click", &m_joystickClick) ||
            !createBoolean("/input/trigger/click", &m_triggerClick) ||
            !createBoolean("/input/menu/click", &m_menuClick) ||
            !createBoolean("/input/system/click", &m_systemClick) ||
            !createBoolean("/input/volume_up/click", &m_volumeUpClick) ||
            !createBoolean("/input/volume_down/click", &m_volumeDownClick)) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: failed to create a Daydream controller input component");
            return vr::VRInitError_Driver_Unknown;
        }
        auto pose = MakeUntrackedPose();
        {
            std::lock_guard<std::mutex> lock(m_poseMutex);
            m_pose = pose;
        }
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(objectId, pose, sizeof(pose));
        if (vr::VRDriverLog()) {
            char line[200]{};
            std::snprintf(line, sizeof(line), "PhoneVR: registered Daydream %s controller device objectId=%u (3DoF)",
                          m_isLeft ? "left" : "right", objectId);
            vr::VRDriverLog()->Log(line);
        }
        return vr::VRInitError_None;
    }

    void Deactivate() override {
        m_objectId.store(vr::k_unTrackedDeviceIndexInvalid, std::memory_order_release);
    }
    void EnterStandby() override {}
    void* GetComponent(const char*) override { return nullptr; }
    void DebugRequest(const char*, char* response, uint32_t size) override {
        if (response && size) response[0] = '\0';
    }
    vr::DriverPose_t GetPose() override {
        std::lock_guard<std::mutex> lock(m_poseMutex);
        return m_pose;
    }

    void Update(const DaydreamControllerSnapshot& state, const vr::DriverPose_t& hmdPose) {
        const uint32_t objectId = m_objectId.load(std::memory_order_acquire);
        if (objectId == vr::k_unTrackedDeviceIndexInvalid) return;
        auto* input = vr::VRDriverInput();
        if (!input) return;
        const uint64_t nowNs = MonotonicNowNs();
        const bool connected = state.connected && state.receivedAtNs != 0 &&
            nowNs >= state.receivedAtNs && nowNs - state.receivedAtNs <= 3000000000ULL;
        const bool trackingValid = connected && state.hasOrientation &&
            hmdPose.deviceIsConnected && hmdPose.poseIsValid &&
            nowNs - state.receivedAtNs <= 500000000ULL;
        const double x = state.touching ? state.touchX * 2.0 - 1.0 : 0.0;
        const double y = state.touching ? (0.5 - state.touchY) * 2.0 : 0.0;
        DaydreamPoseOutput modeledPose{};
        if (trackingValid) {
            const DaydreamQuaternion headQ{
                hmdPose.qRotation.x, hmdPose.qRotation.y, hmdPose.qRotation.z, hmdPose.qRotation.w};
            const std::array<double, 3> headPosition{
                hmdPose.vecPosition[0], hmdPose.vecPosition[1], hmdPose.vecPosition[2]};
            const std::array<double, 3> headAngularVelocity{
                hmdPose.vecAngularVelocity[0], hmdPose.vecAngularVelocity[1], hmdPose.vecAngularVelocity[2]};
            modeledPose = m_armModel.Update(
                DaydreamQuaternion{state.x, state.y, state.z, state.w},
                headPosition, headQ, headAngularVelocity, nowNs, state.app, m_isLeft);
            if (modeledPose.recentered && vr::VRDriverLog()) {
                vr::VRDriverLog()->Log(m_isLeft
                    ? "PhoneVR: left Daydream controller pose recentered by long-pressing App button"
                    : "PhoneVR: right Daydream controller pose recentered by long-pressing App button");
            }
        }
        (void)input->UpdateScalarComponent(m_joystickX, static_cast<float>(x), 0.0);
        (void)input->UpdateScalarComponent(m_joystickY, static_cast<float>(y), 0.0);
        (void)input->UpdateBooleanComponent(m_joystickTouch, connected && state.touching, 0.0);
        (void)input->UpdateBooleanComponent(m_joystickClick, connected && state.click, 0.0);
        (void)input->UpdateBooleanComponent(m_triggerClick, connected && state.click, 0.0);
        (void)input->UpdateScalarComponent(m_triggerValue, connected && state.click ? 1.0f : 0.0f, 0.0);
        (void)input->UpdateBooleanComponent(m_menuClick,
            connected && (trackingValid ? modeledPose.menuPressed : false), 0.0);
        (void)input->UpdateBooleanComponent(m_systemClick, connected && state.home, 0.0);
        (void)input->UpdateBooleanComponent(m_volumeUpClick, connected && state.volumeUp, 0.0);
        (void)input->UpdateBooleanComponent(m_volumeDownClick, connected && state.volumeDown, 0.0);

        vr::DriverPose_t pose = MakeUntrackedPose();
        pose.deviceIsConnected = connected;
        if (trackingValid) {
            pose.qRotation = {modeledPose.orientation.w, modeledPose.orientation.x,
                              modeledPose.orientation.y, modeledPose.orientation.z};
            pose.vecPosition[0] = modeledPose.position[0];
            pose.vecPosition[1] = modeledPose.position[1];
            pose.vecPosition[2] = modeledPose.position[2];
            pose.vecVelocity[0] = modeledPose.velocity[0];
            pose.vecVelocity[1] = modeledPose.velocity[1];
            pose.vecVelocity[2] = modeledPose.velocity[2];
            pose.vecAngularVelocity[0] = modeledPose.angularVelocity[0];
            pose.vecAngularVelocity[1] = modeledPose.angularVelocity[1];
            pose.vecAngularVelocity[2] = modeledPose.angularVelocity[2];
            pose.result = vr::TrackingResult_Running_OK;
            pose.poseIsValid = true;
            pose.willDriftInYaw = true;
            pose.shouldApplyHeadModel = false;
            pose.poseTimeOffset = -std::min<double>(
                static_cast<double>(nowNs - state.receivedAtNs) / 1.0e9, 0.05);
        }
        {
            std::lock_guard<std::mutex> lock(m_poseMutex);
            m_pose = pose;
        }
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(objectId, pose, sizeof(pose));
    }

private:
    static vr::DriverPose_t MakeUntrackedPose() {
        vr::DriverPose_t pose{};
        pose.poseTimeOffset = 0.0;
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {1.0, 0.0, 0.0, 0.0};
        pose.result = vr::TrackingResult_Uninitialized;
        pose.deviceIsConnected = false;
        pose.poseIsValid = false;
        pose.willDriftInYaw = true;
        pose.shouldApplyHeadModel = false;
        return pose;
    }

    bool m_isLeft = false;
    DaydreamArmModel m_armModel;
    std::atomic<uint32_t> m_objectId{vr::k_unTrackedDeviceIndexInvalid};
    mutable std::mutex m_poseMutex;
    vr::DriverPose_t m_pose = MakeUntrackedPose();
    vr::VRInputComponentHandle_t m_joystickX = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_joystickY = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_joystickTouch = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_joystickClick = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_triggerClick = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_triggerValue = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_menuClick = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_systemClick = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_volumeUpClick = vr::k_ulInvalidInputComponentHandle;
    vr::VRInputComponentHandle_t m_volumeDownClick = vr::k_ulInvalidInputComponentHandle;
};

class PhoneVRProvider final : public vr::IServerTrackedDeviceProvider {
public:
    vr::EVRInitError Init(vr::IVRDriverContext* context) override {
        VR_INIT_SERVER_DRIVER_CONTEXT(context);
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: virtual display refresh fixed at 72 Hz");
        if (!m_statusReporter.Start() && vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: failed to start loopback status reporter on UDP 48105");
        m_redirect = new PhoneVRDisplayRedirect();
        m_hmd = new PhoneVRHmd();
        m_leftController = new PhoneVRDaydreamController(true);
        m_rightController = new PhoneVRDaydreamController(false);
        const bool controllerReceiverReady = m_controllerReceiver.Start();
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log(controllerReceiverReady
            ? "PhoneVR: Daydream controller bridge listening on loopback UDP 48108"
            : "PhoneVR: Daydream controller bridge could not bind loopback UDP 48108");
        auto* host = vr::VRServerDriverHost();
        if (!host || !m_redirect->Prepare()) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }

        // Register the redirect first. Its GPU resource setup is already done,
        // so SteamVR can activate it and publish the HMD override container
        // before the HMD's display component is queried.
        if (!host->TrackedDeviceAdded("PHONEVR-DISPLAY-001", vr::TrackedDeviceClass_DisplayRedirect, m_redirect)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        // TrackedDeviceAdded creates the DisplayRedirect registration and lets
        // the runtime prepare its reserved HMD override container. Republish
        // after registration, then add the HMD so its first activation/query
        // sees the virtual-output capability and selected GPU LUID.
        const bool overridePublished = m_redirect->PublishHmdOverrides("after-registration");
        if (vr::VRDriverLog()) {
            vr::VRDriverLog()->Log(overridePublished
                ? "PhoneVR: DisplayRedirect registered and HMD override properties published before HMD registration"
                : "PhoneVR: DisplayRedirect registered before HMD; override publication will be retried during Activate");
        }

        if (!host->TrackedDeviceAdded("PHONEVR-HMD-001", vr::TrackedDeviceClass_HMD, m_hmd)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (!host->TrackedDeviceAdded("PHONEVR-DAYDREAM-LEFT", vr::TrackedDeviceClass_Controller, m_leftController)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (!host->TrackedDeviceAdded("PHONEVR-DAYDREAM-RIGHT", vr::TrackedDeviceClass_Controller, m_rightController)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR 0.5.31 DisplayRedirect and dual Daydream controller devices registered");
        return vr::VRInitError_None;
    }
    void Cleanup() override {
        m_controllerReceiver.Stop();
        m_statusReporter.Stop();
        delete m_leftController; m_leftController = nullptr;
        delete m_rightController; m_rightController = nullptr;
        delete m_hmd; m_hmd = nullptr;
        delete m_redirect; m_redirect = nullptr;
        VR_CLEANUP_SERVER_DRIVER_CONTEXT();
    }
    const char* const* GetInterfaceVersions() override { return vr::k_InterfaceVersions; }
    void RunFrame() override {
        if (m_hmd) m_hmd->RunFrame();
        if (m_hmd) {
            const auto hmdPose = m_hmd->GetPose();
            if (m_leftController) m_leftController->Update(m_controllerReceiver.Snapshot(0), hmdPose);
            if (m_rightController) m_rightController->Update(m_controllerReceiver.Snapshot(1), hmdPose);
        }
        const uint64_t now = MonotonicNowNs();
        if (m_redirect && now - m_lastDisplayDiagnosticsNs >= 5000000000ULL) {
            const double intervalSeconds = m_lastDisplayDiagnosticsNs == 0
                ? 5.0 : static_cast<double>(now - m_lastDisplayDiagnosticsNs) / 1.0e9;
            m_lastDisplayDiagnosticsNs = now;
            const auto timing = m_redirect->TakeTimingSnapshot();
            char line[768]{};
            std::snprintf(line, sizeof(line),
                          "PhoneVR: virtual display active=%s GetComponent=%llu/%llu Present=%.1fHz WaitForPresent=%.1fHz completed=%.1fHz early-wait=%llu VSync[none=%llu wait=%llu nowait=%llu] max-Present=%.2fms max-WaitForPresent=%.2fms max-Present-gap=%.2fms gaps>20.8ms=%llu gaps>41.7ms=%llu pending-overwrite=%llu VSync-overshoot[max=%.2fms >1ms=%llu >5ms=%llu] missed-ticks=%llu keyed-mutex-fail=%llu refresh=%.1fHz",
                          m_redirect->IsActive() ? "yes" : "no",
                          static_cast<unsigned long long>(m_redirect->ComponentQueryCount()),
                          static_cast<unsigned long long>(m_redirect->ComponentMatchCount()),
                          timing.presentCalls / intervalSeconds, timing.waitCalls / intervalSeconds,
                          timing.completedFrames / intervalSeconds,
                          static_cast<unsigned long long>(timing.waitCallsWithoutPresent),
                          static_cast<unsigned long long>(timing.vsyncNoneCalls),
                          static_cast<unsigned long long>(timing.vsyncWaitRenderCalls),
                          static_cast<unsigned long long>(timing.vsyncNoWaitRenderCalls),
                          timing.maxPresentNs / 1.0e6, timing.maxWaitNs / 1.0e6,
                          timing.maxPresentIntervalNs / 1.0e6,
                          static_cast<unsigned long long>(timing.presentGapsOverOneAndHalfFrames),
                          static_cast<unsigned long long>(timing.presentGapsOverThreeFrames),
                          static_cast<unsigned long long>(timing.pendingPresentOverwrites),
                          timing.maxWaitOvershootNs / 1.0e6,
                          static_cast<unsigned long long>(timing.waitOvershootsOverOneMs),
                          static_cast<unsigned long long>(timing.waitOvershootsOverFiveMs),
                          static_cast<unsigned long long>(timing.missedVirtualVsyncs),
                          static_cast<unsigned long long>(timing.keyedMutexAcquireFailures),
                          gDisplayRefreshRateHz);
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log(line);
        }
        uint8_t flags = kStatusDriverLoaded;
        if (m_hmd && m_hmd->IsActivated()) flags |= kStatusHmdActive;
        if (m_hmd && m_hmd->HasValidPose()) flags |= kStatusPoseValid;
        if (m_redirect && m_redirect->IsActive()) flags |= kStatusDisplayActive;
        const uint64_t presented = m_redirect ? m_redirect->PresentCounter() : 0;
        if (presented > 0) flags |= kStatusPresentedFrame;
        m_statusReporter.Send(flags, presented);
    }
    bool ShouldBlockStandbyMode() override { return false; }
    void EnterStandby() override {}
    void LeaveStandby() override {}
private:
    PhoneVRHmd* m_hmd = nullptr;
    PhoneVRDisplayRedirect* m_redirect = nullptr;
    PhoneVRDaydreamController* m_leftController = nullptr;
    PhoneVRDaydreamController* m_rightController = nullptr;
    SteamVRStatusReporter m_statusReporter;
    DaydreamControllerReceiver m_controllerReceiver;
    uint64_t m_lastDisplayDiagnosticsNs = 0;
};

PhoneVRProvider g_provider;
} // namespace phonevr

extern "C" __declspec(dllexport) void* HmdDriverFactory(const char* interfaceName, int* returnCode) {
    if (interfaceName && std::strcmp(interfaceName, vr::IServerTrackedDeviceProvider_Version) == 0) {
        if (returnCode) *returnCode = vr::VRInitError_None;
        return &phonevr::g_provider;
    }
    if (returnCode) *returnCode = vr::VRInitError_Init_InterfaceNotFound;
    return nullptr;
}
