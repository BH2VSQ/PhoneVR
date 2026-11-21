package com.vsq.phonvrstream.network

/** Classifies Android network interfaces for explicit Wi-Fi / USB-tether discovery. */
internal object ConnectionInterfaceClassifier {
    fun classify(interfaceName: String): String {
        val normalized = interfaceName.lowercase()
        return when {
            normalized.contains("rndis") || normalized.contains("usb") || normalized.contains("tether") ||
                normalized.startsWith("eth") -> "USB"
            normalized.contains("wlan") || normalized.contains("wifi") || normalized.contains("wi-fi") -> "Wi-Fi"
            else -> "Other"
        }
    }

    fun accepts(mode: DiscoveryMode, connectionType: String): Boolean = when (mode) {
        DiscoveryMode.AUTO -> true
        DiscoveryMode.WIFI -> connectionType == "Wi-Fi"
        DiscoveryMode.USB -> connectionType == "USB"
    }
}
