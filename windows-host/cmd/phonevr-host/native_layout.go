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
	const topBar = 48
	const metricsArea = 78
	availableWidth := clientWidth - 36
	availableHeight := clientHeight - topBar - metricsArea - 22
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
	panelTop := topBar + 6
	panelBottom := clientHeight - 10
	panelHeight := panelBottom - panelTop
	top := panelTop + (panelHeight-metricsArea-size)/2
	if top < panelTop+6 {
		top = panelTop + 6
	}
	return NativePoseViewport{Left: left, Top: top, Width: size, Height: size}
}
