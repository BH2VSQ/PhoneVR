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

// NativeAppearanceDialogLayout uses directly clickable buttons for the mode
// selector and accent swatches. Native combo boxes are intentionally avoided
// because their popup list was not reliably interactive in the borderless
// custom dialog on some Windows versions.
type NativeAppearanceDialogLayout struct {
	Panel, FooterPanel             NativeLayoutRect
	ModeLabel, ModeDark, ModeLight NativeLayoutRect
	AccentLabel, Accent            NativeLayoutRect
	PaletteLabel                   NativeLayoutRect
	PaletteButtons                 [5]NativeLayoutRect
	Hint, Apply, Close             NativeLayoutRect
}

func ComputeNativeAppearanceDialogLayout(width, height int) NativeAppearanceDialogLayout {
	const margin = 28
	controlX := margin + 220
	controlRight := width - margin
	controlWidth := controlRight - controlX
	if controlWidth < 180 {
		controlWidth = 180
		controlRight = controlX + controlWidth
	}
	buttonGap := 8
	modeButtonWidth := (controlWidth - buttonGap) / 2
	paletteGap := 6
	paletteButtonWidth := (controlWidth - paletteGap*4) / 5
	layout := NativeAppearanceDialogLayout{
		Panel:        NativeLayoutRect{16, 84, width - 16, height - 104},
		FooterPanel:  NativeLayoutRect{12, height - 92, width - 12, height - 10},
		ModeLabel:    NativeLayoutRect{margin, 112, margin + 210, 140},
		ModeDark:     NativeLayoutRect{controlX, 106, controlX + modeButtonWidth, 146},
		ModeLight:    NativeLayoutRect{controlX + modeButtonWidth + buttonGap, 106, controlRight, 146},
		AccentLabel:  NativeLayoutRect{margin, 180, margin + 210, 208},
		Accent:       NativeLayoutRect{controlX, 174, controlRight, 212},
		PaletteLabel: NativeLayoutRect{margin, 246, margin + 210, 274},
		Hint:         NativeLayoutRect{margin, 292, width - margin, 322},
		Apply:        NativeLayoutRect{margin, height - 54, margin + 200, height - 14},
		Close:        NativeLayoutRect{width - margin - 116, height - 54, width - margin, height - 14},
	}
	for i := 0; i < len(layout.PaletteButtons); i++ {
		x := controlX + i*(paletteButtonWidth+paletteGap)
		right := x + paletteButtonWidth
		if i == len(layout.PaletteButtons)-1 {
			right = controlRight
		}
		layout.PaletteButtons[i] = NativeLayoutRect{x, 234, right, 278}
	}
	return layout
}
