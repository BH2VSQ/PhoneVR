//go:build windows

package main

import (
	"fmt"
	"math"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

var gdiplusDLL = syscall.NewLazyDLL("gdiplus.dll")
var (
	pGdipStartup              = gdiplusDLL.NewProc("GdiplusStartup")
	pGdipShutdown             = gdiplusDLL.NewProc("GdiplusShutdown")
	pGdipCreateFromHDC        = gdiplusDLL.NewProc("GdipCreateFromHDC")
	pGdipDeleteGraphics       = gdiplusDLL.NewProc("GdipDeleteGraphics")
	pGdipSetSmoothingMode     = gdiplusDLL.NewProc("GdipSetSmoothingMode")
	pGdipSetTextRenderingHint = gdiplusDLL.NewProc("GdipSetTextRenderingHint")
	pGdipCreateSolidFill      = gdiplusDLL.NewProc("GdipCreateSolidFill")
	pGdipCreatePen1           = gdiplusDLL.NewProc("GdipCreatePen1")
	pGdipCreatePath           = gdiplusDLL.NewProc("GdipCreatePath")
	pGdipAddPathArcI          = gdiplusDLL.NewProc("GdipAddPathArcI")
	pGdipClosePathFigure      = gdiplusDLL.NewProc("GdipClosePathFigure")
	pGdipFillPath             = gdiplusDLL.NewProc("GdipFillPath")
	pGdipDrawPath             = gdiplusDLL.NewProc("GdipDrawPath")
	pGdipDeletePath           = gdiplusDLL.NewProc("GdipDeletePath")
	pGdipDeleteBrush          = gdiplusDLL.NewProc("GdipDeleteBrush")
	pGdipDeletePen            = gdiplusDLL.NewProc("GdipDeletePen")
	pGdipFillEllipseI         = gdiplusDLL.NewProc("GdipFillEllipseI")
	pGdipDrawEllipseI         = gdiplusDLL.NewProc("GdipDrawEllipseI")
	pGdipDrawLineI            = gdiplusDLL.NewProc("GdipDrawLineI")
)

type gdiplusStartupInput struct {
	Version                  uint32
	DebugEventCallback       uintptr
	SuppressBackgroundThread int32
	SuppressExternalCodecs   int32
}

var activeNativeGraphics uintptr

func startNativeGDIPlus() uintptr {
	input := gdiplusStartupInput{Version: 1}
	var token uintptr
	status, _, _ := pGdipStartup.Call(uintptr(unsafe.Pointer(&token)), uintptr(unsafe.Pointer(&input)), 0)
	if status != 0 {
		return 0
	}
	return token
}

func shutdownNativeGDIPlus(token uintptr) { pGdipShutdown.Call(token) }

func beginNativeGraphics(hdc uintptr) uintptr {
	var graphics uintptr
	status, _, _ := pGdipCreateFromHDC.Call(hdc, uintptr(unsafe.Pointer(&graphics)))
	if status != 0 || graphics == 0 {
		return 0
	}
	pGdipSetSmoothingMode.Call(graphics, 4)     // SmoothingModeAntiAlias
	pGdipSetTextRenderingHint.Call(graphics, 5) // ClearTypeGridFit
	return graphics
}

func gdiplusARGB(colorRef uintptr) uint32 {
	r := uint32(colorRef & 0xff)
	g := uint32((colorRef >> 8) & 0xff)
	b := uint32((colorRef >> 16) & 0xff)
	return 0xff000000 | r<<16 | g<<8 | b
}

const (
	dtCenter         = 0x00000001
	dtVCenter        = 0x00000004
	dtSingleLine     = 0x00000020
	dtEndEllipsis    = 0x00008000
	dtNoPrefix       = 0x00000800
	cleartypeQuality = 5
	fwNormal         = 400
	fwSemiBold       = 600
	fwBold           = 700
)

func createNativeFont(face string, height int, weight int) uintptr {
	name, _ := syscall.UTF16PtrFromString(face)
	hwnd, _, _ := pCreateFont.Call(
		uintptr(^uint32(height-1)), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1, 0, 0, cleartypeQuality, 0, uintptr(unsafe.Pointer(name)),
	)
	return hwnd
}

func (app *nativeUI) applyDarkWindow(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	// The custom title bar has no native caption; this also keeps non-client
	// themed borders consistent on Windows versions that support the flag.
	value := int32(0)
	if app.themeConfig.Mode != "light" {
		value = 1
	}
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	app.applyDarkControl(hwnd)
}

func (app *nativeUI) applyDarkControl(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	theme := "DarkMode_Explorer"
	if app.themeConfig.Mode == "light" {
		// Use the explicit light Explorer theme instead of an empty theme name;
		// otherwise common controls can retain dark/system cached parts.
		theme = "Explorer"
	}
	pSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(theme))), 0)
}

func (app *nativeUI) rebuildThemeBrushes() {
	for _, brush := range []uintptr{app.background, app.dialogBackground, app.editBackground, app.buttonBackground, app.surfaceBackground} {
		if brush != 0 {
			pDeleteObject.Call(brush)
		}
	}
	p := app.palette()
	app.background, _, _ = pCreateSolidBrush.Call(p.Background)
	app.dialogBackground, _, _ = pCreateSolidBrush.Call(p.Panel)
	app.editBackground, _, _ = pCreateSolidBrush.Call(p.Input)
	app.buttonBackground, _, _ = pCreateSolidBrush.Call(p.Surface)
	app.surfaceBackground, _, _ = pCreateSolidBrush.Call(p.Surface)
}

type nativeDrawItem struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	HDC        uintptr
	ItemRect   nativeRect
	ItemData   uintptr
}

func (app *nativeUI) drawDialogButton(lParam uintptr) uintptr {
	if lParam == 0 {
		return 0
	}
	var item nativeDrawItem
	pRtlMoveMemory.Call(uintptr(unsafe.Pointer(&item)), lParam, unsafe.Sizeof(item))
	runtime.KeepAlive(&item)
	id := int(item.CtlID)
	p := app.palette()
	primary := id == idApply || id == idPresetApply || id == idNetworkApply || id == idNetworkStart
	if id == idThemeModeDark && app.themeConfig.Mode == "dark" || id == idThemeModeLight && app.themeConfig.Mode == "light" {
		primary = true
	}
	danger := id == idPresetDelete || id == idNetworkStop
	fill, border, foreground := p.Surface, p.Border, p.Text
	if primary {
		fill, border, foreground = p.Accent, p.Accent, p.AccentText
	}
	if danger {
		fill, border, foreground = p.DangerBg, p.DangerLine, p.DangerText
	}
	if item.ItemState&0x0001 != 0 && !primary && !danger {
		fill = p.Panel
	}
	drawNativeRoundRect(item.HDC, item.ItemRect, fill, border, 7)
	label := getNativeText(item.HwndItem)
	drawNativeText(item.HDC, label, nativeRect{item.ItemRect.Left + 6, item.ItemRect.Top + 1, item.ItemRect.Right - 6, item.ItemRect.Bottom - 1}, foreground, app.fontBody, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	runtime.KeepAlive(&item)
	return 1
}

func (app *nativeUI) controlColor(hdc uintptr, message uint32, child uintptr) uintptr {
	p := app.palette()
	pSetTextColor.Call(hdc, p.Text)
	switch message {
	case 0x0133, 0x0134: // EDIT / LISTBOX
		pSetBkMode.Call(hdc, 2)
		pSetBkColor.Call(hdc, p.Input)
		return app.editBackground
	case 0x0135: // BUTTON
		pSetBkMode.Call(hdc, 2)
		pSetBkColor.Call(hdc, p.Surface)
		return app.buttonBackground
	default:
		pSetBkMode.Call(hdc, transparent)
		if message == 0x0138 && child != 0 { // STATIC
			parent, _, _ := pGetParent.Call(child)
			for _, dialog := range app.dialogs {
				if dialog.viewport == parent && dialog.viewport != 0 {
					return app.surfaceBackground
				}
			}
		}
		return app.dialogBackground
	}
}

func (app *nativeUI) paintMain(hdc uintptr) {
	graphics := beginNativeGraphics(hdc)
	activeNativeGraphics = graphics
	defer func() {
		activeNativeGraphics = 0
		if graphics != 0 {
			pGdipDeleteGraphics.Call(graphics)
		}
	}()
	var client nativeRect
	pGetClientRect.Call(app.mainHWnd, uintptr(unsafe.Pointer(&client)))
	w, h := int(client.Right), int(client.Bottom)
	if w <= 0 || h <= 0 {
		return
	}
	p := app.palette()
	fillNativeRect(hdc, nativeRect{0, 0, int32(w), int32(h)}, p.Background)

	runLabel := app.text("运行中", "RUNNING")
	if !app.snapshot.Running {
		runLabel = app.text("已停止", "STOPPED")
	}
	drawNativePill(hdc, 14, 10, 122, 36, runLabel, app.snapshot.Running, !app.snapshot.Running, app.fontBody, p)
	steamText := app.text("SteamVR 未连接", "STEAMVR DISCONNECTED")
	if app.snapshot.SteamVRConnected {
		switch {
		case !app.snapshot.SteamVRHMDActive:
			steamText = app.text("SteamVR · HMD 未激活", "STEAMVR · HMD INACTIVE")
		case !app.snapshot.SteamVRDisplayActive:
			steamText = app.text("SteamVR · 显示设备未激活", "STEAMVR · DISPLAY INACTIVE")
		case !app.snapshot.SteamVRPresentCallbackSeen:
			steamText = app.text("SteamVR · 尚未调用虚拟显示 Present", "STEAMVR · NO VIRTUAL PRESENT")
		case !app.snapshot.SteamVRHasPresentedFrame:
			steamText = app.text("SteamVR · Present 已调用，等待 GPU 同步", "STEAMVR · WAITING GPU SYNC")
		case !app.snapshot.VideoEncoderReady:
			steamText = app.text("GPU 同步正常 · Host 硬件编码器未就绪", "GPU SYNC OK · HW ENCODER NOT READY")
		case len(app.snapshot.ConnectedDevices) == 0 || !app.snapshot.ConnectedDevices[0].VideoEnabled:
			steamText = app.text("画面时序正常 · 手机视频未协商", "DISPLAY OK · VIDEO NOT NEGOTIATED")
		case !app.snapshot.SteamVRCaptureBridgeReady:
			steamText = app.text("视频编码已协商 · 等待采集桥", "VIDEO NEGOTIATED · WAITING BRIDGE")
		case !app.snapshot.SteamVRCaptureResourcesReady:
			steamText = app.text("采集桥已连接 · 等待兼容的 GPU 采集纹理", "BRIDGE READY · WAITING GPU CAPTURE RESOURCES")
		case !app.snapshot.SteamVRRawFrameSent:
			steamText = app.text("GPU 采集资源就绪 · 等待首个原始帧", "GPU CAPTURE READY · WAITING RAW FRAME")
		case app.snapshot.VideoEncodedFrames == 0:
			steamText = app.text("Host 已收到原始画面 · 等待 H.264 编码输出", "RAW FRAME RECEIVED · WAITING H.264 OUTPUT")
		case app.snapshot.VideoUDPPackets == 0:
			steamText = app.text("H.264 已编码 · 等待 UDP 发送", "H.264 ENCODED · WAITING UDP SEND")
		case len(app.snapshot.ConnectedDevices) > 0 && app.snapshot.ConnectedDevices[0].VideoStage != "":
			switch app.snapshot.ConnectedDevices[0].VideoStage {
			case "negotiated":
				steamText = app.text("视频已协商 · 等待 Android 接收器", "VIDEO NEGOTIATED · WAITING ANDROID RECEIVER")
			case "waiting_surface":
				steamText = app.text("手机视频已协商 · 等待显示 Surface", "VIDEO NEGOTIATED · WAITING DISPLAY SURFACE")
			case "receiver_started":
				steamText = app.text("手机 UDP 接收器已启动 · 等待视频包", "PHONE UDP RECEIVER READY · WAITING PACKETS")
			case "udp_wait":
				steamText = app.text("手机已监听视频端口 · 尚未收到 UDP 数据", "PHONE LISTENING · NO VIDEO UDP RECEIVED")
			case "fragment_wait":
				steamText = app.text("手机已收到视频分片 · 尚未组成完整帧", "PHONE RECEIVED FRAGMENTS · NO COMPLETE FRAME")
			case "udp_foreign_source":
				steamText = app.text("手机收到了非预期来源的视频包", "PHONE RECEIVED VIDEO FROM UNEXPECTED SOURCE")
			case "receiver_error":
				steamText = app.text("手机视频接收器启动失败", "PHONE VIDEO RECEIVER FAILED")
			case "udp_first_datagram":
				steamText = app.text("视频 UDP 已到达手机 · 等待完整帧", "VIDEO UDP REACHED PHONE · WAITING FRAME")
			case "first_access_unit", "receiving":
				steamText = app.text("手机已收到视频 · 等待解码器", "PHONE RECEIVING VIDEO · WAITING DECODER")
			case "decoder_ready", "decoder_input":
				steamText = app.text("手机 H.264 解码器已启动", "PHONE H.264 DECODER ACTIVE")
			case "decoded_frame":
				steamText = app.text("手机已解码画面 · 等待屏幕显示", "PHONE DECODED · WAITING DISPLAY")
			case "surface_frame":
				steamText = app.text("双目视频正在显示", "STEREO VIDEO DISPLAYING")
			case "decoder_error":
				steamText = app.text("手机视频解码失败", "PHONE VIDEO DECODER ERROR")
			default:
				steamText = app.text("视频已发送 · 手机阶段："+app.snapshot.ConnectedDevices[0].VideoStage, "VIDEO SENT · PHONE: "+app.snapshot.ConnectedDevices[0].VideoStage)
			}
		default:
			steamText = app.text("视频 UDP 已发送 · 等待手机接收确认", "VIDEO UDP SENT · WAITING PHONE ACK")
		}
	}
	// Fit the status into the free space between the left state pill and the
	// right HMD/battery/menu group. This avoids both truncation and overlap
	// on narrower desktop windows.
	leftLimit := 152
	rightLimit := w - 272
	center := w / 2
	freeHalf := minInt(center-leftLimit, rightLimit-center)
	steamWidth := minInt(320, maxInt(180, freeHalf*2))
	drawNativeSteamPill(hdc, (w-steamWidth)/2, 10, steamWidth, 36, steamText, app.fontSmall, p)

	connected := len(app.snapshot.ConnectedDevices) > 0
	right := w - 14
	menuRect := nativeRect{int32(right - 46), 10, int32(right), 46}
	drawNativeMenuButton(hdc, menuRect, p)
	app.hitTargets = app.hitTargets[:0]
	app.hitTargets = append(app.hitTargets, nativeHitTarget{ID: idMenu, Rect: menuRect})

	batteryText := "--%"
	batteryActive, batteryWarning := false, false
	if connected && app.snapshot.ConnectedDevices[0].BatteryLevel != nil {
		level := *app.snapshot.ConnectedDevices[0].BatteryLevel
		batteryText = strconv.Itoa(level) + "%"
		if app.snapshot.ConnectedDevices[0].Charging {
			batteryText += " ⚡"
		}
		batteryActive = level > 15
		batteryWarning = level <= 15
	}
	drawNativePill(hdc, right-46-10-96, 10, 96, 36, batteryText, batteryActive, batteryWarning, app.fontBody, p)
	drawNativeHMDPill(hdc, right-46-10-96-10-96, 10, 96, 36, connected, app.fontBody, p)
	drawNativeLine(hdc, 14, 54, w-14, 54, p.Border, 1)

	panel := nativeRect{14, 62, int32(w - 14), int32(h - 12)}
	drawNativeRoundRect(hdc, panel, p.Panel, p.Border, 14)

	metricW, metricGap, metricH := 180, 12, 72
	metricTotal := metricW*3 + metricGap*2
	metricX := (w - metricTotal) / 2
	metricY := h - metricH - 22
	values := [3]string{"0.0°", "0.0°", "0.0°"}
	if app.pose.LatestPose != nil {
		values = [3]string{
			formatAngle(app.pose.LatestPose.Yaw),
			formatAngle(app.pose.LatestPose.Pitch),
			formatAngle(app.pose.LatestPose.Roll),
		}
	}
	labels := [3]string{"YAW", "PITCH", "ROLL"}
	for i := 0; i < 3; i++ {
		x := metricX + i*(metricW+metricGap)
		drawNativeRoundRect(hdc, nativeRect{int32(x), int32(metricY), int32(x + metricW), int32(metricY + metricH)}, p.Surface, p.Border, 10)
		drawNativeText(hdc, labels[i], nativeRect{int32(x + 4), int32(metricY + 6), int32(x + metricW - 4), int32(metricY + 27)}, p.Muted, app.fontSmall, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		drawNativeText(hdc, values[i], nativeRect{int32(x + 4), int32(metricY + 26), int32(x + metricW - 4), int32(metricY + 68)}, p.Accent, app.fontValue, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
	}
}

func (app *nativeUI) paintMainMenu(hdc uintptr) {
	graphics := beginNativeGraphics(hdc)
	activeNativeGraphics = graphics
	defer func() {
		activeNativeGraphics = 0
		if graphics != 0 {
			pGdipDeleteGraphics.Call(graphics)
		}
	}()
	width, height := clientSize(app.menuHWnd)
	if width <= 0 || height <= 0 {
		return
	}
	p := app.palette()
	fillNativeRect(hdc, nativeRect{0, 0, int32(width), int32(height)}, p.Background)
	drawNativeRoundRect(hdc, nativeRect{1, 1, int32(width - 1), int32(height - 1)}, p.Panel, p.Border, 14)
	drawNativeText(hdc, app.text("PhoneVR 控制中心", "PhoneVR CONTROL"), nativeRect{20, 14, int32(width - 18), 46}, p.Text, app.fontTitle, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawNativeLine(hdc, 16, 56, width-16, 56, p.Border, 1)

	for index, item := range app.mainMenuItems() {
		y := 66 + index*48
		r := nativeRect{14, int32(y), int32(width - 14), int32(y + 42)}
		drawNativeRoundRect(hdc, r, p.Surface, p.Border, 9)
		drawNativeEllipse(hdc, nativeRect{26, int32(y + 13), 42, int32(y + 29)}, p.Accent, 0)
		drawNativeText(hdc, app.text(item.Chinese, item.English), nativeRect{54, int32(y), int32(width - 28), int32(y + 42)}, p.Text, app.fontBody, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	}
}

func (app *nativeUI) handleMainClick(x, y int) {
	for index := len(app.hitTargets) - 1; index >= 0; index-- {
		target := app.hitTargets[index]
		if x < int(target.Rect.Left) || x > int(target.Rect.Right) || y < int(target.Rect.Top) || y > int(target.Rect.Bottom) {
			continue
		}
		if target.ID == idMenu {
			app.toggleMainMenu()
			return
		}
		app.closeMainMenu()
		app.handleMainCommand(target.ID)
		return
	}
	if app.menuOpen {
		app.closeMainMenu()
	}
}

func drawNativePill(hdc uintptr, x, y, width, height int, text string, active bool, warning bool, font uintptr, p nativePalette) {
	fill, border, foreground, dot := p.Panel, p.Border, p.Muted, p.Muted
	if active {
		border, foreground, dot = p.Good, p.Good, p.Good
	}
	if warning {
		border, foreground, dot = p.Warning, p.Warning, p.Warning
	}
	drawNativeRoundRect(hdc, nativeRect{int32(x), int32(y), int32(x + width), int32(y + height)}, fill, border, height/2)
	drawNativeDot(hdc, x+10, y+height/2-5, dot)
	drawNativeText(hdc, text, nativeRect{int32(x + 24), int32(y + 1), int32(x + width - 6), int32(y + height - 1)}, foreground, font, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawNativeSteamPill(hdc uintptr, x, y, width, height int, text string, font uintptr, p nativePalette) {
	drawNativeRoundRect(hdc, nativeRect{int32(x), int32(y), int32(x + width), int32(y + height)}, p.SteamBg, p.SteamLine, height/2)
	drawNativeDot(hdc, x+12, y+height/2-5, p.Accent)
	// The adaptive outer width reserves sufficient text area for long English
	// connection states instead of clipping them to the old fixed 232 px pill.
	drawNativeText(hdc, text, nativeRect{int32(x + 27), int32(y + 1), int32(x + width - 8), int32(y + height - 1)}, p.SteamText, font, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawNativeHMDPill(hdc uintptr, x, y, width, height int, online bool, font uintptr, p nativePalette) {
	drawNativePill(hdc, x, y, width, height, "HMD", online, false, font, p)
}

func drawNativeMenuButton(hdc uintptr, r nativeRect, p nativePalette) {
	drawNativeRoundRect(hdc, r, p.Surface, p.Border, 8)
	pen, _, _ := pCreatePen.Call(0, 2, p.Text)
	old, _, _ := pSelectObject.Call(hdc, pen)
	cx := int((r.Left + r.Right) / 2)
	for _, yy := range []int{int(r.Top) + 10, int(r.Top) + 17, int(r.Top) + 24} {
		pMoveToEx.Call(hdc, uintptr(cx-8), uintptr(yy), 0)
		pLineTo.Call(hdc, uintptr(cx+8), uintptr(yy))
	}
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(pen)
}

func drawNativeDot(hdc uintptr, x, y int, color uintptr) {
	drawNativeEllipse(hdc, nativeRect{int32(x - 2), int32(y - 2), int32(x + 12), int32(y + 12)}, rgb(22, 42, 43), 0)
	drawNativeEllipse(hdc, nativeRect{int32(x + 1), int32(y + 1), int32(x + 9), int32(y + 9)}, color, 0)
}

func drawNativeRoundRect(hdc uintptr, r nativeRect, fill, border uintptr, radius int) {
	if activeNativeGraphics != 0 {
		path := uintptr(0)
		if status, _, _ := pGdipCreatePath.Call(0, uintptr(unsafe.Pointer(&path))); status == 0 && path != 0 {
			d := radius * 2
			if d > int(r.Bottom-r.Top) {
				d = int(r.Bottom - r.Top)
			}
			if d > int(r.Right-r.Left) {
				d = int(r.Right - r.Left)
			}
			l, t, rr, b := int(r.Left), int(r.Top), int(r.Right), int(r.Bottom)
			pGdipAddPathArcI.Call(path, uintptr(l), uintptr(t), uintptr(d), uintptr(d), uintptr(math.Float32bits(180)), uintptr(math.Float32bits(90)))
			pGdipAddPathArcI.Call(path, uintptr(rr-d), uintptr(t), uintptr(d), uintptr(d), uintptr(math.Float32bits(270)), uintptr(math.Float32bits(90)))
			pGdipAddPathArcI.Call(path, uintptr(rr-d), uintptr(b-d), uintptr(d), uintptr(d), 0, uintptr(math.Float32bits(90)))
			pGdipAddPathArcI.Call(path, uintptr(l), uintptr(b-d), uintptr(d), uintptr(d), uintptr(math.Float32bits(90)), uintptr(math.Float32bits(90)))
			pGdipClosePathFigure.Call(path)
			brush := uintptr(0)
			pen := uintptr(0)
			pGdipCreateSolidFill.Call(uintptr(gdiplusARGB(fill)), uintptr(unsafe.Pointer(&brush)))
			pGdipCreatePen1.Call(uintptr(gdiplusARGB(border)), uintptr(math.Float32bits(1.0)), 2, uintptr(unsafe.Pointer(&pen)))
			if brush != 0 {
				pGdipFillPath.Call(activeNativeGraphics, brush, path)
				pGdipDeleteBrush.Call(brush)
			}
			if pen != 0 {
				pGdipDrawPath.Call(activeNativeGraphics, pen, path)
				pGdipDeletePen.Call(pen)
			}
			pGdipDeletePath.Call(path)
			return
		}
	}
	brush, _, _ := pCreateSolidBrush.Call(fill)
	pen, _, _ := pCreatePen.Call(0, 1, border)
	oldBrush, _, _ := pSelectObject.Call(hdc, brush)
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius*2), uintptr(radius*2))
	pSelectObject.Call(hdc, oldPen)
	pSelectObject.Call(hdc, oldBrush)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(brush)
}

func drawNativeEllipse(hdc uintptr, r nativeRect, fill, border uintptr) {
	if activeNativeGraphics != 0 {
		l, t, w, h := int(r.Left), int(r.Top), int(r.Right-r.Left), int(r.Bottom-r.Top)
		brush := uintptr(0)
		pen := uintptr(0)
		pGdipCreateSolidFill.Call(uintptr(gdiplusARGB(fill)), uintptr(unsafe.Pointer(&brush)))
		pGdipCreatePen1.Call(uintptr(gdiplusARGB(border)), uintptr(math.Float32bits(1.0)), 2, uintptr(unsafe.Pointer(&pen)))
		if brush != 0 {
			pGdipFillEllipseI.Call(activeNativeGraphics, brush, uintptr(l), uintptr(t), uintptr(w), uintptr(h))
			pGdipDeleteBrush.Call(brush)
		}
		if pen != 0 {
			pGdipDrawEllipseI.Call(activeNativeGraphics, pen, uintptr(l), uintptr(t), uintptr(w), uintptr(h))
			pGdipDeletePen.Call(pen)
		}
		return
	}
	brush, _, _ := pCreateSolidBrush.Call(fill)
	pen, _, _ := pCreatePen.Call(0, 1, border)
	oldBrush, _, _ := pSelectObject.Call(hdc, brush)
	oldPen, _, _ := pSelectObject.Call(hdc, pen)
	pEllipse.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
	pSelectObject.Call(hdc, oldPen)
	pSelectObject.Call(hdc, oldBrush)
	pDeleteObject.Call(pen)
	pDeleteObject.Call(brush)
}

func drawNativeLine(hdc uintptr, x1, y1, x2, y2 int, color uintptr, width int) {
	if activeNativeGraphics != 0 {
		pen := uintptr(0)
		pGdipCreatePen1.Call(uintptr(gdiplusARGB(color)), uintptr(math.Float32bits(float32(width))), 2, uintptr(unsafe.Pointer(&pen)))
		if pen != 0 {
			pGdipDrawLineI.Call(activeNativeGraphics, pen, uintptr(x1), uintptr(y1), uintptr(x2), uintptr(y2))
			pGdipDeletePen.Call(pen)
		}
		return
	}
	pen, _, _ := pCreatePen.Call(0, uintptr(width), color)
	old, _, _ := pSelectObject.Call(hdc, pen)
	pMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	pLineTo.Call(hdc, uintptr(x2), uintptr(y2))
	pSelectObject.Call(hdc, old)
	pDeleteObject.Call(pen)
}

func fillNativeRect(hdc uintptr, r nativeRect, color uintptr) {
	brush, _, _ := pCreateSolidBrush.Call(color)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
	pDeleteObject.Call(brush)
}

func drawNativeText(hdc uintptr, value string, r nativeRect, color uintptr, font uintptr, flags uintptr) {
	text, _ := syscall.UTF16FromString(value)
	if len(text) == 0 {
		return
	}
	oldFont := uintptr(0)
	if font != 0 {
		oldFont, _, _ = pSelectObject.Call(hdc, font)
	}
	pSetBkMode.Call(hdc, transparent)
	pSetTextColor.Call(hdc, color)
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(&text[0])), uintptr(len(text)-1), uintptr(unsafe.Pointer(&r)), flags)
	if font != 0 {
		pSelectObject.Call(hdc, oldFont)
	}
}

func formatAngle(value float64) string { return fmt.Sprintf("%+.1f°", value) }
