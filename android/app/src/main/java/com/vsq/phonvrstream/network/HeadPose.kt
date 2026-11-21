package com.vsq.phonvrstream.network

/** Orientation relative to the user's recentered headset pose. */
data class HeadPose(
    val timestampNs: Long,
    val x: Float,
    val y: Float,
    val z: Float,
    val w: Float,
)


data class DiscoveredHost(
    val address: String,
    val controlPort: Int,
    val name: String,
)
