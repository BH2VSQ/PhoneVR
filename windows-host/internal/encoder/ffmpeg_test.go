package encoder

import "testing"

func TestParseFFmpegEncoderListFindsMultipleVendors(t *testing.T) {
	output := ` Encoders:
 V..... libx264              libx264 H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10
 V....D h264_nvenc           NVIDIA NVENC H.264 encoder
 V....D hevc_amf             AMD AMF HEVC encoder
 V..... h264_qsv             H.264 / AVC / MPEG-4 AVC / MPEG-4 part 10 (Intel Quick Sync Video acceleration)
 A....D aac                  AAC (Advanced Audio Coding)
`
	caps := ParseFFmpegEncoderList(output)
	if len(caps) != 4 {
		t.Fatalf("got %d encoders, want 4: %#v", len(caps), caps)
	}
	selected, ok := SelectEncoder(caps, CodecH264, "")
	if !ok || selected.Name != "h264_nvenc" {
		t.Fatalf("auto selection = %#v, %v; want NVENC", selected, ok)
	}
	selected, ok = SelectEncoder(caps, CodecH264, BackendQSV)
	if !ok || selected.Name != "h264_qsv" {
		t.Fatalf("QSV preference = %#v, %v", selected, ok)
	}
}

func TestSelectEncoderFallsBackToSoftware(t *testing.T) {
	caps := ParseFFmpegEncoderList(" V..... libx264 H.264 software encoder\n")
	got, ok := SelectEncoder(caps, CodecH264, BackendNVENC)
	if !ok || got.Backend != BackendSoftware || got.Hardware {
		t.Fatalf("unexpected fallback: %#v, %v", got, ok)
	}
	if _, ok := SelectEncoder(caps, CodecHEVC, ""); ok {
		t.Fatal("unexpected HEVC encoder")
	}
}

func TestParseFFmpegEncoderListIgnoresAudioAndUnknown(t *testing.T) {
	caps := ParseFFmpegEncoderList(" A....D aac AAC\n V....D h264_unknown unknown\n")
	if len(caps) != 0 {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}
}
