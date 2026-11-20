//go:build windows

package main

import (
	"fmt"
	"math"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	gl32     = syscall.NewLazyDLL("opengl32.dll")

	pRegisterClassEx               = user32.NewProc("RegisterClassExW")
	pCreateWindowEx                = user32.NewProc("CreateWindowExW")
	pDefWindowProc                 = user32.NewProc("DefWindowProcW")
	pGetMessage                    = user32.NewProc("GetMessageW")
	pTranslateMessage              = user32.NewProc("TranslateMessage")
	pDispatchMessage               = user32.NewProc("DispatchMessageW")
	pPostQuitMessage               = user32.NewProc("PostQuitMessage")
	pShowWindow                    = user32.NewProc("ShowWindow")
	pUpdateWindow                  = user32.NewProc("UpdateWindow")
	pDestroyWindow                 = user32.NewProc("DestroyWindow")
	pMoveWindow                    = user32.NewProc("MoveWindow")
	pSetWindowText                 = user32.NewProc("SetWindowTextW")
	pGetWindowText                 = user32.NewProc("GetWindowTextW")
	pGetWindowTextLength           = user32.NewProc("GetWindowTextLengthW")
	pSendMessage                   = user32.NewProc("SendMessageW")
	pSetTimer                      = user32.NewProc("SetTimer")
	pKillTimer                     = user32.NewProc("KillTimer")
	pInvalidateRect                = user32.NewProc("InvalidateRect")
	pGetClientRect                 = user32.NewProc("GetClientRect")
	pBeginPaint                    = user32.NewProc("BeginPaint")
	pEndPaint                      = user32.NewProc("EndPaint")
	pFillRect                      = user32.NewProc("FillRect")
	pGetModuleHandle               = kernel32.NewProc("GetModuleHandleW")
	pRtlMoveMemory                 = ntdll.NewProc("RtlMoveMemory")
	pLoadCursor                    = user32.NewProc("LoadCursorW")
	pCreateSolidBrush              = gdi32.NewProc("CreateSolidBrush")
	pCreatePen                     = gdi32.NewProc("CreatePen")
	pCreateFont                    = gdi32.NewProc("CreateFontW")
	pSelectObject                  = gdi32.NewProc("SelectObject")
	pRoundRect                     = gdi32.NewProc("RoundRect")
	pEllipse                       = gdi32.NewProc("Ellipse")
	pMoveToEx                      = gdi32.NewProc("MoveToEx")
	pLineTo                        = gdi32.NewProc("LineTo")
	pDeleteObject                  = gdi32.NewProc("DeleteObject")
	pSetTextColor                  = gdi32.NewProc("SetTextColor")
	pSetBkMode                     = gdi32.NewProc("SetBkMode")
	pSetBkColor                    = gdi32.NewProc("SetBkColor")
	pDrawText                      = user32.NewProc("DrawTextW")
	pSetProcessDPIAware            = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetWindowTheme                = syscall.NewLazyDLL("uxtheme.dll").NewProc("SetWindowTheme")
	pDwmSetWindowAttr              = syscall.NewLazyDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	pChoosePixelFormat             = gdi32.NewProc("ChoosePixelFormat")
	pSetPixelFormat                = gdi32.NewProc("SetPixelFormat")
	pSwapBuffers                   = gdi32.NewProc("SwapBuffers")
	pGetDC                         = user32.NewProc("GetDC")
	pReleaseDC                     = user32.NewProc("ReleaseDC")
	pWGLCreateContext              = gl32.NewProc("wglCreateContext")
	pWGLMakeCurrent                = gl32.NewProc("wglMakeCurrent")
	pWGLDeleteContext              = gl32.NewProc("wglDeleteContext")
	pGLClearColor                  = gl32.NewProc("glClearColor")
	pGLClear                       = gl32.NewProc("glClear")
	pGLViewport                    = gl32.NewProc("glViewport")
	pGLMatrixMode                  = gl32.NewProc("glMatrixMode")
	pGLLoadIdentity                = gl32.NewProc("glLoadIdentity")
	pGLFrustum                     = gl32.NewProc("glFrustum")
	pGLTranslatef                  = gl32.NewProc("glTranslatef")
	pGLRotatef                     = gl32.NewProc("glRotatef")
	pGLMultMatrixf                 = gl32.NewProc("glMultMatrixf")
	pGLEnable                      = gl32.NewProc("glEnable")
	pGLDisable                     = gl32.NewProc("glDisable")
	pGLCullFace                    = gl32.NewProc("glCullFace")
	pGLShadeModel                  = gl32.NewProc("glShadeModel")
	pGLColorMaterial               = gl32.NewProc("glColorMaterial")
	pGLLightfv                     = gl32.NewProc("glLightfv")
	pGLBegin                       = gl32.NewProc("glBegin")
	pGLEnd                         = gl32.NewProc("glEnd")
	pGLColor3f                     = gl32.NewProc("glColor3f")
	pGLNormal3f                    = gl32.NewProc("glNormal3f")
	pGLVertex3f                    = gl32.NewProc("glVertex3f")
	pGLLineWidth                   = gl32.NewProc("glLineWidth")
	pGLGenLists                    = gl32.NewProc("glGenLists")
	pGLNewList                     = gl32.NewProc("glNewList")
	pGLEndList                     = gl32.NewProc("glEndList")
	pGLCallList                    = gl32.NewProc("glCallList")
	pGLClearDepth                  = gl32.NewProc("glClearDepth")
	pGLBlendFunc                   = gl32.NewProc("glBlendFunc")
	pGLHint                        = gl32.NewProc("glHint")
)

const (
	wmCreate            = 0x0001
	wmDestroy           = 0x0002
	wmSize              = 0x0005
	wmPaint             = 0x000F
	wmClose             = 0x0010
	wmCommand           = 0x0111
	wmTimer             = 0x0113
	wmEraseBkgnd        = 0x0014
	wmCtlColor          = 0x0138
	wmNcDestroy         = 0x0082
	wmSizing            = 0x0214
	wmLButtonUp         = 0x0202
	wmDrawItem          = 0x002B
	wmSetFont           = 0x0030
	wsOverlapped        = 0x00000000
	wsCaption           = 0x00C00000
	wsSysMenu           = 0x00080000
	wsThickFrame        = 0x00040000
	wsMinimizeBox       = 0x00020000
	wsMaximizeBox       = 0x00010000
	wsVisible           = 0x10000000
	wsChild             = 0x40000000
	wsTabStop           = 0x00010000
	wsVScroll           = 0x00200000
	wsHScroll           = 0x00100000
	wsBorder            = 0x00800000
	wsClipSiblings      = 0x04000000
	wsClipChildren      = 0x02000000
	esAutoHScroll       = 0x0080
	esReadOnly          = 0x0800
	esMultiline         = 0x0004
	esWantReturn        = 0x1000
	bsPushButton        = 0x00000000
	bsOwnerDraw         = 0x0000000B
	cbDropDownList      = 0x0003
	lbnSelChange        = 1
	cbnSelChange        = 1
	swShow              = 5
	swHide              = 0
	idcArrow            = 32512
	colorStatic         = 0x0138
	transparent         = 1
	wmUser              = 0x0400
	wmAppRefresh        = wmUser + 17
	wmAppOpen           = wmUser + 18
	wmAppSettings       = wmUser + 19
	wmAppPreset         = wmUser + 20
	wmAppNetwork        = wmUser + 21
	wmAppLogs           = wmUser + 22
	wmAppCenter         = wmUser + 23
	wmAppQuit           = wmUser + 24
	glColorBufferBit    = 0x00004000
	glDepthBufferBit    = 0x00000100
	glDepthTest         = 0x0B71
	glCullFace          = 0x0B44
	glBack              = 0x0405
	glLighting          = 0x0B50
	glLight0            = 0x4000
	glNormalize         = 0x0BA1
	glColorMaterialCap  = 0x0B57
	glFrontAndBack      = 0x0408
	glAmbientAndDiffuse = 0x1602
	glSmooth            = 0x1D01
	glProjection        = 0x1701
	glModelview         = 0x1700
	glQuads             = 0x0007
	glLines             = 0x0001
	glTriangles         = 0x0004
	glLineSmooth        = 0x0B20
	glBlend             = 0x0BE2
	glSrcAlpha          = 0x0302
	glOneMinusSrcAlpha  = 0x0303
	glPerspectiveHint   = 0x0C50
	glNicest            = 0x1102
	glCompile           = 0x1300
	glPosition          = 0x1203
	pfdDrawToWindow     = 0x00000004
	pfdSupportOpenGL    = 0x00000020
	pfdDoubleBuffer     = 0x00000001
	pfdTypeRGBA         = 0
	pfdMainPlane        = 0
)

const (
	idCenter       = 100
	idSettings     = 101
	idPresets      = 102
	idNetwork      = 103
	idLogs         = 104
	idLanguage     = 105
	idMenu         = 106
	idProfile      = 200
	idApply        = 201
	idDialogCenter = 202
	idClose        = 203
	idPresetList   = 300
	idPresetName   = 301
	idPresetSave   = 302
	idPresetClone  = 303
	idPresetRename = 304
	idPresetDelete = 305
	idPresetApply  = 306
	idNetworkApply = 400
	idNetworkStop  = 401
	idNetworkStart = 402
	idLogsRefresh  = 500
)

type nativePoint struct{ X, Y int32 }
type nativeRect struct{ Left, Top, Right, Bottom int32 }
type nativeMsg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      nativePoint
	Private uint32
}
type nativePaintStruct struct {
	HDC       uintptr
	Erase     int32
	Paint     nativeRect
	Restore   int32
	IncUpdate int32
	Reserved  [32]byte
}
type nativeWndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSmall  uintptr
}
type nativePixelFormatDescriptor struct {
	Size           uint16
	Version        uint16
	Flags          uint32
	PixelType      byte
	ColorBits      byte
	RedBits        byte
	RedShift       byte
	GreenBits      byte
	GreenShift     byte
	BlueBits       byte
	BlueShift      byte
	AlphaBits      byte
	AlphaShift     byte
	AccumBits      byte
	AccumRedBits   byte
	AccumGreenBits byte
	AccumBlueBits  byte
	AccumAlphaBits byte
	DepthBits      byte
	StencilBits    byte
	AuxBuffers     byte
	LayerType      byte
	Reserved       byte
	LayerMask      uint32
	VisibleMask    uint32
	DamageMask     uint32
}

type nativeHitTarget struct {
	ID   int
	Rect nativeRect
}

type nativeDialog struct {
	kind            string
	hwnd            uintptr
	controls        map[int]uintptr
	status          uintptr
	settings        PhoneDisplaySettings
	pendingSettings *PhoneDisplaySettings
	presets         []OpticalPreset
	config          HostConfig
}
type nativeUI struct {
	service          *HostService
	config           HostConfig
	mainHWnd         uintptr
	glHWnd           uintptr
	glDC             uintptr
	glRC             uintptr
	modelList        uintptr
	model            *nativeHeadsetModel
	pose             PoseFrameSnapshot
	snapshot         HostSnapshot
	statusHost       uintptr
	statusSteam      uintptr
	statusHMD        uintptr
	battery          uintptr
	yaw              uintptr
	pitch            uintptr
	roll             uintptr
	buttons          map[int]uintptr
	dialogs          map[uintptr]*nativeDialog
	currentSettings  PhoneDisplaySettings
	language         string
	languageButton   uintptr
	background       uintptr
	dialogBackground uintptr
	editBackground   uintptr
	buttonBackground uintptr
	fontBody         uintptr
	fontSmall        uintptr
	fontValue        uintptr
	gdiplusToken     uintptr
	hitTargets       []nativeHitTarget
	menuOpen         bool
	lastRender       time.Time
	lastStatus       time.Time
}

var activeNativeUI *nativeUI
var nativeWndProcCallback = syscall.NewCallback(nativeMainWndProc)
var nativeGLProcCallback = syscall.NewCallback(nativeGLWndProc)
var nativeDialogProcCallback = syscall.NewCallback(nativeDialogWndProc)

func runNativeDesktopApp(service *HostService, config HostConfig) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if result, _, _ := pSetProcessDpiAwarenessContext.Call(^uintptr(3)); result == 0 {
		pSetProcessDPIAware.Call()
	}
	app := &nativeUI{service: service, config: config, dialogs: make(map[uintptr]*nativeDialog), buttons: make(map[int]uintptr), currentSettings: DefaultPhoneDisplaySettings(), language: loadNativeLanguage()}
	app.fontBody = createNativeFont("Segoe UI", 14, 400)
	app.fontSmall = createNativeFont("Segoe UI", 11, 600)
	app.fontValue = createNativeFont("Consolas", 21, 700)
	app.gdiplusToken = startNativeGDIPlus()
	if model, err := loadNativeHeadsetModel(); err == nil {
		app.model = model
	} else {
		service.addLog("HMD mesh unavailable: " + err.Error())
	}
	activeNativeUI = app
	defer func() {
		activeNativeUI = nil
		for _, object := range []uintptr{app.background, app.dialogBackground, app.editBackground, app.buttonBackground, app.fontBody, app.fontSmall, app.fontValue} {
			if object != 0 {
				pDeleteObject.Call(object)
			}
		}
		if app.gdiplusToken != 0 {
			shutdownNativeGDIPlus(app.gdiplusToken)
		}
	}()
	instance, _, _ := pGetModuleHandle.Call(0)
	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	app.background, _, _ = pCreateSolidBrush.Call(rgb(9, 17, 27))
	app.dialogBackground, _, _ = pCreateSolidBrush.Call(rgb(16, 29, 42))
	app.editBackground, _, _ = pCreateSolidBrush.Call(rgb(9, 21, 33))
	app.buttonBackground, _, _ = pCreateSolidBrush.Call(rgb(20, 38, 56))
	if err := registerNativeClass(instance, cursor, "PhoneVRNativeMain", nativeWndProcCallback, app.background); err != nil {
		return err
	}
	if err := registerNativeClass(instance, cursor, "PhoneVRNativeGL", nativeGLProcCallback, app.background); err != nil {
		return err
	}
	if err := registerNativeClass(instance, cursor, "PhoneVRNativeDialog", nativeDialogProcCallback, app.background); err != nil {
		return err
	}
	title, _ := syscall.UTF16PtrFromString("PhoneVR Host " + appVersion)
	class, _ := syscall.UTF16PtrFromString("PhoneVRNativeMain")
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsThickFrame | wsVisible | wsClipChildren)
	hwnd, _, callErr := pCreateWindowEx.Call(0x00040000, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), style,
		0x80000000, 0x80000000, 1024, 853, 0, 0, instance, 0)
	if hwnd == 0 {
		return fmt.Errorf("create native PhoneVR Host window: %w", callErr)
	}
	app.mainHWnd = hwnd
	app.applyDarkWindow(hwnd)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	var message nativeMsg
	for {
		result, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&message)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
	return nil
}

func registerNativeClass(instance, cursor uintptr, name string, callback, background uintptr) error {
	className, _ := syscall.UTF16PtrFromString(name)
	class := nativeWndClassEx{Size: uint32(unsafe.Sizeof(nativeWndClassEx{})), Style: 0x0002 | 0x0001, WndProc: callback,
		Instance: instance, Cursor: cursor, Background: background, ClassName: className}
	atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&class)))
	if atom == 0 {
		return fmt.Errorf("register native window class %s: %w", name, err)
	}
	return nil
}

func constrainNativeWindowRect(rect *nativeRect, edge int) {
	if rect == nil {
		return
	}
	const ratio = 1.2
	width := int(rect.Right - rect.Left)
	height := int(rect.Bottom - rect.Top)
	if width < 1 || height < 1 {
		return
	}
	switch edge {
	case 1, 2: // left or right: width is primary
		targetHeight := int(math.Round(float64(width) / ratio))
		if targetHeight < 640 {
			targetHeight = 640
			width = int(math.Round(float64(targetHeight) * ratio))
		}
		centerY := int(rect.Top+rect.Bottom) / 2
		rect.Top = int32(centerY - targetHeight/2)
		rect.Bottom = rect.Top + int32(targetHeight)
		if edge == 1 {
			rect.Left = rect.Right - int32(width)
		} else {
			rect.Right = rect.Left + int32(width)
		}
	case 3, 6: // top or bottom: height is primary
		targetWidth := int(math.Round(float64(height) * ratio))
		if targetWidth < 768 {
			targetWidth = 768
			height = int(math.Round(float64(targetWidth) / ratio))
		}
		centerX := int(rect.Left+rect.Right) / 2
		rect.Left = int32(centerX - targetWidth/2)
		rect.Right = rect.Left + int32(targetWidth)
		if edge == 3 {
			rect.Top = rect.Bottom - int32(height)
		} else {
			rect.Bottom = rect.Top + int32(height)
		}
	case 4, 7: // top-left or bottom-left; keep the opposite right edge
		targetWidth := int(math.Round(float64(height) * ratio))
		if targetWidth < 768 {
			targetWidth = 768
			height = int(math.Round(float64(targetWidth) / ratio))
		}
		rect.Left = rect.Right - int32(targetWidth)
		if edge == 4 {
			rect.Top = rect.Bottom - int32(height)
		} else {
			rect.Bottom = rect.Top + int32(height)
		}
	case 5, 8: // top-right or bottom-right; keep the opposite left edge
		targetWidth := int(math.Round(float64(height) * ratio))
		if targetWidth < 768 {
			targetWidth = 768
			height = int(math.Round(float64(targetWidth) / ratio))
		}
		rect.Right = rect.Left + int32(targetWidth)
		if edge == 5 {
			rect.Top = rect.Bottom - int32(height)
		} else {
			rect.Bottom = rect.Top + int32(height)
		}
	}
}

// constrainNativeWindowRectAddress copies the WM_SIZING RECT through the
// Win32 RtlMoveMemory API. lParam is an address supplied by Windows. Keeping
// it as uintptr avoids converting an arbitrary uintptr back to unsafe.Pointer,
// which go vet correctly warns about. The local RECT remains alive throughout
// both native memory copies.
func constrainNativeWindowRectAddress(rectAddress uintptr, edge int) {
	if rectAddress == 0 {
		return
	}
	var rect nativeRect
	pRtlMoveMemory.Call(uintptr(unsafe.Pointer(&rect)), rectAddress, unsafe.Sizeof(rect))
	constrainNativeWindowRect(&rect, edge)
	pRtlMoveMemory.Call(rectAddress, uintptr(unsafe.Pointer(&rect)), unsafe.Sizeof(rect))
	runtime.KeepAlive(&rect)
}

func nativeMainWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	app := activeNativeUI
	if app == nil {
		result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	switch message {
	case wmCreate:
		app.createMainControls(hwnd)
		pSetTimer.Call(hwnd, 1, 33, 0)
		return 0
	case wmSize:
		app.layoutMain(hwnd)
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmPaint:
		var paint nativePaintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		app.paintMain(hdc)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	case wmEraseBkgnd:
		return 1
	case wmLButtonUp:
		app.handleMainClick(int(int16(lParam&0xffff)), int(int16((lParam>>16)&0xffff)))
		return 0
	case wmSizing:
		constrainNativeWindowRectAddress(lParam, int(wParam))
		return 1
	case wmTimer:
		app.refreshPoseFrame()
		if time.Since(app.lastStatus) >= 500*time.Millisecond {
			app.refreshStatusSnapshot()
			app.updateStatusControls()
			app.updatePendingSettingsAcks()
			app.lastStatus = time.Now()
		}
		if app.glHWnd != 0 {
			pInvalidateRect.Call(app.glHWnd, 0, 0)
		}
		// The static dashboard is redrawn only when status data changes (500 ms),
		// never on every pose tick. This avoids continuous GDI erase/repaint flicker.
		return 0
	case wmCommand:
		app.handleMainCommand(int(wParam & 0xffff))
		return 0
	case 0x0132, 0x0133, 0x0134, 0x0135, 0x0136, wmCtlColor:
		return app.controlColor(wParam, message)
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		pKillTimer.Call(hwnd, 1)
		app.service.Stop()
		pPostQuitMessage.Call(0)
		return 0
	}
	result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func nativeGLWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	app := activeNativeUI
	if app == nil {
		result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	switch message {
	case wmCreate:
		app.glHWnd = hwnd
		app.initOpenGL(hwnd)
		return 0
	case wmPaint:
		var paint nativePaintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		app.renderOpenGL(hwnd, hdc)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&paint)))
		return 0
	case wmEraseBkgnd:
		return 1
	case wmDestroy:
		app.destroyOpenGL()
		return 0
	}
	result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func nativeDialogWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	app := activeNativeUI
	if app == nil {
		result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
		return result
	}
	switch message {
	case wmCommand:
		if dialog := app.dialogs[hwnd]; dialog != nil {
			app.handleDialogCommand(dialog, int(wParam&0xffff))
		}
		return 0
	case wmDrawItem:
		return app.drawDialogButton(lParam)
	case 0x0132, 0x0133, 0x0134, 0x0135, 0x0136, wmCtlColor:
		return app.controlColor(wParam, message)
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmNcDestroy:
		delete(app.dialogs, hwnd)
	}
	result, _, _ := pDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (app *nativeUI) createMainControls(hwnd uintptr) {
	// Main window is fully custom painted to preserve the original PhoneVR
	// dark dashboard style without browser or legacy common-control chrome.
	app.glHWnd = 0
	instance, _, _ := pGetModuleHandle.Call(0)
	class, _ := syscall.UTF16PtrFromString("PhoneVRNativeGL")
	glHWnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0,
		uintptr(wsChild|wsVisible|wsClipSiblings|wsClipChildren), 0, 0, 640, 640, hwnd, 0, instance, 0)
	app.glHWnd = glHWnd
	app.refreshPoseFrame()
	app.refreshStatusSnapshot()
	app.lastStatus = time.Now()
	app.layoutMain(hwnd)
}

func (app *nativeUI) createControl(parent uintptr, class, text string, style uint32, x, y, w, h int, id int) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString(class)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), 0, 0)
	if hwnd != 0 && app.fontBody != 0 {
		pSendMessage.Call(hwnd, wmSetFont, app.fontBody, 1)
	}
	if hwnd != 0 {
		app.applyDarkControl(hwnd)
	}
	return hwnd
}
func (app *nativeUI) createButton(parent uintptr, text string, id int) uintptr {
	return app.createControl(parent, "BUTTON", text, wsChild|wsVisible|wsTabStop|bsOwnerDraw, 0, 0, 90, 30, id)
}

func (app *nativeUI) layoutMain(hwnd uintptr) {
	var rect nativeRect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	w, h := int(rect.Right), int(rect.Bottom)
	if w <= 0 || h <= 0 {
		return
	}
	viewport := ComputeNativePoseViewport(w, h)
	if app.menuOpen {
		// Reserve a right-side rail for the menu so the OpenGL child cannot cover it.
		viewport = ComputeNativePoseViewport(w-240, h)
		viewport.Left += 0
	}
	pMoveWindow.Call(app.glHWnd, uintptr(viewport.Left), uintptr(viewport.Top), uintptr(viewport.Width), uintptr(viewport.Height), 1)
	pInvalidateRect.Call(hwnd, 0, 0)
}

func (app *nativeUI) refreshPoseFrame() {
	app.pose = app.service.PoseFrame()
}
func (app *nativeUI) refreshStatusSnapshot() {
	app.snapshot = app.service.Snapshot()
	if len(app.snapshot.ConnectedDevices) > 0 && app.snapshot.ConnectedDevices[len(app.snapshot.ConnectedDevices)-1].DisplaySettings != nil {
		app.currentSettings = *app.snapshot.ConnectedDevices[len(app.snapshot.ConnectedDevices)-1].DisplaySettings
	}
}
func (app *nativeUI) updateStatusControls() {
	if app.mainHWnd != 0 {
		pInvalidateRect.Call(app.mainHWnd, 0, 0)
	}
}

func setNativeText(hwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	pSetWindowText.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

func (app *nativeUI) handleMainCommand(id int) {
	switch id {
	case idCenter:
		if err := app.service.SendPhoneAction("center_view"); err != nil {
			app.messageBox(err.Error(), "PhoneVR")
		}
	case idSettings:
		app.openDialog("settings")
	case idPresets:
		app.openDialog("presets")
	case idNetwork:
		app.openDialog("network")
	case idLogs:
		app.openDialog("logs")
	case idLanguage:
		app.toggleLanguage()
	}
}

func (app *nativeUI) openDialog(kind string) {
	for _, existing := range app.dialogs {
		if existing.kind == kind {
			pShowWindow.Call(existing.hwnd, swShow)
			return
		}
	}
	width, height, title := 600, 470, "PhoneVR"
	switch kind {
	case "settings":
		width, height, title = 800, 500, app.text("手机显示参数", "Phone Display Settings")
	case "presets":
		width, height, title = 700, 450, app.text("光学预设", "Optical Presets")
	case "network":
		width, height, title = 520, 350, app.text("网络设置", "Network")
	case "logs":
		width, height, title = 780, 520, app.text("运行日志", "Logs")
	}
	instance, _, _ := pGetModuleHandle.Call(0)
	class, _ := syscall.UTF16PtrFromString("PhoneVRNativeDialog")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	hwnd, _, err := pCreateWindowEx.Call(0x00000001, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(titlePtr)), uintptr(wsOverlapped|wsCaption|wsSysMenu|wsVisible|wsClipChildren), 0x80000000, 0x80000000, uintptr(width), uintptr(height), app.mainHWnd, 0, instance, 0)
	if hwnd == 0 {
		app.messageBox(fmt.Sprintf("无法打开窗口：%v", err), "PhoneVR")
		return
	}
	dialog := &nativeDialog{kind: kind, hwnd: hwnd, controls: make(map[int]uintptr), settings: app.currentSettings, config: app.config}
	app.dialogs[hwnd] = dialog
	app.applyDarkWindow(hwnd)
	app.createDialogControls(dialog)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
}

func (app *nativeUI) createDialogControls(d *nativeDialog) {
	addLabel := func(text string, x, y, w, h int) {
		app.createControl(d.hwnd, "STATIC", text, wsChild|wsVisible, x, y, w, h, 0)
	}
	addEdit := func(id int, text string, x, y, w, h int) {
		d.controls[id] = app.createControl(d.hwnd, "EDIT", text, wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll, x, y, w, h, id)
	}
	addButton := func(id int, text string, x, y, w, h int) {
		d.controls[id] = app.createButton(d.hwnd, text, id)
		pMoveWindow.Call(d.controls[id], uintptr(x), uintptr(y), uintptr(w), uintptr(h), 1)
	}
	switch d.kind {
	case "settings":
		addLabel(app.text("光学配置", "Viewer Profile"), 16, 16, 100, 22)
		combo := app.createControl(d.hwnd, "COMBOBOX", "", wsChild|wsVisible|wsTabStop|cbDropDownList|wsVScroll, 120, 12, 240, 220, idProfile)
		d.controls[idProfile] = combo
		app.addComboItem(combo, app.text("自定义", "Custom"))
		app.addComboItem(combo, "Daydream View 2017")
		profileIndex := uintptr(0)
		if d.settings.ViewerProfileID == "daydream_view_2017" {
			profileIndex = 1
		}
		pSendMessage.Call(combo, 0x014E, profileIndex, 0)
		fields := []struct {
			id    int
			label string
			value float32
		}{{1001, app.text("IPD (mm)", "IPD (mm)"), d.settings.IPDMm}, {1002, app.text("镜片中心距 (mm)", "Lens spacing (mm)"), d.settings.LensSeparationMm}, {1003, app.text("屏幕至镜片 (mm)", "Screen to lens (mm)"), d.settings.ScreenToLensDistanceMm}, {1004, app.text("托盘偏移 (mm)", "Tray offset (mm)"), d.settings.TrayToLensCenterMm}, {1005, app.text("镜片中心 X", "Lens center X"), d.settings.LensCenterX}, {1006, app.text("镜片中心 Y", "Lens center Y"), d.settings.LensCenterY}, {1007, app.text("畸变 K1", "Distortion K1"), d.settings.DistortionK1}, {1008, app.text("畸变 K2", "Distortion K2"), d.settings.DistortionK2}, {1009, app.text("模糊度", "Blur"), d.settings.Blur}, {1010, app.text("锐化", "Sharpness"), d.settings.Sharpness}, {1011, app.text("FOV (deg)", "FOV (deg)"), d.settings.FOVDegrees}}
		for index, field := range fields {
			column := index / 6
			row := index % 6
			x := 18 + column*382
			y := 48 + row*54
			addLabel(field.label, x, y, 354, 20)
			addEdit(field.id, strconv.FormatFloat(float64(field.value), 'f', 3, 32), x, y+22, 354, 26)
		}
		d.status = app.createControl(d.hwnd, "STATIC", "", wsChild|wsVisible, 18, 386, 740, 24, 0)
		addButton(idApply, app.text("推送到手机", "Apply to Phone"), 18, 424, 140, 34)
		addButton(idDialogCenter, app.text("视角居中", "Center View"), 170, 424, 120, 34)
		addButton(idClose, app.text("关闭", "Close"), 658, 424, 100, 34)
	case "presets":
		addLabel(app.text("配置预设", "Optical Presets"), 16, 14, 120, 20)
		d.presets = loadNativePresets()
		list := app.createControl(d.hwnd, "LISTBOX", "", wsChild|wsVisible|wsBorder|wsVScroll|wsTabStop, 16, 42, 300, 300, idPresetList)
		d.controls[idPresetList] = list
		for _, preset := range d.presets {
			app.addListItem(list, app.presetDisplayName(preset))
		}
		pSendMessage.Call(list, 0x0186, 0, 0)
		addLabel(app.text("预设名称", "Preset Name"), 338, 46, 180, 20)
		addEdit(idPresetName, "", 338, 70, 240, 26)
		addButton(idPresetApply, app.text("应用到手机", "Apply to Phone"), 338, 112, 130, 30)
		addButton(idPresetSave, app.text("保存当前参数", "Save Current"), 338, 150, 130, 30)
		addButton(idPresetClone, app.text("克隆", "Clone"), 338, 188, 100, 30)
		addButton(idPresetRename, app.text("重命名", "Rename"), 446, 188, 100, 30)
		addButton(idPresetDelete, app.text("删除", "Delete"), 338, 226, 100, 30)
		d.status = app.createControl(d.hwnd, "STATIC", "", wsChild|wsVisible, 16, 360, 560, 26, 0)
		addButton(idClose, app.text("关闭", "Close"), 490, 360, 100, 30)
	case "network":
		addLabel(app.text("监听地址", "Bind Address"), 16, 20, 100, 22)
		addEdit(410, "0.0.0.0", 140, 16, 250, 26)
		addLabel(app.text("发现 UDP 端口", "Discovery UDP Port"), 16, 66, 120, 22)
		addEdit(411, strconv.Itoa(d.config.DiscoveryPort), 140, 62, 250, 26)
		addLabel(app.text("控制 TCP 端口", "Control TCP Port"), 16, 112, 120, 22)
		addEdit(412, strconv.Itoa(d.config.ControlPort), 140, 108, 250, 26)
		addLabel(app.text("姿态 UDP 端口", "Pose UDP Port"), 16, 158, 120, 22)
		addEdit(413, strconv.Itoa(d.config.PosePort), 140, 154, 250, 26)
		d.status = app.createControl(d.hwnd, "STATIC", "", wsChild|wsVisible, 16, 214, 400, 24, 0)
		addButton(idNetworkApply, app.text("应用并重启", "Apply & Restart"), 16, 254, 130, 32)
		addButton(idNetworkStop, app.text("停止服务", "Stop Service"), 156, 254, 110, 32)
		addButton(idNetworkStart, app.text("启动服务", "Start Service"), 276, 254, 110, 32)
		addButton(idClose, app.text("关闭", "Close"), 330, 300, 100, 30)
	case "logs":
		edit := app.createControl(d.hwnd, "EDIT", "", wsChild|wsVisible|wsBorder|wsVScroll|esMultiline|esReadOnly|esWantReturn, 12, 12, 680, 420, 501)
		d.controls[501] = edit
		addButton(idLogsRefresh, app.text("刷新", "Refresh"), 12, 445, 90, 30)
		addButton(idClose, app.text("关闭", "Close"), 600, 445, 90, 30)
		app.refreshLogDialog(d)
	}
}

func (app *nativeUI) handleDialogCommand(d *nativeDialog, id int) {
	switch d.kind {
	case "settings":
		switch id {
		case idApply:
			settings, err := app.readSettingsDialog(d)
			if err != nil {
				setNativeText(d.status, err.Error())
				return
			}
			if err := app.service.SendPhoneDisplaySettings(settings); err != nil {
				setNativeText(d.status, "推送失败："+err.Error())
				return
			}
			app.currentSettings = settings
			d.settings = settings
			pending := settings
			d.pendingSettings = &pending
			setNativeText(d.status, app.text("参数已发送，等待手机确认", "Sent; waiting for phone confirmation"))
		case idDialogCenter:
			if err := app.service.SendPhoneAction("center_view"); err != nil {
				setNativeText(d.status, err.Error())
			} else {
				setNativeText(d.status, app.text("已发送视角居中指令", "Center view command sent"))
			}
		case idClose:
			pDestroyWindow.Call(d.hwnd)
		}
	case "presets":
		switch id {
		case idPresetApply:
			index := int(sendNativeMessage(d.controls[idPresetList], 0x0188, 0, 0))
			if index < 0 || index >= len(d.presets) {
				setNativeText(d.status, "请选择预设")
				return
			}
			settings := d.presets[index].Settings.Normalized()
			if err := app.service.SendPhoneDisplaySettings(settings); err != nil {
				setNativeText(d.status, "推送失败："+err.Error())
				return
			}
			app.currentSettings = settings
			pending := settings
			d.pendingSettings = &pending
			setNativeText(d.status, app.text("预设已发送，等待手机确认", "Preset sent; waiting for phone confirmation"))
		case idPresetSave, idPresetClone:
			name := strings.TrimSpace(getNativeText(d.controls[idPresetName]))
			if name == "" {
				setNativeText(d.status, "请填写预设名称")
				return
			}
			preset := OpticalPreset{ID: newSessionID(), Name: name, Settings: app.currentSettings.Normalized()}
			if id == idPresetClone {
				index := int(sendNativeMessage(d.controls[idPresetList], 0x0188, 0, 0))
				if index < 0 || index >= len(d.presets) {
					setNativeText(d.status, "请选择需要克隆的预设")
					return
				}
				preset.Settings = d.presets[index].Settings
				preset.Name = name
			}
			d.presets = append(d.presets, preset)
			app.persistNativePresets(d)
			app.refreshPresetList(d)
			setNativeText(d.status, "预设已保存")
		case idPresetRename:
			index := int(sendNativeMessage(d.controls[idPresetList], 0x0188, 0, 0))
			if index < 0 || index >= len(d.presets) {
				setNativeText(d.status, "请选择预设")
				return
			}
			if d.presets[index].Builtin {
				setNativeText(d.status, "内置预设不能重命名，请先克隆")
				return
			}
			name := strings.TrimSpace(getNativeText(d.controls[idPresetName]))
			if name == "" {
				setNativeText(d.status, "请填写新名称")
				return
			}
			d.presets[index].Name = name
			app.persistNativePresets(d)
			app.refreshPresetList(d)
			setNativeText(d.status, "预设已重命名")
		case idPresetDelete:
			index := int(sendNativeMessage(d.controls[idPresetList], 0x0188, 0, 0))
			if index < 0 || index >= len(d.presets) {
				setNativeText(d.status, "请选择预设")
				return
			}
			if d.presets[index].Builtin {
				setNativeText(d.status, "内置预设不能删除")
				return
			}
			d.presets = append(d.presets[:index], d.presets[index+1:]...)
			app.persistNativePresets(d)
			app.refreshPresetList(d)
			setNativeText(d.status, "预设已删除")
		case idClose:
			pDestroyWindow.Call(d.hwnd)
		}
	case "network":
		switch id {
		case idNetworkApply, idNetworkStart:
			config, err := app.readNetworkDialog(d)
			if err != nil {
				setNativeText(d.status, err.Error())
				return
			}
			app.service.Stop()
			if err = app.service.Start(config); err != nil {
				setNativeText(d.status, "启动失败："+err.Error())
				return
			}
			app.config = config
			d.config = config
			setNativeText(d.status, app.text("Host 已重新启动", "Host restarted"))
		case idNetworkStop:
			app.service.Stop()
			setNativeText(d.status, app.text("Host 已停止", "Host stopped"))
		case idClose:
			pDestroyWindow.Call(d.hwnd)
		}
	case "logs":
		if id == idLogsRefresh {
			app.refreshLogDialog(d)
		} else if id == idClose {
			pDestroyWindow.Call(d.hwnd)
		}
	}
}

func (app *nativeUI) readSettingsDialog(d *nativeDialog) (PhoneDisplaySettings, error) {
	s := d.settings
	fields := []struct {
		id     int
		target *float32
	}{{1001, &s.IPDMm}, {1002, &s.LensSeparationMm}, {1003, &s.ScreenToLensDistanceMm}, {1004, &s.TrayToLensCenterMm}, {1005, &s.LensCenterX}, {1006, &s.LensCenterY}, {1007, &s.DistortionK1}, {1008, &s.DistortionK2}, {1009, &s.Blur}, {1010, &s.Sharpness}, {1011, &s.FOVDegrees}}
	for _, field := range fields {
		raw := strings.TrimSpace(getNativeText(d.controls[field.id]))
		value, err := strconv.ParseFloat(raw, 32)
		if err != nil {
			return s, fmt.Errorf("参数 %d 格式不正确", field.id)
		}
		*field.target = float32(value)
	}
	profile := int(sendNativeMessage(d.controls[idProfile], 0x0147, 0, 0))
	if profile == 1 {
		s.ViewerProfileID = "daydream_view_2017"
	} else {
		s.ViewerProfileID = "custom"
	}
	return s.Normalized(), nil
}
func (app *nativeUI) readNetworkDialog(d *nativeDialog) (HostConfig, error) {
	config := d.config
	config.ListenAddress = strings.TrimSpace(getNativeText(d.controls[410]))
	values := []struct {
		id     int
		target *int
	}{{411, &config.DiscoveryPort}, {412, &config.ControlPort}, {413, &config.PosePort}}
	for _, field := range values {
		value, err := strconv.Atoi(strings.TrimSpace(getNativeText(d.controls[field.id])))
		if err != nil || value < 0 || value > 65535 {
			return config, fmt.Errorf("端口 %d 无效", field.id)
		}
		*field.target = value
	}
	return normalizeHostConfig(config), nil
}
func (app *nativeUI) addComboItem(hwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	pSendMessage.Call(hwnd, 0x0143, 0, uintptr(unsafe.Pointer(ptr)))
}
func (app *nativeUI) addListItem(hwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	pSendMessage.Call(hwnd, 0x0180, 0, uintptr(unsafe.Pointer(ptr)))
}
func sendNativeMessage(hwnd uintptr, message uintptr, wParam uintptr, lParam uintptr) uintptr {
	result, _, _ := pSendMessage.Call(hwnd, message, wParam, lParam)
	return result
}

func getNativeText(hwnd uintptr) string {
	if hwnd == 0 {
		return ""
	}
	length, _, _ := pGetWindowTextLength.Call(hwnd)
	buffer := make([]uint16, int(length)+1)
	pGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	return syscall.UTF16ToString(buffer)
}
func (app *nativeUI) refreshPresetList(d *nativeDialog) {
	list := d.controls[idPresetList]
	pSendMessage.Call(list, 0x0184, 0, 0)
	for _, preset := range d.presets {
		app.addListItem(list, app.presetDisplayName(preset))
	}
	if len(d.presets) > 0 {
		pSendMessage.Call(list, 0x0186, 0, 0)
	}
}
func (app *nativeUI) refreshLogDialog(d *nativeDialog) {
	snapshot := app.service.Snapshot()
	text := strings.Join(snapshot.Logs, "\r\n")
	setNativeText(d.controls[501], text)
}
func (app *nativeUI) persistNativePresets(d *nativeDialog) { _ = saveNativePresets(d.presets) }
func (app *nativeUI) messageBox(message, title string) {
	msg, _ := syscall.UTF16PtrFromString(message)
	caption, _ := syscall.UTF16PtrFromString(title)
	pMessageBox.Call(app.mainHWnd, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(caption)), 0x10)
}

func createDialogControl(parent uintptr, class, text string, style uint32, x, y, w, h, id int) uintptr {
	classPtr, _ := syscall.UTF16PtrFromString(class)
	textPtr, _ := syscall.UTF16PtrFromString(text)
	hwnd, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(classPtr)), uintptr(unsafe.Pointer(textPtr)), uintptr(style), uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, uintptr(id), 0, 0)
	return hwnd
}

var pMessageBox = user32.NewProc("MessageBoxW")

func (app *nativeUI) initOpenGL(hwnd uintptr) {
	hdc, _, _ := pGetDC.Call(hwnd)
	app.glDC = hdc
	pfd := nativePixelFormatDescriptor{Size: uint16(unsafe.Sizeof(nativePixelFormatDescriptor{})), Version: 1, Flags: pfdDrawToWindow | pfdSupportOpenGL | pfdDoubleBuffer, PixelType: pfdTypeRGBA, ColorBits: 32, DepthBits: 24, StencilBits: 8, LayerType: pfdMainPlane}
	format, _, _ := pChoosePixelFormat.Call(hdc, uintptr(unsafe.Pointer(&pfd)))
	if format == 0 {
		app.service.addLog("OpenGL pixel format selection failed")
		return
	}
	if result, _, _ := pSetPixelFormat.Call(hdc, format, uintptr(unsafe.Pointer(&pfd))); result == 0 {
		app.service.addLog("OpenGL pixel format setup failed")
		return
	}
	rc, _, _ := pWGLCreateContext.Call(hdc)
	if rc == 0 {
		app.service.addLog("OpenGL context creation failed")
		return
	}
	app.glRC = rc
	pWGLMakeCurrent.Call(hdc, rc)
	pGLEnable.Call(glDepthTest)
	pGLEnable.Call(glCullFace)
	pGLCullFace.Call(glBack)
	pGLEnable.Call(glLighting)
	pGLEnable.Call(glLight0)
	pGLEnable.Call(glNormalize)
	pGLEnable.Call(glColorMaterialCap)
	pGLShadeModel.Call(glSmooth)
	pGLColorMaterial.Call(glFrontAndBack, glAmbientAndDiffuse)
	ambient := [4]float32{0.30, 0.30, 0.32, 1}
	diffuse := [4]float32{0.88, 0.88, 0.90, 1}
	position := [4]float32{-0.5, 1, 1.5, 0}
	pGLLightfv.Call(glLight0, 0x1200, uintptr(unsafe.Pointer(&ambient[0])))
	pGLLightfv.Call(glLight0, 0x1201, uintptr(unsafe.Pointer(&diffuse[0])))
	pGLLightfv.Call(glLight0, glPosition, uintptr(unsafe.Pointer(&position[0])))
	pGLClearDepth.Call(uintptr(math.Float64bits(1)))
	if app.model != nil {
		app.modelList = app.compileModelList()
	}
	pWGLMakeCurrent.Call(0, 0)
}
func (app *nativeUI) compileModelList() uintptr {
	list, _, _ := pGLGenLists.Call(1)
	if list == 0 {
		return 0
	}
	pGLNewList.Call(list, glCompile)
	pGLBegin.Call(glTriangles)
	for _, triangle := range app.model.Triangles {
		pGLColor3f.Call(floatArg(triangle.C[0]), floatArg(triangle.C[1]), floatArg(triangle.C[2]))
		for i := 0; i < 3; i++ {
			n := triangle.N[i]
			pGLNormal3f.Call(floatArg(n.X), floatArg(n.Y), floatArg(n.Z))
			v := triangle.P[i]
			pGLVertex3f.Call(floatArg(v.X), floatArg(v.Y), floatArg(v.Z))
		}
	}
	pGLEnd.Call()
	pGLEndList.Call()
	return list
}
func floatArg(value float32) uintptr { return uintptr(math.Float32bits(value)) }
func (app *nativeUI) renderOpenGL(hwnd, hdc uintptr) {
	if app.glDC == 0 || app.glRC == 0 {
		return
	}
	pWGLMakeCurrent.Call(app.glDC, app.glRC)
	var rect nativeRect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	w, h := int(rect.Right), int(rect.Bottom)
	if w <= 0 || h <= 0 {
		pWGLMakeCurrent.Call(0, 0)
		return
	}
	pGLViewport.Call(0, 0, uintptr(w), uintptr(h))
	pGLClearColor.Call(floatArg(0.045), floatArg(0.075), floatArg(0.11), floatArg(1))
	pGLClear.Call(glColorBufferBit | glDepthBufferBit)
	pGLMatrixMode.Call(glProjection)
	pGLLoadIdentity.Call()
	fov := float64(40 * math.Pi / 180)
	near := 0.1
	far := 30.0
	top := near * math.Tan(fov/2)
	aspect := float64(w) / float64(h)
	right := top * aspect
	pGLFrustum.Call(uintptr(math.Float64bits(-right)), uintptr(math.Float64bits(right)), uintptr(math.Float64bits(-top)), uintptr(math.Float64bits(top)), uintptr(math.Float64bits(near)), uintptr(math.Float64bits(far)))
	pGLMatrixMode.Call(glModelview)
	pGLLoadIdentity.Call()
	pGLTranslatef.Call(floatArg(0), floatArg(0), floatArg(-2.8))
	pGLRotatef.Call(floatArg(14), floatArg(1), 0, 0)
	pGLRotatef.Call(floatArg(-22), 0, floatArg(1), 0)
	// Fixed world grid is rendered before the HMD-local orientation transform.
	pGLDisable.Call(glLighting)
	pGLColor3f.Call(floatArg(0.10), floatArg(0.16), floatArg(0.22))
	pGLBegin.Call(glLines)
	for i := -5; i <= 5; i++ {
		v := float32(i) * 0.22
		pGLVertex3f.Call(floatArg(v), floatArg(-0.55), floatArg(-1))
		pGLVertex3f.Call(floatArg(v), floatArg(-0.55), floatArg(1))
		pGLVertex3f.Call(floatArg(-1.1), floatArg(-0.55), floatArg(v))
		pGLVertex3f.Call(floatArg(1.1), floatArg(-0.55), floatArg(v))
	}
	pGLEnd.Call()
	if app.pose.LatestPose != nil {
		matrix := quaternionMatrixOpenGL(app.pose.LatestPose.X, app.pose.LatestPose.Y, app.pose.LatestPose.Z, app.pose.LatestPose.W)
		pGLMultMatrixf.Call(uintptr(unsafe.Pointer(&matrix[0])))
	}
	pGLEnable.Call(glLighting)
	if app.modelList != 0 {
		pGLCallList.Call(app.modelList)
	}
	pGLDisable.Call(glLighting)
	pGLLineWidth.Call(floatArg(2.4))
	pGLBegin.Call(glLines)
	// Headset-local axes: red +X, green +Y, blue +Z.
	pGLColor3f.Call(floatArg(0.95), floatArg(0.35), floatArg(0.32))
	pGLVertex3f.Call(0, 0, 0)
	pGLVertex3f.Call(floatArg(0.75), 0, 0)
	pGLColor3f.Call(floatArg(0.28), floatArg(0.92), floatArg(0.58))
	pGLVertex3f.Call(0, 0, 0)
	pGLVertex3f.Call(0, floatArg(0.75), 0)
	pGLColor3f.Call(floatArg(0.32), floatArg(0.60), floatArg(1))
	pGLVertex3f.Call(0, 0, 0)
	pGLVertex3f.Call(0, 0, floatArg(0.75))
	// Gaze ray projects from the eye/lens centre through the front (-Z).
	pGLColor3f.Call(floatArg(0.18), floatArg(0.88), floatArg(1))
	pGLVertex3f.Call(0, 0, floatArg(0.26))
	pGLVertex3f.Call(0, 0, floatArg(-1.35))
	pGLEnd.Call()
	pSwapBuffers.Call(app.glDC)
	pWGLMakeCurrent.Call(0, 0)
	app.lastRender = time.Now()
	runtime.KeepAlive(app.pose)
}
func (app *nativeUI) destroyOpenGL() {
	if app.glRC != 0 {
		pWGLMakeCurrent.Call(0, 0)
		pWGLDeleteContext.Call(app.glRC)
		app.glRC = 0
	}
	if app.glDC != 0 && app.glHWnd != 0 {
		pReleaseDC.Call(app.glHWnd, app.glDC)
		app.glDC = 0
	}
}
func quaternionMatrixOpenGL(x, y, z, w float32) [16]float32 {
	length := float32(math.Sqrt(float64(x*x + y*y + z*z + w*w)))
	if length < 1e-6 {
		w = 1
		x = 0
		y = 0
		z = 0
	} else {
		x /= length
		y /= length
		z /= length
		w /= length
	}
	return [16]float32{
		1 - 2*(y*y+z*z), 2 * (x*y + w*z), 2 * (x*z - w*y), 0,
		2 * (x*y - w*z), 1 - 2*(x*x+z*z), 2 * (y*z + w*x), 0,
		2 * (x*z + w*y), 2 * (y*z - w*x), 1 - 2*(x*x+y*y), 0,
		0, 0, 0, 1,
	}
}

func (app *nativeUI) createControlBrush() uintptr { return app.background }
func (app *nativeUI) messageBoxInfo(message string) {
	msg, _ := syscall.UTF16PtrFromString(message)
	title, _ := syscall.UTF16PtrFromString("PhoneVR Host")
	pMessageBox.Call(app.mainHWnd, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x40)
}
func (app *nativeUI) text(chinese, english string) string {
	if app.language == "en" {
		return english
	}
	return chinese
}
func (app *nativeUI) presetDisplayName(preset OpticalPreset) string {
	if preset.ID == "builtin-cardboard" {
		return app.text("通用 Cardboard", "Generic Cardboard")
	}
	return preset.Name
}
func (app *nativeUI) updatePendingSettingsAcks() {
	for _, dialog := range app.dialogs {
		if dialog.pendingSettings == nil {
			continue
		}
		for _, device := range app.snapshot.ConnectedDevices {
			if device.DisplaySettings != nil && *device.DisplaySettings == *dialog.pendingSettings {
				setNativeText(dialog.status, app.text("手机已确认应用参数", "Phone confirmed settings"))
				dialog.pendingSettings = nil
				break
			}
		}
	}
}
func (app *nativeUI) toggleLanguage() {
	if app.language == "en" {
		app.language = "zh"
	} else {
		app.language = "en"
	}
	saveNativeLanguage(app.language)
	app.menuOpen = false
	for hwnd := range app.dialogs {
		pDestroyWindow.Call(hwnd)
	}
	if app.mainHWnd != 0 {
		pInvalidateRect.Call(app.mainHWnd, 0, 0)
	}
}

func rgb(r, g, b uint8) uintptr { return uintptr(uint32(r) | uint32(g)<<8 | uint32(b)<<16) }

func parseNativePort(text string) int { v, _ := strconv.Atoi(text); return v }
