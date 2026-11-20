package com.vsq.phonvrstream

import android.app.Activity
import android.content.pm.ActivityInfo
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
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
import com.vsq.phonvrstream.settings.SettingsStore
import kotlin.math.asin
import kotlin.math.atan2

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
    private lateinit var connectionHudText: TextView
    private lateinit var menuPanel: LinearLayout
    private lateinit var dismissLayer: View
    private lateinit var settingsPanel: LinearLayout
    private lateinit var connectionPanel: LinearLayout
    private lateinit var connectionStatusLabel: TextView
    private lateinit var hostAddressInput: EditText
    private lateinit var hostPortInput: EditText
    private lateinit var discoveredHostList: LinearLayout
    private lateinit var networkClient: NetworkClient

    @Volatile private var settings = DisplaySettings()
    @Volatile private var lastRawPose = Quaternion.IDENTITY
    @Volatile private var referencePose: Quaternion? = null
    private var lastPoseUiUpdateMs = 0L
    private var pcConnected = false
    private var connectionMessage = "正在搜索局域网 PC"
    private var startupDiscoveryStarted = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        requestedOrientation = ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        hideSystemUi()

        settingsStore = SettingsStore(this)
        settings = settingsStore.load()
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
            currentDisplaySettings = { settings.toJson() },
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
                    "open_display_calibration" -> {
                        closeOverlayPanels()
                        settingsPanel.visibility = View.VISIBLE
                        updateDismissLayer()
                    }
                }
            },
        )

        sensorManager = getSystemService(SENSOR_SERVICE) as SensorManager
        sensorThread = HandlerThread("PhoneVR-IMU").apply { start() }
        sensorHandler = Handler(sensorThread.looper)
        rotationSensor = sensorManager.getDefaultSensor(Sensor.TYPE_GAME_ROTATION_VECTOR)
            ?: sensorManager.getDefaultSensor(Sensor.TYPE_ROTATION_VECTOR)

        renderer = StereoRenderer()
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
        root.addView(buildTopBar(), FrameLayout.LayoutParams(-1, dp(58), Gravity.TOP))

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
        root.post { startStartupDiscovery() }
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
            setPadding(dp(10), dp(5), dp(10), dp(5))
            background = GradientDrawable(
                GradientDrawable.Orientation.TOP_BOTTOM,
                intArrayOf(Color.argb(105, 4, 10, 18), Color.argb(0, 4, 10, 18)),
            )
        }

        val hud = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(dp(9), dp(4), dp(9), dp(4))
            background = GradientDrawable().apply {
                setColor(Color.argb(160, 5, 13, 23))
                cornerRadius = dp(10).toFloat()
                setStroke(dp(1), Color.argb(65, 135, 175, 215))
            }
        }
        connectionHudText = TextView(this).apply {
            text = "● PC 未连接"
            setTextColor(Color.rgb(242, 183, 112))
            textSize = 11f
            typeface = Typeface.DEFAULT_BOLD
            maxLines = 1
        }
        poseText = TextView(this).apply {
            text = "Y 0°  P 0°  R 0°"
            setTextColor(Color.rgb(195, 216, 235))
            textSize = 10f
            typeface = Typeface.MONOSPACE
            maxLines = 1
        }
        hud.addView(connectionHudText)
        hud.addView(poseText)
        bar.addView(hud, FrameLayout.LayoutParams(-2, dp(44), Gravity.START or Gravity.CENTER_VERTICAL))

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
            minHeight = dp(40)
        }
        bar.addView(menuButton, FrameLayout.LayoutParams(dp(46), dp(40), Gravity.TOP or Gravity.CENTER_HORIZONTAL))
        return bar
    }

    private fun buildMenuPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(8), dp(8), dp(8), dp(8))
            background = GradientDrawable().apply {
                setColor(Color.argb(246, 8, 16, 28))
                cornerRadius = dp(14).toFloat()
                setStroke(dp(1), Color.argb(145, 105, 150, 190))
            }
            elevation = dp(16).toFloat()
        }
        panel.addView(makeButton("视角居中") {
            referencePose = lastRawPose
            renderer.updateOrientation(0f, 0f, 0f, 1f)
            menuPanel.visibility = View.GONE
            updateDismissLayer()
            updatePoseText("视角已居中")
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { bottomMargin = dp(6) } })
        panel.addView(makeButton("连接 PC") {
            connectionPanel.visibility = View.VISIBLE
            settingsPanel.visibility = View.GONE
            menuPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { bottomMargin = dp(6) } })
        panel.addView(makeButton("显示校准") {
            settingsPanel.visibility = View.VISIBLE
            connectionPanel.visibility = View.GONE
            menuPanel.visibility = View.GONE
            updateDismissLayer()
        }.apply { layoutParams = LinearLayout.LayoutParams(-1, dp(42)) })
        return panel
    }

    private fun updateTopHud() {
        if (!::connectionHudText.isInitialized) return
        val searching = connectionMessage.contains("搜索")
        val connecting = connectionMessage.contains("正在连接") || connectionMessage.contains("发现 ")
        connectionHudText.text = when {
            pcConnected -> "● PC 已连接"
            searching -> "◌ 正在搜索 PC"
            connecting -> "◌ 正在连接 PC"
            else -> "○ PC 未连接"
        }
        connectionHudText.setTextColor(
            when {
                pcConnected -> Color.rgb(118, 225, 164)
                searching || connecting -> Color.rgb(130, 195, 255)
                else -> Color.rgb(242, 183, 112)
            }
        )
        connectionHudText.contentDescription = connectionMessage
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
            if (::hostAddressInput.isInitialized) hostAddressInput.setText(selected.address)
            if (::hostPortInput.isInitialized) hostPortInput.setText(selected.controlPort.toString())
            saved.edit().putString("host", selected.address).putInt("port", selected.controlPort).apply()
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

        content.addView(label("PC 地址", secondary = true))
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

        content.addView(makeButton("自动搜索局域网 PC") {
            connectionMessage = "正在搜索局域网内的 PhoneVR Host"
            connectionStatusLabel.text = "$connectionMessage…"
            updateTopHud()
            discoveredHostList.removeAllViews()
            networkClient.discover { hosts ->
                discoveredHostList.removeAllViews()
                if (hosts.isEmpty()) {
                    discoveredHostList.addView(label(
                        "没有发现 PC。请确认主机程序已启动，或手动输入 IP 地址。", true
                    ).apply { setPadding(0, dp(6), 0, dp(6)) })
                } else {
                    hosts.forEach { host -> addDiscoveredHost(host) }
                    connectionStatusLabel.text = "发现 ${hosts.size} 台 PC"
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
            "手机和电脑需要连接同一局域网。若自动搜索失败，可以在上方手动输入 PC 的 IPv4 地址。",
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

    private fun addDiscoveredHost(host: DiscoveredHost) {
        discoveredHostList.addView(makeButton("${host.name}  ·  ${host.address}") {
            hostAddressInput.setText(host.address)
            hostPortInput.setText(host.controlPort.toString())
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(38)).apply { topMargin = dp(4) }
        })
    }

    private fun inputBackground() = GradientDrawable().apply {
        setColor(Color.argb(180, 29, 42, 59))
        cornerRadius = dp(8).toFloat()
        setStroke(dp(1), Color.argb(120, 110, 145, 180))
    }

    private fun buildSettingsPanel(): LinearLayout {
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(12), dp(16), dp(12))
            background = GradientDrawable().apply {
                setColor(Color.argb(232, 9, 16, 28))
                cornerRadius = dp(16).toFloat()
                setStroke(dp(1), Color.argb(100, 110, 145, 180))
            }
        }

        val title = TextView(this).apply {
            text = "显示校准"
            setTextColor(Color.WHITE)
            textSize = 15f
            typeface = Typeface.DEFAULT_BOLD
        }
        panel.addView(title)
        panel.addView(label("双目测试场景 · 参数自动保存", secondary = true))

        val scroll = ScrollView(this).apply {
            isFillViewport = false
            overScrollMode = View.OVER_SCROLL_IF_CONTENT_SCROLLS
        }
        val content = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(0, dp(8), 0, dp(8))
        }
        scroll.addView(content, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
        panel.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))

        val profileDescription = if (settings.viewerProfileId == DisplaySettings.PROFILE_DAYDREAM_VIEW_2017) {
            "当前配置：Google Daydream View 2017"
        } else {
            "当前配置：自定义光学参数"
        }
        content.addView(label(profileDescription, secondary = true).apply {
            setPadding(0, dp(4), 0, dp(6))
        })
        content.addView(makeButton("应用 Google Daydream View 2017 预设") {
            settings = settings.withDaydreamView2017Profile()
            settingsStore.save(settings)
            updateRendererSettings()
            networkClient.publishDisplaySettings(settings.toJson())
            recreateSettingsPanelContent()
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(42)).apply { bottomMargin = dp(8) }
        })
        content.addView(label(
            "2017 版预设：K1 0.4331、K2 -0.085，镜片中心间距初始值 64 mm。公开实验中记录了 Daydream View v2.0 的 64 mm 镜片间距；仍建议测量自己的头显。个人 IPD、屏幕至镜片距离和垂直对齐需单独校准。此配置不提供屈光矫正。",
            secondary = true,
        ).apply { setPadding(0, 0, 0, dp(10)) })

        addSlider(
            content, "屏幕至镜片距离", "测量手机显示面到镜片光学中心的距离", 1000,
            ((settings.screenToLensDistanceMm - DisplaySettings.MIN_SCREEN_TO_LENS_MM) /
                (DisplaySettings.MAX_SCREEN_TO_LENS_MM - DisplaySettings.MIN_SCREEN_TO_LENS_MM) * 1000f).toInt(),
            { p -> "%.1f mm".format(DisplaySettings.MIN_SCREEN_TO_LENS_MM +
                p / 1000f * (DisplaySettings.MAX_SCREEN_TO_LENS_MM - DisplaySettings.MIN_SCREEN_TO_LENS_MM)) },
        ) { p -> settings.copy(screenToLensDistanceMm = DisplaySettings.MIN_SCREEN_TO_LENS_MM +
            p / 1000f * (DisplaySettings.MAX_SCREEN_TO_LENS_MM - DisplaySettings.MIN_SCREEN_TO_LENS_MM)) }

        content.addView(makeButton("根据屏幕高度和镜片距离估算垂直 FOV") {
            settings = settings.copy(fovDegrees = estimateVerticalFovDegrees())
            settingsStore.save(settings)
            updateRendererSettings()
            recreateSettingsPanelContent()
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(40)).apply { topMargin = dp(5) }
        })

        content.addView(label(
            "手机在眼镜盒中的垂直定位方式。请根据手机实际抵靠的位置选择，中心、顶部和底部会改变镜片中心的垂直计算基准。",
            secondary = true,
        ).apply { setPadding(0, dp(10), 0, dp(5)) })
        val alignmentRow = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            gravity = Gravity.CENTER_VERTICAL
        }
        val alignmentOptions = listOf(
            DisplaySettings.ALIGNMENT_CENTER to "屏幕居中",
            DisplaySettings.ALIGNMENT_BOTTOM to "底边对齐",
            DisplaySettings.ALIGNMENT_TOP to "顶边对齐",
        )
        alignmentOptions.forEachIndexed { index, option ->
            val selected = settings.screenVerticalAlignment == option.first
            alignmentRow.addView(makeButton((if (selected) "● " else "") + option.second) {
                settings = settings.copy(screenVerticalAlignment = option.first)
                settingsStore.save(settings)
                updateRendererSettings()
                recreateSettingsPanelContent()
            }.apply {
                textSize = 10f
                layoutParams = LinearLayout.LayoutParams(0, dp(38), 1f).apply {
                    if (index > 0) leftMargin = dp(4)
                }
            })
        }
        content.addView(alignmentRow)

        content.addView(label(
            "托盘距离默认 35 mm 仅作为初始值，不代表 Daydream View 的官方尺寸。Android 屏幕 DPI 也可能存在设备误差，请优先使用卡尺实测并通过镜片中心标记微调。",
            secondary = true,
        ).apply { setPadding(0, dp(5), 0, dp(3)) })

        addSlider(
            content, "托盘边缘至镜片中心", "仅在底边或顶边对齐时使用", 1000,
            ((settings.trayToLensCenterMm - DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM) /
                (DisplaySettings.MAX_TRAY_TO_LENS_CENTER_MM - DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM) * 1000f).toInt(),
            { p -> "%.1f mm".format(DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM +
                p / 1000f * (DisplaySettings.MAX_TRAY_TO_LENS_CENTER_MM - DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM)) },
        ) { p -> settings.copy(trayToLensCenterMm = DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM +
            p / 1000f * (DisplaySettings.MAX_TRAY_TO_LENS_CENTER_MM - DisplaySettings.MIN_TRAY_TO_LENS_CENTER_MM)) }

        addSlider(
            content, "个人瞳距 IPD", "左右眼虚拟相机的间距，应按使用者瞳距设置", 1000,
            ((settings.ipdMm - 50f) / 25f * 1000f).toInt(),
            { p -> "%.1f mm".format(50f + p / 1000f * 25f) },
        ) { p -> settings.copy(ipdMm = 50f + p / 1000f * 25f) }

        addSlider(
            content, "头显镜片中心间距", "两片实体镜片中心之间的距离，决定左右屏幕区域的光学中心", 1000,
            ((settings.lensSeparationMm - DisplaySettings.MIN_LENS_SEPARATION_MM) /
                (DisplaySettings.MAX_LENS_SEPARATION_MM - DisplaySettings.MIN_LENS_SEPARATION_MM) * 1000f).toInt(),
            { p -> "%.1f mm".format(DisplaySettings.MIN_LENS_SEPARATION_MM +
                p / 1000f * (DisplaySettings.MAX_LENS_SEPARATION_MM - DisplaySettings.MIN_LENS_SEPARATION_MM)) },
        ) { p -> settings.copy(lensSeparationMm = DisplaySettings.MIN_LENS_SEPARATION_MM +
            p / 1000f * (DisplaySettings.MAX_LENS_SEPARATION_MM - DisplaySettings.MIN_LENS_SEPARATION_MM)) }

        addSlider(
            content, "镜片中心 X 微调", "在实体镜片间距计算结果上对左右眼作对称微调", 1000,
            ((settings.lensCenterX - 0.40f) / 0.20f * 1000f).toInt(),
            { p -> "%.3f".format(0.40f + p / 1000f * 0.20f) },
        ) { p -> settings.copy(lensCenterX = 0.40f + p / 1000f * 0.20f) }

        addSlider(
            content, "镜片中心 Y", "每只眼睛区域内的垂直光学中心", 1000,
            ((settings.lensCenterY - 0.40f) / 0.20f * 1000f).toInt(),
            { p -> "%.3f".format(0.40f + p / 1000f * 0.20f) },
        ) { p -> settings.copy(lensCenterY = 0.40f + p / 1000f * 0.20f) }

        addSlider(
            content, "桶形畸变 K1", "镜片径向畸变的一阶系数", 1000,
            ((settings.distortionK1 + 0.30f) / 0.90f * 1000f).toInt(),
            { p -> "%.3f".format(-0.30f + p / 1000f * 0.90f) },
        ) { p -> settings.copy(distortionK1 = -0.30f + p / 1000f * 0.90f) }

        addSlider(
            content, "畸变 K2", "镜片径向畸变的二阶系数", 1000,
            ((settings.distortionK2 + 0.20f) / 0.50f * 1000f).toInt(),
            { p -> "%.3f".format(-0.20f + p / 1000f * 0.50f) },
        ) { p -> settings.copy(distortionK2 = -0.20f + p / 1000f * 0.50f) }

        addSlider(
            content, "视场角 FOV", "测试场景水平/垂直投影视场参数", 1000,
            ((settings.fovDegrees - 60f) / 55f * 1000f).toInt(),
            { p -> "%.1f°".format(60f + p / 1000f * 55f) },
        ) { p -> settings.copy(fovDegrees = 60f + p / 1000f * 55f) }

        addSlider(
            content, "画面柔化", "当前用于校准预览，串流纹理接入后作用于视频", 1000,
            (settings.blur * 1000f).toInt(),
            { p -> "${p / 10}%" },
        ) { p -> settings.copy(blur = p / 1000f) }

        addSlider(
            content, "边缘增强", "当前为测试场景对比度预览", 1000,
            (settings.sharpness * 1000f).toInt(),
            { p -> "${p / 10}%" },
        ) { p -> settings.copy(sharpness = p / 1000f) }

        content.addView(label(
            "校准顺序建议：先调 IPD，使中心标记单一清晰；再调镜片中心 X/Y，使十字落在双眼可视中心；最后微调 K1/K2 和 FOV，直到直线边缘自然。真实屈光度和屏幕到镜片焦距由物理镜片决定。",
            secondary = true,
        ).apply { setPadding(0, dp(16), 0, dp(12)) })

        panel.addView(makeButton("恢复默认校准") {
            settings = DisplaySettings()
            settingsStore.save(settings)
            updateRendererSettings()
            networkClient.publishDisplaySettings(settings.toJson())
            recreateSettingsPanelContent()
        }.apply {
            layoutParams = LinearLayout.LayoutParams(-1, dp(42))
        })
        return panel
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
     * DPI is an estimate, so the manual Y slider remains available for fine tuning.
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
        lastRawPose = Quaternion.fromRotationMatrix(matrix)

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
        if (Looper.myLooper() == Looper.getMainLooper()) {
            poseText.text = value
        } else {
            mainHandler.post { if (::poseText.isInitialized && !isFinishing && !isDestroyed) poseText.text = value }
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

    private data class Quaternion(val x: Float, val y: Float, val z: Float, val w: Float) {
        fun normalized(): Quaternion {
            val length = kotlin.math.sqrt(x * x + y * y + z * z + w * w).coerceAtLeast(1e-8f)
            return Quaternion(x / length, y / length, z / length, w / length)
        }

        fun toEulerDegrees(): Triple<Float, Float, Float> {
            val q = normalized()
            val sinr = 2f * (q.w * q.x + q.y * q.z)
            val cosr = 1f - 2f * (q.x * q.x + q.y * q.y)
            val roll = atan2(sinr, cosr)
            val sinp = 2f * (q.w * q.y - q.z * q.x)
            val pitch = if (kotlin.math.abs(sinp) >= 1f) {
                Math.copySign((Math.PI / 2.0).toFloat(), sinp)
            } else asin(sinp)
            val siny = 2f * (q.w * q.z + q.x * q.y)
            val cosy = 1f - 2f * (q.y * q.y + q.z * q.z)
            val yaw = atan2(siny, cosy)
            val factor = (180.0 / Math.PI).toFloat()
            return Triple(yaw * factor, pitch * factor, roll * factor)
        }

        companion object {
            val IDENTITY = Quaternion(0f, 0f, 0f, 1f)

            fun inverse(value: Quaternion): Quaternion {
                val q = value.normalized()
                return Quaternion(-q.x, -q.y, -q.z, q.w)
            }

            fun multiply(a: Quaternion, b: Quaternion): Quaternion = Quaternion(
                a.w * b.x + a.x * b.w + a.y * b.z - a.z * b.y,
                a.w * b.y - a.x * b.z + a.y * b.w + a.z * b.x,
                a.w * b.z + a.x * b.y - a.y * b.x + a.z * b.w,
                a.w * b.w - a.x * b.x - a.y * b.y - a.z * b.z,
            ).normalized()

            fun fromRotationMatrix(m: FloatArray): Quaternion {
                val m00 = m[0]; val m01 = m[1]; val m02 = m[2]
                val m10 = m[3]; val m11 = m[4]; val m12 = m[5]
                val m20 = m[6]; val m21 = m[7]; val m22 = m[8]
                val trace = m00 + m11 + m22
                return if (trace > 0f) {
                    val s = kotlin.math.sqrt(trace + 1f) * 2f
                    Quaternion((m21 - m12) / s, (m02 - m20) / s, (m10 - m01) / s, 0.25f * s)
                } else if (m00 > m11 && m00 > m22) {
                    val s = kotlin.math.sqrt(1f + m00 - m11 - m22) * 2f
                    Quaternion(0.25f * s, (m01 + m10) / s, (m02 + m20) / s, (m21 - m12) / s)
                } else if (m11 > m22) {
                    val s = kotlin.math.sqrt(1f + m11 - m00 - m22) * 2f
                    Quaternion((m01 + m10) / s, 0.25f * s, (m12 + m21) / s, (m02 - m20) / s)
                } else {
                    val s = kotlin.math.sqrt(1f + m22 - m00 - m11) * 2f
                    Quaternion((m02 + m20) / s, (m12 + m21) / s, 0.25f * s, (m10 - m01) / s)
                }.normalized()
            }
        }
    }
}
