package com.vsq.phonvrstream.network

/** Orientation relative to the user's recentered headset pose. */
data class HeadPose(
    val timestampNs: Long,
    val x: Float,
    val y: Float,
    val z: Float,
    val w: Float,
)

enum class DiscoveryMode(val preferenceValue: String) {
    AUTO("auto"),
    WIFI("wifi"),
    USB("usb");

    companion object {
        fun fromPreference(value: String?): DiscoveryMode = values().firstOrNull { it.preferenceValue == value } ?: AUTO
    }
}

data class DiscoveredHost(
    val address: String,
    val controlPort: Int,
    val name: String,
    val connectionType: String = "LAN",
    val interfaceName: String = "",
)
