package com.vsq.phonvrstream.controller

import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothProfile
import android.bluetooth.BluetoothStatusCodes
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.content.Context
import android.os.Build
import android.os.Handler
import android.os.HandlerThread
import android.os.SystemClock
import android.util.Log
import com.vsq.phonvrstream.tracking.Quaternion
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap

internal enum class ControllerHand(val wireValue: String, val label: String) {
    LEFT("left", "左手柄"),
    RIGHT("right", "右手柄"),
}

internal data class DaydreamDeviceCandidate(
    val address: String,
    val name: String,
    val rssi: Int,
)

/** Owns two independent BLE GATT links and assigns each discovered remote to a hand explicitly. */
internal class DaydreamControllerManager(
    context: Context,
    private val onState: (ControllerHand, ControllerInputState) -> Unit,
    private val onStatus: (ControllerHand, String) -> Unit,
    private val onScanResults: (List<DaydreamDeviceCandidate>) -> Unit,
) {
    companion object {
        private const val TAG = "PhoneVRDaydream"
        private const val PREFS = "phonevr_daydream_controllers"
        private const val SCAN_DURATION_MS = 10_000L
        private const val RECONNECT_BASE_MS = 1_000L
        private const val RECONNECT_MAX_MS = 8_000L
        private const val PACKET_STALE_TIMEOUT_MS = 750L
        private val SERVICE_UUID = UUID.fromString(DaydreamControllerProtocol.SERVICE_UUID)
        private val REPORT_UUID = UUID.fromString(DaydreamControllerProtocol.REPORT_CHARACTERISTIC_UUID)
        private val CONTROL_UUID = UUID.fromString(DaydreamControllerProtocol.CONTROL_CHARACTERISTIC_UUID)
        private val CCCD_UUID = UUID.fromString(DaydreamControllerProtocol.CLIENT_CONFIGURATION_UUID)
        private const val APP_RECENTER_HOLD_MS = 800L
    }

    private val appContext = context.applicationContext
    private val bluetoothAdapter: BluetoothAdapter? =
        (appContext.getSystemService(Context.BLUETOOTH_SERVICE) as? android.bluetooth.BluetoothManager)?.adapter
    private val preferences = appContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val callbackThread = HandlerThread("PhoneVR-Daydream-BLE").apply { start() }
    private val handler = Handler(callbackThread.looper)
    private val links = ConcurrentHashMap<ControllerHand, Link>()
    private val candidates = ConcurrentHashMap<String, DaydreamDeviceCandidate>()
    @Volatile private var scanCallback: ScanCallback? = null
    @Volatile private var scanGeneration = 0L
    @Volatile private var closed = false

    private class Link(
        val hand: ControllerHand,
        val address: String,
        @Volatile var gatt: BluetoothGatt? = null,
        @Volatile var notificationsReady: Boolean = false,
        @Volatile var manualDisconnect: Boolean = false,
        @Volatile var reconnectAttempt: Int = 0,
        @Volatile var reconnectScheduled: Boolean = false,
        @Volatile var packetGeneration: Long = 0L,
        @Volatile var appPressGeneration: Long = 0L,
        @Volatile var lastState: ControllerInputState = ControllerInputState(false, SystemClock.elapsedRealtimeNanos()),
    )

    fun hasBluetoothAdapter(): Boolean = bluetoothAdapter != null

    fun isBluetoothEnabled(): Boolean = try {
        bluetoothAdapter?.isEnabled == true
    } catch (_: SecurityException) {
        false
    }

    fun scan() {
        if (closed) return
        val adapter = bluetoothAdapter
        val scanner = try { adapter?.bluetoothLeScanner } catch (error: SecurityException) { null }
        if (scanner == null) {
            onScanResults(emptyList())
            onStatus(ControllerHand.LEFT, "蓝牙扫描不可用，请检查蓝牙权限和系统蓝牙状态")
            return
        }
        stopScan()
        val currentScanGeneration = scanGeneration
        candidates.clear()
        onScanResults(emptyList())
        val callback = object : ScanCallback() {
            override fun onScanResult(callbackType: Int, result: ScanResult) {
                val name = result.scanRecord?.deviceName
                    ?: runCatching { result.device.name }.getOrNull()
                    ?: return
                if (!name.contains("Daydream controller", ignoreCase = true)) return
                val candidate = DaydreamDeviceCandidate(result.device.address, name, result.rssi)
                candidates[candidate.address] = candidate
                onScanResults(candidates.values.sortedByDescending { it.rssi })
            }

            override fun onBatchScanResults(results: MutableList<ScanResult>) {
                results.forEach { onScanResult(0, it) }
            }

            override fun onScanFailed(errorCode: Int) {
                onStatus(ControllerHand.LEFT, "Daydream BLE 扫描失败：error=$errorCode")
                stopScan()
            }
        }
        scanCallback = callback
        try {
            scanner.startScan(
                null,
                ScanSettings.Builder().setScanMode(ScanSettings.SCAN_MODE_LOW_LATENCY).build(),
                callback,
            )
            onStatus(ControllerHand.LEFT, "正在扫描 Daydream Controller…")
            handler.postDelayed({
                if (scanGeneration == currentScanGeneration) stopScan()
            }, SCAN_DURATION_MS)
        } catch (error: SecurityException) {
            scanCallback = null
            onStatus(ControllerHand.LEFT, "缺少蓝牙扫描权限：${error.message}")
        } catch (error: IllegalStateException) {
            scanCallback = null
            onStatus(ControllerHand.LEFT, "蓝牙扫描启动失败：${error.message}")
        }
    }

    fun stopScan() {
        scanGeneration++
        val callback = scanCallback ?: return
        scanCallback = null
        try {
            bluetoothAdapter?.bluetoothLeScanner?.stopScan(callback)
        } catch (_: SecurityException) {
        } catch (_: IllegalStateException) {
        }
    }

    fun connect(address: String, hand: ControllerHand) {
        if (closed) return
        val normalizedAddress = address.trim().uppercase()
        require(BluetoothAdapter.checkBluetoothAddress(normalizedAddress)) { "蓝牙地址无效" }
        val otherHand = if (hand == ControllerHand.LEFT) ControllerHand.RIGHT else ControllerHand.LEFT
        require(links[otherHand]?.address != normalizedAddress) { "同一个手柄不能同时分配给左右手" }
        stopScan()
        disconnectInternal(hand, clearSavedAddress = false)
        preferences.edit().putString(handPreferenceKey(hand), normalizedAddress).apply()
        val link = Link(hand, normalizedAddress)
        links[hand] = link
        onStatus(hand, "正在连接 $normalizedAddress…")
        onState(hand, link.lastState)
        openGatt(link)
    }

    fun disconnect(hand: ControllerHand) {
        disconnectInternal(hand, clearSavedAddress = true)
    }

    fun restoreSavedConnections() {
        if (closed) return
        if (!isBluetoothEnabled()) {
            onStatus(ControllerHand.LEFT, "请先在 Android 系统设置中打开蓝牙")
            return
        }
        for (hand in ControllerHand.values()) {
            val address = preferences.getString(handPreferenceKey(hand), null)?.takeIf { it.isNotBlank() } ?: continue
            if (links[hand] == null) {
                runCatching { connect(address, hand) }
                    .onFailure { onStatus(hand, "恢复 ${hand.label} 失败：${it.message}") }
            }
        }
    }

    fun connectedState(hand: ControllerHand): ControllerInputState =
        links[hand]?.lastState ?: ControllerInputState(false, SystemClock.elapsedRealtimeNanos())

    fun close() {
        closed = true
        stopScan()
        ControllerHand.values().forEach { disconnectInternal(it, clearSavedAddress = false) }
        callbackThread.quitSafely()
    }

    private fun openGatt(link: Link) {
        if (closed || links[link.hand] !== link) return
        val device = try {
            bluetoothAdapter?.getRemoteDevice(link.address)
        } catch (error: Exception) {
            onStatus(link.hand, "无法打开蓝牙设备：${error.message}")
            scheduleReconnect(link)
            return
        }
        if (device == null) {
            onStatus(link.hand, "找不到蓝牙设备 ${link.address}")
            scheduleReconnect(link)
            return
        }
        try {
            val gatt = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                device.connectGatt(appContext, false, callbackFor(link), BluetoothDevice.TRANSPORT_LE)
            } else {
                @Suppress("DEPRECATION")
                device.connectGatt(appContext, false, callbackFor(link))
            }
            if (gatt == null) {
                onStatus(link.hand, "Android 未能创建 BLE GATT 连接")
                scheduleReconnect(link)
            } else {
                link.gatt = gatt
            }
        } catch (error: SecurityException) {
            onStatus(link.hand, "缺少蓝牙连接权限：${error.message}")
        } catch (error: Exception) {
            onStatus(link.hand, "BLE GATT 连接失败：${error.message}")
            scheduleReconnect(link)
        }
    }

    private fun callbackFor(link: Link): BluetoothGattCallback = object : BluetoothGattCallback() {
        override fun onConnectionStateChange(gatt: BluetoothGatt, status: Int, newState: Int) {
            if (links[link.hand] !== link || closed) {
                gatt.close()
                return
            }
            if (status == BluetoothGatt.GATT_SUCCESS && newState == BluetoothProfile.STATE_CONNECTED) {
                link.reconnectAttempt = 0
                link.reconnectScheduled = false
                onStatus(link.hand, "蓝牙已连接，正在发现 Daydream 服务…")
                try {
                    if (!gatt.discoverServices()) {
                        onStatus(link.hand, "Daydream 服务发现请求失败")
                        disconnectGattForRetry(link, gatt)
                    }
                } catch (error: SecurityException) {
                    onStatus(link.hand, "服务发现缺少蓝牙权限：${error.message}")
                    disconnectGattForRetry(link, gatt)
                }
            } else if (newState == BluetoothProfile.STATE_DISCONNECTED) {
                link.notificationsReady = false
                closeGatt(link, gatt)
                emitDisconnected(link)
                if (!link.manualDisconnect && !closed && links[link.hand] === link) {
                    onStatus(link.hand, "${link.hand.label}已断开，准备重连…")
                    scheduleReconnect(link)
                }
            } else if (status != BluetoothGatt.GATT_SUCCESS) {
                onStatus(link.hand, "BLE 连接状态异常：status=$status")
                disconnectGattForRetry(link, gatt)
            }
        }

        @Suppress("DEPRECATION")
        override fun onServicesDiscovered(gatt: BluetoothGatt, status: Int) {
            if (links[link.hand] !== link || status != BluetoothGatt.GATT_SUCCESS) {
                onStatus(link.hand, "Daydream GATT 服务发现失败：status=$status")
                disconnectGattForRetry(link, gatt)
                return
            }
            val characteristic = gatt.getService(SERVICE_UUID)?.getCharacteristic(REPORT_UUID)
            if (characteristic == null) {
                onStatus(link.hand, "设备没有提供 FE55 / Daydream 状态特征")
                disconnectGattForRetry(link, gatt)
                return
            }
            val canNotify = characteristic.properties and BluetoothGattCharacteristic.PROPERTY_NOTIFY != 0
            val canIndicate = characteristic.properties and BluetoothGattCharacteristic.PROPERTY_INDICATE != 0
            if (!canNotify && !canIndicate) {
                onStatus(link.hand, "Daydream 状态特征不支持通知或指示")
                disconnectGattForRetry(link, gatt)
                return
            }
            try {
                if (!gatt.setCharacteristicNotification(characteristic, true)) {
                    onStatus(link.hand, "启用 Daydream 状态通知失败")
                    disconnectGattForRetry(link, gatt)
                    return
                }
                val descriptor = characteristic.getDescriptor(CCCD_UUID)
                if (descriptor == null) {
                    onStatus(link.hand, "Daydream 状态特征缺少 CCCD 描述符")
                    disconnectGattForRetry(link, gatt)
                    return
                }
                val value = if (canNotify) BluetoothGattDescriptor.ENABLE_NOTIFICATION_VALUE
                    else BluetoothGattDescriptor.ENABLE_INDICATION_VALUE
                descriptor.value = value
                if (!gatt.writeDescriptor(descriptor)) {
                    onStatus(link.hand, "写入 Daydream 通知配置失败")
                    disconnectGattForRetry(link, gatt)
                }
            } catch (error: SecurityException) {
                onStatus(link.hand, "启用 Daydream 通知缺少蓝牙权限：${error.message}")
                disconnectGattForRetry(link, gatt)
            }
        }

        override fun onDescriptorWrite(gatt: BluetoothGatt, descriptor: BluetoothGattDescriptor, status: Int) {
            if (descriptor.uuid != CCCD_UUID || links[link.hand] !== link) return
            if (status == BluetoothGatt.GATT_SUCCESS) {
                link.notificationsReady = true
                link.lastState = ControllerInputState(connected = true, timestampNs = SystemClock.elapsedRealtimeNanos())
                onState(link.hand, link.lastState)
                schedulePacketWatchdog(link)
                onStatus(link.hand, "${link.hand.label}已连接，正在接收输入")
            } else {
                onStatus(link.hand, "Daydream 通知订阅失败：status=$status")
                disconnectGattForRetry(link, gatt)
            }
        }

        override fun onCharacteristicChanged(gatt: BluetoothGatt, characteristic: BluetoothGattCharacteristic) {
            if (characteristic.uuid != REPORT_UUID) return
            @Suppress("DEPRECATION")
            handleNotification(link, characteristic.value)
        }

        override fun onCharacteristicChanged(
            gatt: BluetoothGatt,
            characteristic: BluetoothGattCharacteristic,
            value: ByteArray,
        ) {
            if (characteristic.uuid == REPORT_UUID) handleNotification(link, value)
        }

        override fun onCharacteristicWrite(
            gatt: BluetoothGatt,
            characteristic: BluetoothGattCharacteristic,
            status: Int,
        ) {
            if (characteristic.uuid != CONTROL_UUID || links[link.hand] !== link) return
            if (status == BluetoothGatt.GATT_SUCCESS) {
                Log.i(TAG, "Daydream hardware fusion reference recentered: hand=${link.hand.wireValue}")
            } else {
                Log.w(TAG, "Daydream hardware recenter write failed: hand=${link.hand.wireValue} status=$status")
            }
        }
    }

    private fun handleNotification(link: Link, payload: ByteArray?) {
        if (payload == null || links[link.hand] !== link || !link.notificationsReady) return
        val decoded = DaydreamControllerProtocol.decode(payload, SystemClock.elapsedRealtimeNanos()) ?: run {
            Log.w(TAG, "Ignoring malformed Daydream packet: hand=${link.hand.wireValue} bytes=${payload.size}")
            return
        }
        val next = decoded.state
        val previous = link.lastState
        val state = if (previous.hasOrientation && next.hasOrientation) {
            val aligned = Quaternion(next.orientationX, next.orientationY, next.orientationZ, next.orientationW).alignedTo(
                Quaternion(previous.orientationX, previous.orientationY, previous.orientationZ, previous.orientationW),
            )
            next.copy(
                orientationX = aligned.x,
                orientationY = aligned.y,
                orientationZ = aligned.z,
                orientationW = aligned.w,
            )
        } else {
            next
        }
        link.lastState = state
        link.packetGeneration++
        if (next.app && !previous.app) {
            val generation = synchronized(link) {
                link.appPressGeneration += 1L
                link.appPressGeneration
            }
            handler.postDelayed({
                if (!closed && links[link.hand] === link && link.notificationsReady &&
                    link.appPressGeneration == generation && link.lastState.app) {
                    requestHardwareRecenter(link)
                }
            }, APP_RECENTER_HOLD_MS)
        } else if (!next.app && previous.app) {
            synchronized(link) { link.appPressGeneration += 1L }
        }
        onState(link.hand, state)
        schedulePacketWatchdog(link)
    }

    private fun requestHardwareRecenter(link: Link) {
        if (closed || links[link.hand] !== link || !link.notificationsReady || !link.lastState.app) return
        val gatt = link.gatt ?: return
        try {
            val characteristic = gatt.getService(SERVICE_UUID)?.getCharacteristic(CONTROL_UUID)
            if (characteristic == null) {
                Log.i(TAG, "Daydream controller has no hardware recenter characteristic; using host-side alignment only")
                return
            }
            val properties = characteristic.properties
            if (properties and BluetoothGattCharacteristic.PROPERTY_WRITE == 0 &&
                properties and BluetoothGattCharacteristic.PROPERTY_WRITE_NO_RESPONSE == 0) {
                Log.w(TAG, "Daydream control characteristic is not writable")
                return
            }
            val writeType = if (properties and BluetoothGattCharacteristic.PROPERTY_WRITE != 0) {
                BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT
            } else {
                BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
            }
            val accepted = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
                gatt.writeCharacteristic(characteristic, byteArrayOf(0x00), writeType) == BluetoothStatusCodes.SUCCESS
            } else {
                @Suppress("DEPRECATION")
                run {
                    characteristic.writeType = writeType
                    characteristic.value = byteArrayOf(0x00)
                    gatt.writeCharacteristic(characteristic)
                }
            }
            if (!accepted) {
                Log.w(TAG, "Daydream hardware recenter request was rejected: hand=${link.hand.wireValue}")
            }
        } catch (error: SecurityException) {
            Log.w(TAG, "Daydream hardware recenter missing Bluetooth permission", error)
        } catch (error: Exception) {
            Log.w(TAG, "Daydream hardware recenter request failed", error)
        }
    }

    private fun schedulePacketWatchdog(link: Link) {
        val expectedGeneration = link.packetGeneration
        handler.postDelayed({
            if (closed || links[link.hand] !== link || !link.notificationsReady ||
                link.packetGeneration != expectedGeneration || !link.lastState.connected) return@postDelayed
            link.lastState = ControllerInputState(
                connected = false,
                timestampNs = SystemClock.elapsedRealtimeNanos(),
            )
            onState(link.hand, link.lastState)
            onStatus(link.hand, "${link.hand.label}数据超时，等待手柄恢复输入")
        }, PACKET_STALE_TIMEOUT_MS)
    }

    private fun disconnectGattForRetry(link: Link, gatt: BluetoothGatt) {
        link.notificationsReady = false
        try { gatt.disconnect() } catch (_: SecurityException) { }
        closeGatt(link, gatt)
        emitDisconnected(link)
        if (!link.manualDisconnect && !closed && links[link.hand] === link) scheduleReconnect(link)
    }

    private fun closeGatt(link: Link, gatt: BluetoothGatt) {
        if (link.gatt === gatt) link.gatt = null
        runCatching { gatt.close() }
    }

    private fun emitDisconnected(link: Link) {
        link.packetGeneration++
        link.lastState = ControllerInputState(connected = false, timestampNs = SystemClock.elapsedRealtimeNanos())
        onState(link.hand, link.lastState)
    }

    private fun scheduleReconnect(link: Link) {
        val delay = synchronized(link) {
            if (closed || link.manualDisconnect || links[link.hand] !== link || link.reconnectScheduled) return
            link.reconnectScheduled = true
            val attempt = (link.reconnectAttempt + 1).coerceAtMost(8)
            link.reconnectAttempt = attempt
            (RECONNECT_BASE_MS shl (attempt - 1)).coerceAtMost(RECONNECT_MAX_MS)
        }
        handler.postDelayed({
            synchronized(link) { link.reconnectScheduled = false }
            if (!closed && !link.manualDisconnect && links[link.hand] === link && link.gatt == null) {
                onStatus(link.hand, "正在重连 ${link.address}…")
                openGatt(link)
            }
        }, delay)
    }

    private fun disconnectInternal(hand: ControllerHand, clearSavedAddress: Boolean) {
        val link = links.remove(hand)
        link?.manualDisconnect = true
        link?.notificationsReady = false
        link?.packetGeneration = (link?.packetGeneration ?: 0L) + 1L
        link?.gatt?.let { gatt ->
            try { gatt.disconnect() } catch (_: SecurityException) { }
            closeGatt(link, gatt)
        }
        if (clearSavedAddress) preferences.edit().remove(handPreferenceKey(hand)).apply()
        link?.let(::emitDisconnected)
        if (link == null) onState(hand, ControllerInputState(false, SystemClock.elapsedRealtimeNanos()))
        onStatus(hand, if (clearSavedAddress) "${hand.label}已断开" else "${hand.label}未连接")
    }

    private fun handPreferenceKey(hand: ControllerHand): String = when (hand) {
        ControllerHand.LEFT -> "left_address"
        ControllerHand.RIGHT -> "right_address"
    }
}
