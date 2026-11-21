package com.vsq.phonvrstream.network

/** Classifies Android network interfaces for automatic or Wi-Fi-only discovery. */
internal object ConnectionInterfaceClassifier {
    fun classify(interfaceName: String): String {
        val normalized = interfaceName.lowercase()
        return when {
            normalized.contains("wlan") || normalized.contains("wifi") || normalized.contains("wi-fi") -> "Wi-Fi"
            else -> "Network"
        }
    }

    fun accepts(mode: DiscoveryMode, connectionType: String): Boolean = when (mode) {
        DiscoveryMode.AUTO -> true
        DiscoveryMode.WIFI -> connectionType == "Wi-Fi"
    }
}
