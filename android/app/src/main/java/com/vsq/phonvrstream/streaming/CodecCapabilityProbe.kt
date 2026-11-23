package com.vsq.phonvrstream.streaming

import android.media.MediaCodecInfo
import android.media.MediaCodecList
import org.json.JSONArray
import org.json.JSONObject
import android.os.Build
import android.util.Log

/** Android decoder capabilities with a codec-name fallback on APIs before 29. */
data class VideoDecoderCapability(
    val name: String,
    val mimeType: String,
    val hardwareAccelerated: Boolean,
    val hardwareClassificationReliable: Boolean,
    val maxWidth: Int,
    val maxHeight: Int,
    val maxFrameRate: Double,
    val maxInstances: Int?,
    val supportsSbs1600x720At60: Boolean,
    val supportedSbsProfiles: List<String>,
    val profileLevels: List<String>,
    val colorFormats: List<Int>,
)

object CodecCapabilityProbe {
    private const val TAG = "PhoneVRCodecProbe"
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
                    // Android 9 and earlier do not expose isHardwareAccelerated.
                    // Use the codec naming convention as a fallback and keep the
                    // reliability flag false so diagnostics can distinguish it.
                    !isLikelySoftwareCodec(info.name)
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
                    supportsSbs1600x720At60 = hardware && runCatching {
                        video.areSizeAndRateSupported(1600, 720, 60.0)
                    }.getOrDefault(false),
                    supportedSbsProfiles = if (hardware) listOf(
                        1280 to 576, 1600 to 720, 1920 to 864, 2400 to 1080,
                    ).filter { (width, height) -> runCatching {
                        video.areSizeAndRateSupported(width, height, 60.0)
                    }.getOrDefault(false) }.map { (width, height) -> "${width}x${height}" } else emptyList(),
                    profileLevels = capability.profileLevels.take(32).map { "${it.profile}:${it.level}" },
                    colorFormats = capability.colorFormats.take(32).toList(),
                )
            }
        }
        // Log the exact codec enumeration on device. Vendor ROMs may expose
        // different component names and size/rate limits for the same SoC.
        result.filter { it.mimeType == "video/avc" }.forEach { codec ->
            Log.i(TAG, "H.264 decoder ${codec.name}: hardware=${codec.hardwareAccelerated}, " +
                "classificationReliable=${codec.hardwareClassificationReliable}, " +
                "max=${codec.maxWidth}x${codec.maxHeight}@${codec.maxFrameRate}, " +
                "supports1600x720@60=${codec.supportsSbs1600x720At60}, " +
                "supportedSbsProfiles=${codec.supportedSbsProfiles.joinToString(",")}, " +
                "profiles=${codec.profileLevels.joinToString(",")}")
        }
        Log.i(TAG, "MediaCodec probe: SDK=${Build.VERSION.SDK_INT}, H.264 decoders=${result.count { it.mimeType == "video/avc" }}")

        // H.264 remains the universal baseline; within each MIME type prefer
        // hardware implementations. Never trust MediaCodecList enumeration order.
        return result.sortedWith(
            compareBy<VideoDecoderCapability> { preferredMimeTypes.indexOf(it.mimeType) }
                .thenByDescending { it.hardwareAccelerated }
                .thenBy { it.name },
        ).take(24)
    }

    private fun isLikelySoftwareCodec(name: String): Boolean {
        val lower = name.lowercase()
        return lower.contains("google") || lower.contains("software") ||
            lower.startsWith("c2.android.") || lower.startsWith("omx.google.") ||
            lower.contains(".sw.") || lower.endsWith(".sw")
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
                .put("supportsSbs1600x720At60", capability.supportsSbs1600x720At60)
                .put("supportedSbsProfiles", JSONArray(capability.supportedSbsProfiles))
                .put("maxInstances", capability.maxInstances)
                .put("profileLevels", JSONArray(capability.profileLevels))
                .put("colorFormats", JSONArray().also { formats -> capability.colorFormats.forEach { formats.put(it) } }))
        }
    }


}
