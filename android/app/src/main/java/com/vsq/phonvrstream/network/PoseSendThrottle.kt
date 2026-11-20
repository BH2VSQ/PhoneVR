package com.vsq.phonvrstream.network

/**
 * Bounds outgoing pose datagrams while allowing the sender to always use the
 * newest sensor sample. Calls are made from the single pose-sender thread.
 */
internal class PoseSendThrottle(maxRateHz: Int) {
    private val intervalNs: Long
    private var nextAllowedAtNs = Long.MIN_VALUE
    private var lastTimestampNs = Long.MIN_VALUE

    init {
        require(maxRateHz in 1..240) { "maxRateHz must be between 1 and 240" }
        intervalNs = 1_000_000_000L / maxRateHz
    }

    fun tryAcquire(timestampNs: Long, nowNs: Long): Boolean {
        if (timestampNs == lastTimestampNs) return false
        if (nextAllowedAtNs != Long.MIN_VALUE && nowNs < nextAllowedAtNs) return false
        lastTimestampNs = timestampNs
        nextAllowedAtNs = nowNs + intervalNs
        return true
    }

    fun nanosUntilNextAllowed(nowNs: Long): Long {
        if (nextAllowedAtNs == Long.MIN_VALUE) return 0L
        return (nextAllowedAtNs - nowNs).coerceAtLeast(0L)
    }
}
