package com.vsq.phonvrstream.network

import java.io.IOException
import java.io.Reader
import java.net.SocketTimeoutException

/**
 * Reads newline-delimited control messages without losing a partially received
 * line when the underlying socket/USB stream reports a polling timeout.
 *
 * BufferedReader.readLine() discards its local partial-line buffer when the
 * underlying Reader throws. The USB accessory InputStream intentionally uses
 * short polling timeouts, so a control message split across PVRU frames can be
 * interrupted between chunks. Keep the line buffer in this object instead.
 */
internal class ControlLineReader(
    private val source: Reader,
    private val maxLineChars: Int = DEFAULT_MAX_LINE_CHARS,
) {
    private val pending = StringBuilder()
    private var skipLfAfterCr = false

    init {
        require(maxLineChars > 0) { "maxLineChars must be positive" }
    }

    /**
     * Returns the next line, or null on EOF. [onTimeout] is called whenever
     * the source times out; it may service heartbeats or throw an overall
     * connection deadline exception. Partial line contents are retained.
     */
    @Throws(IOException::class)
    fun readLine(onTimeout: () -> Unit = {}): String? {
        while (true) {
            val next = try {
                source.read()
            } catch (_: SocketTimeoutException) {
                onTimeout()
                continue
            }

            if (next < 0) {
                if (pending.isEmpty()) return null
                return takePendingLine()
            }

            if (skipLfAfterCr) {
                skipLfAfterCr = false
                if (next == '\n'.code) continue
            }

            when (next) {
                '\n'.code -> return takePendingLine()
                '\r'.code -> {
                    skipLfAfterCr = true
                    return takePendingLine()
                }
                else -> {
                    if (pending.length >= maxLineChars) {
                        pending.setLength(0)
                        throw IOException("Control JSON line exceeded $maxLineChars characters")
                    }
                    pending.append(next.toChar())
                }
            }
        }
    }

    private fun takePendingLine(): String {
        val line = pending.toString()
        pending.setLength(0)
        return line
    }

    companion object {
        const val DEFAULT_MAX_LINE_CHARS = 64 * 1024
    }
}
