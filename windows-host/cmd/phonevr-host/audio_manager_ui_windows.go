//go:build windows

package main

import (
	"fmt"
	"strings"
)

// openAudioManager presents the current Windows virtual microphone route and
// only offers VB-CABLE installation when no compatible VAC/VB-CABLE pair is
// installed. It reports disabled endpoints separately. The install is asynchronous so the native UI stays responsive
// while the vendor package downloads and UAC installer is running.
func (app *nativeUI) openAudioManager() {
	status, detected := currentVirtualCableStatus()
	if detected && isCompatibleVirtualCableStatus(status) {
		if !status.MicrophoneRouteActive {
			message := fmt.Sprintf(
				"已发现已安装的虚拟音频线缆，但播放端或录音端当前处于禁用状态。\n\n提供程序：%s\n手机麦克风播放端：%s\nWindows 录音端点：%s\n\n请在 Windows 设置 → 系统 → 声音 → 更多声音设置中启用这两个配对端点，再重新打开本页面。PhoneVR 不会重复安装第二套虚拟声卡驱动。",
				status.MicrophoneRouteProvider,
				status.MicrophoneRenderFriendlyName,
				status.MicrophoneFriendlyName,
			)
			app.messageBox(message, app.text("音频与麦克风", "Audio & Microphone"))
			return
		}
		provider := strings.TrimSpace(status.MicrophoneRouteProvider)
		if provider == "" {
			provider = "Virtual Audio Cable"
		}
		message := fmt.Sprintf(
			"已检测到可用的虚拟音频线缆。\n\n提供程序：%s\n手机麦克风写入：%s\nWindows 录音设备：%s\n\nPhoneVR 正在复用现有驱动，不会重复安装，也不会修改、更新或卸载现有虚拟音频驱动。\n请在 VRChat 的输入设备设置中选择对应的录音端点。",
			provider,
			status.MicrophoneRenderFriendlyName,
			status.MicrophoneFriendlyName,
		)
		app.messageBox(message, app.text("音频与麦克风", "Audio & Microphone"))
		return
	}

	message := app.text(
		"当前没有检测到可用的 VAC / VB-CABLE 播放与录音端点。\n\n如果已安装 VAC，请确认 Windows 声音设置中同时存在配对的播放和录音端点，并重新启动 PhoneVR Host。\n\n现在是否安装 VB-Audio 官方 VB-CABLE？安装前会验证原厂安装器的 Authenticode 签名。",
		"No compatible VAC / VB-CABLE playback and recording endpoint pair is active.\n\nIf VAC is already installed, make sure both paired playback and recording endpoints are enabled in Windows Sound settings, then restart PhoneVR Host.\n\nInstall the official VB-Audio VB-CABLE package now? The vendor installer's Authenticode signature will be verified first.",
	)
	if !app.confirmMessage(message, app.text("音频与麦克风", "Audio & Microphone")) {
		return
	}
	app.service.addLog("User requested VB-CABLE fallback installation from Audio & Microphone manager")
	app.messageBox(app.text("已开始准备官方 VB-CABLE 安装包。下载和 UAC 安装过程将在后台执行，详细结果请查看运行日志。", "Preparing the official VB-CABLE package. Download and UAC installation will run in the background; see Runtime Logs for the result."), app.text("音频与麦克风", "Audio & Microphone"))
	go func() {
		message, installErr := app.service.InstallVirtualCable()
		if installErr != nil {
			app.service.addLog("VB-CABLE installation failed: " + installErr.Error())
			return
		}
		app.service.addLog(message)
	}()
}
