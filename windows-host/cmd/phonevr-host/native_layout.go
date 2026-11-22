package main

// NativePoseViewport is the square OpenGL viewport rectangle in client pixels.
type NativePoseViewport struct {
	Left, Top, Width, Height int
}

// ComputeNativePoseViewport follows the original PhoneVR dashboard composition:
// centered 1:1 model canvas, compact header and reserved telemetry footer.
func ComputeNativePoseViewport(clientWidth, clientHeight int) NativePoseViewport {
	if clientWidth <= 0 || clientHeight <= 0 {
		return NativePoseViewport{}
	}
	const (
		panelTop      = 62
		metricHeight  = 72
		metricBottom  = 22
		metricTopGap  = 16
		horizontalGap = 36
	)
	availableWidth := clientWidth - horizontalGap
	metricTop := clientHeight - metricHeight - metricBottom
	availableHeight := metricTop - metricTopGap - panelTop
	size := availableWidth
	if availableHeight < size {
		size = availableHeight
	}
	if size < 220 {
		size = 220
	}
	if size > 920 {
		size = 920
	}
	left := (clientWidth - size) / 2
	top := panelTop + (availableHeight-size)/2
	if top < panelTop {
		top = panelTop
	}
	return NativePoseViewport{Left: left, Top: top, Width: size, Height: size}
}

// NativeLayoutRect is a platform-independent rectangle used by native dialog
// layout calculations. Coordinates are client-relative pixels.
type NativeLayoutRect struct {
	Left, Top, Right, Bottom int
}

func (r NativeLayoutRect) Width() int  { return r.Right - r.Left }
func (r NativeLayoutRect) Height() int { return r.Bottom - r.Top }

// NativePresetDialogLayout keeps the optical-preset controls and their painted
// cards in one shared geometry model so they cannot drift apart.
type NativePresetDialogLayout struct {
	LeftPanel, RightPanel, FooterPanel NativeLayoutRect
	PresetLabel, PresetList            NativeLayoutRect
	NameLabel, NameEdit, Apply         NativeLayoutRect
	Edit, New, Clone, Rename, Delete   NativeLayoutRect
	Status, Close                      NativeLayoutRect
}

func ComputeNativePresetDialogLayout(width, height int) NativePresetDialogLayout {
	const margin, columnGap = 22, 24
	usable := width - 2*margin - columnGap
	if usable < 560 {
		usable = 560
	}
	leftWidth := int(float64(usable) * 0.47)
	if leftWidth < 280 {
		leftWidth = 280
	}
	if rightWidth := usable - leftWidth; rightWidth < 300 {
		leftWidth = usable - 300
	}
	if leftWidth < 260 {
		leftWidth = 260
	}
	rightX := margin + leftWidth + columnGap
	rightWidth := width - margin - rightX
	if rightWidth < 1 {
		rightWidth = 1
	}

	panelBottom := height - 104
	footerTop := height - 92
	layout := NativePresetDialogLayout{
		LeftPanel:   NativeLayoutRect{14, 80, margin + leftWidth + 8, panelBottom},
		RightPanel:  NativeLayoutRect{rightX - 8, 80, width - 14, panelBottom},
		FooterPanel: NativeLayoutRect{12, footerTop, width - 12, height - 10},
		PresetLabel: NativeLayoutRect{margin, 86, margin + leftWidth, 112},
		PresetList:  NativeLayoutRect{margin, 116, margin + leftWidth, height - 112},
		NameLabel:   NativeLayoutRect{rightX, 86, rightX + rightWidth, 110},
		NameEdit:    NativeLayoutRect{rightX, 114, rightX + rightWidth, 152},
		Apply:       NativeLayoutRect{rightX, 166, rightX + rightWidth, 206},
		Status:      NativeLayoutRect{margin, height - 70, width - margin - 140, height - 42},
		Close:       NativeLayoutRect{width - margin - 116, height - 48, width - margin, height - 10},
	}
	buttonGap := 10
	buttonWidth := (rightWidth - buttonGap) / 2
	if buttonWidth < 1 {
		buttonWidth = 1
	}
	row := func(y int) (NativeLayoutRect, NativeLayoutRect) {
		return NativeLayoutRect{rightX, y, rightX + buttonWidth, y + 40},
			NativeLayoutRect{rightX + buttonWidth + buttonGap, y, rightX + rightWidth, y + 40}
	}
	layout.Edit, layout.New = row(216)
	layout.Clone, layout.Rename = row(266)
	layout.Delete = NativeLayoutRect{rightX, 316, rightX + rightWidth, 356}
	return layout
}

// NativeAppearanceDialogLayout keeps mode buttons and the five color-only
// swatches on a single responsive geometry model. Swatches are painted by the
// dialog itself so left-click and right-click gestures are handled reliably.
type NativeAppearanceDialogLayout struct {
	Panel, FooterPanel             NativeLayoutRect
	ModeLabel, ModeDark, ModeLight NativeLayoutRect
	PaletteButtons                 [5]NativeLayoutRect
	Close                          NativeLayoutRect
}

func ComputeNativeAppearanceDialogLayout(width, height int) NativeAppearanceDialogLayout {
	const margin = 28
	controlX := margin + 170
	controlRight := width - margin
	controlWidth := controlRight - controlX
	if controlWidth < 180 {
		controlWidth = 180
		controlRight = controlX + controlWidth
	}
	buttonGap := 10
	modeButtonWidth := (controlWidth - buttonGap) / 2
	paletteGap := 10
	paletteButtonWidth := (controlWidth - paletteGap*4) / 5
	layout := NativeAppearanceDialogLayout{
		Panel:       NativeLayoutRect{16, 84, width - 16, height - 104},
		FooterPanel: NativeLayoutRect{12, height - 92, width - 12, height - 10},
		ModeLabel:   NativeLayoutRect{margin, 114, margin + 156, 150},
		ModeLight:   NativeLayoutRect{controlX, 108, controlX + modeButtonWidth, 152},
		ModeDark:    NativeLayoutRect{controlX + modeButtonWidth + buttonGap, 108, controlRight, 152},
		Close:       NativeLayoutRect{width - margin - 116, height - 54, width - margin, height - 14},
	}
	for i := range layout.PaletteButtons {
		x := controlX + i*(paletteButtonWidth+paletteGap)
		right := x + paletteButtonWidth
		if i == len(layout.PaletteButtons)-1 {
			right = controlRight
		}
		layout.PaletteButtons[i] = NativeLayoutRect{x, 198, right, 276}
	}
	return layout
}
