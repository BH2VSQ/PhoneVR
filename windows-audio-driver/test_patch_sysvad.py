from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).with_name("patch_sysvad.py")
SPEC = importlib.util.spec_from_file_location("phonevr_patch_sysvad", MODULE_PATH)
assert SPEC and SPEC.loader
PATCHER = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PATCHER)


class SysVadPatchTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        root = Path(self.temp.name) / "audio" / "sysvad"
        self.endpoints = root / "EndpointsCommon"
        self.tablet = root / "TabletAudioSample"
        self.endpoints.mkdir(parents=True)
        self.tablet.mkdir(parents=True)

        (self.endpoints / "minwavert.h").write_text(
            """#ifndef _SYSVAD_MINWAVERT_H_
#define _SYSVAD_MINWAVERT_H_
class CMiniportWaveRT {
protected:
    PENDPOINT_MINIPAIR                  m_pMiniportPair;
public:
    DECLARE_PROPERTYHANDLER(Get_SoundDetectorSupportedPatterns);
    CMiniportWaveRT()
        :CUnknown(0),
        m_pPortClsNotifications(NULL)
    {
        PAGED_CODE();
    }
};
""",
            encoding="utf-8",
        )
        (self.endpoints / "minwavert.cpp").write_text(
            """NTSTATUS CMiniportWaveRT::Init()
{
    PAGED_CODE();
    return ntStatus;
} // Init
CMiniportWaveRT::~CMiniportWaveRT()
{
    PAGED_CODE();
}
//=============================================================================
#pragma code_seg("PAGE")
STDMETHODIMP_(NTSTATUS)
CMiniportWaveRT::NewStream
""",
            encoding="utf-8",
        )
        (self.endpoints / "minwavertstream.h").write_text(
            """#include "tonegenerator.h"
class CMiniportWaveRTStream {
    ULONG                       m_AudioModuleCount;
    NTSTATUS ReadRegistrySettings();
};
""",
            encoding="utf-8",
        )
        (self.endpoints / "minwavertstream.cpp").write_text(
            """NTSTATUS CMiniportWaveRTStream::Init()
{
    PAGED_CODE();

    PWAVEFORMATEX pWfEx = NULL;
}
CMiniportWaveRTStream::~CMiniportWaveRTStream()
{
    PAGED_CODE();
}
NTSTATUS CMiniportWaveRTStream::SetState()
{
    switch (State_)
    {
    }
}
VOID CMiniportWaveRTStream::WriteBytes(_In_ ULONG ByteDisplacement)
{
    ULONG bufferOffset = m_ullLinearPosition % m_ulDmaBufferSize;

    // Normally this will loop no more than once for a single wrap, but if
    // many bytes have been displaced then this may loops many times.
    while (ByteDisplacement > 0)
}
VOID CMiniportWaveRTStream::ReadBytes(_In_ ULONG ByteDisplacement)
{
    m_SaveData.WriteData(nullptr, 0);
    ULONG bufferOffset = m_ullLinearPosition % m_ulDmaBufferSize;

    // Normally this will loop no more than once for a single wrap, but if
    // many bytes have been displaced then this may loops many times.
    while (ByteDisplacement > 0)
    {
        m_SaveData.WriteData(m_pDmaBuffer + bufferOffset, runWrite);
    }
}
//=============================================================================
#pragma code_seg()
VOID CMiniportWaveRTStream::WriteBytes
""",
            encoding="utf-8",
        )
        (self.tablet / "micinwavtable.h").write_text(
            """#define MICIN_MAX_INPUT_STREAMS             5
#define MICIN_MIN_SAMPLE_RATE               8000
static
KSDATAFORMAT_WAVEFORMATEXTENSIBLE MicInPinSupportedDeviceFormats[] =
{
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE },
    { WAVE_FORMAT_EXTENSIBLE }
};

//
// Supported modes
&MicInPinSupportedDeviceFormats[2].DataFormat
&MicInPinSupportedDeviceFormats[4].DataFormat
""",
            encoding="utf-8",
        )
        (self.tablet / "ComponentizedAudioSample.inx").write_text(
            """SYSVAD.WaveMicIn.szPname="SYSVAD Wave Microphone Headphone"
SYSVAD.TopologyMicIn.szPname="SYSVAD Topology Microphone Headphone"
MicInCustomName= "External Microphone Headphone"
""",
            encoding="utf-8",
        )
        (self.tablet / "minipairs.h").write_text(
            """static
PENDPOINT_MINIPAIR  g_RenderEndpoints[] =
{
    &SpeakerMiniports,
    &SpeakerHpMiniports,
    &HdmiMiniports,
    &SpdifMiniports,
};

#define g_cRenderEndpoints  (SIZEOF_ARRAY(g_RenderEndpoints))
static
PENDPOINT_MINIPAIR  g_CaptureEndpoints[] =
{
    &MicInMiniports,
    &MicArray1Miniports,
    &MicArray2Miniports,
    &MicArray3Miniports,
};
""",
            encoding="utf-8",
        )

    def tearDown(self) -> None:
        self.temp.cleanup()

    def test_all_transforms_are_applied_and_idempotent(self) -> None:
        transforms = [
            (self.endpoints / "minwavert.h", PATCHER.patch_minwavert_h),
            (self.endpoints / "minwavert.cpp", PATCHER.patch_miniport_cpp),
            (self.endpoints / "minwavertstream.h", PATCHER.patch_stream_h),
            (self.endpoints / "minwavertstream.cpp", PATCHER.patch_stream_cpp),
            (self.tablet / "minipairs.h", PATCHER.patch_minipairs),
            (self.tablet / "micinwavtable.h", PATCHER.patch_micinwav),
            (self.tablet / "ComponentizedAudioSample.inx", PATCHER.patch_inf),
        ]
        for path, transform in transforms:
            original = path.read_text(encoding="utf-8")
            patched = transform(original)
            path.write_text(patched, encoding="utf-8")
            self.assertEqual(transform(path.read_text(encoding="utf-8")), patched, path.name)

        miniport_h = (self.endpoints / "minwavert.h").read_text(encoding="utf-8")
        miniport_cpp = (self.endpoints / "minwavert.cpp").read_text(encoding="utf-8")
        stream_cpp = (self.endpoints / "minwavertstream.cpp").read_text(encoding="utf-8")
        pairs = (self.tablet / "minipairs.h").read_text(encoding="utf-8")
        self.assertIn("m_PhoneVRMicSectionHandle = NULL;", miniport_h)
        self.assertIn("PhoneVRMicCreateSharedSection", miniport_cpp)
        self.assertIn("PhoneVRMicReadBytes(ByteDisplacement)", stream_cpp)
        self.assertIn("m_SaveData.WriteData", stream_cpp)
        read_bytes = stream_cpp.split("VOID CMiniportWaveRTStream::ReadBytes", 1)[1].split("//=============================================================================", 1)[0]
        self.assertNotIn("PhoneVRMicReadBytes(ByteDisplacement)", read_bytes)
        self.assertIn("complete 20 ms units", PATCHER.METHODS)
        self.assertIn("KeAcquireSpinLockAtDpcLevel(&m_PhoneVRMicQueueLock)", PATCHER.METHODS)
        self.assertIn("KeReleaseSpinLockFromDpcLevel(&m_PhoneVRMicQueueLock)", PATCHER.METHODS)
        frame_bytes_declaration = PATCHER.METHODS.index(
            "const ULONG frameBytes = static_cast<ULONG>(PHONEVR_MIC_FRAME_BYTES);"
        )
        overflow_loop = PATCHER.METHODS.index("while (capacity - m_PhoneVRMicQueueCount < frameBytes)")
        self.assertLess(frame_bytes_declaration, overflow_loop, "frameBytes must be in scope after the overflow loop")
        self.assertIn("const ULONG first = frameBytes < tailBytes ? frameBytes : tailBytes;", PATCHER.METHODS)
        self.assertIn("g_cRenderEndpoints  0", pairs)
        # The pinned Microsoft sample has trailing spaces after these two
        # declarations. The patcher must normalize them and remain idempotent.
        upstream_spacing = pairs.replace("g_RenderEndpoints[] =\n", "g_RenderEndpoints[] = \n")
        upstream_spacing = upstream_spacing.replace("g_CaptureEndpoints[] =\n", "g_CaptureEndpoints[] = \n")
        self.assertEqual(PATCHER.patch_minipairs(upstream_spacing), pairs)
        mic_formats = (self.tablet / "micinwavtable.h").read_text(encoding="utf-8")
        self.assertIn("MICIN_MAX_INPUT_STREAMS             1", mic_formats)
        self.assertIn("MICIN_MIN_SAMPLE_RATE               48000", mic_formats)
        self.assertEqual(mic_formats.count("WAVE_FORMAT_EXTENSIBLE"), 1)
        self.assertIn("&MicInPinSupportedDeviceFormats[0].DataFormat", mic_formats)
        self.assertNotIn("MicInPinSupportedDeviceFormats[2]", mic_formats)
        self.assertNotIn("MicInPinSupportedDeviceFormats[4]", mic_formats)

    def test_driver_install_script_creates_the_root_enumerated_device(self) -> None:
        script = (MODULE_PATH.parent / "Build-PhoneVRMicrophone.ps1").read_text(encoding="utf-8")
        self.assertIn('"Root\\sysvad_ComponentizedAudioSample"', script)
        self.assertIn('install $packageInf.FullName $rootHardwareId', script)
        self.assertIn('ComponentizedAudioSample.inf', script)
        self.assertIn('$DevconPath', script)
        self.assertNotIn('pnputil.exe /add-driver $packageInf.FullName /install', script)

    def test_shared_ring_header_size_and_kernel_worker_contract(self) -> None:
        header = (MODULE_PATH.parent / "shared" / "PhoneVRAudioShared.h").read_text(encoding="utf-8")
        self.assertIn("32u + PHONEVR_MIC_RING_SLOT_COUNT", header)
        self.assertIn("PHONEVR_MIC_RING_SLOT Slots[PHONEVR_MIC_RING_SLOT_COUNT]", header)
        self.assertIn("MmMapViewInSystemSpace", PATCHER.MINIPORT_SECTION_IMPL)
        self.assertIn("ZwCreateSection(&sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE | SECTION_QUERY", PATCHER.MINIPORT_SECTION_IMPL)
        self.assertIn("ZwOpenSection(&sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE", PATCHER.METHODS)
        self.assertIn("ObReferenceObjectByHandle(sectionHandle, SECTION_MAP_READ | SECTION_MAP_WRITE", PATCHER.METHODS)
        helper = (MODULE_PATH.parent.parent / "windows-host" / "native-audio" / "PhoneVRAudioBridge.cpp").read_text(encoding="utf-8")
        self.assertIn("OpenFileMappingW", helper)


if __name__ == "__main__":
    unittest.main()
