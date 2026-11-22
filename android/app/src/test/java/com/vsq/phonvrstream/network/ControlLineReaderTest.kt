package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.net.SocketTimeoutException

class ControlLineReaderTest {
    @Test
    fun preservesPartialJsonLineAcrossUsbPollingTimeouts() {
        val source = ScriptedInputStream(
            Step.Bytes("{\"type\":\"pon".toByteArray()),
            Step.Timeout,
            Step.Bytes("g\",\"timeMs\":123}".toByteArray()),
            Step.Timeout,
            Step.Bytes(byteArrayOf('\n'.code.toByte())),
            Step.Eof,
        )
        val reader = ControlLineReader(source)
        var timeouts = 0

        assertEquals("{\"type\":\"pong\",\"timeMs\":123}", reader.readLine { timeouts++ })
        assertEquals(2, timeouts)
        assertNull(reader.readLine())
    }

    @Test
    fun preservesUtf8CodePointSplitAcrossTimeouts() {
        val encoded = "{\"name\":\"设备\"}".toByteArray(Charsets.UTF_8)
        val split = encoded.indexOfFirst { (it.toInt() and 0x80) != 0 } + 1
        val reader = ControlLineReader(
            ScriptedInputStream(
                Step.Bytes(encoded.copyOfRange(0, split)),
                Step.Timeout,
                Step.Bytes(encoded.copyOfRange(split, encoded.size)),
                Step.Bytes(byteArrayOf('\n'.code.toByte())),
            ),
        )
        assertEquals("{\"name\":\"设备\"}", reader.readLine())
    }

    @Test
    fun supportsCrLfAndCleanEofAfterCompleteLine() {
        val reader = ControlLineReader(ByteArrayInputStream("one\r\ntwo\n".toByteArray()))
        assertEquals("one", reader.readLine())
        assertEquals("two", reader.readLine())
        assertNull(reader.readLine())
    }

    @Test
    fun rejectsUnterminatedPartialLineAtEof() {
        val reader = ControlLineReader(ByteArrayInputStream("{\"type\":\"welcome\"".toByteArray()))
        assertThrows(EOFException::class.java) { reader.readLine() }
    }

    @Test
    fun rejectsOversizedControlLine() {
        val reader = ControlLineReader(ByteArrayInputStream("12345".toByteArray()), maxLineBytes = 4)
        assertThrows(IOException::class.java) { reader.readLine() }
    }

    private sealed class Step {
        data class Bytes(val value: ByteArray) : Step()
        object Timeout : Step()
        object Eof : Step()
    }

    private class ScriptedInputStream(vararg steps: Step) : InputStream() {
        private val pending = java.util.ArrayDeque<Step>().apply { addAll(steps) }
        private var current = ByteArray(0)
        private var offset = 0

        override fun read(): Int {
            while (offset >= current.size) {
                when (val step = pending.pollFirst() ?: Step.Eof) {
                    is Step.Bytes -> {
                        current = step.value
                        offset = 0
                        if (current.isEmpty()) continue
                    }
                    Step.Timeout -> throw SocketTimeoutException("injected polling timeout")
                    Step.Eof -> return -1
                }
            }
            return current[offset++].toInt() and 0xFF
        }
    }
}
