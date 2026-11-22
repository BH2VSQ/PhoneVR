package com.vsq.phonvrstream.network

import java.io.EOFException
import java.io.IOException
import java.io.InputStream
import java.net.SocketTimeoutException
import java.nio.charset.StandardCharsets

/**
 * Reads newline-delimited UTF-8 control messages directly from a byte stream.
 * Partial bytes remain buffered across polling timeouts. Decoding happens only
 * after a complete line is received, so an InputStreamReader decoder buffer
 * cannot obscure or lose a fragment when the USB stream times out.
 */
internal class ControlLineReader(
    private val source: InputStream,
    private val maxLineBytes: Int = DEFAULT_MAX_LINE_BYTES,
) {
    private val pending = java.io.ByteArrayOutputStream()
    private var skipLfAfterCr = false

    init {
        require(maxLineBytes > 0) { "maxLineBytes must be positive" }
    }

    /**
     * Returns the next complete line, or null on clean EOF. [onTimeout] is
     * called whenever the source times out; partial bytes are retained.
     * EOF with an unfinished line is a transport truncation and is rejected.
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
                if (pending.size() == 0) return null
                val partialBytes = pending.size()
                pending.reset()
                throw EOFException("Control channel ended with an unterminated line ($partialBytes bytes)")
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
                    if (pending.size() >= maxLineBytes) {
                        val size = pending.size()
                        pending.reset()
                        throw IOException("Control JSON line exceeded $maxLineBytes bytes (received at least ${size + 1})")
                    }
                    pending.write(next)
                }
            }
        }
    }

    private fun takePendingLine(): String {
        val bytes = pending.toByteArray()
        pending.reset()
        return String(bytes, StandardCharsets.UTF_8)
    }

    companion object {
        const val DEFAULT_MAX_LINE_BYTES = 64 * 1024
    }
}
