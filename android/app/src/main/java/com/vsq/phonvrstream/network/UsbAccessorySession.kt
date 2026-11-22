package com.vsq.phonvrstream.network

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.hardware.usb.UsbAccessory
import android.hardware.usb.UsbManager
import android.os.Build
import android.os.ParcelFileDescriptor
import android.util.Log
import java.io.Closeable
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream
import java.io.OutputStream
import java.net.SocketTimeoutException
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

/**
 * Android Open Accessory bulk transport. Android is the USB peripheral in AOA
 * mode; this class opens the accessory file descriptor exposed by UsbManager.
 * No ADB, RNDIS, USB tethering or Android USB-host APIs are used.
 */
class UsbAccessorySession private constructor(
    private val accessory: UsbAccessory,
    private val descriptor: ParcelFileDescriptor,
) : Closeable {
    private val input = FileInputStream(descriptor.fileDescriptor)
    private val output = FileOutputStream(descriptor.fileDescriptor)
    private val running = AtomicBoolean(true)
    private val sequence = AtomicInteger(0)
    private val writeLock = Any()
    private val controlChunks = ArrayBlockingQueue<ByteArray>(128)
    @Volatile private var currentControlChunk: ByteArray? = null
    @Volatile private var currentControlOffset = 0
    @Volatile var videoPacketListener: ((ByteArray) -> Unit)? = null
    @Volatile private var readerThread: Thread? = null
    @Volatile private var receiveFailure: String? = null
    private val controlRxLogBudget = AtomicInteger(6)
    private val controlTxLogBudget = AtomicInteger(6)
    private val videoDispatcher = UsbVideoPacketDispatcher(
        onPacket = { packet -> videoPacketListener?.invoke(packet) },
        onStats = { stats ->
            Log.i(TAG, "AOA video RX dispatcher: accepted=${stats.acceptedPackets} " +
                "forwarded=${stats.deliveredPackets} dropped=${stats.droppedPackets} " +
                "callbackFailures=${stats.callbackFailures} queued=${stats.queueDepth} " +
                "callbackMax=${"%.1f".format(stats.maxCallbackMs)}ms")
        },
        onError = { error -> Log.w(TAG, "AOA video packet forwarding failed: ${error.message}", error) },
    )

    val accessoryName: String
        get() = listOf(accessory.manufacturer, accessory.model).filter { !it.isNullOrBlank() }.joinToString(" ").ifBlank { "Android USB accessory" }

    val isClosed: Boolean get() = !running.get()

    val controlInput: InputStream = object : InputStream() {
        override fun read(): Int {
            val buffer = ByteArray(1)
            val count = read(buffer, 0, 1)
            return if (count < 0) -1 else buffer[0].toInt() and 0xFF
        }

        override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
            require(offset >= 0 && length >= 0 && offset + length <= buffer.size)
            if (length == 0) return 0
            while (true) {
                if (!running.get() && currentControlChunk == null && controlChunks.isEmpty()) {
                    val failure = receiveFailure
                    if (failure != null) throw java.io.IOException("AOA USB receive stopped: $failure")
                    return -1
                }
                var chunk = currentControlChunk
                if (chunk == null || currentControlOffset >= chunk.size) {
                    chunk = try {
                        controlChunks.poll(CONTROL_READ_POLL_MS, TimeUnit.MILLISECONDS)
                    } catch (_: InterruptedException) {
                        Thread.currentThread().interrupt()
                        throw java.io.InterruptedIOException("USB control read interrupted")
                    }
                    if (chunk == null) {
                        if (!running.get()) {
                            val failure = receiveFailure
                            if (failure != null) throw java.io.IOException("AOA USB receive stopped: $failure")
                            return -1
                        }
                        throw SocketTimeoutException("USB control read timeout")
                    }
                    currentControlChunk = chunk
                    currentControlOffset = 0
                }
                val start = currentControlOffset
                val count = minOf(length, chunk.size - start)
                System.arraycopy(chunk, start, buffer, offset, count)
                currentControlOffset += count
                if (currentControlOffset >= chunk.size) {
                    currentControlChunk = null
                    currentControlOffset = 0
                }
                return count
            }
        }
    }

    val controlOutput: OutputStream = object : OutputStream() {
        override fun write(value: Int) {
            write(byteArrayOf(value.toByte()), 0, 1)
        }

        override fun write(buffer: ByteArray, offset: Int, length: Int) {
            require(offset >= 0 && length >= 0 && offset + length <= buffer.size)
            if (length == 0) return
            writeFrame(CHANNEL_CONTROL, buffer.copyOfRange(offset, offset + length))
        }

        override fun flush() = Unit
        override fun close() = this@UsbAccessorySession.close()
    }

    fun start() {
        check(running.get()) { "USB accessory session is closed" }
        videoDispatcher.start()
        readerThread = Thread({ readLoop() }, "PhoneVR-AOA-RX").apply {
            isDaemon = true
            start()
        }
        Log.i(TAG, "AOA Bulk session opened: $accessoryName")
    }

    fun sendPose(payload: ByteArray) = writeFrame(CHANNEL_POSE, payload)


    private fun writeFrame(channel: Int, payload: ByteArray) {
        check(running.get()) { "USB accessory session is closed" }
        val frameSequence = sequence.incrementAndGet()
        val header = UsbMuxProtocol.encodeHeader(channel, payload.size, frameSequence)
        val frame = ByteArray(header.size + payload.size)
        System.arraycopy(header, 0, frame, 0, header.size)
        if (payload.isNotEmpty()) System.arraycopy(payload, 0, frame, header.size, payload.size)
        synchronized(writeLock) {
            check(running.get()) { "USB accessory session is closed" }
            output.write(frame)
            output.flush()
        }
        if (channel == CHANNEL_CONTROL) logControlFrame("TX", frameSequence, payload, controlTxLogBudget)
    }

    private fun readLoop() {
        try {
            val header = ByteArray(HEADER_SIZE)
            while (running.get()) {
                readFully(input, header)
                val decoded = UsbMuxProtocol.decodeHeader(header)
                val payload = ByteArray(decoded.payloadLength)
                if (decoded.payloadLength > 0) readFully(input, payload)
                when (decoded.channel) {
                    CHANNEL_CONTROL -> {
                        logControlFrame("RX", decoded.sequence, payload, controlRxLogBudget)
                        controlChunks.put(payload)
                    }
                    CHANNEL_VIDEO -> {
                        // Never run loopback UDP forwarding on the USB read
                        // thread. A slow local receiver must not stop Bulk IN
                        // from being drained, otherwise the PC Bulk OUT stalls.
                        if (videoPacketListener != null) videoDispatcher.offer(payload)
                    }
                }
            }
        } catch (error: Throwable) {
            if (running.get()) {
                receiveFailure = "${error.javaClass.simpleName}: ${error.message ?: "no details"}"
                Log.w(TAG, "AOA Bulk receive stopped: $receiveFailure", error)
            }
        } finally {
            running.set(false)
            videoDispatcher.close()
            runCatching { descriptor.close() }
        }
    }

    private fun logControlFrame(direction: String, frameSequence: Int, payload: ByteArray, budget: AtomicInteger) {
        if (budget.getAndDecrement() <= 0) return
        val previewBytes = payload.copyOfRange(0, minOf(payload.size, 96))
        val preview = previewBytes.toString(Charsets.UTF_8)
            .replace("\\", "\\\\")
            .replace("\r", "\\r")
            .replace("\n", "\\n")
        val hex = previewBytes.joinToString("") { "%02X".format(it.toInt() and 0xFF) }
        Log.i(TAG, "AOA control $direction frame: seq=$frameSequence bytes=${payload.size} preview=[$preview] hex=[$hex]")
    }

    private fun readFully(stream: InputStream, buffer: ByteArray) {
        var offset = 0
        while (offset < buffer.size && running.get()) {
            val count = stream.read(buffer, offset, buffer.size - offset)
            if (count < 0) throw java.io.EOFException("USB accessory disconnected")
            if (count == 0) continue
            offset += count
        }
        if (offset != buffer.size) throw java.io.EOFException("USB accessory session closed")
    }

    override fun close() {
        if (!running.getAndSet(false)) return
        videoDispatcher.close()
        runCatching { descriptor.close() }
        runCatching { input.close() }
        runCatching { output.close() }
        readerThread?.interrupt()
        controlChunks.clear()
        Log.i(TAG, "AOA Bulk session closed")
    }

    companion object {
        private const val TAG = "PhoneVRAOA"
        private const val ACTION_USB_PERMISSION = "com.vsq.phonvrstream.USB_ACCESSORY_PERMISSION"
        private const val PERMISSION_TIMEOUT_MS = 15_000L
        private const val CONTROL_READ_POLL_MS = 500L
        private const val HEADER_SIZE = UsbMuxProtocol.HEADER_SIZE
        private const val CHANNEL_CONTROL = UsbMuxProtocol.CHANNEL_CONTROL
        private const val CHANNEL_POSE = UsbMuxProtocol.CHANNEL_POSE
        private const val CHANNEL_VIDEO = UsbMuxProtocol.CHANNEL_VIDEO

        /** Blocks on a worker thread while Android displays the USB permission prompt. */
        fun openFirstAccessory(context: Context): UsbAccessorySession {
            val appContext = context.applicationContext
            val manager = appContext.getSystemService(Context.USB_SERVICE) as UsbManager
            val accessory = manager.accessoryList?.firstOrNull()
                ?: throw IllegalStateException("等待 PC Host 将手机切换到 Android Accessory Mode（AOA）")
            if (!manager.hasPermission(accessory)) requestPermission(appContext, manager, accessory)
            val descriptor = manager.openAccessory(accessory)
                ?: throw IllegalStateException("Android 无法打开 USB accessory 文件描述符")
            return UsbAccessorySession(accessory, descriptor).also { it.start() }
        }

        private fun requestPermission(context: Context, manager: UsbManager, accessory: UsbAccessory) {
            val latch = CountDownLatch(1)
            val granted = AtomicBoolean(false)
            val receiver = object : BroadcastReceiver() {
                override fun onReceive(context: Context?, intent: Intent?) {
                    if (intent?.action != ACTION_USB_PERMISSION) return
                    granted.set(intent.getBooleanExtra(UsbManager.EXTRA_PERMISSION_GRANTED, false))
                    latch.countDown()
                }
            }
            val filter = IntentFilter(ACTION_USB_PERMISSION)
            if (Build.VERSION.SDK_INT >= 33) {
                context.registerReceiver(receiver, filter, Context.RECEIVER_NOT_EXPORTED)
            } else {
                @Suppress("DEPRECATION")
                context.registerReceiver(receiver, filter)
            }
            try {
                val flags = PendingIntent.FLAG_UPDATE_CURRENT or
                    (if (Build.VERSION.SDK_INT >= 31) PendingIntent.FLAG_MUTABLE else 0)
                val pendingIntent = PendingIntent.getBroadcast(
                    context, 705, Intent(ACTION_USB_PERMISSION).setPackage(context.packageName), flags,
                )
                manager.requestPermission(accessory, pendingIntent)
                if (!latch.await(PERMISSION_TIMEOUT_MS, TimeUnit.MILLISECONDS)) {
                    throw SocketTimeoutException("等待 Android USB accessory 权限确认超时")
                }
                if (!granted.get()) throw SecurityException("用户拒绝 PhoneVR USB accessory 权限")
            } finally {
                runCatching { context.unregisterReceiver(receiver) }
            }
        }

    }
}
