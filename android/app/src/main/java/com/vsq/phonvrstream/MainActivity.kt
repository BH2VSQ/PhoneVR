package com.vsq.phonvrstream

import android.app.Activity
import android.content.Intent
import android.content.pm.ActivityInfo
import android.content.res.Configuration
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
import android.os.Process
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.opengl.GLSurfaceView
import android.os.Bundle
import android.os.SystemClock
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.view.WindowManager
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.text.InputType
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.SeekBar
import android.widget.TextView
import com.vsq.phonvrstream.display.StereoRenderer
import com.vsq.phonvrstream.network.DiscoveredHost
import com.vsq.phonvrstream.network.HeadPose
import com.vsq.phonvrstream.network.NetworkClient
import com.vsq.phonvrstream.settings.DisplaySettings
import com.vsq.phonvrstream.settings.OpticalPreset
import com.vsq.phonvrstream.settings.SettingsStore
import com.vsq.phonvrstream.streaming.CodecCapabilityProbe
import com.vsq.phonvrstream.streaming.VideoStreamStats
import com.vsq.phonvrstream.tracking.Quaternion

class MainActivity : Activity(), SensorEventListener {
    companion object {
        // Request 200 Hz updates where the handset IMU supports it; Android may cap this to hardware rate.
        private const val HEAD_TRACKING_SAMPLE_PERIOD_US = 5_000
    }

    private lateinit var glView: GLSurfaceView
    private lateinit var renderer: StereoRenderer
    private lateinit var sensorManager: SensorManager
    private lateinit var sensorThread: HandlerThread
    private lateinit var sensorHandler: Handler
    private val mainHandler = Handler(Looper.getMainLooper())
    private val rawRotationMatrix = FloatArray(9)
    private val remappedRotationMatrix = FloatArray(9)
    private var rotationSensor: Sensor? = null
    private lateinit var settingsStore: SettingsStore
    private lateinit var poseText: TextView
    private lateinit var menuConnectionText: TextView
    private lateinit var menuTransportText: TextView
    private lateinit var menuVideoDetailText: TextView
    private lateinit var streamStatusText: TextView
    private lateinit var screenFlipButton: Button
    private lateinit var menuPanel: LinearLayout
    private lateinit var dismissLayer: View
    private lateinit var settingsPanel: LinearLayout
    private lateinit var connectionPanel: LinearLayout
    private lateinit var connectionStatusLabel: TextView
    private lateinit var connectionTransportLabel: TextView
    private lateinit var hostAddressInput: EditText
    private lateinit var hostPortInput: EditText
    private lateinit var discoveredHostList: LinearLayout
    private lateinit var networkClient: NetworkClient

    @Volatile private var settings = DisplaySettings()
    @Volatile private var opticalPresets = emptyList<OpticalPreset>()
    @Volatile private var activeOpticalPresetId = "builtin-cardboard"
    @Volatile private var lastRawPose = Quaternion.IDENTITY
    @Volatile private var referencePose: Quaternion? = null
    private var lastPoseUiUpdateMs = 0L
    private var pcConnected = false
    private var connectionMessage = "正在搜索局域网 PC"
    private var connectionTransportMessage = "当前连接方式：未连接"
    private var startupDiscoveryStarted = false
    private var screenFlipped = false
    @Volatile private var videoStreamStats = VideoStreamStats()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        hideSystemUi()

        settingsStore = SettingsStore(this)
        screenFlipped = settingsStore.loadScreenFlipped()
        applyScreenOrientation()
        settings = settingsStore.load()
        opticalPresets = settingsStore.loadOpticalPresets(settings)
        val storedActivePresetId = settingsStore.loadActiveOpticalPresetId()
        val selectedPreset = opticalPresets.firstOrNull { it.id == storedActivePresetId }
            ?: opticalPresets.first()
        activeOpticalPresetId = selectedPreset.id
        // The active preset is authoritative. Rehydrate renderer settings from
        // its persistent record on every launch so a stale legacy settings key
        // can never disagree with the selected local optical preset.
        settings = selectedPreset.settings.normalized()
        settingsStore.saveActiveOpticalPresetId(activeOpticalPresetId)
        settingsStore.save(settings)
        networkClient = NetworkClient(
            applicationContext,
            statusListener = { message, connected ->
                pcConnected = connected
                connectionMessage = message
                if (::connectionStatusLabel.isInitialized) {
                    connectionStatusLabel.text = message
                    connectionStatusLabel.setTextColor(
                        if (connected) Color.rgb(118, 225, 164) else Color.rgb(242, 183, 112)
                    )
                }
                updateTopHud()
            },
            transportStatusListener = { message ->
                connectionTransportMessage = message
                if (::connectionTransportLabel.isInitialized) connectionTransportLabel.text = message
                if (::menuTransportText.isInitialized) menuTransportText.text = message
            },
            currentDisplaySettings = { settings.toJson() },
            currentOpticalPresets = { OpticalPreset.listToJson(opticalPresets) },
            currentMediaCapabilities = { CodecCapabilityProbe.discoverJson() },
            videoStatsListener = { stats ->
                videoStreamStats = stats
                if (::renderer.isInitialized) {
                    // Once a real stream has appeared, never leave the previous
                    // frame displayed as if it were live when any pipeline stage stalls.
                    renderer.setVideoStreamStale(stats.state != "解码流畅")
                }
                updateTopStreamStatus()
            },
            applyRemoteOpticalPresets = { json ->
                val parsed = OpticalPreset.listFromJson(json).ifEmpty { OpticalPreset.defaults() }
                opticalPresets = parsed
                settingsStore.saveOpticalPresets(parsed)
                val activePreset = parsed.firstOrNull { it.id == activeOpticalPresetId }
                if (activePreset == null) {
                    val fallback = parsed.first()
                    activeOpticalPresetId = fallback.id
                    settings = fallback.settings.normalized()
                    settingsStore.saveActiveOpticalPresetId(fallback.id)
                    settingsStore.save(settings)
                    updateRendererSettings()
                } else {
                    // Saving edits to the currently active preset updates the
                    // phone's optics immediately; edits to other presets stay
                    // stored until the user chooses to apply them.
                    val updatedSettings = activePreset.settings.normalized()
                    if (settings != updatedSettings) {
                        settings = updatedSettings
                        settingsStore.save(settings)
                        updateRendererSettings()
                    }
                }
                if (::settingsPanel.isInitialized && settingsPanel.visibility == View.VISIBLE) {
                    recreateSettingsPanelContent()
                }
                OpticalPreset.listToJson(parsed)
            },
            applyRemoteOpticalPreset = { presetId ->
                val preset = opticalPresets.firstOrNull { it.id == presetId }
                    ?: error("未找到光学预设：$presetId")
                settings = preset.settings.normalized()
                activeOpticalPresetId = preset.id
                settingsStore.save(settings)
                settingsStore.saveActiveOpticalPresetId(preset.id)
                updateRendererSettings()
                if (::settingsPanel.isInitialized && settingsPanel.visibility == View.VISIBLE) {
                    recreateSettingsPanelContent()
                }
                settings.toJson()
            },
            applyRemoteDisplaySettings = { json ->
                check(!isFinishing && !isDestroyed) { "Activity is no longer active" }
                settings = DisplaySettings.fromJson(json, settings).normalized()
                settingsStore.save(settings)
                updateRendererSettings()
                // Do not rebuild the panel during a remote transaction. Its widgets
                // read the persisted state when reopened, avoiding view-tree races.
                settings.toJson()
            },
            applyRemoteDeviceAction = { action ->
                check(!isFinishing && !isDestroyed) { "Activity is no longer active" }
                when (action) {
                    "center_view" -> {
                        referencePose = lastRawPose
                        updatePoseText("视角已居中")
                    }
                    "open_optical_presets", "open_display_calibration" -> {
                        closeOverlayPanels()
                        settingsPanel.visibility = View.VISIBLE
                        updateDismissLayer()
                    }
                }
            },
        )

        sensorManager = getSystemService(SENSOR_SERVICE) as SensorManager
        sensorThread = HandlerThread("PhoneVR-IMU", Process.THREAD_PRIORITY_DISPLAY).apply { start() }
        sensorHandler = Handler(sensorThread.looper)
        rotationSensor = sensorManager.getDefaultSensor(Sensor.TYPE_GAME_ROTATION_VECTOR)
            ?: sensorManager.getDefaultSensor(Sensor.TYPE_ROTATION_VECTOR)

        renderer = StereoRenderer()
        renderer.setVideoSurfaceListener { surface -> networkClient.attachVideoSurface(surface) }
        renderer.setVideoFrameListener { networkClient.reportVideoFrameRendered() }
        glView = GLSurfaceView(this).apply {
            setEGLContextClientVersion(3)
            preserveEGLContextOnPause = true
            setRenderer(renderer)
            renderMode = GLSurfaceView.RENDERMODE_CONTINUOUSLY
        }

        val root = FrameLayout(this)
        root.addView(glView, FrameLayout.LayoutParams(-1, -1))

        // A transparent full-screen hit target dismisses menus and side panels
        // when the user taps the surrounding display area.
        dismissLayer = View(this).apply {
            setBackgroundColor(Color.argb(74, 0, 0, 0))
            visibility = View.GONE
            isClickable = true
            isFocusable = true
            setOnClickListener { closeOverlayPanels() }
        }
        root.addView(dismissLayer, FrameLayout.LayoutParams(-1, -1))
        root.addView(buildTopBar(), FrameLayout.LayoutParams(-1, dp(44), Gravity.TOP))

        buildSettingsPanel().also { panel ->
            settingsPanel = panel
            val availableWidth = (resources.displayMetrics.widthPixels - dp(20)).coerceAtLeast(dp(1))
            val panelWidth = minOf(dp(370), availableWidth)
            val params = FrameLayout.LayoutParams(panelWidth, -1, Gravity.END)
            params.setMargins(0, dp(62), dp(10), dp(10))
            root.addView(panel, params)
        }
        settingsPanel.visibility = View.GONE

        buildConnectionPanel().also { panel ->
            connectionPanel = panel
            val availableWidth = (resources.displayMetrics.widthPixels - dp(20)).coerceAtLeast(dp(1))
            val panelWidth = minOf(dp(370), availableWidth)
            val params = FrameLayout.LayoutParams(panelWidth, -1, Gravity.END)
            params.setMargins(0, dp(62), dp(10), dp(10))
            root.addView(panel, params)
        }
        connectionPanel.visibility = View.GONE

        buildMenuPanel().also { panel ->
            menuPanel = panel
            val params = FrameLayout.LayoutParams(dp(198), -2, Gravity.TOP or Gravity.CENTER_HORIZONTAL)
            params.topMargin = dp(54)
            root.addView(panel, params)
        }
        menuPanel.visibility = View.GONE
        updateDismissLayer()

        setContentView(root)
        updateRendererSettings()
        updateTopHud()
        updatePoseText("IMU 初始化中")
        root.post {
            // Keep AOA armed as an independent backup while LAN discovery and
            // the primary network control/pose session run normally.
            networkClient.connectUsbVideoBackup(Build.MODEL ?: "Android")
            startStartupDiscovery()
        }
    }

    override fun onNewIntent(intent: Intent?) {
        super.onNewIntent(intent)
        setIntent(intent)
        if (intent?.action == "android.hardware.usb.action.USB_ACCESSORY_ATTACHED") {
            if (::networkClient.isInitialized) {
                networkClient.connectUsbVideoBackup(Build.MODEL ?: "Android")
                startStartupDiscovery()
            }
        }
    }

    private fun updateDismissLayer() {
        if (!::dismissLayer.isInitialized) return
        val hasOverlay =
            (::menuPanel.isInitialized && menuPanel.visibility == View.VISIBLE) ||
            (::settingsPanel.isInitialized && settingsPanel.visibility == View.VISIBLE) ||
            (::connectionPanel.isInitialized && connectionPanel.visibility == View.VISIBLE)
        dismissLayer.visibility = if (hasOverlay) View.VISIBLE else View.GONE
    }

    private fun closeOverlayPanels() {
        if (::menuPanel.isInitialized) menuPanel.visibility = View.GONE
        if (::settingsPanel.isInitialized) settingsPanel.visibility = View.GONE
        if (::connectionPanel.isInitialized) connectionPanel.visibility = View.GONE
        updateDismissLayer()
    }

    private fun buildTopBar(): View {
        val bar = FrameLayout(this).apply {
            setPadding(dp(8), dp(4), dp(8), dp(4))
            background = GradientDrawable(
                GradientDrawable.Orientation.TOP_BOTTOM,
                intArrayOf(Color.argb(82, 4, 10, 18), Color.argb(0, 4, 10, 18)),
            )
        }

        streamStatusText = TextView(this).apply {
            text = "● 等待 PC 视频流"
            setTextColor(Color.rgb(170, 190, 212))
            textSize = 9.5f
            typeface = Typeface.DEFAULT_BOLD
            maxLines = 1
            ellipsize = android.text.TextUtils.TruncateAt.END
            gravity = Gravity.CENTER
            setPadding(dp(8), dp(3), dp(8), dp(3))
            background = GradientDrawable().apply {
                setColor(Color.argb(112, 5, 13, 23))
                cornerRadius = dp(8).toFloat()
                setStroke(dp(1), Color.argb(42, 135, 175, 215))
            }
        }
        bar.addView(
            streamStatusText,
            FrameLayout.LayoutParams(dp(142), dp(25), Gravity.END or Gravity.CENTER_VERTICAL),
        )

        val menuButton = makeButton("⋮") {
            val shouldShow = menuPanel.visibility != View.VISIBLE
            menuPanel.visibility = if (shouldShow) View.VISIBLE else View.GONE
            settingsPanel.visibility = View.GONE
            connectionPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply {
            textSize = 24f
            setPadding(0, 0, 0, dp(4))
            minWidth = dp(44)
            minHeight = dp(38)
        }
        bar.addView(menuButton, FrameLayout.LayoutParams(dp(42), dp(38), Gravity.TOP or Gravity.CENTER_HORIZONTAL))
        return bar
    }

    private fun buildMenuPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(9), dp(9), dp(9), dp(9))
            background = GradientDrawable().apply {
                setColor(Color.argb(246, 8, 16, 28))
                cornerRadius = dp(14).toFloat()
                setStroke(dp(1), Color.argb(145, 105, 150, 190))
            }
            elevation = dp(16).toFloat()
        }
        val statusPanel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(9), dp(7), dp(9), dp(7))
            background = GradientDrawable().apply {
                setColor(Color.argb(155, 17, 31, 48))
                cornerRadius = dp(9).toFloat()
                setStroke(dp(1), Color.argb(55, 125, 165, 205))
            }
        }
        statusPanel.addView(label("连接状态", secondary = true).apply { textSize = 9f })
        menuConnectionText = TextView(this).apply {
            text = "○ PC 未连接"
            textSize = 11f
            typeface = Typeface.DEFAULT_BOLD
            setTextColor(Color.rgb(242, 183, 112))
            maxLines = 2
        }
        statusPanel.addView(menuConnectionText)
        menuTransportText = TextView(this).apply {
            text = connectionTransportMessage
            textSize = 9f
            setTextColor(Color.rgb(166, 190, 216))
            maxLines = 2
            setPadding(0, dp(3), 0, 0)
        }
        statusPanel.addView(menuTransportText)
        poseText = TextView(this).apply {
            text = "姿态  Y 0°  P 0°  R 0°"
            textSize = 10f
            typeface = Typeface.MONOSPACE
            setTextColor(Color.rgb(195, 216, 235))
            maxLines = 2
            setPadding(0, dp(4), 0, 0)
        }
        statusPanel.addView(poseText)
        menuVideoDetailText = TextView(this).apply {
            text = "视频：等待 PC · 接收 0 / 解码 0 / 显示 0 FPS"
            textSize = 9f
            typeface = Typeface.MONOSPACE
            setTextColor(Color.rgb(166, 190, 216))
            maxLines = 4
            ellipsize = android.text.TextUtils.TruncateAt.END
            setPadding(0, dp(5), 0, 0)
        }
        statusPanel.addView(menuVideoDetailText)
        panel.addView(statusPanel, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(8) })

        panel.addView(makeButton("视角居中") {
            referencePose = lastRawPose
            renderer.updateOrientation(0f, 0f, 0f, 1f)
            menuPanel.visibility = View.GONE
            updateDismissLayer()
            updatePoseText("视角已居中")
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(40)).apply { bottomMargin = dp(6) } })
        panel.addView(makeButton("连接 PC") {
            connectionPanel.visibility = View.VISIBLE
            settingsPanel.visibility = View.GONE
            menuPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(40)).apply { bottomMargin = dp(6) } })
        screenFlipButton = makeButton(screenFlipButtonText()) {
            screenFlipped = !screenFlipped
            settingsStore.saveScreenFlipped(screenFlipped)
            screenFlipButton.text = screenFlipButtonText()
            applyScreenOrientation()
        }
        panel.addView(screenFlipButton, LinearLayout.LayoutParams(-1, dp(40)).apply { bottomMargin = dp(2) })
        panel.addView(makeButton("光学预设") {
            settingsPanel.visibility = View.VISIBLE
            connectionPanel.visibility = View.GONE
            menuPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(40)) })
        return panel
    }

    private fun screenFlipButtonText(): String =
        "屏幕旋转 180°：${if (screenFlipped) "开" else "关"}"

    private fun applyScreenOrientation() {
        requestedOrientation = if (screenFlipped) {
            ActivityInfo.SCREEN_ORIENTATION_REVERSE_LANDSCAPE
        } else {
            ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
        }
    }

    private fun updateTopHud() {
        if (::menuConnectionText.isInitialized) {
            val searching = connectionMessage.contains("搜索")
            val connecting = connectionMessage.contains("正在连接") || connectionMessage.contains("发现 ")
            menuConnectionText.text = when {
                pcConnected -> "● PC 已连接"
                searching -> "◌ 正在搜索 PC"
                connecting -> "◌ 正在连接 PC"
                else -> "○ PC 未连接"
            }
            menuConnectionText.setTextColor(
                when {
                    pcConnected -> Color.rgb(118, 225, 164)
                    searching || connecting -> Color.rgb(130, 195, 255)
                    else -> Color.rgb(242, 183, 112)
                },
            )
            menuConnectionText.contentDescription = connectionMessage
        }
        updateTopStreamStatus()
        updateMenuVideoDetails()
    }

    private fun updateMenuVideoDetails() {
        if (!::menuVideoDetailText.isInitialized) return
        val stats = videoStreamStats
        menuVideoDetailText.text = buildString {
            append("视频：${stats.state}")
            append("\n接收 ${stats.receiveFps.toInt()} / 解码 ${stats.decodeFps.toInt()} / 显示 ${stats.displayFps.toInt()} FPS")
            append("\n分片 ${stats.fragmentsPerSecond}/s · 组帧 ${stats.reassemblyInFlight} · 解码队列 ${stats.decoderQueueDepth}")
            append("\n网络 ${(stats.networkReceiveBytesPerSecond / 1024.0).toInt()} KiB/s · USB AOA ${(stats.usbReceiveBytesPerSecond / 1024.0).toInt()} KiB/s")
            append("\n丢帧 ${stats.queueDrops + stats.decoderDrops} · 超时组帧 ${stats.expiredAssemblies} · 恢复 ${stats.decoderRecoveries}")
        }
    }

    private fun updateTopStreamStatus() {
        if (!::streamStatusText.isInitialized) return
        val stats = videoStreamStats
        val text = when {
            !pcConnected -> "○ 等待 PC 视频流"
            stats.state == "解码流畅" && stats.displayFps >= 1.0 ->
                "● 视频 ${stats.displayFps.toInt()} FPS · 解码 ${stats.decodeFps.toInt()}"
            stats.state == "显示停滞" -> "! 手机显示停滞"
            stats.state.endsWith("组帧中") -> "◌ 视频组帧 ${stats.receiveFps.toInt()} FPS"
            stats.state == "解码停滞" -> "! 视频解码停滞"
            stats.state == "视频无数据" -> "! 视频流中断"
            stats.state == "视频已停止" -> "○ 视频已停止"
            stats.state == "视频未启用" -> "○ 视频未协商"
            stats.state == "视频参数无效" -> "! 视频参数错误"
            else -> "◌ 等待视频帧"
        }
        streamStatusText.text = text
        streamStatusText.setTextColor(
            when {
                !pcConnected -> Color.rgb(170, 190, 212)
                stats.state == "解码流畅" && stats.displayFps >= 1.0 -> Color.rgb(118, 225, 164)
                stats.state == "显示停滞" || stats.state == "解码停滞" || stats.state == "视频无数据" || stats.state == "视频参数无效" -> Color.rgb(255, 145, 130)
                else -> Color.rgb(242, 198, 112)
            },
        )
        streamStatusText.contentDescription =
            "${stats.state}，接收 ${stats.receiveFps.toInt()} 帧每秒，解码 ${stats.decodeFps.toInt()} 帧每秒，显示 ${stats.displayFps.toInt()} 帧每秒"
        updateMenuVideoDetails()
    }

    private fun startStartupDiscovery() {
        if (startupDiscoveryStarted) return
        startupDiscoveryStarted = true
        pcConnected = false
        connectionMessage = "正在搜索局域网 PhoneVR Host"
        updateTopHud()
        networkClient.discover { hosts ->
            if (hosts.isEmpty()) {
                connectionMessage = "未发现服务端，请通过顶部菜单手动连接"
                updateTopHud()
                if (::connectionStatusLabel.isInitialized && connectionPanel.visibility == View.VISIBLE) {
                    connectionStatusLabel.text = connectionMessage
                }
                return@discover
            }

            val saved = getSharedPreferences("phone_vr_connection", MODE_PRIVATE)
            val rememberedAddress = saved.getString("host", null)
            val selected = hosts.firstOrNull { it.address == rememberedAddress } ?: hosts.first()
            fillConnectionAddress(selected, persist = true)
            connectionMessage = "发现 ${hosts.size} 台主机，正在连接 ${selected.name}"
            updateTopHud()
            if (::connectionStatusLabel.isInitialized) {
                connectionStatusLabel.text = connectionMessage
                connectionStatusLabel.setTextColor(Color.rgb(242, 183, 112))
            }
            networkClient.connect(selected.address, selected.controlPort, Build.MODEL ?: "Android")
        }
    }

    private fun buildConnectionPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(14), dp(12), dp(14), dp(12))
            background = GradientDrawable().apply {
                setColor(Color.argb(244, 9, 16, 28))
                cornerRadius = dp(16).toFloat()
                setStroke(dp(1), Color.argb(115, 110, 145, 180))
            }
            elevation = dp(12).toFloat()
        }

        // Fixed header keeps the title and connection status visible while the form scrolls.
        val header = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
        }
        header.addView(TextView(this).apply {
            text = "连接 PC"
            setTextColor(Color.WHITE)
            textSize = 15f
            typeface = Typeface.DEFAULT_BOLD
        }, LinearLayout.LayoutParams(-1, dp(34)))
        panel.addView(header)

        connectionStatusLabel = label("尚未连接 PC", secondary = true).apply {
            textSize = 11f
            setPadding(0, dp(6), 0, dp(9))
            setTextColor(Color.rgb(242, 183, 112))
        }
        panel.addView(connectionStatusLabel)

        connectionTransportLabel = label(connectionTransportMessage, secondary = true).apply {
            textSize = 10f
            setPadding(0, 0, 0, dp(9))
            setTextColor(Color.rgb(130, 195, 255))
        }
        panel.addView(connectionTransportLabel)

        // All variable-height content lives in a weighted ScrollView. This avoids
        // hiding the connect controls on short landscape displays or small phones.
        val scroll = ScrollView(this).apply {
            isFillViewport = false
            clipToPadding = false
            overScrollMode = View.OVER_SCROLL_IF_CONTENT_SCROLLS
            setPadding(0, 0, 0, dp(8))
            scrollBarStyle = View.SCROLLBARS_INSIDE_OVERLAY
            isVerticalScrollBarEnabled = true
            descendantFocusability = ViewGroup.FOCUS_AFTER_DESCENDANTS
        }
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(2), 0, dp(8))
        }
        scroll.addView(content, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
        panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))

        content.addView(label("PC 地址", secondary = true).apply { setPadding(0, dp(6), 0, 0) })
        hostAddressInput = EditText(this).apply {
            setSingleLine(true)
            hint = "例如 192.168.1.100"
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
            textSize = 13f
            setTextColor(Color.WHITE)
            setHintTextColor(Color.rgb(115, 132, 153))
            setPadding(dp(10), 0, dp(10), 0)
            background = inputBackground()
            imeOptions = android.view.inputmethod.EditorInfo.IME_ACTION_DONE
        }
        content.addView(hostAddressInput, LinearLayout.LayoutParams(-1, dp(42)))

        content.addView(label("控制端口", secondary = true).apply {
            setPadding(0, dp(8), 0, dp(2))
        })
        hostPortInput = EditText(this).apply {
            setSingleLine(true)
            setText(NetworkClient.DEFAULT_CONTROL_PORT.toString())
            inputType = InputType.TYPE_CLASS_NUMBER
            textSize = 13f
            setTextColor(Color.WHITE)
            setPadding(dp(10), 0, dp(10), 0)
            background = inputBackground()
            imeOptions = android.view.inputmethod.EditorInfo.IME_ACTION_DONE
        }
        content.addView(hostPortInput, LinearLayout.LayoutParams(-1, dp(42)))

        content.addView(makeButton("USB AOA 直连") {
            connectionMessage = "正在等待 USB Accessory（AOA）连接"
            connectionStatusLabel.text = connectionMessage
            connectionStatusLabel.setTextColor(Color.rgb(130, 195, 255))
            updateTopHud()
            networkClient.connectUsb(Build.MODEL ?: "Android")
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { topMargin = dp(4) }
        })

        content.addView(makeButton("自动搜索局域网 PC") {
            connectionMessage = "正在搜索局域网 PhoneVR Host"
            connectionStatusLabel.text = "$connectionMessage…"
            updateTopHud()
            discoveredHostList.removeAllViews()
            networkClient.discover { hosts ->
                discoveredHostList.removeAllViews()
                if (hosts.isEmpty()) {
                    discoveredHostList.addView(label(
                        "没有发现 PC。请确认手机和电脑连接同一局域网、主机程序已启动且防火墙允许 PhoneVRHost，或手动输入 IP 地址。",
                        true,
                    ).apply { setPadding(0, dp(6), 0, dp(6)) })
                } else {
                    val saved = getSharedPreferences("phone_vr_connection", MODE_PRIVATE)
                    val rememberedAddress = saved.getString("host", null)
                    val selected = hosts.firstOrNull { it.address == rememberedAddress } ?: hosts.first()
                    fillConnectionAddress(selected, persist = true)
                    hosts.forEach { host -> addDiscoveredHost(host, selected.address == host.address) }
                    connectionStatusLabel.text = if (hosts.size == 1) {
                        "已发现 ${selected.name}，IP 地址已自动填入"
                    } else {
                        "发现 ${hosts.size} 个网络入口，已填入 ${selected.name}，可点击切换"
                    }
                    connectionStatusLabel.setTextColor(Color.rgb(118, 225, 164))
                }
            }
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { topMargin = dp(10) }
        })

        discoveredHostList = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(6), 0, dp(6))
        }
        content.addView(discoveredHostList)

        content.addView(label(
            "手机和电脑连接同一局域网即可自动搜索。若发现失败，可手动输入 PC 对应网卡的 IPv4 地址。",
            secondary = true,
        ).apply { setPadding(0, dp(8), 0, dp(10)) })

        // Sticky action area: these buttons remain visible even when the form above scrolls.
        val actions = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(0, dp(8), 0, 0)
        }
        actions.addView(makeButton("连接 PC") {
            val address = hostAddressInput.text.toString().trim()
            val port = hostPortInput.text.toString().toIntOrNull()
            if (address.isBlank() || port == null || port !in 1..65535) {
                connectionStatusLabel.text = "请输入有效的 PC 地址和端口"
                connectionStatusLabel.setTextColor(Color.rgb(242, 183, 112))
                return@makeButton
            }
            getSharedPreferences("phone_vr_connection", MODE_PRIVATE).edit()
                .putString("host", address).putInt("port", port).apply()
            networkClient.connect(address, port, Build.MODEL ?: "Android")
        }.apply {
            layoutParams = LinearLayout.LayoutParams(0, dp(44), 1f).apply { rightMargin = dp(5) }
        })
        actions.addView(makeButton("断开连接") {
            networkClient.disconnect()
        }.apply {
            layoutParams = LinearLayout.LayoutParams(0, dp(44), 1f).apply { leftMargin = dp(5) }
        })
        panel.addView(actions)

        val saved = getSharedPreferences("phone_vr_connection", MODE_PRIVATE)
        hostAddressInput.setText(saved.getString("host", "") ?: "")
        hostPortInput.setText(saved.getInt("port", NetworkClient.DEFAULT_CONTROL_PORT).toString())
        return panel
    }


    private fun fillConnectionAddress(host: DiscoveredHost, persist: Boolean) {
        if (::hostAddressInput.isInitialized) hostAddressInput.setText(host.address)
        if (::hostPortInput.isInitialized) hostPortInput.setText(host.controlPort.toString())
        if (persist) {
            getSharedPreferences("phone_vr_connection", MODE_PRIVATE).edit()
                .putString("host", host.address)
                .putInt("port", host.controlPort)
                .apply()
        }
    }

    private fun addDiscoveredHost(host: DiscoveredHost, selected: Boolean = false) {
        discoveredHostList.addView(makeButton(
            "${if (selected) "✓ " else ""}${host.name}  ·  ${host.address}"
        ) {
            fillConnectionAddress(host, persist = true)
            connectionStatusLabel.text = "已选择 ${host.name}（${host.address}）"
            connectionStatusLabel.setTextColor(Color.rgb(118, 225, 164))
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(38)).apply { topMargin = dp(4) }
        })
    }

    private fun inputBackground() = GradientDrawable().apply {
        setColor(Color.argb(180, 29, 42, 59))
        cornerRadius = dp(8).toFloat()
        setStroke(dp(1), Color.argb(120, 110, 145, 180))
    }

    /**
     * The phone is the source of truth for optical presets. The host manages
     * preset contents; the Android app only lists local presets and applies one.
     */
    private fun buildSettingsPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(12), dp(16), dp(12))
            background = GradientDrawable().apply {
                setColor(Color.argb(238, 9, 16, 28))
                cornerRadius = dp(16).toFloat()
                setStroke(dp(1), Color.argb(100, 110, 145, 180))
            }
            elevation = dp(14).toFloat()
        }

        panel.addView(TextView(this).apply {
            text = "光学预设"
            setTextColor(Color.WHITE)
            textSize = 19f
            typeface = Typeface.DEFAULT_BOLD
        })
        panel.addView(label(
            "预设保存在本机。连接 PhoneVR Host 后，电脑端可管理和更新预设；手机端选择本地预设即可应用。",
            secondary = true,
        ).apply { setPadding(0, dp(5), 0, dp(10)) })

        val active = opticalPresets.firstOrNull { it.id == activeOpticalPresetId }
        panel.addView(label(
            "当前使用：${active?.name ?: "未知预设"}",
            secondary = false,
        ).apply {
            textSize = 13f
            setTextColor(Color.rgb(91, 206, 250))
            setPadding(0, 0, 0, dp(8))
        })

        val scroll = ScrollView(this).apply {
            isFillViewport = false
            clipToPadding = false
            isVerticalScrollBarEnabled = true
            scrollBarStyle = View.SCROLLBARS_INSIDE_OVERLAY
            overScrollMode = View.OVER_SCROLL_IF_CONTENT_SCROLLS
        }
        val list = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(2), 0, dp(8))
        }
        if (opticalPresets.isEmpty()) {
            list.addView(label("尚无可用预设。请连接电脑端同步。", secondary = true))
        } else {
            opticalPresets.forEach { preset ->
                val selected = preset.id == activeOpticalPresetId
                val card = LinearLayout(this).apply {
                    orientation = LinearLayout.VERTICAL
                    setPadding(dp(10), dp(8), dp(10), dp(8))
                    background = GradientDrawable().apply {
                        setColor(if (selected) Color.argb(210, 24, 60, 82) else Color.argb(185, 23, 37, 54))
                        cornerRadius = dp(10).toFloat()
                        setStroke(dp(1), if (selected) Color.rgb(91, 206, 250) else Color.argb(80, 110, 145, 180))
                    }
                }
                card.addView(TextView(this).apply {
                    text = (if (selected) "✓  " else "") + preset.name
                    setTextColor(Color.WHITE)
                    textSize = 15f
                    typeface = Typeface.DEFAULT_BOLD
                })
                card.addView(label(
                    if (preset.builtin) "内置预设 · 本地存储" else "自定义预设 · 本地存储",
                    secondary = true,
                ).apply { setPadding(0, dp(3), 0, dp(5)) })
                card.addView(makeButton(if (selected) "当前已应用" else "应用此预设") {
                    applyLocalOpticalPreset(preset.id)
                }.apply {
                    isEnabled = !selected
                    alpha = if (selected) 0.72f else 1f
                    layoutParams = LinearLayout.LayoutParams(-1, dp(38))
                })
                list.addView(card, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(8) })
            }
        }
        scroll.addView(list, FrameLayout.LayoutParams(-1, -2))
        panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        panel.addView(makeButton("关闭") {
            settingsPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { topMargin = dp(8) }
        })
        return panel
    }

    private fun applyLocalOpticalPreset(presetId: String) {
        val preset = opticalPresets.firstOrNull { it.id == presetId } ?: return
        settings = preset.settings.normalized()
        activeOpticalPresetId = preset.id
        settingsStore.save(settings)
        settingsStore.saveActiveOpticalPresetId(preset.id)
        updateRendererSettings()
        if (pcConnected) networkClient.publishDisplaySettings(settings.toJson())
        recreateSettingsPanelContent()
    }

    private fun recreateSettingsPanelContent() {
        val parent = settingsPanel.parent as? FrameLayout ?: return
        val oldIndex = parent.indexOfChild(settingsPanel)
        val params = settingsPanel.layoutParams
        parent.removeView(settingsPanel)
        settingsPanel = buildSettingsPanel()
        parent.addView(settingsPanel, oldIndex, params)
    }

    /**
     * Converts the stored physical viewer alignment into the normalized lens
     * center used by the OpenGL post-process pass. The phone's reported physical
     * DPI is an estimate; optical values are now selected from host-managed presets.
     */
    private fun updateRendererSettings() {
        if (!::renderer.isInitialized) return
        val metrics = resources.displayMetrics
        val pixelsPerInchX = metrics.xdpi.takeIf { it > 1f }
            ?: metrics.densityDpi.toFloat().coerceAtLeast(1f)
        val screenWidthMm = metrics.widthPixels * 25.4f / pixelsPerInchX
        val screenHeightMm = if (metrics.ydpi > 1f) {
            metrics.heightPixels * 25.4f / metrics.ydpi
        } else {
            metrics.heightPixels * 25.4f / metrics.densityDpi.coerceAtLeast(1)
        }
        val physicalCenter = when (settings.screenVerticalAlignment) {
            DisplaySettings.ALIGNMENT_BOTTOM -> settings.trayToLensCenterMm / screenHeightMm.coerceAtLeast(1f)
            DisplaySettings.ALIGNMENT_TOP -> 1f - settings.trayToLensCenterMm / screenHeightMm.coerceAtLeast(1f)
            else -> 0.5f
        }
        val manualOffset = settings.lensCenterY - 0.5f
        val effectiveCenterY = (physicalCenter + manualOffset).coerceIn(0.2f, 0.8f)
        renderer.updateSettings(settings.copy(lensCenterY = effectiveCenterY).normalized(), screenWidthMm)
    }

    /** Estimates the vertical FOV from the panel's physical height and measured lens distance. */
    private fun estimateVerticalFovDegrees(): Float {
        val metrics = resources.displayMetrics
        val pixelsPerInch = metrics.ydpi.takeIf { it > 1f } ?: metrics.densityDpi.toFloat().coerceAtLeast(1f)
        val screenHeightMm = metrics.heightPixels * 25.4f / pixelsPerInch
        val halfScreenHeightMm = screenHeightMm / 2f
        val radians = 2.0 * kotlin.math.atan(halfScreenHeightMm / settings.screenToLensDistanceMm)
        return Math.toDegrees(radians).toFloat().coerceIn(60f, 115f)
    }

    private fun addSlider(
        parent: LinearLayout,
        title: String,
        description: String,
        maxValue: Int,
        initial: Int,
        valueText: (Int) -> String,
        update: (Int) -> DisplaySettings,
    ) {
        val block = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(8), 0, dp(4))
        }
        val top = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
        }
        top.addView(label(title), LinearLayout.LayoutParams(0, -2, 1f))
        val currentValue = label(valueText(initial)).apply {
            typeface = Typeface.MONOSPACE
            setTextColor(Color.rgb(130, 195, 255))
        }
        top.addView(currentValue)
        block.addView(top)
        block.addView(label(description, secondary = true))

        val seek = SeekBar(this).apply {
            this.max = maxValue
            progress = initial.coerceIn(0, maxValue)
        }
        seek.setOnSeekBarChangeListener(object : SeekBar.OnSeekBarChangeListener {
            override fun onProgressChanged(seekBar: SeekBar?, progress: Int, fromUser: Boolean) {
                currentValue.text = valueText(progress)
                settings = update(progress).normalized()
                updateRendererSettings()
                settingsStore.save(settings)
            }
            override fun onStartTrackingTouch(seekBar: SeekBar?) = Unit
            override fun onStopTrackingTouch(seekBar: SeekBar?) {
                networkClient.publishDisplaySettings(settings.toJson())
            }
        })
        block.addView(seek)
        parent.addView(block)
    }

    private fun label(textValue: String, secondary: Boolean = false): TextView = TextView(this).apply {
        text = textValue
        textSize = if (secondary) 10f else 12f
        setTextColor(if (secondary) Color.rgb(145, 163, 185) else Color.rgb(232, 239, 248))
    }

    private fun makeButton(textValue: String, action: () -> Unit): Button = Button(this).apply {
        text = textValue
        textSize = 11f
        isAllCaps = false
        setTextColor(Color.WHITE)
        background = GradientDrawable().apply {
            setColor(Color.argb(210, 31, 67, 104))
            cornerRadius = dp(9).toFloat()
            setStroke(dp(1), Color.argb(120, 110, 175, 230))
        }
        setPadding(dp(10), 0, dp(10), 0)
        setOnClickListener { action() }
    }

    @Deprecated("Deprecated in Android API; retained for headset back-button compatibility")
    override fun onBackPressed() {
        val overlayVisible =
            (::menuPanel.isInitialized && menuPanel.visibility == View.VISIBLE) ||
            (::settingsPanel.isInitialized && settingsPanel.visibility == View.VISIBLE) ||
            (::connectionPanel.isInitialized && connectionPanel.visibility == View.VISIBLE)
        if (overlayVisible) closeOverlayPanels() else super.onBackPressed()
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        hideSystemUi()
        updateRendererSettings()
        updateTopStreamStatus()
    }

    override fun onResume() {
        super.onResume()
        glView.onResume()
        hideSystemUi()
        rotationSensor?.let {
            sensorManager.registerListener(this, it, HEAD_TRACKING_SAMPLE_PERIOD_US, sensorHandler)
            updatePoseText("IMU ${it.name}")
        } ?: updatePoseText("未检测到旋转传感器")
        updateTopHud()
    }

    override fun onPause() {
        sensorManager.unregisterListener(this)
        glView.onPause()
        super.onPause()
    }

    override fun onSensorChanged(event: SensorEvent) {
        if (event.sensor.type != Sensor.TYPE_GAME_ROTATION_VECTOR &&
            event.sensor.type != Sensor.TYPE_ROTATION_VECTOR
        ) return

        // Keep sensor processing off the UI thread and reuse matrix buffers.
        SensorManager.getRotationMatrixFromVector(rawRotationMatrix, event.values)
        val (xAxis, yAxis) = when (windowManager.defaultDisplay.rotation) {
            android.view.Surface.ROTATION_90 -> SensorManager.AXIS_Y to SensorManager.AXIS_MINUS_X
            android.view.Surface.ROTATION_180 -> SensorManager.AXIS_MINUS_X to SensorManager.AXIS_MINUS_Y
            android.view.Surface.ROTATION_270 -> SensorManager.AXIS_MINUS_Y to SensorManager.AXIS_X
            else -> SensorManager.AXIS_X to SensorManager.AXIS_Y
        }
        val valid = SensorManager.remapCoordinateSystem(rawRotationMatrix, xAxis, yAxis, remappedRotationMatrix)
        val matrix = if (valid) remappedRotationMatrix else rawRotationMatrix
        // Enforce quaternion hemisphere continuity before deriving the relative pose.
        // This prevents equivalent q/-q sensor outputs near large pitch rotations
        // from looking like a discontinuous full turn to SteamVR interpolation.
        lastRawPose = Quaternion.fromRotationMatrix(matrix).alignedTo(lastRawPose)

        if (referencePose == null) referencePose = lastRawPose
        val relative = Quaternion.multiply(Quaternion.inverse(referencePose!!), lastRawPose).normalized()
        renderer.updateOrientation(relative.x, relative.y, relative.z, relative.w)
        networkClient.updatePose(
            HeadPose(event.timestamp, relative.x, relative.y, relative.z, relative.w)
        )

        val now = SystemClock.elapsedRealtime()
        if (now - lastPoseUiUpdateMs >= 120L) {
            lastPoseUiUpdateMs = now
            val (yaw, pitch, roll) = relative.toEulerDegrees()
            val summary = "Y ${yaw.toInt()}°  P ${pitch.toInt()}°  R ${roll.toInt()}°"
            mainHandler.post { if (!isFinishing && !isDestroyed) updatePoseText(summary) }
        }
    }

    override fun onAccuracyChanged(sensor: Sensor?, accuracy: Int) = Unit

    override fun onDestroy() {
        sensorManager.unregisterListener(this)
        if (::sensorThread.isInitialized) sensorThread.quitSafely()
        if (::networkClient.isInitialized) networkClient.shutdown()
        super.onDestroy()
    }

    private fun updatePoseText(value: String) {
        if (!::poseText.isInitialized) return
        val displayValue = if (value.startsWith("姿态")) value else "姿态  $value"
        if (Looper.myLooper() == Looper.getMainLooper()) {
            poseText.text = displayValue
        } else {
            mainHandler.post { if (::poseText.isInitialized && !isFinishing && !isDestroyed) poseText.text = displayValue }
        }
    }

    private fun hideSystemUi() {
        @Suppress("DEPRECATION")
        window.decorView.systemUiVisibility = (
            View.SYSTEM_UI_FLAG_FULLSCREEN or
                View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or
                View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY or
                View.SYSTEM_UI_FLAG_LAYOUT_STABLE or
                View.SYSTEM_UI_FLAG_LAYOUT_HIDE_NAVIGATION or
                View.SYSTEM_UI_FLAG_LAYOUT_FULLSCREEN
            )
    }

    private fun dp(value: Int): Int = (value * resources.displayMetrics.density).toInt()

}
