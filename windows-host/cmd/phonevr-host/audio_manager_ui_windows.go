//go:build windows

package main

import (
	"fmt"
	"strings"
)

func (app *nativeUI) openAudioManager() {
	app.openDialog("audio")
}

func (app *nativeUI) refreshAudioSettingsDialog(dialog *nativeDialog) {
	if dialog == nil || dialog.kind != "audio" {
		return
	}
	inventory, err := listWindowsAudioEndpoints()
	if err != nil {
		dialog.audioListError = err.Error()
		dialog.audioInventory = AudioEndpointInventory{Version: 1, RenderEndpoints: []AudioEndpointInfo{}, CaptureEndpoints: []AudioEndpointInfo{}}
	} else {
		dialog.audioListError = ""
		dialog.audioInventory = inventory
	}
	app.populateAudioEndpointCombos(dialog)
	app.updateAudioRoutingStatus(dialog)
	app.layoutDialog(dialog)
}

func (app *nativeUI) populateAudioEndpointCombos(dialog *nativeDialog) {
	if dialog == nil {
		return
	}
	for _, comboID := range []int{idAudioLoopback, idAudioMicRender, idAudioMicCapture} {
		if hwnd := dialog.controls[comboID]; hwnd != 0 {
			sendNativeMessage(hwnd, 0x014B, 0, 0) // CB_RESETCONTENT
		}
	}
	app.addComboItem(dialog.controls[idAudioLoopback], app.text("使用 Windows 默认播放设备", "Use Windows default playback device"))
	app.addComboItem(dialog.controls[idAudioMicRender], app.text("自动匹配 VAC / VB-CABLE 播放端", "Auto-detect VAC / VB-CABLE playback endpoint"))
	app.addComboItem(dialog.controls[idAudioMicCapture], app.text("自动匹配 VAC / VB-CABLE 录音端", "Auto-detect VAC / VB-CABLE recording endpoint"))

	for _, endpoint := range dialog.audioInventory.RenderEndpoints {
		app.addComboItem(dialog.controls[idAudioLoopback], app.audioEndpointDisplayName(endpoint))
		app.addComboItem(dialog.controls[idAudioMicRender], app.audioEndpointDisplayName(endpoint))
	}
	for _, endpoint := range dialog.audioInventory.CaptureEndpoints {
		app.addComboItem(dialog.controls[idAudioMicCapture], app.audioEndpointDisplayName(endpoint))
	}

	selectEndpoint := func(hwnd uintptr, endpoints []AudioEndpointInfo, selectedID string) {
		selection := uintptr(0)
		if selectedID != "" {
			for index, endpoint := range endpoints {
				if endpoint.ID == selectedID {
					selection = uintptr(index + 1)
					break
				}
			}
		}
		sendNativeMessage(hwnd, 0x014E, selection, 0) // CB_SETCURSEL
	}
	selectEndpoint(dialog.controls[idAudioLoopback], dialog.audioInventory.RenderEndpoints, dialog.audioRouting.PCToPhoneRenderEndpointID)
	selectEndpoint(dialog.controls[idAudioMicRender], dialog.audioInventory.RenderEndpoints, dialog.audioRouting.PhoneToPCRenderEndpointID)
	selectEndpoint(dialog.controls[idAudioMicCapture], dialog.audioInventory.CaptureEndpoints, dialog.audioRouting.PhoneToPCCaptureEndpointID)
}

func (app *nativeUI) audioEndpointDisplayName(endpoint AudioEndpointInfo) string {
	name := strings.TrimSpace(endpoint.FriendlyName)
	if endpoint.IsDefault {
		name += app.text("  [默认]", "  [Default]")
	}
	if !endpoint.Active() {
		name += app.text("  [已禁用]", "  [Disabled]")
	}
	return name
}

func audioMicRouteSelectionMade(dialog *nativeDialog) bool {
	if dialog == nil || dialog.kind != "audio" {
		return false
	}
	for _, controlID := range []int{idAudioMicRender, idAudioMicCapture} {
		if index := int(sendNativeMessage(dialog.controls[controlID], 0x0147, 0, 0)); index > 0 {
			return true
		}
	}
	return false
}

func selectedAudioEndpointID(hwnd uintptr, endpoints []AudioEndpointInfo, direction string) (string, error) {
	index := int(sendNativeMessage(hwnd, 0x0147, 0, 0)) // CB_GETCURSEL
	if index < 0 {
		return "", fmt.Errorf("%s endpoint selection is unavailable", direction)
	}
	if index == 0 {
		return "", nil
	}
	if index-1 >= len(endpoints) {
		return "", fmt.Errorf("%s endpoint selection is outside the current device list", direction)
	}
	endpoint := endpoints[index-1]
	if !endpoint.Active() {
		return "", fmt.Errorf("%s endpoint %q is disabled. Enable it in Windows Sound settings, then refresh the device list", direction, endpoint.FriendlyName)
	}
	return endpoint.ID, nil
}

func (app *nativeUI) readAudioRoutingDialog(dialog *nativeDialog) (AudioRoutingConfig, error) {
	if dialog == nil || dialog.kind != "audio" {
		return AudioRoutingConfig{}, fmt.Errorf("audio routing dialog is unavailable")
	}
	loopbackID, err := selectedAudioEndpointID(dialog.controls[idAudioLoopback], dialog.audioInventory.RenderEndpoints, app.text("PC 到手机播放源", "PC-to-phone playback source"))
	if err != nil {
		return AudioRoutingConfig{}, err
	}
	micRenderID, err := selectedAudioEndpointID(dialog.controls[idAudioMicRender], dialog.audioInventory.RenderEndpoints, app.text("手机麦克风写入端", "phone-microphone render target"))
	if err != nil {
		return AudioRoutingConfig{}, err
	}
	micCaptureID, err := selectedAudioEndpointID(dialog.controls[idAudioMicCapture], dialog.audioInventory.CaptureEndpoints, app.text("Windows 麦克风录音端", "Windows microphone capture endpoint"))
	if err != nil {
		return AudioRoutingConfig{}, err
	}
	return AudioRoutingConfig{
		Version:                    1,
		PCToPhoneRenderEndpointID:  loopbackID,
		PhoneToPCRenderEndpointID:  micRenderID,
		PhoneToPCCaptureEndpointID: micCaptureID,
	}.normalized(), nil
}

func endpointNameByID(endpoints []AudioEndpointInfo, id, fallback string) string {
	if id == "" {
		return fallback
	}
	for _, endpoint := range endpoints {
		if endpoint.ID == id {
			return endpoint.FriendlyName
		}
	}
	return fallback + " (" + id + ")"
}

func (app *nativeUI) audioRoutingStatusText(dialog *nativeDialog, prefix string) string {
	if dialog == nil {
		return prefix
	}
	config := dialog.audioRouting.normalized()
	loopbackName := endpointNameByID(dialog.audioInventory.RenderEndpoints, config.PCToPhoneRenderEndpointID, app.text("Windows 默认播放设备", "Windows default playback endpoint"))
	micRenderName := endpointNameByID(dialog.audioInventory.RenderEndpoints, config.PhoneToPCRenderEndpointID, app.text("自动匹配 VAC / VB-CABLE 播放端", "Auto-detected VAC / VB-CABLE playback endpoint"))
	micCaptureName := endpointNameByID(dialog.audioInventory.CaptureEndpoints, config.PhoneToPCCaptureEndpointID, app.text("自动匹配 VAC / VB-CABLE 录音端", "Auto-detected VAC / VB-CABLE recording endpoint"))
	lines := []string{
		prefix,
		app.text("PC → 手机：", "PC → Phone: ") + loopbackName,
		app.text("手机麦克风 → PC 写入：", "Phone microphone → PC render target: ") + micRenderName,
		app.text("手机麦克风 → PC 录音端：", "Phone microphone → PC recording endpoint: ") + micCaptureName,
		app.text("方向说明：PC → 手机使用播放设备的 WASAPI Loopback；手机 → PC 将手机麦克风 PCM 写入所选播放端，再由对应录音端提供给 VRChat / SteamVR。", "Signal direction: PC → Phone uses WASAPI loopback on a playback endpoint. Phone → PC renders phone microphone PCM to the selected playback side; its paired recording endpoint exposes the signal to VRChat / SteamVR."),
		app.text("提示：PC → 手机可选任意播放设备。手机 → PC 必须把音频写入虚拟声卡播放端，并选择它对应的录音端；不要将普通实体麦克风选作配对录音端。VAC / VB-CABLE 以外的驱动请手动选择同一条虚拟音频链路的播放端和录音端，即使它们的名称相同也不会自动配对。", "Tip: PC → Phone supports any playback device. Phone → PC requires rendering into a virtual audio playback endpoint and selecting its paired recording endpoint. Do not choose a physical microphone as the paired capture endpoint. For drivers other than VAC / VB-CABLE, manually select both endpoints from the same virtual-audio route even when their friendly names match."),
	}
	loopbackID := config.PCToPhoneRenderEndpointID
	if loopbackID == "" {
		loopbackID = dialog.audioInventory.DefaultRenderEndpointID
	}
	if loopbackID != "" && loopbackID == config.PhoneToPCRenderEndpointID {
		lines = append(lines, app.text("警告：PC → 手机 Loopback 来源与手机麦克风写入端相同，可能形成音频回送。若希望避免回声，请将 PC → 手机来源改为其他播放设备。", "Warning: The PC-to-phone loopback source and phone-microphone render target are the same endpoint. This may create feedback. Choose another playback endpoint for PC → Phone to avoid echo."))
	}
	if dialog.audioListError != "" {
		lines = append(lines, app.text("设备枚举失败：", "Device enumeration failed: ")+dialog.audioListError)
	}
	return strings.Join(lines, "\r\n")
}

func (app *nativeUI) updateAudioRoutingStatus(dialog *nativeDialog) {
	if dialog == nil || dialog.status == 0 {
		return
	}
	config, err := app.readAudioRoutingDialog(dialog)
	if err == nil {
		dialog.audioRouting = config
	}
	prefix := app.text("当前选择预览。保存后立即应用。", "Preview of current selections. Save to apply.")
	if err != nil {
		prefix = app.text("设备选择尚未完成：", "Device selection needs attention: ") + err.Error()
	}
	setNativeEditTextAndRedraw(dialog.status, app.audioRoutingStatusText(dialog, prefix))
}
