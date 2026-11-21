package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionInterfaceClassifierTest {
    @Test fun classifiesUsbTetherInterfaces() {
        assertEquals("USB", ConnectionInterfaceClassifier.classify("rndis0"))
        assertEquals("USB", ConnectionInterfaceClassifier.classify("usb0"))
        assertEquals("USB", ConnectionInterfaceClassifier.classify("eth0"))
    }

    @Test fun classifiesWifiInterfaces() {
        assertEquals("Wi-Fi", ConnectionInterfaceClassifier.classify("wlan0"))
        assertEquals("Wi-Fi", ConnectionInterfaceClassifier.classify("wifi0"))
    }

    @Test fun appliesExplicitDiscoveryMode() {
        assertTrue(ConnectionInterfaceClassifier.accepts(DiscoveryMode.AUTO, "Other"))
        assertTrue(ConnectionInterfaceClassifier.accepts(DiscoveryMode.USB, "USB"))
        assertFalse(ConnectionInterfaceClassifier.accepts(DiscoveryMode.USB, "Wi-Fi"))
        assertTrue(ConnectionInterfaceClassifier.accepts(DiscoveryMode.WIFI, "Wi-Fi"))
        assertFalse(ConnectionInterfaceClassifier.accepts(DiscoveryMode.WIFI, "USB"))
    }
}
