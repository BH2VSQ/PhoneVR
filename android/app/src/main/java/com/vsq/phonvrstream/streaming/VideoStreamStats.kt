package com.vsq.phonvrstream.streaming

/** Compact live video telemetry for the Android headset status strip and diagnostics menu. */
data class VideoStreamStats(
    val state: String = "等待 PC",
    val receiveFps: Double = 0.0,
    val decodeFps: Double = 0.0,
    val displayFps: Double = 0.0,
    val fragmentsPerSecond: Int = 0,
    val networkReceiveBytesPerSecond: Long = 0L,
    val usbReceiveBytesPerSecond: Long = 0L,
    val decoderQueueDepth: Int = 0,
    val reassemblyInFlight: Int = 0,
    val expiredAssemblies: Long = 0,
    val queueDrops: Long = 0,
    val decoderDrops: Long = 0,
    val decoderInputWaits: Long = 0,
    val decoderRecoveries: Long = 0,
    val lastPacketAgeMs: Long = -1L,
    val lastCompleteFrameAgeMs: Long = -1L,
    val lastDecodedAgeMs: Long = -1L,
    val lastSourceAddress: String? = null,
)
