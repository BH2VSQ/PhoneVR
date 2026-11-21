#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <winsock2.h>
#include <ws2tcpip.h>
#include <windows.h>
#include <openvr_driver.h>

#include <array>
#include <atomic>
#include <chrono>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <mutex>
#include <thread>

namespace phonevr {
constexpr uint16_t kPoseBridgePort = 48103;
constexpr uint32_t kPosePacketSize = 36;
constexpr uint32_t kEyeWidth = 1200;
constexpr uint32_t kEyeHeight = 1080;

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
        properties->SetFloatProperty(container, vr::Prop_DisplayFrequency_Float, 72.0f);
        if (!m_receiver.Start() && vr::VRDriverLog())
            vr::VRDriverLog()->Log("PhoneVR: failed to bind local pose bridge on UDP 48103");
        return vr::VRInitError_None;
    }
    void Deactivate() override {
        m_receiver.Stop();
        m_objectId = vr::k_unTrackedDeviceIndexInvalid;
        m_hasPublished = false;
        m_lastTrackingValid = false;
        m_lastConnected = false;
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

    void RunFrame() {
        if (m_objectId == vr::k_unTrackedDeviceIndexInvalid) return;
        const Quaternion sample = m_receiver.Snapshot();
        if (!sample.valid || sample.receivedAtNs == 0) return;
        const uint64_t now = MonotonicNowNs();
        const uint64_t ageNs = now >= sample.receivedAtNs ? now - sample.receivedAtNs : 0;
        const bool trackingValid = ageNs <= 250000000ULL;
        const bool deviceConnected = ageNs <= 2000000000ULL;
        if (m_hasPublished && sample.sequence == m_lastSequence &&
            trackingValid == m_lastTrackingValid && deviceConnected == m_lastConnected) return;

        vr::DriverPose_t pose{};
        pose.poseTimeOffset = 0.0;
        pose.qWorldFromDriverRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qDriverFromHeadRotation = {1.0, 0.0, 0.0, 0.0};
        pose.qRotation = {sample.w, sample.x, sample.y, sample.z};
        pose.result = trackingValid ? vr::TrackingResult_Running_OK : vr::TrackingResult_Uninitialized;
        pose.deviceIsConnected = deviceConnected;
        pose.poseIsValid = trackingValid;
        pose.willDriftInYaw = true;
        pose.shouldApplyHeadModel = false;
        m_lastPose = pose;
        m_lastSequence = sample.sequence;
        m_lastTrackingValid = trackingValid;
        m_lastConnected = deviceConnected;
        m_hasPublished = true;
        if (auto* host = vr::VRServerDriverHost())
            host->TrackedDevicePoseUpdated(m_objectId, pose, sizeof(pose));
    }
private:
    uint32_t m_objectId = vr::k_unTrackedDeviceIndexInvalid;
    PoseReceiver m_receiver;
    HmdDisplay m_display;
    vr::DriverPose_t m_lastPose{};
    uint32_t m_lastSequence = 0;
    bool m_lastTrackingValid = false;
    bool m_lastConnected = false;
    bool m_hasPublished = false;
};

class VirtualDisplayComponent final : public vr::IVRVirtualDisplay {
public:
    void Present(const vr::PresentInfo_t*, uint32_t) override {
        // The 0.4.0 foundation deliberately keeps this callback non-blocking.
        // D3D11 texture acquisition + encoder queueing is the next implementation step.
        m_lastPresentNs.store(NowNs(), std::memory_order_release);
        m_frameCounter.fetch_add(1, std::memory_order_relaxed);
    }
    void WaitForPresent() override {}
    bool GetTimeSinceLastVsync(float* seconds, uint64_t* frameCounter) override {
        const uint64_t last = m_lastPresentNs.load(std::memory_order_acquire);
        if (!last || !seconds || !frameCounter) return false;
        *seconds = static_cast<float>(static_cast<double>(NowNs() - last) / 1.0e9);
        *frameCounter = m_frameCounter.load(std::memory_order_relaxed);
        return true;
    }
private:
    static uint64_t NowNs() {
        return static_cast<uint64_t>(std::chrono::duration_cast<std::chrono::nanoseconds>(
            std::chrono::steady_clock::now().time_since_epoch()).count());
    }
    std::atomic<uint64_t> m_lastPresentNs{0};
    std::atomic<uint64_t> m_frameCounter{0};
};

class PhoneVRDisplayRedirect final : public vr::ITrackedDeviceServerDriver {
public:
    vr::EVRInitError Activate(uint32_t objectId) override {
        const auto container = vr::VRProperties()->TrackedDeviceToPropertyContainer(objectId);
        auto* properties = vr::VRProperties();
        properties->SetStringProperty(container, vr::Prop_TrackingSystemName_String, "phonevr");
        properties->SetStringProperty(container, vr::Prop_ModelNumber_String, "PhoneVR Wireless Display Redirect");
        properties->SetStringProperty(container, vr::Prop_SerialNumber_String, "PHONEVR-DISPLAY-001");
        properties->SetStringProperty(container, vr::Prop_RegisteredDeviceType_String, "phonevr/phonevr_display_redirect");
        properties->SetBoolProperty(container, vr::Prop_HasVirtualDisplayComponent_Bool, true);
        return vr::VRInitError_None;
    }
    void Deactivate() override {}
    void EnterStandby() override {}
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
};

class PhoneVRProvider final : public vr::IServerTrackedDeviceProvider {
public:
    vr::EVRInitError Init(vr::IVRDriverContext* context) override {
        VR_INIT_SERVER_DRIVER_CONTEXT(context);
        m_hmd = new PhoneVRHmd();
        m_redirect = new PhoneVRDisplayRedirect();
        auto* host = vr::VRServerDriverHost();
        if (!host || !host->TrackedDeviceAdded("PHONEVR-HMD-001", vr::TrackedDeviceClass_HMD, m_hmd)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (!host->TrackedDeviceAdded("PHONEVR-DISPLAY-001", vr::TrackedDeviceClass_DisplayRedirect, m_redirect)) {
            Cleanup(); return vr::VRInitError_Driver_Unknown;
        }
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR 0.4.1 SteamVR driver initialized");
        return vr::VRInitError_None;
    }
    void Cleanup() override {
        delete m_hmd; m_hmd = nullptr;
        delete m_redirect; m_redirect = nullptr;
        VR_CLEANUP_SERVER_DRIVER_CONTEXT();
    }
    const char* const* GetInterfaceVersions() override { return vr::k_InterfaceVersions; }
    void RunFrame() override { if (m_hmd) m_hmd->RunFrame(); }
    bool ShouldBlockStandbyMode() override { return false; }
    void EnterStandby() override {}
    void LeaveStandby() override {}
private:
    PhoneVRHmd* m_hmd = nullptr;
    PhoneVRDisplayRedirect* m_redirect = nullptr;
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
