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
#include "../../windows-audio-driver/shared/PhoneVRAudioShared.h"

#include <algorithm>
#include <array>
#include <atomic>
#include <cmath>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <string>
#include <vector>
#include <chrono>
#include <climits>
#include <cstddef>

#pragma comment(lib, "ole32.lib")
#pragma comment(lib, "uuid.lib")
#pragma comment(lib, "avrt.lib")
#pragma comment(lib, "propsys.lib")
#pragma comment(lib, "advapi32.lib")

using Microsoft::WRL::ComPtr;

namespace {
constexpr DWORD kSampleRate = 48000;
constexpr DWORD kFrameSamples = 960;
constexpr DWORD kStereoFrameBytes = kFrameSamples * 2 * sizeof(int16_t);
constexpr DWORD kMonoFrameBytes = PHONEVR_MIC_FRAME_BYTES;
constexpr DWORD kMicRingMagic = PHONEVR_MIC_RING_MAGIC;
constexpr DWORD kMicRingVersion = PHONEVR_MIC_RING_VERSION;
constexpr LONG64 kMicRingSlots = PHONEVR_MIC_RING_SLOT_COUNT;
constexpr wchar_t kMicMappingName[] = PHONEVR_MIC_SECTION_WIN32;

using MicRingSlot = PHONEVR_MIC_RING_SLOT;
using MicRing = PHONEVR_MIC_SHARED_RING;

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
        default:
            if (c >= 0x20) out += static_cast<char>(c);
            break;
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

bool FindPhoneVRMicrophone(IMMDeviceEnumerator* enumerator, std::wstring& id, std::wstring& name) {
    ComPtr<IMMDeviceCollection> collection;
    if (FAILED(enumerator->EnumAudioEndpoints(eCapture, DEVICE_STATE_ACTIVE, &collection))) return false;
    UINT count = 0;
    if (FAILED(collection->GetCount(&count))) return false;
    for (UINT i = 0; i < count; ++i) {
        ComPtr<IMMDevice> device;
        if (FAILED(collection->Item(i, &device))) continue;
        const auto candidateName = GetFriendlyName(device.Get());
        if (candidateName.find(L"PhoneVR Microphone") == std::wstring::npos) continue;
        id = GetDeviceId(device.Get());
        name = candidateName;
        return !id.empty();
    }
    return false;
}

void EmitStatus(const std::wstring& renderId, const std::wstring& renderName,
                const std::wstring& micId, const std::wstring& micName) {
    const std::string json = std::string("{\"version\":1,\"sampleRate\":48000,\"renderEndpointId\":\"") +
        JsonEscape(Utf8(renderId.c_str())) + "\",\"renderFriendlyName\":\"" + JsonEscape(Utf8(renderName.c_str())) +
        "\",\"microphoneEndpointId\":\"" + JsonEscape(Utf8(micId.c_str())) +
        "\",\"microphoneFriendlyName\":\"" + JsonEscape(Utf8(micName.c_str())) + "\"}";
    const DWORD length = static_cast<DWORD>(json.size());
    const HANDLE output = GetStdHandle(STD_OUTPUT_HANDLE);
    const char tag[4] = {'P','V','R','S'};
    if (!WriteExact(output, tag, sizeof(tag)) || !WriteExact(output, &length, sizeof(length)) ||
        !WriteExact(output, json.data(), length)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: failed to publish endpoint status\n");
    }
}

MicRing* OpenMicRing(HANDLE& mapping) {
    // The SysVAD miniport creates this Global section with a DACL for SYSTEM
    // and interactive users. Creating a Global file mapping from a normal
    // desktop process would require SeCreateGlobalPrivilege.
    mapping = OpenFileMappingW(FILE_MAP_READ | FILE_MAP_WRITE, FALSE, kMicMappingName);
    if (!mapping) return nullptr;
    auto* ring = static_cast<MicRing*>(MapViewOfFile(mapping, FILE_MAP_READ | FILE_MAP_WRITE, 0, 0, sizeof(MicRing)));
    if (!ring) {
        const DWORD error = GetLastError();
        CloseHandle(mapping);
        mapping = nullptr;
        std::fprintf(stderr, "PhoneVRAudioBridge: MapViewOfFile for PhoneVR microphone failed (%lu)\n", error);
        return nullptr;
    }
    if (ring->Magic != kMicRingMagic || ring->Version != kMicRingVersion ||
        ring->FrameBytes != kMonoFrameBytes || ring->SlotCount != kMicRingSlots) {
        UnmapViewOfFile(ring);
        CloseHandle(mapping);
        mapping = nullptr;
        return nullptr;
    }
    return ring;
}

bool PushMicFrame(MicRing* ring, const BYTE* frame) {
    if (!ring || ring->Magic != kMicRingMagic) return false;
    for (int attempt = 0; attempt < 4; ++attempt) {
        const LONG64 position = InterlockedCompareExchange64(&ring->EnqueuePosition, 0, 0);
        if (position < 0 || position > LLONG_MAX - kMicRingSlots) return false;
        MicRingSlot& slot = ring->Slots[position % kMicRingSlots];
        const LONG64 sequence = InterlockedCompareExchange64(&slot.Sequence, 0, 0);
        if (sequence < position) return false; // full: discard this newest frame
        if (sequence > position) continue;
        if (InterlockedCompareExchange64(&ring->EnqueuePosition, position + 1, position) != position) continue;
        std::memcpy(slot.Payload, frame, kMonoFrameBytes);
        MemoryBarrier();
        InterlockedExchange64(&slot.Sequence, position + 1);
        return true;
    }
    return false;
}

float DecodeSample(const BYTE* data, const WAVEFORMATEX* format, DWORD channel) {
    const DWORD channels = std::max<DWORD>(1, format->nChannels);
    const BYTE* sample = data + channel * (format->wBitsPerSample / 8);
    GUID subtype = KSDATAFORMAT_SUBTYPE_PCM;
    if (format->wFormatTag == WAVE_FORMAT_IEEE_FLOAT) subtype = KSDATAFORMAT_SUBTYPE_IEEE_FLOAT;
    else if (format->wFormatTag == WAVE_FORMAT_EXTENSIBLE && format->cbSize >= sizeof(WAVEFORMATEXTENSIBLE) - sizeof(WAVEFORMATEX)) {
        subtype = reinterpret_cast<const WAVEFORMATEXTENSIBLE*>(format)->SubFormat;
    }
    (void)channels;
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
    if (format->wFormatTag == WAVE_FORMAT_IEEE_FLOAT) {
        subtype = KSDATAFORMAT_SUBTYPE_IEEE_FLOAT;
    } else if (format->wFormatTag == WAVE_FORMAT_PCM) {
        subtype = KSDATAFORMAT_SUBTYPE_PCM;
    } else if (format->wFormatTag == WAVE_FORMAT_EXTENSIBLE &&
               format->cbSize >= sizeof(WAVEFORMATEXTENSIBLE) - sizeof(WAVEFORMATEX)) {
        subtype = reinterpret_cast<const WAVEFORMATEXTENSIBLE*>(format)->SubFormat;
    } else {
        return false;
    }

    if (IsEqualGUID(subtype, KSDATAFORMAT_SUBTYPE_IEEE_FLOAT))
        return format->wBitsPerSample == 32 || format->wBitsPerSample == 64;
    if (IsEqualGUID(subtype, KSDATAFORMAT_SUBTYPE_PCM))
        return format->wBitsPerSample == 16 || format->wBitsPerSample == 24 || format->wBitsPerSample == 32;
    return false;
}

class WasapiLoopback {
public:
    HRESULT Start(std::wstring& endpointId, std::wstring& friendlyName) {
        ComPtr<IMMDeviceEnumerator> enumerator;
        HRESULT hr = CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL,
                                      IID_PPV_ARGS(&enumerator));
        if (FAILED(hr)) return hr;
        ComPtr<IMMDevice> device;
        hr = enumerator->GetDefaultAudioEndpoint(eRender, eConsole, &device);
        if (FAILED(hr)) return hr;
        endpointId = GetDeviceId(device.Get());
        friendlyName = GetFriendlyName(device.Get());
        endpointId_ = endpointId;
        friendlyName_ = friendlyName;
        enumerator_ = enumerator;
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
                                 200000, 0, format_, nullptr); // 20 ms shared-mode buffer target.
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
        std::wstring micId, micName;
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
                if (enumerator_ && now - lastEndpointCheck >= std::chrono::seconds(3)) {
                    ComPtr<IMMDevice> currentDefault;
                    if (SUCCEEDED(enumerator_->GetDefaultAudioEndpoint(eRender, eConsole, &currentDefault)) &&
                        GetDeviceId(currentDefault.Get()) != endpointId_) {
                        std::fprintf(stderr, "PhoneVRAudioBridge: Windows default render endpoint changed; restarting WASAPI session\n");
                        return false;
                    }
                    std::wstring currentId, currentName;
                    FindPhoneVRMicrophone(enumerator_.Get(), currentId, currentName);
                    if (currentId != micId) {
                        micId = currentId;
                        micName = currentName;
                        EmitStatus(endpointId_, friendlyName_, micId, micName);
                        std::fprintf(stderr, "PhoneVRAudioBridge: PhoneVR Microphone endpoint %s\n", micId.empty() ? "not available" : "detected");
                    }
                    lastEndpointCheck = now;
                }
                continue;
            }
            if (wait != WAIT_OBJECT_0) {
                return false;
            }
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
            // Retain the final source sample so interpolation remains valid
            // when the next WASAPI packet starts between source sample times.
            const size_t consumed = std::min(
                static_cast<size_t>(sourcePosition), source.size() > 0 ? source.size() - 1 : size_t{0});
            if (consumed > 0) {
                source.erase(source.begin(), source.begin() + static_cast<ptrdiff_t>(consumed));
                sourcePosition -= static_cast<double>(consumed);
            }
            const auto now = std::chrono::steady_clock::now();
            if (enumerator_ && now - lastEndpointCheck >= std::chrono::seconds(3)) {
                ComPtr<IMMDevice> currentDefault;
                if (SUCCEEDED(enumerator_->GetDefaultAudioEndpoint(eRender, eConsole, &currentDefault)) &&
                    GetDeviceId(currentDefault.Get()) != endpointId_) {
                    std::fprintf(stderr, "PhoneVRAudioBridge: Windows default render endpoint changed; restarting WASAPI session\n");
                    return false;
                }
                std::wstring currentId, currentName;
                FindPhoneVRMicrophone(enumerator_.Get(), currentId, currentName);
                if (currentId != micId) {
                    micId = currentId;
                    micName = currentName;
                    EmitStatus(endpointId_, friendlyName_, micId, micName);
                    std::fprintf(stderr, "PhoneVRAudioBridge: PhoneVR Microphone endpoint %s\n", micId.empty() ? "not available" : "detected");
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

DWORD WINAPI MicPipeThread(void*) {
    const HANDLE input = GetStdHandle(STD_INPUT_HANDLE);
    std::array<BYTE, kMonoFrameBytes> frame{};
    HANDLE mapping = nullptr;
    MicRing* ring = nullptr;
    auto lastOpenAttempt = std::chrono::steady_clock::time_point{};
    auto lastMissingLog = std::chrono::steady_clock::time_point{};

    while (ReadExact(input, frame.data(), static_cast<DWORD>(frame.size()))) {
        const auto now = std::chrono::steady_clock::now();
        if (!ring && now - lastOpenAttempt >= std::chrono::milliseconds(250)) {
            lastOpenAttempt = now;
            ring = OpenMicRing(mapping);
            if (!ring && (lastMissingLog.time_since_epoch().count() == 0 ||
                          now - lastMissingLog >= std::chrono::seconds(5))) {
                std::fprintf(stderr, "PhoneVRAudioBridge: PhoneVR Microphone shared section is not available; install/start the virtual microphone driver\n");
                lastMissingLog = now;
            }
        }
        if (ring && !PushMicFrame(ring, frame.data())) {
            // Drop newest on full. The kernel consumer drains the ring at a
            // higher priority and the bounded queue prevents stale voice buildup.
        }
    }

    if (ring) UnmapViewOfFile(ring);
    if (mapping) CloseHandle(mapping);
    return 0;
}
} // namespace

int wmain() {
    SetConsoleOutputCP(CP_UTF8);
    HRESULT hr = CoInitializeEx(nullptr, COINIT_MULTITHREADED);
    if (FAILED(hr)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: CoInitializeEx failed 0x%08lx\n", hr);
        return 2;
    }
    // Declared before all COM smart pointers so they are released first.
    ComUninitializer comUninitializer;

    WasapiLoopback loopback;
    std::wstring renderId, renderName, micId, micName;
    hr = loopback.Start(renderId, renderName);
    if (FAILED(hr)) {
        std::fprintf(stderr, "PhoneVRAudioBridge: WASAPI loopback initialization failed 0x%08lx\n", hr);
        return 4;
    }

    ComPtr<IMMDeviceEnumerator> enumerator;
    if (SUCCEEDED(CoCreateInstance(__uuidof(MMDeviceEnumerator), nullptr, CLSCTX_ALL, IID_PPV_ARGS(&enumerator)))) {
        if (!FindPhoneVRMicrophone(enumerator.Get(), micId, micName)) {
            std::fprintf(stderr, "PhoneVRAudioBridge: capture endpoint 'PhoneVR Microphone' is not installed or active; mic frames will be discarded until the driver is installed\n");
        }
    }
    EmitStatus(renderId, renderName, micId, micName);
    std::fprintf(stderr, "PhoneVRAudioBridge: WASAPI loopback active: %s\n", Utf8(renderName.c_str()).c_str());
    if (!micId.empty()) std::fprintf(stderr, "PhoneVRAudioBridge: recording endpoint active: PhoneVR Microphone\n");

    HANDLE micThread = CreateThread(nullptr, 0, MicPipeThread, nullptr, 0, nullptr);
    if (!micThread) {
        std::fprintf(stderr, "PhoneVRAudioBridge: microphone pipe thread creation failed (%lu)\n", GetLastError());
        return 5;
    }

    const bool ok = loopback.Run();
    // The process is supervised by PhoneVR Host. Closing stdin/stdout or a
    // capture-device invalidation causes the Host to restart this bridge.
    CancelSynchronousIo(micThread);
    WaitForSingleObject(micThread, INFINITE);
    CloseHandle(micThread);
    return ok ? 0 : 6;
}
