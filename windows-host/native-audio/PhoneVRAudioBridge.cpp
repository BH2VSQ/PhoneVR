#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <audioclient.h>
#include <mmdeviceapi.h>
#include <functiondiscoverykeys_devpkey.h>
#include <propvarutil.h>
#include <wrl/client.h>
#include <ksmedia.h>
#include <avrt.h>

#include <algorithm>
#include <array>
#include <atomic>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <deque>
#include <mutex>
#include <string>
#include <thread>
#include <vector>
#include <chrono>
#include <climits>
#include <cwctype>

#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "uuid.lib")
#pragma comment(lib, "avrt.lib")
#pragma comment(lib, "propsys.lib")

using Microsoft::WRL::ComPtr;

namespace {
constexpr DWORD kSampleRate = 48000;
constexpr DWORD kFrameSamples = 960;
constexpr DWORD kStereoFrameBytes = kFrameSamples * 2 * sizeof(int16_t);
constexpr DWORD kMonoFrameBytes = kFrameSamples * sizeof(int16_t);
constexpr size_t kMicQueueFrames = 12; // 240 ms hard limit; discard oldest queued speech.
constexpr wchar_t kCableInputName[] = L"CABLE Input";
constexpr wchar_t kCableOutputName[] = L"CABLE Output";
constexpr wchar_t kVirtualAudioCableMarker[] = L"Virtual Audio Cable";

struct ComUninitializer {
    ~ComUninitializer() { CoUninitialize(); }
};

struct AvrtReverter {
    HANDLE Handle = nullptr;
    ~AvrtReverter() {
        if (Handle) AvRevertMmThreadCharacteristics(Handle);
    }
};

struct StereoSample { float Left; float Right; };
using MonoFrame = std::array<int16_t, kFrameSamples>;

bool WriteExact(HANDLE handle, const void* data, DWORD size) {
    const BYTE* cursor = static_cast<const BYTE*>(data);
    DWORD remaining = size;
    while (remaining) {
        DWORD written = 0;
        if (!WriteFile(handle, cursor, remaining, &written, nullptr) || written == 0) return false;
        cursor += written;
        remaining -= written;
    }
    return true;
}

bool ReadExact(HANDLE handle, void* data, DWORD size) {
    BYTE* cursor = static_cast<BYTE*>(data);
    DWORD remaining = size;
    while (remaining) {
        DWORD read = 0;
        if (!ReadFile(handle, cursor, remaining, &read, nullptr) || read == 0) return false;
        cursor += read;
        remaining -= read;
    }
    return true;
}

std::string Utf8(const wchar_t* text) {
    if (!text || !*text) return {};
    int length = WideCharToMultiByte(CP_UTF8, 0, text, -1, nullptr, 0, nullptr, nullptr);
    if (length <= 1) return {};
    std::string result(static_cast<size_t>(length), '\0');
    WideCharToMultiByte(CP_UTF8, 0, text, -1, result.data(), length, nullptr, nullptr);
    result.pop_back();
    return result;
}

std::string JsonEscape(const std::string& input) {
    std::string out;
    out.reserve(input.size() + 8);
    for (unsigned char c : input) {
        switch (c) {
        case '\\': out += "\\\\"; break;
        case '"': out += "\\\""; break;
        case '\n': out += "\\n"; break;
        case '\r': out += "\\r"; break;
        case '\t': out += "\\t"; break;
        default: if (c >= 0x20) out += static_cast<char>(c); break;
        }
    }
    return out;
}

std::wstring GetDeviceId(IMMDevice* device) {
    if (!device) return {};
    LPWSTR id = nullptr;
    if (FAILED(device->GetId(&id)) || !id) return {};
    std::wstring value(id);
    CoTaskMemFree(id);
    return value;
}

std::wstring GetFriendlyName(IMMDevice* device) {
    if (!device) return {};
    ComPtr<IPropertyStore> store;
    if (FAILED(device->OpenPropertyStore(STGM_READ, &store))) return {};
    PROPVARIANT value;
    PropVariantInit(&value);
    std::wstring name;
    if (SUCCEEDED(store->GetValue(PKEY_Device_FriendlyName, &value)) && value.vt == VT_LPWSTR && value.pwszVal) {
        name = value.pwszVal;
    }
    PropVariantClear(&value);
    return name;
}

bool FindCableEndpoint(IMMDeviceEnumerator* enumerator, EDataFlow flow,
                       const std::wstring& token, std::wstring& id, std::wstring& name) {
    ComPtr<IMMDeviceCollection> collection;
    if (FAILED(enumerator->EnumAudioEndpoints(flow, DEVICE_STATE_ACTIVE, &collection))) return false;
    UINT count = 0;
    if (FAILED(collection->GetCount(&count))) return false;
    for (UINT i = 0; i < count; ++i) {
        ComPtr<IMMDevice> device;
        if (FAILED(collection->Item(i, &device))) continue;
        const auto candidateName = GetFriendlyName(device.Get());
        if (candidateName.find(token) == std::wstring::npos) continue;
        const auto candidateId = GetDeviceId(device.Get());
        if (candidateId.empty()) continue;
        id = candidateId;
        name = candidateName;
        return true;
    }
    id.clear();
    name.clear();
    return false;
}

bool FindCableEndpointAnyState(IMMDeviceEnumerator* enumerator, EDataFlow flow,
                                const std::wstring& token, std::wstring& id,
                                std::wstring& name, DWORD& state) {
    ComPtr<IMMDeviceCollection> collection;
    if (FAILED(enumerator->EnumAudioEndpoints(flow, DEVICE_STATEMASK_ALL, &collection))) return false;
    UINT count = 0;
    if (FAILED(collection->GetCount(&count))) return false;
    for (UINT i = 0; i < count; ++i) {
        ComPtr<IMMDevice> device;
        if (FAILED(collection->Item(i, &device))) continue;
        const auto candidateName = GetFriendlyName(device.Get());
        if (candidateName.find(token) == std::wstring::npos) continue;
        const auto candidateId = GetDeviceId(device.Get());
        if (candidateId.empty()) continue;
        DWORD candidateState = 0;
        if (FAILED(device->GetState(&candidateState))) continue;
        id = candidateId;
        name = candidateName;
        state = candidateState;
        return true;
    }
    id.clear();
    name.clear();
    state = 0;
    return false;
}

bool FindVBCableEndpoints(IMMDeviceEnumerator* enumerator,
                          std::wstring& renderId, std::wstring& renderName,
                          std::wstring& captureId, std::wstring& captureName) {
    const bool inputFound = FindCableEndpoint(enumerator, eRender, kCableInputName, renderId, renderName);
    const bool outputFound = FindCableEndpoint(enumerator, eCapture, kCableOutputName, captureId, captureName);
    return inputFound && outputFound;
}

std::wstring NormalizeEndpointName(std::wstring value) {
    std::transform(value.begin(), value.end(), value.begin(), [](wchar_t ch) {
        return static_cast<wchar_t>(towlower(ch));
    });
    while (!value.empty() && iswspace(value.front())) value.erase(value.begin());
    while (!value.empty() && iswspace(value.back())) value.pop_back();
    return value;
}

bool FindVACEndpoints(IMMDeviceEnumerator* enumerator,
                      std::wstring& renderId, std::wstring& renderName,
                      std::wstring& captureId, std::wstring& captureName) {
    ComPtr<IMMDeviceCollection> renders;
    ComPtr<IMMDeviceCollection> captures;
    if (FAILED(enumerator->EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE, &renders)) ||
        FAILED(enumerator->EnumAudioEndpoints(eCapture, DEVICE_STATE_ACTIVE, &captures))) return false;

    UINT renderCount = 0, captureCount = 0;
    if (FAILED(renders->GetCount(&renderCount)) || FAILED(captures->GetCount(&captureCount))) return false;

    // Eugene Muzychenko VAC exposes a matching render and capture endpoint
    // with the same friendly name, e.g. "Line 1 (Virtual Audio Cable)".
    // Pair by the complete normalized name so Line 1 cannot be crossed with
    // Line 2 when multiple cables are installed.
    for (UINT r = 0; r < renderCount; ++r) {
        ComPtr<IMMDevice> renderDevice;
        if (FAILED(renders->Item(r, &renderDevice))) continue;
        const auto candidateRenderName = GetFriendlyName(renderDevice.Get());
        const auto normalizedRenderName = NormalizeEndpointName(candidateRenderName);
        if (normalizedRenderName.find(NormalizeEndpointName(kVirtualAudioCableMarker)) == std::wstring::npos) continue;
        const auto candidateRenderId = GetDeviceId(renderDevice.Get());
        if (candidateRenderId.empty()) continue;

        for (UINT c = 0; c < captureCount; ++c) {
            ComPtr<IMMDevice> captureDevice;
            if (FAILED(captures->Item(c, &captureDevice))) continue;
            const auto candidateCaptureName = GetFriendlyName(captureDevice.Get());
            if (NormalizeEndpointName(candidateCaptureName) != normalizedRenderName) continue;
            const auto candidateCaptureId = GetDeviceId(captureDevice.Get());
            if (candidateCaptureId.empty()) continue;

            renderId = candidateRenderId;
            renderName = candidateRenderName;
            captureId = candidateCaptureId;
            captureName = candidateCaptureName;
            return true;
        }
    }
    renderId.clear(); renderName.clear(); captureId.clear(); captureName.clear();
    return false;
}

bool FindVirtualCableEndpoints(IMMDeviceEnumerator* enumerator,
                               std::wstring& renderId, std::wstring& renderName,
                               std::wstring& captureId, std::wstring& captureName,
                               std::wstring& provider) {
    // Prefer an already-installed Eugene Muzychenko VAC route. This avoids
    // introducing another virtual driver when the user already has VAC.
    if (FindVACEndpoints(enumerator, renderId, renderName, captureId, captureName)) {
        provider = L"VAC";
        return true;
    }
    if (FindVBCableEndpoints(enumerator, renderId, renderName, captureId, captureName)) {
        provider = L"VB-CABLE";
        return true;
    }
    renderId.clear(); renderName.clear(); captureId.clear(); captureName.clear(); provider.clear();
    return false;
}

bool FindInstalledVirtualCableEndpoints(IMMDeviceEnumerator* enumerator,
                                        std::wstring& renderId, std::wstring& renderName,
                                        std::wstring& captureId, std::wstring& captureName,
                                        std::wstring& provider, bool& routeActive) {
    // Prefer any currently usable pair first. In particular, an installed but
    // disabled VAC must not hide a working VB-CABLE pair from the Host.
    if (FindVirtualCableEndpoints(enumerator, renderId, renderName, captureId,
                                  captureName, provider)) {
        routeActive = true;
        return true;
    }
    // This probe includes disabled and unplugged endpoint records. It is used
    // only to distinguish an installed-but-disabled driver from a missing one;
    // the streaming path below continues to require DEVICE_STATE_ACTIVE.
    ComPtr<IMMDeviceCollection> renders;
    ComPtr<IMMDeviceCollection> captures;
    if (SUCCEEDED(enumerator->EnumAudioEndpoints(eRender, DEVICE_STATEMASK_ALL, &renders)) &&
        SUCCEEDED(enumerator->EnumAudioEndpoints(eCapture, DEVICE_STATEMASK_ALL, &captures))) {
        UINT renderCount = 0, captureCount = 0;
        if (SUCCEEDED(renders->GetCount(&renderCount)) && SUCCEEDED(captures->GetCount(&captureCount))) {
            for (UINT r = 0; r < renderCount; ++r) {
                ComPtr<IMMDevice> renderDevice;
                if (FAILED(renders->Item(r, &renderDevice))) continue;
                const auto renderCandidateName = GetFriendlyName(renderDevice.Get());
                const auto normalizedName = NormalizeEndpointName(renderCandidateName);
                if (normalizedName.find(NormalizeEndpointName(kVirtualAudioCableMarker)) == std::wstring::npos) continue;
                const auto renderCandidateId = GetDeviceId(renderDevice.Get());
                if (renderCandidateId.empty()) continue;

                DWORD renderState = 0;
                if (FAILED(renderDevice->GetState(&renderState))) continue;
                for (UINT c = 0; c < captureCount; ++c) {
                    ComPtr<IMMDevice> captureDevice;
                    if (FAILED(captures->Item(c, &captureDevice))) continue;
                    const auto captureCandidateName = GetFriendlyName(captureDevice.Get());
                    if (NormalizeEndpointName(captureCandidateName) != normalizedName) continue;
                    const auto captureCandidateId = GetDeviceId(captureDevice.Get());
                    if (captureCandidateId.empty()) continue;
                    DWORD captureState = 0;
                    if (FAILED(captureDevice->GetState(&captureState))) continue;
                    renderId = renderCandidateId;
                    renderName = renderCandidateName;
                    captureId = captureCandidateId;
                    captureName = captureCandidateName;
                    provider = L"VAC";
                    routeActive = renderState == DEVICE_STATE_ACTIVE && captureState == DEVICE_STATE_ACTIVE;
                    return true;
                }
            }
        }
    }

    DWORD renderState = 0, captureState = 0;
    const bool inputFound = FindCableEndpointAnyState(enumerator, eRender, kCableInputName,
                                                       renderId, renderName, renderState);
    const bool outputFound = FindCableEndpointAnyState(enumerator, eCapture, kCableOutputName,
                                                        captureId, captureName, captureState);
    if (inputFound && outputFound) {
        provider = L"VB-CABLE";
        routeActive = renderState == DEVICE_STATE_ACTIVE && captureState == DEVICE_STATE_ACTIVE;
        return true;
    }
    renderId.clear(); renderName.clear(); captureId.clear(); captureName.clear(); provider.clear();
    routeActive = false;
    return false;
}

bool IsVirtualCableRenderEndpoint(IMMDevice* device) {
    const auto name = NormalizeEndpointName(GetFriendlyName(device));
    return name.find(NormalizeEndpointName(kVirtualAudioCableMarker)) != std::wstring::npos ||
           name.find(NormalizeEndpointName(kCableInputName)) != std::wstring::npos;
}

HRESULT FindLoopbackRenderEndpoint(IMMDeviceEnumerator* enumerator,
                                   ComPtr<IMMDevice>& selected,
                                   bool& usedFallback) {
    usedFallback = false;
    HRESULT hr = enumerator->GetDefaultAudioEndpoint(eRender, eConsole, &selected);
    if (SUCCEEDED(hr) && selected && !IsVirtualCableRenderEndpoint(selected.Get())) return S_OK;

    // VAC/VB-CABLE may become the Windows default render endpoint after
    // installation. Looping that endpoint back would send the phone mic back
    // to the phone. Prefer an active non-virtual render endpoint in that case.
    selected.Reset();
    ComPtr<IMMDeviceCollection> renders;
    hr = enumerator->EnumAudioEndpoints(eRender, DEVICE_STATE_ACTIVE, &renders);
    if (FAILED(hr)) return hr;
    UINT count = 0;
    hr = renders->GetCount(&count);
    if (FAILED(hr)) return hr;
    for (UINT i = 0; i < count; ++i) {
        ComPtr<IMMDevice> candidate;
        if (FAILED(renders->Item(i, &candidate))) continue;
        if (IsVirtualCableRenderEndpoint(candidate.Get())) continue;
        selected = candidate;
        usedFallback = true;
        return S_OK;
    }
    return AUDCLNT_E_DEVICE_INVALIDATED;
}

void EmitStatus(const std::wstring& renderId, const std::wstring& renderName,
                const std::wstring& micId, const std::wstring& micName,
                const std::wstring& micRenderId, const std::wstring& micRenderName,
                const std::wstring& provider, bool microphoneRouteActive) {
    const std::string json = std::string("{\"version\":1,\"sampleRate\":48000,\"renderEndpointId\":\"") +
        JsonEscape(Utf8(renderId.c_str())) + "\",\"renderFriendlyName\":\"" + JsonEscape(Utf8(renderName.c_str())) +
        "\",\"microphoneEndpointId\":\"" + JsonEscape(Utf8(micId.c_str())) +
        "\",\"microphoneFriendlyName\":\"" + JsonEscape(Utf8(micName.c_str())) +
        "\",\"microphoneRenderEndpointId\":\"" + JsonEscape(Utf8(micRenderId.c_str())) +
        "\",\"microphoneRenderFriendlyName\":\"" + JsonEscape(Utf8(micRenderName.c_str())) +
        "\",\"microphoneRouteProvider\":\"" + JsonEscape(Utf8(provider.c_str())) +
        "\",\"microphoneRouteActive\":" + (microphoneRouteActive ? "true" : "false") + "}";
    const DWORD length = static_cast<DWORD>(json.size());
    const HANDLE output = GetStdHandle(STD_OUTPUT_HANDLE);
    const char tag[4] = {'P','V','R','S'};
    if (!WriteExact(output, tag, sizeof(tag)) || !WriteExact(output, &length, sizeof(length)) ||
        !WriteExact(output, json.data(), length)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: failed to publish endpoint status\n");
    }
}

float DecodeSample(const BYTE* data, const WAVEFORMATEX* format, DWORD channel) {
    const BYTE* sample = data + channel * (format->wBitsPerSample / 8);
    GUID subtype = KSDATAFORMAT_SUBTYPE_PCM;
    if (format->wFormatTag == WAVE_FORMAT_IEEE_FLOAT) subtype = KSDATAFORMAT_SUBTYPE_IEEE_FLOAT;
    else if (format->wFormatTag == WAVE_FORMAT_EXTENSIBLE && format->cbSize >= sizeof(WAVEFORMATEXTENSIBLE) - sizeof(WAVEFORMATEX)) {
        subtype = reinterpret_cast<const WAVEFORMATEXTENSIBLE*>(format)->SubFormat;
    }
    if (IsEqualGUID(subtype, KSDATAFORMAT_SUBTYPE_IEEE_FLOAT)) {
        if (format->wBitsPerSample == 32) { float value; std::memcpy(&value, sample, 4); return std::clamp(value, -1.0f, 1.0f); }
        if (format->wBitsPerSample == 64) { double value; std::memcpy(&value, sample, 8); return static_cast<float>(std::clamp(value, -1.0, 1.0)); }
    }
    if (format->wBitsPerSample == 16) { int16_t value; std::memcpy(&value, sample, 2); return static_cast<float>(value) / 32768.0f; }
    if (format->wBitsPerSample == 24) {
        int32_t value = static_cast<int32_t>(sample[0]) | (static_cast<int32_t>(sample[1]) << 8) | (static_cast<int32_t>(sample[2]) << 16);
        if (value & 0x800000) value |= ~0xFFFFFF;
        return static_cast<float>(value) / 8388608.0f;
    }
    if (format->wBitsPerSample == 32) { int32_t value; std::memcpy(&value, sample, 4); return static_cast<float>(static_cast<double>(value) / 2147483648.0); }
    return 0.0f;
}

bool IsSupportedMixFormat(const WAVEFORMATEX* format) {
    if (!format || format->nSamplesPerSec < 8000 || format->nSamplesPerSec > 192000 ||
        format->nChannels == 0 || format->nChannels > 8) return false;
    GUID subtype = GUID_NULL;
    if (format->wFormatTag == WAVE_FORMAT_IEEE_FLOAT) subtype = KSDATAFORMAT_SUBTYPE_IEEE_FLOAT;
    else if (format->wFormatTag == WAVE_FORMAT_PCM) subtype = KSDATAFORMAT_SUBTYPE_PCM;
    else if (format->wFormatTag == WAVE_FORMAT_EXTENSIBLE && format->cbSize >= sizeof(WAVEFORMATEXTENSIBLE) - sizeof(WAVEFORMATEX))
        subtype = reinterpret_cast<const WAVEFORMATEXTENSIBLE*>(format)->SubFormat;
    else return false;
    if (IsEqualGUID(subtype, KSDATAFORMAT_SUBTYPE_IEEE_FLOAT)) return format->wBitsPerSample == 32 || format->wBitsPerSample == 64;
    if (IsEqualGUID(subtype, KSDATAFORMAT_SUBTYPE_PCM)) return format->wBitsPerSample == 16 || format->wBitsPerSample == 24 || format->wBitsPerSample == 32;
    return false;
}

// Receives 20 ms PCM16 mono frames from the Host and renders them into an
// installed virtual cable playback endpoint (VAC Line N or VB-CABLE Input).
// Windows exposes its paired capture endpoint to applications as a microphone.
class VirtualCableMicRenderer {
public:
    VirtualCableMicRenderer() { stopEvent_ = CreateEventW(nullptr, TRUE, FALSE, nullptr); }
    ~VirtualCableMicRenderer() { Stop(); if (stopEvent_) CloseHandle(stopEvent_); }

    bool Start() {
        if (!stopEvent_ || running_.exchange(true)) return false;
        worker_ = std::thread(&VirtualCableMicRenderer::Run, this);
        return true;
    }

    void Stop() {
        if (!running_.exchange(false)) return;
        if (stopEvent_) SetEvent(stopEvent_);
        if (worker_.joinable()) worker_.join();
    }

    void SubmitFrame(const BYTE* data) {
        if (!data) return;
        MonoFrame frame{};
        std::memcpy(frame.data(), data, kMonoFrameBytes);
        std::lock_guard<std::mutex> lock(queueMutex_);
        if (queue_.size() >= kMicQueueFrames) {
            queue_.pop_front();
            droppedFrames_.fetch_add(1, std::memory_order_relaxed);
        }
        queue_.push_back(frame);
    }

private:
    bool RenderSession() {
        ComPtr<IMMDeviceEnumerator> enumerator;
        HRESULT hr = CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL, IID_PPV_ARGS(&enumerator));
        if (FAILED(hr)) return false;

        std::wstring endpointId, endpointName, captureId, captureName, provider;
        if (!FindVirtualCableEndpoints(enumerator.Get(), endpointId, endpointName, captureId, captureName, provider)) {
            const auto now = std::chrono::steady_clock::now();
            if (lastMissingLog_.time_since_epoch().count() == 0 || now - lastMissingLog_ >= std::chrono::seconds(10)) {
                std::fprintf(stderr, "PhoneVRAudioBridge: no supported virtual cable pair detected (VB-CABLE or VAC); phone microphone frames are buffered briefly and retried\n");
                lastMissingLog_ = now;
            }
            return false;
        }

        ComPtr<IMMDevice> device;
        hr = enumerator->GetDevice(endpointId.c_str(), &device);
        if (FAILED(hr)) return false;
        ComPtr<IAudioClient> client;
        hr = device->Activate(__uuidof(IAudioClient), CLSCTX_ALL, nullptr, reinterpret_cast<void**>(client.GetAddressOf()));
        if (FAILED(hr)) {
            std::fprintf(stderr, "PhoneVRAudioBridge: opening virtual cable render endpoint failed 0x%08lx\n", hr);
            return false;
        }

        WAVEFORMATEX format{};
        format.wFormatTag = WAVE_FORMAT_PCM;
        format.nChannels = 2;
        format.nSamplesPerSec = kSampleRate;
        format.wBitsPerSample = 16;
        format.nBlockAlign = 4;
        format.nAvgBytesPerSec = kSampleRate * 4;
        // Shared-mode automatic conversion lets the signed cable driver's
        // endpoint mix format differ without changing the PhoneVR wire format.
        const DWORD flags = AUDCLNT_STREAMFLAGS_EVENTCALLBACK |
                            AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM |
                            AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY;
        hr = client->Initialize(AUDCLNT_SHAREMODE_SHARED, flags, 200000, 0, &format, nullptr);
        if (FAILED(hr)) {
            std::fprintf(stderr, "PhoneVRAudioBridge: initialize virtual cable 48 kHz stereo render failed 0x%08lx\n", hr);
            return false;
        }

        HANDLE audioEvent = CreateEventW(nullptr, FALSE, FALSE, nullptr);
        if (!audioEvent) return false;
        struct EventCloser { HANDLE Handle; ~EventCloser() { if (Handle) CloseHandle(Handle); } } closer{audioEvent};
        hr = client->SetEventHandle(audioEvent);
        if (FAILED(hr)) return false;
        UINT32 bufferFrames = 0;
        hr = client->GetBufferSize(&bufferFrames);
        if (FAILED(hr) || bufferFrames == 0) return false;
        ComPtr<IAudioRenderClient> render;
        hr = client->GetService(IID_PPV_ARGS(&render));
        if (FAILED(hr)) return false;
        hr = client->Start();
        if (FAILED(hr)) return false;
        std::fprintf(stderr, "PhoneVRAudioBridge: phone microphone is rendering to %s (%s)\n", Utf8(endpointName.c_str()).c_str(), Utf8(provider.c_str()).c_str());

        MonoFrame current{};
        UINT32 currentOffset = kFrameSamples;
        bool hasCurrent = false;
        HANDLE waits[2] = {stopEvent_, audioEvent};
        bool healthy = true;
        while (running_) {
            const DWORD wait = WaitForMultipleObjects(2, waits, FALSE, 1000);
            if (wait == WAIT_OBJECT_0) break;
            if (wait == WAIT_TIMEOUT) {
                UINT32 padding = 0;
                if (FAILED(client->GetCurrentPadding(&padding))) { healthy = false; break; }
                continue;
            }
            if (wait != WAIT_OBJECT_0 + 1) { healthy = false; break; }

            UINT32 padding = 0;
            hr = client->GetCurrentPadding(&padding);
            if (FAILED(hr) || padding > bufferFrames) { healthy = false; break; }
            const UINT32 available = bufferFrames - padding;
            if (available == 0) continue;

            BYTE* output = nullptr;
            hr = render->GetBuffer(available, &output);
            if (FAILED(hr) || !output) { healthy = false; break; }
            auto* stereo = reinterpret_cast<int16_t*>(output);
            {
                std::lock_guard<std::mutex> lock(queueMutex_);
                for (UINT32 i = 0; i < available; ++i) {
                    if (!hasCurrent || currentOffset >= kFrameSamples) {
                        if (!queue_.empty()) {
                            current = queue_.front();
                            queue_.pop_front();
                            currentOffset = 0;
                            hasCurrent = true;
                        } else {
                            hasCurrent = false;
                        }
                    }
                    const int16_t sample = hasCurrent ? current[currentOffset++] : 0;
                    stereo[i * 2] = sample;
                    stereo[i * 2 + 1] = sample;
                }
            }
            hr = render->ReleaseBuffer(available, 0);
            if (FAILED(hr)) { healthy = false; break; }
        }
        client->Stop();
        return healthy;
    }

    void Run() {
        const HRESULT co = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
        if (FAILED(co)) {
            std::fprintf(stderr, "PhoneVRAudioBridge: microphone renderer COM initialization failed 0x%08lx\n", co);
            running_ = false;
            return;
        }
        ComUninitializer uninitializer;
        DWORD mmcssIndex = 0;
        AvrtReverter mmcss{AvSetMmThreadCharacteristicsW(L"Pro Audio", &mmcssIndex)};
        if (mmcss.Handle) AvSetMmThreadPriority(mmcss.Handle, AVRT_PRIORITY_HIGH);
        while (running_) {
            if (!RenderSession() && running_) {
                if (WaitForSingleObject(stopEvent_, 1000) == WAIT_OBJECT_0) break;
            }
        }
    }

    HANDLE stopEvent_ = nullptr;
    std::atomic<bool> running_{false};
    std::thread worker_;
    std::mutex queueMutex_;
    std::deque<MonoFrame> queue_;
    std::atomic<uint64_t> droppedFrames_{0};
    std::chrono::steady_clock::time_point lastMissingLog_{};
};

class WasapiLoopback {
public:
    HRESULT Start(std::wstring& endpointId, std::wstring& friendlyName) {
        HRESULT hr = CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL,
                                      IID_PPV_ARGS(&enumerator_));
        if (FAILED(hr)) return hr;
        ComPtr<IMMDevice> device;
        bool usedFallbackEndpoint = false;
        hr = FindLoopbackRenderEndpoint(enumerator_.Get(), device, usedFallbackEndpoint);
        if (FAILED(hr)) return hr;
        endpointId = GetDeviceId(device.Get());
        friendlyName = GetFriendlyName(device.Get());
        if (usedFallbackEndpoint) {
            std::fprintf(stderr, "PhoneVRAudioBridge: Windows default playback endpoint is a virtual microphone cable; avoiding mic feedback and using physical endpoint %s for game-audio loopback\n",
                Utf8(friendlyName.c_str()).c_str());
        }
        endpointId_ = endpointId;
        friendlyName_ = friendlyName;
        if (endpointId.empty()) return E_FAIL;
        hr = device->Activate(__uuidof(IAudioClient), CLSCTX_ALL, nullptr,
                              reinterpret_cast<void**>(client_.GetAddressOf()));
        if (FAILED(hr)) return hr;
        hr = client_->GetMixFormat(&format_);
        if (FAILED(hr)) return hr;
        if (!IsSupportedMixFormat(format_)) return AUDCLNT_E_UNSUPPORTED_FORMAT;
        event_ = CreateEventW(nullptr, FALSE, FALSE, nullptr);
        if (!event_) return HRESULT_FROM_WIN32(GetLastError());
        hr = client_->Initialize(AUDCLNT_SHAREMODE_SHARED,
                                 AUDCLNT_STREAMFLAGS_LOOPBACK | AUDCLNT_STREAMFLAGS_EVENTCALLBACK,
                                 200000, 0, format_, nullptr);
        if (FAILED(hr)) return hr;
        hr = client_->SetEventHandle(event_);
        if (FAILED(hr)) return hr;
        hr = client_->GetService(IID_PPV_ARGS(&capture_));
        if (FAILED(hr)) return hr;
        return client_->Start();
    }

    ~WasapiLoopback() {
        if (client_) client_->Stop();
        capture_.Reset();
        client_.Reset();
        if (format_) CoTaskMemFree(format_);
        if (event_) CloseHandle(event_);
    }

    bool Run() {
        std::vector<StereoSample> source;
        source.reserve(16384);
        double sourcePosition = 0.0;
        const double step = static_cast<double>(format_->nSamplesPerSec) / kSampleRate;
        std::array<int16_t, kFrameSamples * 2> output{};
        DWORD outputFrames = 0;

        DWORD mmcssTaskIndex = 0;
        AvrtReverter mmcss{AvSetMmThreadCharacteristicsW(L"Pro Audio", &mmcssTaskIndex)};
        if (mmcss.Handle) AvSetMmThreadPriority(mmcss.Handle, AVRT_PRIORITY_HIGH);
        auto lastEndpointCheck = std::chrono::steady_clock::now();
        std::wstring micId, micName, micRenderId, micRenderName, micProvider;
        bool cableActive = FindVirtualCableEndpoints(enumerator_.Get(), micRenderId, micRenderName, micId, micName, micProvider);
        if (!cableActive) { micId.clear(); micName.clear(); micRenderId.clear(); micRenderName.clear(); micProvider.clear(); }
        EmitStatus(endpointId_, friendlyName_, micId, micName, micRenderId, micRenderName, micProvider, cableActive);
        std::fprintf(stderr, "PhoneVRAudioBridge: WASAPI loopback active: %s\n", Utf8(friendlyName_.c_str()).c_str());
        if (cableActive) {
            std::fprintf(stderr, "PhoneVRAudioBridge: existing virtual cable route active (%s): %s -> %s\n",
                Utf8(micProvider.c_str()).c_str(), Utf8(micRenderName.c_str()).c_str(), Utf8(micName.c_str()).c_str());
        } else {
            std::fprintf(stderr, "PhoneVRAudioBridge: no supported virtual cable detected; existing audio drivers were left untouched. Install VB-CABLE only if no compatible route is available.\n");
        }

        while (true) {
            const DWORD wait = WaitForSingleObject(event_, 1000);
            if (wait == WAIT_TIMEOUT) {
                UINT32 padding = 0;
                const HRESULT health = client_->GetCurrentPadding(&padding);
                if (FAILED(health)) {
                    std::fprintf(stderr, "PhoneVRAudioBridge: render endpoint invalidated (0x%08lx)\n", health);
                    return false;
                }
                const auto now = std::chrono::steady_clock::now();
                if (now - lastEndpointCheck >= std::chrono::seconds(3)) {
                    ComPtr<IMMDevice> currentLoopbackEndpoint;
                    bool usingFallbackEndpoint = false;
                    if (SUCCEEDED(FindLoopbackRenderEndpoint(enumerator_.Get(), currentLoopbackEndpoint, usingFallbackEndpoint)) &&
                        GetDeviceId(currentLoopbackEndpoint.Get()) != endpointId_) {
                        std::fprintf(stderr, "PhoneVRAudioBridge: effective game-audio render endpoint changed; restarting WASAPI session\n");
                        return false;
                    }
                    std::wstring currentMicId, currentMicName, currentMicRenderId, currentMicRenderName, currentMicProvider;
                    const bool currentCableActive = FindVirtualCableEndpoints(enumerator_.Get(), currentMicRenderId, currentMicRenderName, currentMicId, currentMicName, currentMicProvider);
                    if (!currentCableActive) { currentMicId.clear(); currentMicName.clear(); currentMicRenderId.clear(); currentMicRenderName.clear(); currentMicProvider.clear(); }
                    if (currentMicId != micId || currentMicRenderId != micRenderId || currentMicProvider != micProvider) {
                        micId = currentMicId; micName = currentMicName;
                        micRenderId = currentMicRenderId; micRenderName = currentMicRenderName; micProvider = currentMicProvider;
                        EmitStatus(endpointId_, friendlyName_, micId, micName, micRenderId, micRenderName, micProvider, currentCableActive);
                        std::fprintf(stderr, "PhoneVRAudioBridge: virtual cable route %s (%s)\n", currentCableActive ? "detected" : "not available", Utf8(micProvider.c_str()).c_str());
                    }
                    lastEndpointCheck = now;
                }
                continue;
            }
            if (wait != WAIT_OBJECT_0) return false;

            UINT32 packetFrames = 0;
            HRESULT hr = capture_->GetNextPacketSize(&packetFrames);
            if (FAILED(hr)) { std::fprintf(stderr, "PhoneVRAudioBridge: GetNextPacketSize failed 0x%08lx\n", hr); return false; }
            while (packetFrames > 0) {
                BYTE* data = nullptr;
                UINT32 frames = 0;
                DWORD flags = 0;
                UINT64 devicePosition = 0, qpcPosition = 0;
                hr = capture_->GetBuffer(&data, &frames, &flags, &devicePosition, &qpcPosition);
                if (FAILED(hr)) { std::fprintf(stderr, "PhoneVRAudioBridge: GetBuffer failed 0x%08lx\n", hr); return false; }
                const DWORD channels = format_->nChannels;
                for (UINT32 i = 0; i < frames; ++i) {
                    float left = 0.0f, right = 0.0f;
                    if (!(flags & AUDCLNT_BUFFERFLAGS_SILENT) && data) {
                        const BYTE* frame = data + static_cast<size_t>(i) * format_->nBlockAlign;
                        if (channels == 1) {
                            left = right = DecodeSample(frame, format_, 0);
                        } else {
                            left = DecodeSample(frame, format_, 0);
                            right = DecodeSample(frame, format_, 1);
                            if (channels > 2) {
                                const float center = DecodeSample(frame, format_, 2) * 0.70710678f;
                                left += center; right += center;
                                if (channels > 4) left += DecodeSample(frame, format_, 4) * 0.5f;
                                if (channels > 5) right += DecodeSample(frame, format_, 5) * 0.5f;
                            }
                            left = std::clamp(left, -1.0f, 1.0f);
                            right = std::clamp(right, -1.0f, 1.0f);
                        }
                    }
                    source.push_back({left, right});
                }
                hr = capture_->ReleaseBuffer(frames);
                if (FAILED(hr)) { std::fprintf(stderr, "PhoneVRAudioBridge: ReleaseBuffer failed 0x%08lx\n", hr); return false; }
                hr = capture_->GetNextPacketSize(&packetFrames);
                if (FAILED(hr)) return false;
            }

            while (sourcePosition + 1.0 < static_cast<double>(source.size())) {
                const size_t index = static_cast<size_t>(sourcePosition);
                const float fraction = static_cast<float>(sourcePosition - static_cast<double>(index));
                const auto& a = source[index];
                const auto& b = source[index + 1];
                const float left = a.Left + (b.Left - a.Left) * fraction;
                const float right = a.Right + (b.Right - a.Right) * fraction;
                output[outputFrames * 2] = static_cast<int16_t>(std::lrint(std::clamp(left, -1.0f, 1.0f) * 32767.0f));
                output[outputFrames * 2 + 1] = static_cast<int16_t>(std::lrint(std::clamp(right, -1.0f, 1.0f) * 32767.0f));
                ++outputFrames;
                sourcePosition += step;
                if (outputFrames == kFrameSamples) {
                    const HANDLE out = GetStdHandle(STD_OUTPUT_HANDLE);
                    const char tag[4] = {'P','V','R','A'};
                    if (!WriteExact(out, tag, sizeof(tag)) || !WriteExact(out, output.data(), kStereoFrameBytes)) return false;
                    outputFrames = 0;
                }
            }
            const size_t consumed = std::min(static_cast<size_t>(sourcePosition), source.size() > 0 ? source.size() - 1 : size_t{0});
            if (consumed > 0) {
                source.erase(source.begin(), source.begin() + static_cast<ptrdiff_t>(consumed));
                sourcePosition -= static_cast<double>(consumed);
            }
            const auto now = std::chrono::steady_clock::now();
            if (now - lastEndpointCheck >= std::chrono::seconds(3)) {
                ComPtr<IMMDevice> currentLoopbackEndpoint;
                bool usingFallbackEndpoint = false;
                if (SUCCEEDED(FindLoopbackRenderEndpoint(enumerator_.Get(), currentLoopbackEndpoint, usingFallbackEndpoint)) &&
                    GetDeviceId(currentLoopbackEndpoint.Get()) != endpointId_) {
                    std::fprintf(stderr, "PhoneVRAudioBridge: effective game-audio render endpoint changed; restarting WASAPI session\n");
                    return false;
                }
                std::wstring currentMicId, currentMicName, currentMicRenderId, currentMicRenderName, currentMicProvider;
                const bool currentCableActive = FindVirtualCableEndpoints(enumerator_.Get(), currentMicRenderId, currentMicRenderName, currentMicId, currentMicName, currentMicProvider);
                if (!currentCableActive) { currentMicId.clear(); currentMicName.clear(); currentMicRenderId.clear(); currentMicRenderName.clear(); currentMicProvider.clear(); }
                if (currentMicId != micId || currentMicRenderId != micRenderId || currentMicProvider != micProvider) {
                    micId = currentMicId; micName = currentMicName;
                    micRenderId = currentMicRenderId; micRenderName = currentMicRenderName; micProvider = currentMicProvider;
                    EmitStatus(endpointId_, friendlyName_, micId, micName, micRenderId, micRenderName, micProvider, currentCableActive);
                    std::fprintf(stderr, "PhoneVRAudioBridge: virtual cable route %s (%s)\n", currentCableActive ? "detected" : "not available", Utf8(micProvider.c_str()).c_str());
                }
                lastEndpointCheck = now;
            }
        }
    }

private:
    ComPtr<IAudioClient> client_;
    ComPtr<IAudioCaptureClient> capture_;
    WAVEFORMATEX* format_ = nullptr;
    HANDLE event_ = nullptr;
    ComPtr<IMMDeviceEnumerator> enumerator_;
    std::wstring endpointId_;
    std::wstring friendlyName_;
};

DWORD WINAPI MicPipeThread(void* context) {
    auto* renderer = static_cast<VirtualCableMicRenderer*>(context);
    const HANDLE input = GetStdHandle(STD_INPUT_HANDLE);
    std::array<BYTE, kMonoFrameBytes> frame{};
    while (ReadExact(input, frame.data(), static_cast<DWORD>(frame.size()))) {
        if (renderer) renderer->SubmitFrame(frame.data());
    }
    return 0;
}

int ProbeVirtualCableEndpoints() {
    const HRESULT co = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(co)) return 2;
    ComUninitializer comUninitializer;

    ComPtr<IMMDeviceEnumerator> enumerator;
    HRESULT hr = CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL,
                                  IID_PPV_ARGS(&enumerator));
    if (FAILED(hr)) return 2;

    std::wstring micRenderId, micRenderName, micId, micName, provider;
    bool microphoneRouteActive = false;
    if (!FindInstalledVirtualCableEndpoints(enumerator.Get(), micRenderId, micRenderName,
                                            micId, micName, provider, microphoneRouteActive)) return 3;

    ComPtr<IMMDevice> loopbackDevice;
    bool usedFallback = false;
    std::wstring renderId, renderName;
    if (SUCCEEDED(FindLoopbackRenderEndpoint(enumerator.Get(), loopbackDevice, usedFallback))) {
        renderId = GetDeviceId(loopbackDevice.Get());
        renderName = GetFriendlyName(loopbackDevice.Get());
    }
    // PVRS is emitted without starting WASAPI streams. This lets the Host's
    // Audio & Microphone page reuse an existing VAC even while streaming is
    // stopped and the normal audio supervisor has removed its runtime state.
    EmitStatus(renderId, renderName, micId, micName, micRenderId, micRenderName,
               provider, microphoneRouteActive);
    return 0;
}
} // namespace

int wmain(int argc, wchar_t** argv) {
    if (argc > 1 && std::wstring(argv[1]) == L"--probe") {
        return ProbeVirtualCableEndpoints();
    }
    SetConsoleOutputCP(CP_UTF8);
    HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(hr)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: CoInitializeEx failed 0x%08lx\n", hr);
        return 2;
    }
    ComUninitializer comUninitializer;

    WasapiLoopback loopback;
    std::wstring renderId, renderName;
    hr = loopback.Start(renderId, renderName);
    if (FAILED(hr)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: WASAPI loopback initialization failed 0x%08lx\n", hr);
        return 4;
    }

    VirtualCableMicRenderer micRenderer;
    if (!micRenderer.Start()) {
        std::fprintf(stderr, "PhoneVRAudioBridge: virtual microphone cable render worker could not start\n");
        return 5;
    }
    HANDLE micThread = CreateThread(nullptr, 0, MicPipeThread, &micRenderer, 0, nullptr);
    if (!micThread) {
        std::fprintf(stderr, "PhoneVRAudioBridge: microphone pipe thread creation failed (%lu)\n", GetLastError());
        micRenderer.Stop();
        return 6;
    }

    const bool ok = loopback.Run();
    CancelSynchronousIo(micThread);
    WaitForSingleObject(micThread, INFINITE);
    CloseHandle(micThread);
    micRenderer.Stop();
    return ok ? 0 : 7;
}
