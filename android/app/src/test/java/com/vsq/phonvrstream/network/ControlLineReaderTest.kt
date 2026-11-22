package com.vsq.phonvrstream.network

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertThrows
import org.junit.Test
import java.io.IOException
import java.io.Reader
import java.net.SocketTimeoutException

class ControlLineReaderTest {
    @Test
    fun preservesPartialJsonLineAcrossUsbPollingTimeouts() {
        val source = ScriptedReader(
            Step.Text("{\"type\":\"pon"),
            Step.Timeout,
            Step.Text("g\",\"timeMs\":123}"),
            Step.Timeout,
            Step.Text("\n"),
            Step.Eof,
        )
        val reader = ControlLineReader(source)
        var timeouts = 0

        assertEquals("{\"type\":\"pong\",\"timeMs\":123}", reader.readLine { timeouts++ })
        assertEquals(2, timeouts)
        assertNull(reader.readLine())
    }

    @Test
    fun supportsCrLfAndReturnsFinalUnterminatedLineAtEof() {
        val reader = ControlLineReader(ScriptedReader(Step.Text("one\r\ntwo"), Step.Eof))
        assertEquals("one", reader.readLine())
        assertEquals("two", reader.readLine())
        assertNull(reader.readLine())
    }

    @Test
    fun rejectsOversizedControlLine() {
        val reader = ControlLineReader(ScriptedReader(Step.Text("12345"), Step.Eof), maxLineChars = 4)
        assertThrows(IOException::class.java) { reader.readLine() }
    }

    private sealed class Step {
        data class Text(val value: String) : Step()
        object Timeout : Step()
        object Eof : Step()
    }

    private class ScriptedReader(vararg steps: Step) : Reader() {
        private val pending = java.util.ArrayDeque<Step>().apply { addAll(steps) }
        private var current = ""

        override fun read(buffer: CharArray, offset: Int, length: Int): Int {
            if (length == 0) return 0
            while (current.isEmpty()) {
                when (val step = pending.pollFirst() ?: Step.Eof) {
                    is Step.Text -> current = step.value
                    Step.Timeout -> throw SocketTimeoutException("injected polling timeout")
                    Step.Eof -> return -1
                }
            }
            val count = minOf(length, current.length)
            current.toCharArray(buffer, offset, 0, count)
            current = current.substring(count)
            return count
        }

        override fun close() = Unit
    }
}
