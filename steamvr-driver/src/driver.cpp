#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
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

    bool IsActivated() const { return m_objectId != vr::k_unTrackedDeviceIndexInvalid; }
    bool HasValidPose() const { return m_lastTrackingValid; }

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
    VirtualDisplayComponent() = default;
    ~VirtualDisplayComponent() override { Shutdown(); }

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
            if (SUCCEEDED(candidate->GetDesc1(&desc)) && !(desc.Flags & DXGI_ADAPTER_FLAG_SOFTWARE)) {
                m_adapterLuid = (static_cast<uint64_t>(static_cast<uint32_t>(desc.AdapterLuid.HighPart)) << 32) |
                                static_cast<uint32_t>(desc.AdapterLuid.LowPart);
                adapter = candidate;
                break;
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
        if (!info || infoSize < sizeof(vr::PresentInfo_t) || !info->backbufferTextureHandle) return;
        std::lock_guard<std::mutex> lock(m_mutex);
        if (!m_device || !m_context) return;

        HANDLE sharedHandle = reinterpret_cast<HANDLE>(info->backbufferTextureHandle);
        if (sharedHandle != m_sharedHandle) {
            ReleaseTexture(m_sharedTexture);
            ReleaseTexture(m_stagingTexture);
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
            if (!EnsureStagingTexture()) return;
        }
        if (!m_sharedTexture) return;
        if ((!m_stagingTexture || !m_flushTexture) && !EnsureStagingTexture()) return;

        IDXGIKeyedMutex* keyedMutex = nullptr;
        const HRESULT mutexQuery = m_sharedTexture->QueryInterface(
            __uuidof(IDXGIKeyedMutex), reinterpret_cast<void**>(&keyedMutex));
        bool mutexAcquired = false;
        if (SUCCEEDED(mutexQuery) && keyedMutex) {
            const HRESULT acquire = keyedMutex->AcquireSync(0, 2);
            if (acquire != S_OK) {
                keyedMutex->Release();
                return; // Drop this frame rather than blocking the compositor.
            }
            mutexAcquired = true;
        }

        // Queue the full frame copy for the future encoder, then queue a tiny
        // readback copy behind it. Mapping only the tiny texture in
        // WaitForPresent fences all preceding immediate-context work without
        // forcing a CPU readback of the full 1200x1080-per-eye surface.
        m_context->CopyResource(m_stagingTexture, m_sharedTexture);
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
        if (!m_pendingReadback || !m_context || !m_stagingTexture || !m_flushTexture) return;
        D3D11_MAPPED_SUBRESOURCE mapped{};
        const HRESULT hr = m_context->Map(m_flushTexture, 0, D3D11_MAP_READ, 0, &mapped);
        if (SUCCEEDED(hr)) {
            // The tiny readback copy was queued after the full staging copy on
            // the same immediate context. Mapping it waits for both GPU copies
            // while avoiding a full-frame CPU/GPU synchronization.
            m_context->Unmap(m_flushTexture, 0);
            m_pendingReadback = false;
            m_frameCounter = std::max(m_frameCounter + 1, m_lastFrameId);
            m_vsyncOriginTimeSeconds = m_lastVsyncTimeSeconds;
            m_vsyncOriginFrameCounter = m_frameCounter;
            m_completedFrameCounter.fetch_add(1, std::memory_order_relaxed);
        } else {
            LogD3DFailureOnce("Map(sync texture)", hr, m_loggedMapFailure);
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
    bool EnsureStagingTexture() {
        if (!m_sharedTexture || !m_device) return false;
        D3D11_TEXTURE2D_DESC source{};
        m_sharedTexture->GetDesc(&source);
        if (source.Width == 0 || source.Height == 0 || source.ArraySize != 1 ||
            source.MipLevels != 1 || source.SampleDesc.Count != 1) {
            if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR: unsupported virtual display texture layout");
            return false;
        }
        if (m_stagingTexture) {
            D3D11_TEXTURE2D_DESC current{};
            m_stagingTexture->GetDesc(&current);
            const bool stagingMatches = current.Width == source.Width && current.Height == source.Height &&
                                        current.Format == source.Format;
            if (!stagingMatches) ReleaseTexture(m_stagingTexture);
        }
        if (!m_stagingTexture) {
            D3D11_TEXTURE2D_DESC staging = source;
            staging.Usage = D3D11_USAGE_STAGING;
            staging.BindFlags = 0;
            staging.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
            staging.MiscFlags = 0;
            const HRESULT hr = m_device->CreateTexture2D(&staging, nullptr, &m_stagingTexture);
            if (FAILED(hr) || !m_stagingTexture) {
                LogD3DFailureOnce("CreateTexture2D(staging)", hr, m_loggedStagingFailure);
                return false;
            }
            m_loggedStagingFailure = false;
        }
        const UINT syncWidth = std::min<UINT>(32, source.Width);
        const UINT syncHeight = std::min<UINT>(32, source.Height);
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
            flush.CPUAccessFlags = D3D11_CPU_ACCESS_READ;
            const HRESULT hr = m_device->CreateTexture2D(&flush, nullptr, &m_flushTexture);
            if (FAILED(hr) || !m_flushTexture) {
                LogD3DFailureOnce("CreateTexture2D(sync staging)", hr, m_loggedStagingFailure);
                return false;
            }
        }
        return m_stagingTexture != nullptr && m_flushTexture != nullptr;
    }
    void ShutdownUnlocked() {
        m_pendingReadback = false;
        m_sharedHandle = nullptr;
        ReleaseTexture(m_sharedTexture);
        ReleaseTexture(m_stagingTexture);
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
    ID3D11Texture2D* m_stagingTexture = nullptr;
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
    bool m_loggedStagingFailure = false;
    bool m_loggedMapFailure = false;
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
        if (vr::VRDriverLog()) vr::VRDriverLog()->Log("PhoneVR 0.4.7 SteamVR driver initialized");
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
