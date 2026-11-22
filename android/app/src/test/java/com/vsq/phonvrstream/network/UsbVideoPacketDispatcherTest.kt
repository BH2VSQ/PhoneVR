package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class UsbVideoPacketDispatcherTest {
    private fun awaitCondition(timeoutMs: Long, condition: () -> Boolean): Boolean {
        val deadline = System.nanoTime() + TimeUnit.MILLISECONDS.toNanos(timeoutMs)
        while (System.nanoTime() < deadline) {
            if (condition()) return true
            Thread.sleep(1)
        }
        return condition()
    }

    @Test
    fun offerDoesNotWaitForSlowPacketConsumerAndQueueIsBounded() {
        val callbackEntered = CountDownLatch(1)
        val releaseCallback = CountDownLatch(1)
        val secondDelivered = CountDownLatch(1)
        val dispatcher = UsbVideoPacketDispatcher(
            capacity = 1,
            onPacket = { packet ->
                if (packet[0].toInt() == 1) {
                    callbackEntered.countDown()
                    check(releaseCallback.await(2, TimeUnit.SECONDS))
                } else {
                    secondDelivered.countDown()
                }
            },
        )
        dispatcher.start()
        try {
            assertTrue(dispatcher.offer(byteArrayOf(1)))
            assertTrue(callbackEntered.await(1, TimeUnit.SECONDS))

            val startedAt = System.nanoTime()
            assertTrue(dispatcher.offer(byteArrayOf(2)))
            val offerDurationMs = (System.nanoTime() - startedAt) / 1_000_000.0
            assertTrue("offer blocked for ${offerDurationMs}ms", offerDurationMs < 100.0)

            assertFalse("bounded queue should reject overflow", dispatcher.offer(byteArrayOf(3)))
            assertEquals(1L, dispatcher.snapshot().droppedPackets)
        } finally {
            releaseCallback.countDown()
            assertTrue(secondDelivered.await(1, TimeUnit.SECONDS))
            dispatcher.close()
        }
    }

    @Test
    fun callbackFailureDoesNotStopFollowingPackets() {
        val secondDelivered = CountDownLatch(1)
        val dispatcher = UsbVideoPacketDispatcher(
            capacity = 2,
            onPacket = { packet ->
                if (packet[0].toInt() == 1) error("simulated forward failure")
                secondDelivered.countDown()
            },
        )
        dispatcher.start()
        try {
            assertTrue(dispatcher.offer(byteArrayOf(1)))
            assertTrue(dispatcher.offer(byteArrayOf(2)))
            assertTrue(secondDelivered.await(1, TimeUnit.SECONDS))
            assertEquals(1L, dispatcher.snapshot().callbackFailures)
            assertTrue(
                "second packet callback completed, but deliveredPackets was not updated",
                awaitCondition(1_000) { dispatcher.snapshot().deliveredPackets == 1L },
            )
            assertEquals(1L, dispatcher.snapshot().deliveredPackets)
        } finally {
            dispatcher.close()
        }
    }
}
