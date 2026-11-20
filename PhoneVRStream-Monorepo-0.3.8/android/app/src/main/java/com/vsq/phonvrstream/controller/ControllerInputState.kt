package com.vsq.phonvrstream.controller

/**
 * Normalized input state for a single 3DoF handheld VR controller.
 *
 * This model is intentionally independent from the HMD pose data model and
 * transport. A controller is an input device, not a second head tracker.
 */
data class ControllerInputState(
    val connected: Boolean,
    val timestampNs: Long,
    val isTouching: Boolean = false,
    /** Normalized touchpad coordinates in the inclusive range 0.0..1.0. */
    val touchX: Float = 0f,
    val touchY: Float = 0f,
    val click: Boolean = false,
    val app: Boolean = false,
    val home: Boolean = false,
    val grip: Boolean = false,
    val trigger: Boolean = false,
    val volumeUp: Boolean = false,
    val volumeDown: Boolean = false,
    val orientationX: Float = 0f,
    val orientationY: Float = 0f,
    val orientationZ: Float = 0f,
    val orientationW: Float = 1f,
    /** True only when the provider supplies a valid orientation quaternion. */
    val hasOrientation: Boolean = false,
    /** Google SDK elbow-model position estimate; not room-scale tracking. */
    val positionX: Float = 0f,
    val positionY: Float = 0f,
    val positionZ: Float = 0f,
    val hasPosition: Boolean = false,
    val isCharging: Boolean = false,
    /** Provider-native battery level bucket; not necessarily a percentage. */
    val batteryLevelBucket: Int? = null,
    val trackingStatusFlags: Int = 0,
) {
    fun normalized(): ControllerInputState = copy(
        touchX = touchX.coerceIn(0f, 1f),
        touchY = touchY.coerceIn(0f, 1f),
    )
}

/**
 * Provider boundary for future Daydream Controller / generic Android input
 * adapters. Implementations must report disconnects and release resources
 * from [stop]. No provider is activated by the HMD-only 0.3.0 build.
 */
interface ControllerInputProvider {
    val providerId: String
    fun start(onState: (ControllerInputState) -> Unit)
    fun stop()
}
