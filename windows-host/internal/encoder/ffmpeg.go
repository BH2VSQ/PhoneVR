// Package encoder discovers FFmpeg video encoders at runtime. It deliberately
// does not bind PhoneVR to one GPU vendor: a machine may contain several GPUs
// and the active encoder is selected from the capabilities actually exposed by
// the installed FFmpeg build and driver stack.
package encoder

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

type Codec string

const (
	CodecH264 Codec = "h264"
	CodecHEVC Codec = "hevc"
	CodecAV1  Codec = "av1"
)

type Backend string

const (
	BackendNVENC    Backend = "nvenc"
	BackendAMF      Backend = "amf"
	BackendQSV      Backend = "qsv"
	BackendSoftware Backend = "software"
)

type Capability struct {
	Name     string  `json:"name"`
	Codec    Codec   `json:"codec"`
	Backend  Backend `json:"backend"`
	Hardware bool    `json:"hardware"`
}

var encoderMap = map[string]Capability{
	"h264_nvenc": {Name: "h264_nvenc", Codec: CodecH264, Backend: BackendNVENC, Hardware: true},
	"hevc_nvenc": {Name: "hevc_nvenc", Codec: CodecHEVC, Backend: BackendNVENC, Hardware: true},
	"av1_nvenc":  {Name: "av1_nvenc", Codec: CodecAV1, Backend: BackendNVENC, Hardware: true},
	"h264_amf":   {Name: "h264_amf", Codec: CodecH264, Backend: BackendAMF, Hardware: true},
	"hevc_amf":   {Name: "hevc_amf", Codec: CodecHEVC, Backend: BackendAMF, Hardware: true},
	"av1_amf":    {Name: "av1_amf", Codec: CodecAV1, Backend: BackendAMF, Hardware: true},
	"h264_qsv":   {Name: "h264_qsv", Codec: CodecH264, Backend: BackendQSV, Hardware: true},
	"hevc_qsv":   {Name: "hevc_qsv", Codec: CodecHEVC, Backend: BackendQSV, Hardware: true},
	"av1_qsv":    {Name: "av1_qsv", Codec: CodecAV1, Backend: BackendQSV, Hardware: true},
	"libx264":    {Name: "libx264", Codec: CodecH264, Backend: BackendSoftware, Hardware: false},
	"libx265":    {Name: "libx265", Codec: CodecHEVC, Backend: BackendSoftware, Hardware: false},
	"libaom-av1": {Name: "libaom-av1", Codec: CodecAV1, Backend: BackendSoftware, Hardware: false},
	"libsvtav1":  {Name: "libsvtav1", Codec: CodecAV1, Backend: BackendSoftware, Hardware: false},
}

// ParseFFmpegEncoderList parses `ffmpeg -hide_banner -encoders` output.
// Unknown encoders are ignored so new FFmpeg builds remain forward compatible.
func ParseFFmpegEncoderList(output string) []Capability {
	seen := make(map[string]bool)
	var found []Capability
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || len(fields[0]) != 6 || fields[0][0] != 'V' {
			continue
		}
		name := fields[1]
		capability, ok := encoderMap[name]
		if !ok || seen[name] {
			continue
		}
		seen[name] = true
		found = append(found, capability)
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].Codec != found[j].Codec {
			return found[i].Codec < found[j].Codec
		}
		return backendRank(found[i].Backend) < backendRank(found[j].Backend)
	})
	return found
}

func backendRank(backend Backend) int {
	switch backend {
	case BackendNVENC:
		return 0
	case BackendAMF:
		return 1
	case BackendQSV:
		return 2
	default:
		return 3
	}
}

// ProbeFFmpeg returns encoders available in the FFmpeg executable selected by
// path. Pass an empty path to resolve ffmpeg through PATH.
func ProbeFFmpeg(ctx context.Context, path string) ([]Capability, error) {
	if path == "" {
		resolved, err := exec.LookPath("ffmpeg")
		if err != nil {
			return nil, fmt.Errorf("find FFmpeg: %w", err)
		}
		path = resolved
	}
	cmd := exec.CommandContext(ctx, path, "-hide_banner", "-encoders")
	configureFFmpegProcess(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("probe FFmpeg encoders: %w: %s", err, strings.TrimSpace(string(output)))
	}
	capabilities := ParseFFmpegEncoderList(string(output))
	if len(capabilities) == 0 {
		return nil, errors.New("FFmpeg exposes no supported H.264, HEVC, or AV1 video encoder")
	}
	return capabilities, nil
}

// SelectEncoder selects a codec implementation from the capabilities found on
// this PC. Hardware encoders are preferred; software is an explicit fallback.
func SelectEncoder(capabilities []Capability, codec Codec, preferred Backend) (Capability, bool) {
	candidates := make([]Capability, 0, len(capabilities))
	for _, candidate := range capabilities {
		if candidate.Codec == codec {
			candidates = append(candidates, candidate)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if preferred != "" {
			if candidates[i].Backend == preferred && candidates[j].Backend != preferred {
				return true
			}
			if candidates[j].Backend == preferred && candidates[i].Backend != preferred {
				return false
			}
		}
		return backendRank(candidates[i].Backend) < backendRank(candidates[j].Backend)
	})
	if len(candidates) == 0 {
		return Capability{}, false
	}
	return candidates[0], true
}
