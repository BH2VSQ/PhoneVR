package main

import (
	"bufio"
	"strconv"
	"strings"
	"testing"
)

func TestNativeDaydreamModelLoadsAsSolidMesh(t *testing.T) {
	model, err := loadNativeHeadsetModel()
	if err != nil {
		t.Fatalf("load native HMD mesh: %v", err)
	}
	if len(model.Triangles) < 10000 {
		t.Fatalf("mesh triangles = %d, want a detailed solid mesh", len(model.Triangles))
	}
	for index := 0; index < len(model.Triangles); index += 997 {
		triangle := model.Triangles[index]
		if triangle.C == [3]float32{} {
			t.Fatalf("triangle %d has no material colour", index)
		}
	}
}

func TestNativePresetDefaultsIncludeDaydreamAndCustom(t *testing.T) {
	presets := nativePresetDefaults()
	if len(presets) < 2 {
		t.Fatalf("default preset count = %d", len(presets))
	}
	if presets[0].Settings.ViewerProfileID != "daydream_view_2017" {
		t.Fatalf("first preset profile = %q", presets[0].Settings.ViewerProfileID)
	}
	if presets[0].Settings.DistortionK1 < 0.43 {
		t.Fatalf("Daydream K1 = %f", presets[0].Settings.DistortionK1)
	}
}

func TestNativePresetPersistenceKeepsOnlyUserPresets(t *testing.T) {
	path := t.TempDir() + "/optical-presets.json"
	presets := append(nativePresetDefaults(), OpticalPreset{ID: "test-user", Name: "My preset", Settings: DefaultPhoneDisplaySettings()})
	if err := saveNativePresetsTo(path, presets); err != nil {
		t.Fatal(err)
	}
	loaded := loadNativePresetsFrom(path)
	if len(loaded) != 3 {
		t.Fatalf("loaded presets = %d, want 3", len(loaded))
	}
	if loaded[2].ID != "test-user" {
		t.Fatalf("loaded user preset = %q", loaded[2].ID)
	}
}

func TestNativeDaydreamOpticalAssembliesAreRecessed(t *testing.T) {
	file, err := nativeModelAssets.Open("ui/assets/daydream_view_2017.obj")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	group := ""
	foamMax, lensMax := -1e9, -1e9
	glassCount := 0
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "g":
			if len(fields) > 1 {
				group = fields[1]
			}
		case "v":
			if len(fields) < 4 {
				continue
			}
			z, parseErr := strconv.ParseFloat(fields[3], 64)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if strings.HasPrefix(group, "foam_pad_") && z > foamMax {
				foamMax = z
			}
			if strings.HasPrefix(group, "lens_") && z > lensMax {
				lensMax = z
			}
		case "o":
			if len(fields) > 1 && strings.HasPrefix(fields[1], "lens_glass_") {
				glassCount++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if glassCount != 2 {
		t.Fatalf("lens glass components = %d, want 2", glassCount)
	}
	if foamMax < -1e8 || lensMax < -1e8 {
		t.Fatalf("missing eye-side geometry: foam=%f lens=%f", foamMax, lensMax)
	}
	if lensMax > foamMax-0.04 {
		t.Fatalf("lens assemblies are not sufficiently recessed: lens max Z %.4f, face pad max Z %.4f", lensMax, foamMax)
	}
}

func TestNativePoseViewportRemainsSquareAndKeepsTelemetryFooter(t *testing.T) {
	for _, size := range [][2]int{{1024, 820}, {1440, 1200}, {800, 700}, {900, 650}} {
		viewport := ComputeNativePoseViewport(size[0], size[1])
		if viewport.Width != viewport.Height {
			t.Fatalf("viewport for %dx%d is %dx%d, want square", size[0], size[1], viewport.Width, viewport.Height)
		}
		if viewport.Left < 0 || viewport.Top < 0 || viewport.Left+viewport.Width > size[0] {
			t.Fatalf("viewport outside client bounds for %dx%d: %+v", size[0], size[1], viewport)
		}
		metricTop := size[1] - 72 - 22
		if gap := metricTop - (viewport.Top + viewport.Height); gap < 16 {
			t.Fatalf("viewport-to-telemetry gap for %dx%d = %d px, want at least 16: %+v", size[0], size[1], gap, viewport)
		}
	}
}

func TestNativePresetDialogLayoutKeepsControlsInsideCards(t *testing.T) {
	for _, size := range [][2]int{{700, 520}, {900, 660}, {1100, 760}} {
		layout := ComputeNativePresetDialogLayout(size[0], size[1])
		if layout.LeftPanel.Right >= layout.RightPanel.Left {
			t.Fatalf("preset panels overlap for %dx%d: left=%+v right=%+v", size[0], size[1], layout.LeftPanel, layout.RightPanel)
		}
		if layout.LeftPanel.Bottom >= layout.FooterPanel.Top || layout.RightPanel.Bottom >= layout.FooterPanel.Top {
			t.Fatalf("preset content overlaps footer for %dx%d: left=%+v right=%+v footer=%+v", size[0], size[1], layout.LeftPanel, layout.RightPanel, layout.FooterPanel)
		}
		widthDelta := layout.Edit.Width() - layout.New.Width()
		if widthDelta < -1 || widthDelta > 1 || layout.Clone.Width()-layout.Rename.Width() < -1 || layout.Clone.Width()-layout.Rename.Width() > 1 {
			t.Fatalf("preset action buttons are not paired evenly for %dx%d: %+v", size[0], size[1], layout)
		}
		if layout.Edit.Left != layout.Clone.Left || layout.New.Left != layout.Rename.Left {
			t.Fatalf("preset action columns are not aligned for %dx%d: %+v", size[0], size[1], layout)
		}
		if layout.New.Right != layout.NameEdit.Right || layout.Rename.Right != layout.NameEdit.Right || layout.Delete.Right != layout.NameEdit.Right {
			t.Fatalf("preset action controls do not fill the right card for %dx%d: %+v", size[0], size[1], layout)
		}
		if layout.PresetList.Left < layout.LeftPanel.Left || layout.PresetList.Right > layout.LeftPanel.Right || layout.PresetList.Bottom > layout.LeftPanel.Bottom {
			t.Fatalf("preset list escapes left card for %dx%d: %+v", size[0], size[1], layout)
		}
	}
}

func TestNativeAppearanceDialogLayoutKeepsThemeControlsAndFooterSeparated(t *testing.T) {
	for _, size := range [][2]int{{640, 500}, {660, 460}, {900, 640}} {
		layout := ComputeNativeAppearanceDialogLayout(size[0], size[1])
		if layout.Panel.Bottom >= layout.FooterPanel.Top {
			t.Fatalf("appearance panel overlaps footer for %dx%d: panel=%+v footer=%+v", size[0], size[1], layout.Panel, layout.FooterPanel)
		}
		for name, rect := range map[string]NativeLayoutRect{
			"mode": layout.Mode, "accent": layout.Accent, "palette": layout.Palette, "hint": layout.Hint,
		} {
			if rect.Left < layout.Panel.Left || rect.Right > layout.Panel.Right || rect.Bottom > layout.Panel.Bottom {
				t.Fatalf("%s control escapes appearance card for %dx%d: control=%+v panel=%+v", name, size[0], size[1], rect, layout.Panel)
			}
		}
		if layout.Apply.Bottom > layout.FooterPanel.Bottom || layout.Close.Bottom > layout.FooterPanel.Bottom {
			t.Fatalf("appearance action button escapes footer for %dx%d: %+v", size[0], size[1], layout)
		}
	}
}
