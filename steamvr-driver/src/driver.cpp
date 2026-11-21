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
#include <dxgi1_2.h>
#include <openvr_driver.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <cstdio>
#include <mutex>
#include <thread>

namespace phonevr {
constexpr uint16_t kPoseBridgePort = 48103;
constexpr uint32_t kPosePacketSize = 36;
constexpr uint32_t kEyeWidth = 1200;
constexpr uint32_t kEyeHeight = 1080;
constexpr double kDisplayRefreshRateHz = 72.0;
constexpr double kDisplayFrameIntervalSeconds = 1.0 / kDisplayRefreshRateHz;
// Tracking can briefly pause under Android scheduling or Wi-Fi jitter. A stale
// pose becomes untracked, but it must never disconnect the HMD device itself.
constexpr uint64_t kTrackingPoseTimeoutNs = 1000000000ULL;

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

struct Quaternion {
    double x = 0.0, y = 0.0, z = 0.0, w = 1.0;
    uint32_t sequence = 0;
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

    ~PoseReceiver() { Stop(); }

private:
    void Run() {
        // Capture the socket handle once. Stop() closes this underlying socket
        // to wake recv(), but does not race a write to the worker's handle copy.
        const SOCKET socket = m_socket;
        std::array<uint8_t, 1500> buffer{};
        while (m_running.load()) {
            const int received = recv(socket, reinterpret_cast<char*>(buffer.data()),
                                      static_cast<int>(buffer.size()), 0);
            if (received == SOCKET_ERROR) {
                if (!m_running.load()) break;
                continue;
            }
            if (received != static_cast<int>(kPosePacketSize) ||
                std::memcmp(buffer.data(), "PVSP", 4) != 0 || buffer[4] != 1) continue;
            double x = ReadBEFloat(buffer.data() + 20);
            double y = ReadBEFloat(buffer.data() + 24);
            double z = ReadBEFloat(buffer.data() + 28);
            double w = ReadBEFloat(buffer.data() + 32);
            if (!std::isfinite(x) || !std::isfinite(y) || !std::isfinite(z) || !std::isfinite(w)) continue;
            const double norm = std::sqrt(x*x + y*y + z*z + w*w);
            if (norm < 0.5 || norm > 1.5) continue;
            Quaternion next{};
            next.x = x / norm; next.y = y / norm; next.z = z / norm; next.w = w / norm;
            next.sequence = ReadBE32(buffer.data() + 8);
            next.receivedAtNs = MonotonicNowNs();
            next.valid = true;
            std::lock_guard<std::mutex> lock(m_mutex);
            m_pose = next;
        }
    }

    std::atomic<bool> m_running{false};
    SOCKET m_socket = INVALID_SOCKET;
    std::thread m_thread;
    mutable std::mutex m_mutex;
    Quaternion m_pose{};
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
        *x = 0; *y = 0; *width = kEyeWidth * 2; *height = kEyeHeight;
    }
    bool IsDisplayOnDesktop() override { return false; }
    bool IsDisplayRealDisplay() override { return false; }
    void GetRecommendedRenderTargetSize(uint32_t* width, uint32_t* height) override {
        *width = kEyeWidth * 2; *height = kEyeHeight;
    }
    void GetEyeOutputViewport(vr::EVREye eye, uint32_t* x, uint32_t* y,
                              uint32_t* width, uint32_t* height) override {
        *x = eye == vr::Eye_Left ? 0 : kEyeWidth;
        *y = 0; *width = kEyeWidth; *height = kEyeHeight;
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
    vr::EVRInitError Activate(uint32_t objectId) override {
        m_objectId = objectId;
        const auto container = vr::VRProperties()->TrackedDeviceToPropertyContainer(objectId);
        auto* properties = vr::VRProperties();
        properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "PhoneVR Wireless HMD");
        properties->SetStringProperty(container, vr::Prop_SerialNumber_String, "PHONEVR-HMD-001");
        properties->SetStringProperty(container, vr::Prop_ManufacturerName_String, "BH2VSQ");
        properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_hmd");
        properties->SetBoolProperty(container, vr::Prop_HasDisplayComponent_Bool, true);
        properties->SetBoolProperty(container, vr::Prop_DeviceIsWireless_Bool, true);
        properties->SetFloatProperty(container, vr::Prop_UserIpdMeters_Float, 0.064f);
        properties->SetFloatProperty(container, vr::Prop_DisplayFrequency_Float,
                                     static_cast<float>(kDisplayRefreshRateHz));

        // PhoneVR supplies orientation only. Publish an explicit, stable
        // driver-owned Chaperone universe so SteamVR does not depend on a
        // user-created room-scale calibration for this 3DoF device. Empty
        // collision bounds are intentional: PhoneVR has no positional
        // tracking and must not advertise a fictitious room-scale boundary.
        const auto universeResult = properties->SetUint64Property(
            container, vr::Prop_CurrentUniverseId_Uint64,
            static_cast<uint64_t>(kPhoneVRUniverseId));
        const auto chaperoneResult = properties->SetStringProperty(
            container, vr::Prop_DriverProvidedChaperoneJson_String,
            kDriverProvidedChaperoneJson);
        if (universeResult != vr::TrackedProp_Success ||
            chaperoneResult != vr::TrackedProp_Success) {
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

        if (!m_receiver.Start() && vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: failed to bind local pose bridge on UDP 48103; HMD remains connected but untracked");

        // Publish a connected-but-untracked initial pose immediately. OpenVR's
        // deviceIsConnected describes device presence, not packet freshness.
        // This prevents SteamVR from removing and rediscovering the HMD while
        // the phone app is starting or briefly pauses sensor delivery.
        m_lastPose = MakeUntrackedPose();
        m_lastTrackingValid = false;
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(m_objectId, m_lastPose, sizeof(m_lastPose));
        if (vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: HMD activation published; tracking awaits fresh IMU samples");
        return vr::VRInitError_None;
    }
    void Deactivate() override {
        m_receiver.Stop();
        m_objectId = vr::k_unTrackedDeviceIndexInvalid;
        m_lastTrackingValid = false;
    }
    void EnterStandby() override {}
    void* GetComponent(const char* nameAndVersion) override {
        if (nameAndVersion && std::strcmp(nameAndVersion, vr::IVRDisplayComponent_Version) == 0) return &m_display;
        return nullptr;
    }
    void DebugRequest(const char*, char* response, uint32_t size) override {
        if (response && size) response[0] = '\0';
    }
    // OpenVR marks ITrackedDeviceServerDriver::GetPose as unused. Pose updates
    // must be pushed to IVRServerDriverHost from RunFrame instead.
    vr::DriverPose_t GetPose() override { return m_lastPose; }

    bool IsActivated() const { return m_objectId != vr::k_unTrackedDeviceIndexInvalid; }
    bool HasValidPose() const { return m_lastTrackingValid; }

    static vr::DriverPose_t MakeUntrackedPose() {
        vr::DriverPose_t pose{};
        pose.poseTimeOffset = 0.0;
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {1.0, 0.0, 0.0, 0.0};
        pose.result = vr::TrackingResult_Uninitialized;
        pose.deviceIsConnected = true;
        pose.poseIsValid = false;
        pose.willDriftInYaw = true;
        pose.shouldApplyHeadModel = false;
        return pose;
    }

    void RunFrame() {
        if (m_objectId == vr::k_unTrackedDeviceIndexInvalid) return;
        const Quaternion sample = m_receiver.Snapshot();
        if (!sample.valid || sample.receivedAtNs == 0) {
            if (m_lastTrackingValid && vr::VRDriverLog())
                vr::VRDriverLog()->Log("PhoneVR: IMU sample unavailable; HMD remains connected and pose is marked untracked");
            m_lastTrackingValid = false;
            m_lastPose.deviceIsConnected = true;
            m_lastPose.poseIsValid = false;
            m_lastPose.result = vr::TrackingResult_Uninitialized;
            if (auto* host = vr::VRServerDriverHost())
                host->TrackedDevicePoseUpdated(m_objectId, m_lastPose, sizeof(m_lastPose));
            return;
        }

        const uint64_t now = MonotonicNowNs();
        const uint64_t ageNs = now >= sample.receivedAtNs ? now - sample.receivedAtNs : 0;
        const bool trackingValid = ageNs <= kTrackingPoseTimeoutNs;
        const bool trackingStateChanged = trackingValid != m_lastTrackingValid;
        if (trackingStateChanged && vr::VRDriverLog()) {
            char message[160]{};
            std::snprintf(message, sizeof(message),
                          "PhoneVR: IMU tracking %s (sample age %llu ms); HMD device remains connected",
                          trackingValid ? "resumed" : "stale",
                          static_cast<unsigned long long>(ageNs / 1000000ULL));
            vr::VRDriverLog()->Log(message);
        }

        vr::DriverPose_t pose{};
        pose.poseTimeOffset = 0.0;
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {sample.w, sample.x, sample.y, sample.z};
        pose.result = trackingValid ? vr::TrackingResult_Running_OK : vr::TrackingResult_Uninitialized;
        // The HMD has been activated by SteamVR and remains physically present.
        // Sensor timeout only affects pose validity; never set this false on a
        // transient IMU/network gap or SteamVR will remove and rediscover it.
        pose.deviceIsConnected = true;
        pose.poseIsValid = trackingValid;
        pose.willDriftInYaw = true;
        pose.shouldApplyHeadModel = false;
        m_lastPose = pose;
        m_lastTrackingValid = trackingValid;

        // OpenVR expects drivers to submit poses at least once per frame. Do not
        // suppress an update merely because the latest UDP sequence is unchanged:
        // the runtime only extrapolates briefly and otherwise invalidates old poses.
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(m_objectId, pose, sizeof(pose));
    }

private:
    uint32_t m_objectId = vr::k_unTrackedDeviceIndexInvalid;
    PoseReceiver m_receiver;
    HmdDisplay m_display;
    vr::DriverPose_t m_lastPose{};
    bool m_lastTrackingValid = false;
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
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: D3D11 virtual display capture initialized");
        return true;
    }

    void Shutdown() {
        std::lock_guard<std::mutex> lock(m_mutex);
        ShutdownUnlocked();
    }

    void Present(const vr::PresentInfo_t* info, uint32_t infoSize) override {
        std::lock_guard<std::mutex> lock(m_mutex);
        if (!info || infoSize < sizeof(vr::PresentInfo_t) || !info->backbufferTextureHandle) {
            return;
        }
        if (!m_device || !m_context) return;

        HANDLE sharedHandle = reinterpret_cast<HANDLE>(info->backbufferTextureHandle);
        if (sharedHandle != m_sharedHandle) {
            ReleaseTexture(m_sharedTexture);
            ReleaseTexture(m_flushTexture);
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
            if (!EnsureSyncTexture()) return;
        }
        if (!m_sharedTexture || !m_flushTexture) return;

        IDXGIKeyedMutex* keyedMutex = nullptr;
        const HRESULT mutexQuery = m_sharedTexture->QueryInterface(
            __uuidof(IDXGIKeyedMutex), reinterpret_cast<void**>(&keyedMutex));
        bool mutexAcquired = false;
        if (SUCCEEDED(mutexQuery) && keyedMutex) {
            // Match Valve's virtual_display sample. A 2 ms timeout was too short
            // for cross-process compositor scheduling on some GPUs and caused
            // repeated frame drops/black flashes. Do not wait indefinitely.
            const HRESULT acquire = keyedMutex->AcquireSync(0, 10);
            if (acquire != S_OK) {
                ++m_mutexAcquireFailures;
                LogMutexAcquireFailure(acquire);
                keyedMutex->Release();
                return; // Drop a contended frame instead of hanging the compositor.
            }
            mutexAcquired = true;
        } else if (FAILED(mutexQuery)) {
            LogD3DFailureOnce("QueryInterface(IDXGIKeyedMutex)", mutexQuery, m_loggedMutexQueryFailure);
        }

        // P0 only needs to confirm that the compositor's final backbuffer is
        // available and that the GPU has completed the submitted frame. There
        // is no encoder/consumer in this version, so copying the entire 2400x1080
        // surface into an unused staging texture only burns GPU bandwidth.
        // Keep the small readback fence used by Valve's virtual-display sample.
        const D3D11_BOX syncBox{0, 0, 0, 1, 1, 1};
        m_context->CopySubresourceRegion(m_flushTexture, 0, 0, 0, 0,
                                         m_sharedTexture, 0, &syncBox);
        m_context->Flush();

        if (mutexAcquired) keyedMutex->ReleaseSync(0);
        if (keyedMutex) keyedMutex->Release();

        m_pendingReadback = true;
        m_lastFrameId = info->nFrameId;
        m_lastVsyncTimeSeconds = (std::isfinite(info->flVSyncTimeInSeconds) && info->flVSyncTimeInSeconds > 0.0)
                                     ? info->flVSyncTimeInSeconds : NowSeconds();
    }

    void WaitForPresent() override {
        std::lock_guard<std::mutex> lock(m_mutex);
        if (!m_pendingReadback || !m_context || !m_flushTexture) return;
        D3D11_MAPPED_SUBRESOURCE mapped{};
        const HRESULT hr = m_context->Map(m_flushTexture, 0, D3D11_MAP_READ, 0, &mapped);
        if (SUCCEEDED(hr)) {
            // The small staging readback was queued after the shared-backbuffer
            // fence copy. Mapping it waits for the preceding GPU work to finish.
            m_context->Unmap(m_flushTexture, 0);
            m_pendingReadback = false;
            m_frameCounter = std::max(m_frameCounter + 1, m_lastFrameId);
            m_vsyncOriginTimeSeconds = m_lastVsyncTimeSeconds;
            m_vsyncOriginFrameCounter = m_frameCounter;
            const uint64_t completed = m_completedFrameCounter.fetch_add(1, std::memory_order_relaxed) + 1;
            if (completed == 1 && vr::VRDriverLog())
                vr::VRDriverLog()->Log("PhoneVR: first virtual display frame synchronized successfully");
        } else {
            m_pendingReadback = false;
            ++m_mapFailures;
            LogMapFailure(hr);
        }
    }

    bool GetTimeSinceLastVsync(float* seconds, uint64_t* frameCounter) override {
        if (!seconds || !frameCounter) return false;
        std::lock_guard<std::mutex> lock(m_mutex);
        if (m_frameCounter == 0 || m_vsyncOriginTimeSeconds <= 0.0) return false;
        const double elapsed = std::max(0.0, NowSeconds() - m_vsyncOriginTimeSeconds);
        const uint64_t elapsedVsyncs = static_cast<uint64_t>(elapsed / kDisplayFrameIntervalSeconds);
        const double lastVsyncOffset = elapsed -
            static_cast<double>(elapsedVsyncs) * kDisplayFrameIntervalSeconds;
        const uint64_t virtualFrame = m_vsyncOriginFrameCounter + elapsedVsyncs;
        m_frameCounter = std::max(m_frameCounter, virtualFrame);
        *seconds = static_cast<float>(lastVsyncOffset);
        *frameCounter = m_frameCounter;
        return true;
    }

    uint64_t PresentCounter() const { return m_completedFrameCounter.load(std::memory_order_relaxed); }

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
    bool EnsureSyncTexture() {
        if (!m_sharedTexture || !m_device) return false;
        D3D11_TEXTURE2D_DESC source{};
        m_sharedTexture->GetDesc(&source);
        if (source.Width == 0 || source.Height == 0 || source.SampleDesc.Count != 1) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: unsupported compositor backbuffer dimensions or sample count");
            return false;
        }
        // D3D11 staging texture used solely as a GPU completion fence. The
        // actual source rectangle copied into it is 1x1, as in Valve's sample.
        constexpr UINT syncWidth = 32;
        constexpr UINT syncHeight = 32;
        if (m_flushTexture) {
            D3D11_TEXTURE2D_DESC current{};
            m_flushTexture->GetDesc(&current);
            if (current.Width != syncWidth || current.Height != syncHeight || current.Format != source.Format)
                ReleaseTexture(m_flushTexture);
        }
        if (!m_flushTexture) {
            D3D11_TEXTURE2D_DESC flush{};
            flush.Width = syncWidth;
            flush.Height = syncHeight;
            flush.MipLevels = 1;
            flush.ArraySize = 1;
            flush.Format = source.Format;
            flush.SampleDesc.Count = 1;
            flush.Usage = D3D11_USAGE_STAGING;
            flush.BindFlags = 0;
            flush.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
            flush.MiscFlags = 0;
            const HRESULT hr = m_device->CreateTexture2D(&flush, nullptr, &m_flushTexture);
            if (FAILED(hr) || !m_flushTexture) {
                LogD3DFailureOnce("CreateTexture2D(sync staging)", hr, m_loggedSyncTextureFailure);
                return false;
            }
            m_loggedSyncTextureFailure = false;
        }
        return m_flushTexture != nullptr;
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

    void LogMapFailure(HRESULT hr) {
        const uint64_t now = MonotonicNowNs();
        if (now - m_lastMapFailureLogNs < 2000000000ULL) return;
        m_lastMapFailureLogNs = now;
        HRESULT removedReason = m_device ? m_device->GetDeviceRemovedReason() : E_FAIL;
        char line[224]{};
        std::snprintf(line, sizeof(line),
                      "PhoneVR: GPU completion Map failed (HRESULT 0x%08X, device removed reason 0x%08X, failures %llu)",
                      static_cast<unsigned>(hr), static_cast<unsigned>(removedReason),
                      static_cast<unsigned long long>(m_mapFailures));
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log(line);
    }
    void ShutdownUnlocked() {
        m_pendingReadback = false;
        m_sharedHandle = nullptr;
        ReleaseTexture(m_sharedTexture);
        ReleaseTexture(m_flushTexture);
        if (m_context) { m_context->ClearState(); m_context->Flush(); m_context->Release(); m_context = nullptr; }
        if (m_device) { m_device->Release(); m_device = nullptr; }
        m_adapterLuid = 0;
        m_lastVsyncTimeSeconds = 0.0;
        m_vsyncOriginTimeSeconds = 0.0;
        m_lastFrameId = 0;
        m_frameCounter = 0;
        m_vsyncOriginFrameCounter = 0;
        m_completedFrameCounter.store(0, std::memory_order_relaxed);
    }

    mutable std::mutex m_mutex;
    ID3D11Device* m_device = nullptr;
    ID3D11DeviceContext* m_context = nullptr;
    ID3D11Texture2D* m_sharedTexture = nullptr;
    ID3D11Texture2D* m_flushTexture = nullptr;
    HANDLE m_sharedHandle = nullptr;
    uint64_t m_adapterLuid = 0;
    uint64_t m_lastFrameId = 0;
    uint64_t m_frameCounter = 0;
    double m_lastVsyncTimeSeconds = 0.0;
    double m_vsyncOriginTimeSeconds = 0.0;
    uint64_t m_vsyncOriginFrameCounter = 0;
    bool m_pendingReadback = false;
    bool m_loggedOpenFailure = false;
    bool m_loggedSyncTextureFailure = false;
    bool m_loggedMutexQueryFailure = false;
    uint64_t m_mutexAcquireFailures = 0;
    uint64_t m_mapFailures = 0;
    uint64_t m_lastMutexFailureLogNs = 0;
    uint64_t m_lastMapFailureLogNs = 0;
    std::atomic<uint64_t> m_completedFrameCounter{0};
};

class PhoneVRDisplayRedirect final : public vr::ITrackedDeviceServerDriver {
public:
    vr::EVRInitError Activate(uint32_t objectId) override {
        uint64_t adapterLuid = 0;
        if (!m_display.Initialize(&adapterLuid)) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: virtual display capture initialization failed; display redirect is inactive");
            return vr::VRInitError_Driver_Unknown;
        }
        m_active = true;
        const auto container = vr::VRProperties()->TrackedDeviceToPropertyContainer(objectId);
        auto* properties = vr::VRProperties();
        properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "PhoneVR Wireless Display Redirect");
        properties->SetStringProperty(container, vr::Prop_SerialNumber_String, "PHONEVR-DISPLAY-001");
        properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_display_redirect");
        properties->SetBoolProperty(container, vr::Prop_HasVirtualDisplayComponent_Bool, true);
        properties->SetUint64Property(container, vr::Prop_GraphicsAdapterLuid_Uint64, adapterLuid);
        return vr::VRInitError_None;
    }
    void Deactivate() override { m_active = false; m_display.Shutdown(); }
    void EnterStandby() override {}
    bool IsActive() const { return m_active; }
    uint64_t PresentCounter() const { return m_display.PresentCounter(); }
    void* GetComponent(const char* nameAndVersion) override {
        if (nameAndVersion && std::strcmp(nameAndVersion, vr::IVRVirtualDisplay_Version) == 0) return &m_display;
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
    bool m_active = false;
};

class PhoneVRProvider final : public vr::IServerTrackedDeviceProvider {
public:
    vr::EVRInitError Init(vr::IVRDriverContext* context) override {
        VR_INIT_SERVER_DRIVER_CONTEXT(context);
        if (!m_statusReporter.Start() && vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: failed to start loopback status reporter on UDP 48105");
        m_hmd = new PhoneVRHmd();
        m_redirect = new PhoneVRDisplayRedirect();
        auto* host = vr::VRServerDriverHost();
        if (!host || !host->TrackedDeviceAdded("PHONEVR-HMD-001", vr::TrackedDeviceClass_HMD, m_hmd)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (!host->TrackedDeviceAdded("PHONEVR-DISPLAY-001", vr::TrackedDeviceClass_DisplayRedirect, m_redirect)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR 0.4.12 SteamVR driver initialized");
        return vr::VRInitError_None;
    }
    void Cleanup() override {
        m_statusReporter.Stop();
        delete m_hmd; m_hmd = nullptr;
        delete m_redirect; m_redirect = nullptr;
        VR_CLEANUP_SERVER_DRIVER_CONTEXT();
    }
    const char* const* GetInterfaceVersions() override { return vr::k_InterfaceVersions; }
    void RunFrame() override {
        if (m_hmd) m_hmd->RunFrame();
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
    SteamVRStatusReporter m_statusReporter;
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
