package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ConnectionInterfaceClassifierTest {
    @Test fun classifiesWifiInterfaces() {
        assertEquals("Wi-Fi", ConnectionInterfaceClassifier.classify("wlan0"))
        assertEquals("Wi-Fi", ConnectionInterfaceClassifier.classify("wifi0"))
    }

    @Test fun classifiesOtherInterfacesWithoutUsbMode() {
        assertEquals("Network", ConnectionInterfaceClassifier.classify("eth0"))
        assertEquals("Network", ConnectionInterfaceClassifier.classify("rndis0"))
        assertEquals("Network", ConnectionInterfaceClassifier.classify("usb0"))
    }

    @Test fun appliesAutoAndWifiModes() {
        assertTrue(ConnectionInterfaceClassifier.accepts(DiscoveryMode.AUTO, "Network"))
        assertTrue(ConnectionInterfaceClassifier.accepts(DiscoveryMode.WIFI, "Wi-Fi"))
        assertFalse(ConnectionInterfaceClassifier.accepts(DiscoveryMode.WIFI, "Network"))
    }
}
