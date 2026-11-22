package com.vsq.phonvrstream.network

import java.io.Closeable
import java.util.concurrent.ArrayBlockingQueue
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong

/**
 * Moves AOA video packet forwarding off the USB input thread. The USB reader
 * must keep draining Bulk IN even when the next transport stage is temporarily
 * slow. A bounded queue prevents an unresponsive consumer from growing memory
 * without limit.
 */
internal class UsbVideoPacketDispatcher(
    capacity: Int = DEFAULT_CAPACITY,
    private val onPacket: (ByteArray) -> Unit,
    private val onStats: (Stats) -> Unit = {},
    private val onError: (Throwable) -> Unit = {},
) : Closeable {
    data class Stats(
        val acceptedPackets: Long,
        val deliveredPackets: Long,
        val droppedPackets: Long,
        val callbackFailures: Long,
        val queueDepth: Int,
        val maxCallbackMs: Double,
    )

    private val queue = ArrayBlockingQueue<ByteArray>(capacity.also { require(it > 0) })
    private val running = AtomicBoolean(false)
    private val started = AtomicBoolean(false)
    private val accepted = AtomicLong(0)
    private val delivered = AtomicLong(0)
    private val dropped = AtomicLong(0)
    private val failures = AtomicLong(0)
    private val maxCallbackNs = AtomicLong(0)
    @Volatile private var worker: Thread? = null

    fun start() {
        if (!started.compareAndSet(false, true)) return
        running.set(true)
        worker = Thread(::dispatchLoop, "PhoneVR-AOA-VideoDispatch").apply {
            isDaemon = true
            start()
        }
    }

    /** Fast, non-blocking handoff from the AOA Bulk IN reader. */
    fun offer(packet: ByteArray): Boolean {
        if (!running.get()) return false
        if (!queue.offer(packet)) {
            dropped.incrementAndGet()
            return false
        }
        accepted.incrementAndGet()
        return true
    }

    fun snapshot(): Stats = Stats(
        acceptedPackets = accepted.get(),
        deliveredPackets = delivered.get(),
        droppedPackets = dropped.get(),
        callbackFailures = failures.get(),
        queueDepth = queue.size,
        maxCallbackMs = maxCallbackNs.get() / 1_000_000.0,
    )

    private fun dispatchLoop() {
        var lastStatsNs = System.nanoTime()
        while (running.get()) {
            val packet = try {
                queue.poll(POLL_INTERVAL_MS, TimeUnit.MILLISECONDS)
            } catch (_: InterruptedException) {
                if (!running.get()) break
                continue
            }
            if (packet != null) {
                val startedNs = System.nanoTime()
                try {
                    onPacket(packet)
                    delivered.incrementAndGet()
                } catch (error: Exception) {
                    failures.incrementAndGet()
                    runCatching { onError(error) }
                } finally {
                    val elapsed = System.nanoTime() - startedNs
                    while (true) {
                        val previousMax = maxCallbackNs.get()
                        if (elapsed <= previousMax || maxCallbackNs.compareAndSet(previousMax, elapsed)) break
                    }
                }
            }

            val now = System.nanoTime()
            if (now - lastStatsNs >= STATS_INTERVAL_NS) {
                runCatching { onStats(snapshot()) }
                lastStatsNs = now
            }
        }
    }

    override fun close() {
        if (!running.getAndSet(false)) return
        queue.clear()
        worker?.interrupt()
    }

    companion object {
        const val DEFAULT_CAPACITY = 256
        private const val POLL_INTERVAL_MS = 250L
        private const val STATS_INTERVAL_NS = 5_000_000_000L
    }
}
