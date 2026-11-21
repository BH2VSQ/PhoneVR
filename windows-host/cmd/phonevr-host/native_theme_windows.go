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
	// DWMWA_USE_IMMERSIVE_DARK_MODE. Unsupported versions simply ignore it.
	value := int32(1)
	pDwmSetWindowAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&value)), unsafe.Sizeof(value))
	pSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("DarkMode_Explorer"))), 0)
}

func (app *nativeUI) applyDarkControl(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	pSetWindowTheme.Call(hwnd, uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr("DarkMode_Explorer"))), 0)
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
	primary := id == idApply || id == idPresetApply || id == idNetworkApply || id == idNetworkStart
	danger := id == idPresetDelete || id == idNetworkStop
	fill, border, foreground := rgb(20, 38, 56), rgb(45, 70, 93), rgb(211, 226, 240)
	if primary {
		fill, border, foreground = rgb(91, 184, 248), rgb(91, 184, 248), rgb(7, 21, 34)
	}
	if danger {
		fill, border, foreground = rgb(64, 31, 39), rgb(111, 55, 67), rgb(255, 190, 197)
	}
	if item.ItemState&0x0001 != 0 {
		fill = rgb(28, 53, 74)
		if primary {
			fill = rgb(130, 207, 255)
		}
		if danger {
			fill = rgb(88, 39, 49)
		}
	}
	drawNativeRoundRect(item.HDC, item.ItemRect, fill, border, 7)
	label := getNativeText(item.HwndItem)
	drawNativeText(item.HDC, label, nativeRect{item.ItemRect.Left + 6, item.ItemRect.Top + 1, item.ItemRect.Right - 6, item.ItemRect.Bottom - 1}, foreground, app.fontBody, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	runtime.KeepAlive(&item)
	return 1
}

func (app *nativeUI) controlColor(hdc uintptr, message uint32) uintptr {
	pSetTextColor.Call(hdc, rgb(229, 238, 248))
	switch message {
	case 0x0133, 0x0134: // EDIT / LISTBOX
		pSetBkMode.Call(hdc, 2)
		pSetBkColor.Call(hdc, rgb(9, 21, 33))
		return app.editBackground
	case 0x0135: // BUTTON
		pSetBkMode.Call(hdc, 2)
		pSetBkColor.Call(hdc, rgb(20, 38, 56))
		return app.buttonBackground
	default:
		pSetBkMode.Call(hdc, transparent)
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
	fillNativeRect(hdc, nativeRect{0, 0, int32(w), int32(h)}, rgb(9, 17, 27))

	runLabel := app.text("运行中", "RUNNING")
	if !app.snapshot.Running {
		runLabel = app.text("已停止", "STOPPED")
	}
	drawNativePill(hdc, 14, 10, 122, 36, runLabel, app.snapshot.Running, !app.snapshot.Running, app.fontBody)
	steamText := app.text("SteamVR 未连接", "STEAMVR DISCONNECTED")
	drawNativeSteamPill(hdc, (w-232)/2, 10, 232, 36, steamText, app.fontBody)

	connected := len(app.snapshot.ConnectedDevices) > 0
	right := w - 14
	menuRect := nativeRect{int32(right - 46), 10, int32(right), 46}
	drawNativeMenuButton(hdc, menuRect)
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
	drawNativePill(hdc, right-46-10-96, 10, 96, 36, batteryText, batteryActive, batteryWarning, app.fontBody)
	drawNativeHMDPill(hdc, right-46-10-96-10-96, 10, 96, 36, connected, app.fontBody)
	drawNativeLine(hdc, 14, 54, w-14, 54, rgb(33, 52, 71), 1)

	panel := nativeRect{14, 62, int32(w - 14), int32(h - 12)}
	drawNativeRoundRect(hdc, panel, rgb(16, 29, 42), rgb(38, 59, 80), 14)

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
		drawNativeRoundRect(hdc, nativeRect{int32(x), int32(metricY), int32(x + metricW), int32(metricY + metricH)}, rgb(12, 24, 36), rgb(38, 62, 84), 10)
		drawNativeText(hdc, labels[i], nativeRect{int32(x + 4), int32(metricY + 6), int32(x + metricW - 4), int32(metricY + 27)}, rgb(119, 144, 169), app.fontSmall, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
		drawNativeText(hdc, values[i], nativeRect{int32(x + 4), int32(metricY + 26), int32(x + metricW - 4), int32(metricY + 68)}, rgb(185, 221, 249), app.fontValue, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
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
	fillNativeRect(hdc, nativeRect{0, 0, int32(width), int32(height)}, rgb(12, 22, 34))
	drawNativeRoundRect(hdc, nativeRect{1, 1, int32(width - 1), int32(height - 1)}, rgb(13, 25, 38), rgb(53, 82, 108), 14)
	drawNativeText(hdc, app.text("PhoneVR 控制中心", "PhoneVR CONTROL"), nativeRect{20, 14, int32(width - 18), 46}, rgb(222, 237, 250), app.fontTitle, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawNativeText(hdc, app.text("快速操作与主机设置", "Quick actions and host settings"), nativeRect{20, 46, int32(width - 18), 68}, rgb(126, 154, 180), app.fontSmall, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	drawNativeLine(hdc, 16, 70, width-16, 70, rgb(42, 65, 86), 1)

	for index, item := range app.mainMenuItems() {
		y := 76 + index*48
		r := nativeRect{14, int32(y), int32(width - 14), int32(y + 42)}
		drawNativeRoundRect(hdc, r, rgb(20, 38, 56), rgb(40, 64, 86), 9)
		drawNativeEllipse(hdc, nativeRect{26, int32(y + 13), 42, int32(y + 29)}, rgb(80, 177, 238), 0)
		drawNativeText(hdc, app.text(item.Chinese, item.English), nativeRect{54, int32(y), int32(width - 28), int32(y + 42)}, rgb(207, 224, 240), app.fontBody, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
	}
	drawNativeText(hdc, app.text("点击空白区域可关闭菜单", "Click outside to dismiss"), nativeRect{16, int32(height - 31), int32(width - 16), int32(height - 8)}, rgb(105, 132, 157), app.fontSmall, dtCenter|dtVCenter|dtSingleLine|dtNoPrefix)
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

func drawNativePill(hdc uintptr, x, y, width, height int, text string, active bool, warning bool, font uintptr) {
	fill, border, foreground, dot := rgb(14, 26, 39), rgb(38, 59, 80), rgb(129, 150, 173), rgb(116, 134, 154)
	if active {
		border, foreground, dot = rgb(40, 87, 68), rgb(85, 217, 160), rgb(85, 217, 160)
	}
	if warning {
		border, foreground, dot = rgb(91, 72, 45), rgb(231, 183, 110), rgb(231, 183, 110)
	}
	drawNativeRoundRect(hdc, nativeRect{int32(x), int32(y), int32(x + width), int32(y + height)}, fill, border, height/2)
	drawNativeDot(hdc, x+10, y+height/2-5, dot)
	drawNativeText(hdc, text, nativeRect{int32(x + 24), int32(y + 1), int32(x + width - 6), int32(y + height - 1)}, foreground, font, dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawNativeSteamPill(hdc uintptr, x, y, width, height int, text string, font uintptr) {
	drawNativeRoundRect(hdc, nativeRect{int32(x), int32(y), int32(x + width), int32(y + height)}, rgb(21, 21, 42), rgb(60, 58, 98), height/2)
	drawNativeDot(hdc, x+12, y+height/2-5, rgb(140, 136, 216))
	drawNativeText(hdc, text, nativeRect{int32(x + 27), int32(y + 1), int32(x + width - 8), int32(y + height - 1)}, rgb(184, 182, 239), font, dtCenter|dtVCenter|dtSingleLine|dtEndEllipsis|dtNoPrefix)
}

func drawNativeHMDPill(hdc uintptr, x, y, width, height int, online bool, font uintptr) {
	// Keep the compact HMD label and communicate state through the lamp and tint.
	drawNativePill(hdc, x, y, width, height, "HMD", online, false, font)
}

func drawNativeMenuButton(hdc uintptr, r nativeRect) {
	drawNativeRoundRect(hdc, r, rgb(20, 38, 56), rgb(44, 67, 88), 8)
	pen, _, _ := pCreatePen.Call(0, 2, rgb(185, 214, 238))
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
