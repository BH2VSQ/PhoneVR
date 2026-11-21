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
