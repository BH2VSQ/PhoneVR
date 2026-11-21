package com.vsq.phonvrstream.streaming

import android.media.MediaCodecInfo
import android.media.MediaCodecList
import org.json.JSONArray
import org.json.JSONObject
import android.os.Build

/** Runtime-reported Android decoder capabilities; no model-name assumptions. */
data class VideoDecoderCapability(
    val name: String,
    val mimeType: String,
    val hardwareAccelerated: Boolean,
    val hardwareClassificationReliable: Boolean,
    val maxWidth: Int,
    val maxHeight: Int,
    val maxFrameRate: Double,
    val maxInstances: Int?,
    val profileLevels: List<String>,
    val colorFormats: List<Int>,
)

object CodecCapabilityProbe {
    private val preferredMimeTypes = listOf(
        "video/avc",   // H.264: baseline interoperability codec
        "video/hevc",  // HEVC: optional bandwidth-saving codec
        "video/av01",  // AV1: optional, capability-gated
    )

    fun discover(): List<VideoDecoderCapability> {
        val result = mutableListOf<VideoDecoderCapability>()
        val codecInfos = try {
            MediaCodecList(MediaCodecList.ALL_CODECS).codecInfos
        } catch (_: Throwable) {
            return emptyList()
        }

        codecInfos.asSequence().filterNot { it.isEncoder }.forEach { info ->
            info.supportedTypes.forEach typeLoop@{ type ->
                val mime = type.lowercase()
                if (mime !in preferredMimeTypes) return@typeLoop
                val capability = runCatching { info.getCapabilitiesForType(type) }.getOrNull()
                    ?: return@typeLoop
                val video = capability.videoCapabilities ?: return@typeLoop
                val widthRange = video.supportedWidths
                val heightRange = video.supportedHeights
                val frameRateRange = video.supportedFrameRates
                val hardware = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
                    info.isHardwareAccelerated
                } else {
                    !info.name.contains("google", ignoreCase = true) &&
                        !info.name.contains("software", ignoreCase = true)
                }
                val maxInstances = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
                    runCatching { capability.maxSupportedInstances }.getOrNull()
                } else null
                result += VideoDecoderCapability(
                    name = info.name,
                    mimeType = mime,
                    hardwareAccelerated = hardware,
                    hardwareClassificationReliable = Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q,
                    maxWidth = widthRange.upper,
                    maxHeight = heightRange.upper,
                    maxFrameRate = frameRateRange.upper.toDouble(),
                    maxInstances = maxInstances,
                    profileLevels = capability.profileLevels.take(32).map { "${it.profile}:${it.level}" },
                    colorFormats = capability.colorFormats.take(32).toList(),
                )
            }
        }
        // H.264 remains the universal baseline; within each MIME type prefer
        // hardware implementations. Never trust MediaCodecList enumeration order.
        return result.sortedWith(
            compareBy<VideoDecoderCapability> { preferredMimeTypes.indexOf(it.mimeType) }
                .thenByDescending { it.hardwareAccelerated }
                .thenBy { it.name },
        ).take(24)
    }


    fun discoverJson(): JSONArray = JSONArray().also { array ->
        discover().forEach { capability ->
            array.put(JSONObject()
                .put("name", capability.name)
                .put("mimeType", capability.mimeType)
                .put("hardwareAccelerated", capability.hardwareAccelerated)
                .put("hardwareClassificationReliable", capability.hardwareClassificationReliable)
                .put("maxWidth", capability.maxWidth)
                .put("maxHeight", capability.maxHeight)
                .put("maxFrameRate", capability.maxFrameRate)
                .put("maxInstances", capability.maxInstances)
                .put("profileLevels", JSONArray(capability.profileLevels))
                .put("colorFormats", JSONArray().also { formats -> capability.colorFormats.forEach { formats.put(it) } }))
        }
    }

    /** True only when the OS codec reports the requested coded frame dimensions/rate. */
    fun supports(
        width: Int,
        height: Int,
        frameRate: Double,
        mimeType: String = "video/avc",
    ): Boolean {
        if (width <= 0 || height <= 0 || frameRate <= 0.0) return false
        val list = try { MediaCodecList(MediaCodecList.REGULAR_CODECS) } catch (_: Throwable) { return false }
        val decoderName = runCatching {
            val format = android.media.MediaFormat.createVideoFormat(mimeType, width, height)
            list.findDecoderForFormat(format)
        }.getOrNull() ?: return false
        val info = list.codecInfos.firstOrNull { it.name == decoderName } ?: return false
        val caps = runCatching { info.getCapabilitiesForType(mimeType).videoCapabilities }.getOrNull()
            ?: return false
        return runCatching { caps.areSizeAndRateSupported(width, height, frameRate) }.getOrDefault(false)
    }
}
