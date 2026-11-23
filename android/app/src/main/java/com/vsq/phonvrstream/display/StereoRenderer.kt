package com.vsq.phonvrstream.display

import android.opengl.GLES30
import android.opengl.GLES11Ext
import android.graphics.SurfaceTexture
import android.os.Handler
import android.os.HandlerThread
import android.os.Process
import android.view.Surface
import android.opengl.GLSurfaceView
import com.vsq.phonvrstream.settings.DisplaySettings
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.nio.FloatBuffer
import java.util.concurrent.atomic.AtomicBoolean
import javax.microedition.khronos.egl.EGLConfig
import javax.microedition.khronos.opengles.GL10
import kotlin.math.tan

/**
 * Two-pass stereoscopic renderer.
 * Pass 1 draws a test environment into a side-by-side framebuffer.
 * Pass 2 applies lens warp, vignette, blur and sharpening independently to each eye.
 */
class StereoRenderer : GLSurfaceView.Renderer {
    @Volatile private var displaySettings = DisplaySettings()
    // Reuse one quaternion buffer; synchronize only the four-float snapshot.
    private val orientationLock = Any()
    private val orientation = floatArrayOf(0f, 0f, 0f, 1f)
    @Volatile private var leftLensCenterX = 0.5f
    @Volatile private var rightLensCenterX = 0.5f

    private var sceneProgram = 0
    private var postProgram = 0
    private var videoProgram = 0
    private var calibrationProgram = 0
    @Volatile private var calibrationMode = CALIBRATION_OFF
    @Volatile private var calibrationDistortionEnabled = false
    private var videoTextureId = 0
    private var videoSurfaceTexture: SurfaceTexture? = null
    private var videoSurface: Surface? = null
    private var frameCallbackThread: HandlerThread? = null
    private val videoFramePending = AtomicBoolean(false)
    @Volatile private var videoSurfaceListener: ((Surface) -> Unit)? = null
    @Volatile private var videoFrameListener: (() -> Unit)? = null
    private var videoDisplayReported = false
    private var hasVideoFrame = false
    @Volatile private var videoStreamStale = false
    private var hasEverDisplayedVideo = false
    private val videoTextureMatrix = FloatArray(16)
    private data class VideoLocations(val position: Int, val texture: Int, val transform: Int)
    private lateinit var video: VideoLocations
    private var width = 1
    private var height = 1
    private var sceneTexture = 0
    private var framebuffer = 0
    private lateinit var vertexBuffer: FloatBuffer

    private data class SceneLocations(
        val position: Int, val resolution: Int, val eyeSign: Int, val ipd: Int,
        val fov: Int, val orientation: Int,
    )
    private data class PostLocations(
        val position: Int, val eyeSign: Int, val lensCenter: Int, val distortion: Int,
        val blur: Int, val sharpness: Int, val sourceTexel: Int, val aspect: Int,
        val sceneTexture: Int, val swapEyes: Int, val eyeOffset: Int, val verticalTilt: Int,
    )
    private data class CalibrationLocations(
        val position: Int, val eyeSign: Int, val mode: Int,
    )
    private lateinit var scene: SceneLocations
    private lateinit var post: PostLocations
    private lateinit var calibration: CalibrationLocations

    fun setVideoSurfaceListener(listener: (Surface) -> Unit) {
        videoSurfaceListener = listener
        videoSurface?.let(listener)
    }

    fun setVideoFrameListener(listener: () -> Unit) {
        videoFrameListener = listener
    }

    /** Hide a frozen frame after the receiver watchdog declares the stream stale. */
    fun setVideoStreamStale(stale: Boolean) {
        videoStreamStale = stale
    }

    private fun createVideoInputSurface() {
        videoSurface?.release()
        videoSurfaceTexture?.release()
        frameCallbackThread?.quitSafely()
        frameCallbackThread = null
        videoSurface = null
        videoSurfaceTexture = null
        val ids = IntArray(1)
        GLES30.glGenTextures(1, ids, 0)
        videoTextureId = ids[0]
        GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, videoTextureId)
        GLES30.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES30.GL_TEXTURE_MIN_FILTER, GLES30.GL_LINEAR)
        GLES30.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES30.GL_TEXTURE_MAG_FILTER, GLES30.GL_LINEAR)
        GLES30.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES30.GL_TEXTURE_WRAP_S, GLES30.GL_CLAMP_TO_EDGE)
        GLES30.glTexParameteri(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, GLES30.GL_TEXTURE_WRAP_T, GLES30.GL_CLAMP_TO_EDGE)
        GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, 0)
        val callbackThread = HandlerThread("PhoneVR-Video-FrameCallback", Process.THREAD_PRIORITY_DEFAULT).apply { start() }
        frameCallbackThread = callbackThread
        val callbackHandler = Handler(callbackThread.looper)
        val texture = SurfaceTexture(videoTextureId).apply {
            setDefaultBufferSize(VIDEO_WIDTH, VIDEO_HEIGHT)
            // Frame notifications are a high-rate signal. Keep them off the UI
            // thread so menu/connection work cannot delay the GL video pump.
            setOnFrameAvailableListener({ videoFramePending.set(true) }, callbackHandler)
        }
        val surface = Surface(texture)
        videoSurfaceTexture = texture
        videoSurface = surface
        videoFramePending.set(false)
        hasVideoFrame = false
        hasEverDisplayedVideo = false
        videoStreamStale = false
        videoDisplayReported = false
        videoSurfaceListener?.invoke(surface)
    }

    /** Render a built-in stereo diagnostic chart instead of the received video. */
    fun setCalibrationMode(mode: Int) {
        calibrationMode = mode.coerceIn(CALIBRATION_OFF, CALIBRATION_DEPTH)
    }

    /** Lens warp is disabled by default for raw SBS checks and can be enabled for optical checks. */
    fun setCalibrationDistortionEnabled(enabled: Boolean) {
        calibrationDistortionEnabled = enabled
    }

    fun updateSettings(value: DisplaySettings, physicalScreenWidthMm: Float) {
        val normalized = value.normalized()
        displaySettings = normalized
        // Lens centers are symmetric around the physical screen center. Each eye
        // samples a separate half-screen texture, so the local centers are mirrored.
        leftLensCenterX = normalized.lensCenterXForEye(true, physicalScreenWidthMm)
        rightLensCenterX = normalized.lensCenterXForEye(false, physicalScreenWidthMm)
    }

    fun updateOrientation(x: Float, y: Float, z: Float, w: Float) {
        synchronized(orientationLock) {
            orientation[0] = x
            orientation[1] = y
            orientation[2] = z
            orientation[3] = w
        }
    }

    override fun onSurfaceCreated(gl: GL10?, config: EGLConfig?) {
        GLES30.glClearColor(0f, 0f, 0f, 1f)
        GLES30.glDisable(GLES30.GL_DEPTH_TEST)
        GLES30.glDisable(GLES30.GL_CULL_FACE)

        val vertices = floatArrayOf(-1f, -1f, 1f, -1f, -1f, 1f, 1f, 1f)
        vertexBuffer = ByteBuffer.allocateDirect(vertices.size * 4)
            .order(ByteOrder.nativeOrder())
            .asFloatBuffer()
            .put(vertices)
        vertexBuffer.position(0)

        sceneProgram = createProgram(VERTEX_SHADER, SCENE_FRAGMENT_SHADER)
        postProgram = createProgram(VERTEX_SHADER, POST_FRAGMENT_SHADER)
        videoProgram = createProgram(VERTEX_SHADER, VIDEO_FRAGMENT_SHADER)
        calibrationProgram = createProgram(VERTEX_SHADER, CALIBRATION_FRAGMENT_SHADER)
        calibration = CalibrationLocations(
            position = GLES30.glGetAttribLocation(calibrationProgram, "aPosition"),
            eyeSign = GLES30.glGetUniformLocation(calibrationProgram, "uEyeSign"),
            mode = GLES30.glGetUniformLocation(calibrationProgram, "uMode"),
        )
        video = VideoLocations(
            position = GLES30.glGetAttribLocation(videoProgram, "aPosition"),
            texture = GLES30.glGetUniformLocation(videoProgram, "uVideoTexture"),
            transform = GLES30.glGetUniformLocation(videoProgram, "uTextureMatrix"),
        )
        createVideoInputSurface()

        scene = SceneLocations(
            position = GLES30.glGetAttribLocation(sceneProgram, "aPosition"),
            resolution = GLES30.glGetUniformLocation(sceneProgram, "uResolution"),
            eyeSign = GLES30.glGetUniformLocation(sceneProgram, "uEyeSign"),
            ipd = GLES30.glGetUniformLocation(sceneProgram, "uIpdMeters"),
            fov = GLES30.glGetUniformLocation(sceneProgram, "uTanHalfFov"),
            orientation = GLES30.glGetUniformLocation(sceneProgram, "uOrientation"),
        )
        post = PostLocations(
            position = GLES30.glGetAttribLocation(postProgram, "aPosition"),
            eyeSign = GLES30.glGetUniformLocation(postProgram, "uEyeSign"),
            lensCenter = GLES30.glGetUniformLocation(postProgram, "uLensCenter"),
            distortion = GLES30.glGetUniformLocation(postProgram, "uDistortion"),
            blur = GLES30.glGetUniformLocation(postProgram, "uBlur"),
            sharpness = GLES30.glGetUniformLocation(postProgram, "uSharpness"),
            sourceTexel = GLES30.glGetUniformLocation(postProgram, "uSourceTexel"),
            aspect = GLES30.glGetUniformLocation(postProgram, "uAspect"),
            sceneTexture = GLES30.glGetUniformLocation(postProgram, "uSceneTexture"),
            swapEyes = GLES30.glGetUniformLocation(postProgram, "uSwapEyes"),
            eyeOffset = GLES30.glGetUniformLocation(postProgram, "uEyeOffset"),
            verticalTilt = GLES30.glGetUniformLocation(postProgram, "uVerticalTiltRadians"),
        )
    }

    override fun onSurfaceChanged(gl: GL10?, surfaceWidth: Int, surfaceHeight: Int) {
        width = surfaceWidth.coerceAtLeast(2)
        height = surfaceHeight.coerceAtLeast(1)
        createSceneFramebuffer()
    }

    override fun onDrawFrame(gl: GL10?) {
        if (sceneProgram == 0 || postProgram == 0 || framebuffer == 0) return
        val settings = displaySettings.normalized()
        var orientationX = 0f
        var orientationY = 0f
        var orientationZ = 0f
        var orientationW = 1f
        synchronized(orientationLock) {
            orientationX = orientation[0]
            orientationY = orientation[1]
            orientationZ = orientation[2]
            orientationW = orientation[3]
        }
        val halfWidth = width / 2
        val tanHalfFov = tan(Math.toRadians(settings.fovDegrees.toDouble() * 0.5)).toFloat()

        val inputTexture = videoSurfaceTexture
        if (inputTexture != null && videoFramePending.getAndSet(false)) {
            try {
                inputTexture.updateTexImage()
                inputTexture.getTransformMatrix(videoTextureMatrix)
                hasVideoFrame = true
                hasEverDisplayedVideo = true
                videoStreamStale = false
                if (!videoDisplayReported) videoDisplayReported = true
                // Count every newly latched SurfaceTexture frame, not only the
                // first one, so the top status indicator reflects displayed FPS.
                videoFrameListener?.invoke()
            } catch (_: RuntimeException) {
                hasVideoFrame = false
            }
        }

        // Pass 1: use the live decoded SBS stream when available. Before the
        // first complete decoder frame, retain the built-in test scene.
        GLES30.glBindFramebuffer(GLES30.GL_FRAMEBUFFER, framebuffer)
        GLES30.glViewport(0, 0, width, height)
        GLES30.glClear(GLES30.GL_COLOR_BUFFER_BIT)
        val activeCalibrationMode = calibrationMode
        if (activeCalibrationMode != CALIBRATION_OFF && calibrationProgram != 0) {
            // Draw the diagnostic SBS chart in source-eye order. The post pass
            // below then applies the same eye routing, offsets and lens warp as
            // the live video path, so the test reveals the actual optical path.
            GLES30.glUseProgram(calibrationProgram)
            bindPosition(calibration.position)
            GLES30.glUniform1i(calibration.mode, activeCalibrationMode)
            GLES30.glViewport(0, 0, halfWidth, height)
            GLES30.glUniform1f(calibration.eyeSign, -1f)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glViewport(halfWidth, 0, width - halfWidth, height)
            GLES30.glUniform1f(calibration.eyeSign, 1f)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glDisableVertexAttribArray(calibration.position)
        } else if (hasVideoFrame && !videoStreamStale && videoProgram != 0 && videoTextureId != 0) {
            GLES30.glUseProgram(videoProgram)
            bindPosition(video.position)
            GLES30.glActiveTexture(GLES30.GL_TEXTURE0)
            GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, videoTextureId)
            GLES30.glUniform1i(video.texture, 0)
            GLES30.glUniformMatrix4fv(video.transform, 1, false, videoTextureMatrix, 0)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glDisableVertexAttribArray(video.position)
            GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, 0)
        } else if (videoStreamStale && hasEverDisplayedVideo) {
            // Do not leave an old head pose/video image frozen in the viewer.
            // The top status strip reports the transport fault separately.
            GLES30.glClearColor(0f, 0f, 0f, 1f)
            GLES30.glClear(GLES30.GL_COLOR_BUFFER_BIT)
            GLES30.glClearColor(0f, 0f, 0f, 1f)
        } else {
            GLES30.glUseProgram(sceneProgram)
            bindPosition(scene.position)
            GLES30.glUniform2f(scene.resolution, halfWidth.toFloat(), height.toFloat())
            GLES30.glUniform1f(scene.ipd, settings.ipdMm * 0.0005f)
            GLES30.glUniform1f(scene.fov, tanHalfFov)
            GLES30.glUniform4f(scene.orientation, orientationX, orientationY, orientationZ, orientationW)
            GLES30.glViewport(0, 0, halfWidth, height)
            GLES30.glUniform1f(scene.eyeSign, -1f)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glViewport(halfWidth, 0, width - halfWidth, height)
            GLES30.glUniform1f(scene.eyeSign, 1f)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glDisableVertexAttribArray(scene.position)
        }

        // Pass 2: warp each eye independently and apply a real multi-sample blur filter.
        GLES30.glBindFramebuffer(GLES30.GL_FRAMEBUFFER, 0)
        GLES30.glClear(GLES30.GL_COLOR_BUFFER_BIT)
        GLES30.glUseProgram(postProgram)
        bindPosition(post.position)
        GLES30.glActiveTexture(GLES30.GL_TEXTURE0)
        GLES30.glBindTexture(GLES30.GL_TEXTURE_2D, sceneTexture)
        GLES30.glUniform1i(post.sceneTexture, 0)
        GLES30.glUniform2f(post.sourceTexel, 1f / width, 1f / height)
        GLES30.glUniform1f(post.aspect, halfWidth.toFloat() / height.toFloat())
        val applyLensProcessing = activeCalibrationMode == CALIBRATION_OFF || calibrationDistortionEnabled
        GLES30.glUniform2f(
            post.distortion,
            if (applyLensProcessing) settings.distortionK1 else 0f,
            if (applyLensProcessing) settings.distortionK2 else 0f,
        )
        GLES30.glUniform1f(post.blur, if (applyLensProcessing) settings.blur else 0f)
        GLES30.glUniform1f(post.sharpness, if (applyLensProcessing) settings.sharpness else 0f)
        GLES30.glUniform1f(post.swapEyes, if (settings.swapEyes) 1f else 0f)
        GLES30.glUniform1f(post.verticalTilt, Math.toRadians(settings.verticalTiltDegrees.toDouble()).toFloat())

        GLES30.glViewport(0, 0, halfWidth, height)
        GLES30.glUniform1f(post.eyeSign, -1f)
        GLES30.glUniform2f(post.lensCenter, leftLensCenterX, settings.lensCenterY)
        GLES30.glUniform2f(post.eyeOffset, settings.leftEyeOffsetX, settings.leftEyeOffsetY)
        GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
        GLES30.glViewport(halfWidth, 0, width - halfWidth, height)
        GLES30.glUniform1f(post.eyeSign, 1f)
        GLES30.glUniform2f(post.lensCenter, rightLensCenterX, settings.lensCenterY)
        GLES30.glUniform2f(post.eyeOffset, settings.rightEyeOffsetX, settings.rightEyeOffsetY)
        GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
        GLES30.glDisableVertexAttribArray(post.position)
        GLES30.glBindTexture(GLES30.GL_TEXTURE_2D, 0)
    }

    private fun bindPosition(location: Int) {
        GLES30.glEnableVertexAttribArray(location)
        vertexBuffer.position(0)
        GLES30.glVertexAttribPointer(location, 2, GLES30.GL_FLOAT, false, 8, vertexBuffer)
    }

    private fun createSceneFramebuffer() {
        if (framebuffer != 0) GLES30.glDeleteFramebuffers(1, intArrayOf(framebuffer), 0)
        if (sceneTexture != 0) GLES30.glDeleteTextures(1, intArrayOf(sceneTexture), 0)

        val textureIds = IntArray(1)
        GLES30.glGenTextures(1, textureIds, 0)
        sceneTexture = textureIds[0]
        GLES30.glBindTexture(GLES30.GL_TEXTURE_2D, sceneTexture)
        GLES30.glTexParameteri(GLES30.GL_TEXTURE_2D, GLES30.GL_TEXTURE_MIN_FILTER, GLES30.GL_LINEAR)
        GLES30.glTexParameteri(GLES30.GL_TEXTURE_2D, GLES30.GL_TEXTURE_MAG_FILTER, GLES30.GL_LINEAR)
        GLES30.glTexParameteri(GLES30.GL_TEXTURE_2D, GLES30.GL_TEXTURE_WRAP_S, GLES30.GL_CLAMP_TO_EDGE)
        GLES30.glTexParameteri(GLES30.GL_TEXTURE_2D, GLES30.GL_TEXTURE_WRAP_T, GLES30.GL_CLAMP_TO_EDGE)
        GLES30.glTexImage2D(
            GLES30.GL_TEXTURE_2D, 0, GLES30.GL_RGBA, width, height, 0,
            GLES30.GL_RGBA, GLES30.GL_UNSIGNED_BYTE, null,
        )

        val framebufferIds = IntArray(1)
        GLES30.glGenFramebuffers(1, framebufferIds, 0)
        framebuffer = framebufferIds[0]
        GLES30.glBindFramebuffer(GLES30.GL_FRAMEBUFFER, framebuffer)
        GLES30.glFramebufferTexture2D(
            GLES30.GL_FRAMEBUFFER, GLES30.GL_COLOR_ATTACHMENT0,
            GLES30.GL_TEXTURE_2D, sceneTexture, 0,
        )
        val status = GLES30.glCheckFramebufferStatus(GLES30.GL_FRAMEBUFFER)
        GLES30.glBindFramebuffer(GLES30.GL_FRAMEBUFFER, 0)
        GLES30.glBindTexture(GLES30.GL_TEXTURE_2D, 0)
        check(status == GLES30.GL_FRAMEBUFFER_COMPLETE) {
            "Stereo scene framebuffer is incomplete: 0x${status.toString(16)}"
        }
    }

    private fun createProgram(vertexSource: String, fragmentSource: String): Int {
        val vertex = compileShader(GLES30.GL_VERTEX_SHADER, vertexSource)
        val fragment = compileShader(GLES30.GL_FRAGMENT_SHADER, fragmentSource)
        val result = GLES30.glCreateProgram()
        GLES30.glAttachShader(result, vertex)
        GLES30.glAttachShader(result, fragment)
        GLES30.glLinkProgram(result)
        val status = IntArray(1)
        GLES30.glGetProgramiv(result, GLES30.GL_LINK_STATUS, status, 0)
        if (status[0] == 0) {
            val log = GLES30.glGetProgramInfoLog(result)
            GLES30.glDeleteProgram(result)
            throw IllegalStateException("OpenGL program link failed: $log")
        }
        GLES30.glDeleteShader(vertex)
        GLES30.glDeleteShader(fragment)
        return result
    }

    private fun compileShader(type: Int, source: String): Int {
        val shader = GLES30.glCreateShader(type)
        GLES30.glShaderSource(shader, source)
        GLES30.glCompileShader(shader)
        val status = IntArray(1)
        GLES30.glGetShaderiv(shader, GLES30.GL_COMPILE_STATUS, status, 0)
        if (status[0] == 0) {
            val log = GLES30.glGetShaderInfoLog(shader)
            GLES30.glDeleteShader(shader)
            throw IllegalStateException("OpenGL shader compile failed: $log")
        }
        return shader
    }

    companion object {
        const val CALIBRATION_OFF = 0
        const val CALIBRATION_ALIGNMENT = 1
        const val CALIBRATION_EYE_LABELS = 2
        const val CALIBRATION_LEFT_ONLY = 3
        const val CALIBRATION_RIGHT_ONLY = 4
        const val CALIBRATION_DEPTH = 5

        private const val VIDEO_WIDTH = 1600
        private const val VIDEO_HEIGHT = 720
        private const val VERTEX_SHADER = """
            #version 300 es
            layout(location = 0) in vec2 aPosition;
            out vec2 vUv;
            void main() {
                vUv = aPosition * 0.5 + 0.5;
                gl_Position = vec4(aPosition, 0.0, 1.0);
            }
        """

        private const val VIDEO_FRAGMENT_SHADER = """
            #version 300 es
            #extension GL_OES_EGL_image_external_essl3 : require
            precision highp float;
            in vec2 vUv;
            out vec4 outColor;
            uniform samplerExternalOES uVideoTexture;
            uniform mat4 uTextureMatrix;
            void main() {
                vec2 uv = (uTextureMatrix * vec4(vUv, 0.0, 1.0)).xy;
                outColor = texture(uVideoTexture, uv);
            }
        """

        private const val SCENE_FRAGMENT_SHADER = """
            #version 300 es
            precision highp float;
            in vec2 vUv;
            out vec4 outColor;
            uniform vec2 uResolution;
            uniform float uEyeSign;
            uniform float uIpdMeters;
            uniform float uTanHalfFov;
            uniform vec4 uOrientation;

            vec3 rotateByQuaternion(vec3 v, vec4 q) {
                vec3 t = 2.0 * cross(q.xyz, v);
                return v + q.w * t + cross(q.xyz, t);
            }

            float sphereHit(vec3 origin, vec3 direction, vec3 center, float radius) {
                vec3 oc = origin - center;
                float b = dot(oc, direction);
                float c = dot(oc, oc) - radius * radius;
                float h = b * b - c;
                if (h < 0.0) return -1.0;
                float t = -b - sqrt(h);
                return t > 0.0 ? t : -1.0;
            }

            vec3 shadeSphere(vec3 origin, vec3 direction, vec3 center, float radius, vec3 baseColor) {
                float t = sphereHit(origin, direction, center, radius);
                vec3 hit = origin + direction * t;
                vec3 normal = normalize(hit - center);
                vec3 lightDir = normalize(vec3(-0.4, 0.7, 0.8));
                float diffuse = max(dot(normal, lightDir), 0.0);
                float rim = pow(1.0 - max(dot(normal, -direction), 0.0), 2.0);
                return baseColor * (0.22 + diffuse * 0.78) + vec3(0.18, 0.25, 0.36) * rim * 0.35;
            }

            void main() {
                float aspect = uResolution.x / max(uResolution.y, 1.0);
                vec2 p = vUv * 2.0 - 1.0;
                vec3 localRay = normalize(vec3(p.x * aspect * uTanHalfFov,
                                               p.y * uTanHalfFov, -1.0));
                vec3 ray = normalize(rotateByQuaternion(localRay, uOrientation));
                vec3 eyeOrigin = vec3(uEyeSign * uIpdMeters, 0.0, 0.0);

                float longitude = atan(ray.x, -ray.z);
                float latitude = asin(clamp(ray.y, -1.0, 1.0));
                float lonLine = abs(fract((longitude + 3.14159265) * 10.0 / 6.2831853 + 0.5) - 0.5);
                float latLine = abs(fract((latitude + 1.5707963) * 12.0 / 3.1415926 + 0.5) - 0.5);
                float grid = 1.0 - smoothstep(0.018, 0.045, min(lonLine, latLine));
                vec3 color = mix(vec3(0.012, 0.022, 0.043), vec3(0.035, 0.085, 0.13),
                                 clamp(ray.y * 0.5 + 0.5, 0.0, 1.0));
                color += vec3(0.12, 0.22, 0.29) * grid * 0.48;

                float nearest = -1.0;
                float t = sphereHit(eyeOrigin, ray, vec3(0.0, 0.0, -2.6), 0.34);
                if (t > 0.0) {
                    nearest = t;
                    color = shadeSphere(eyeOrigin, ray, vec3(0.0, 0.0, -2.6), 0.34,
                                        vec3(0.12, 0.72, 0.96));
                }
                t = sphereHit(eyeOrigin, ray, vec3(-0.92, 0.16, -4.0), 0.58);
                if (t > 0.0 && (nearest < 0.0 || t < nearest)) {
                    nearest = t;
                    color = shadeSphere(eyeOrigin, ray, vec3(-0.92, 0.16, -4.0), 0.58,
                                        vec3(0.96, 0.43, 0.16));
                }
                t = sphereHit(eyeOrigin, ray, vec3(1.05, -0.22, -5.2), 0.82);
                if (t > 0.0 && (nearest < 0.0 || t < nearest)) {
                    color = shadeSphere(eyeOrigin, ray, vec3(1.05, -0.22, -5.2), 0.82,
                                        vec3(0.43, 0.88, 0.42));
                }

                float vignette = 1.0 - smoothstep(0.20, 1.18, length(p));
                color *= vignette;
                outColor = vec4(color, 1.0);
            }
        """

        private const val CALIBRATION_FRAGMENT_SHADER = """
            #version 300 es
            precision highp float;
            in vec2 vUv;
            out vec4 outColor;
            uniform float uEyeSign;
            uniform int uMode;

            float rectMask(vec2 p, vec2 center, vec2 halfSize) {
                vec2 d = abs(p - center) - halfSize;
                return 1.0 - smoothstep(0.0, 0.004, max(d.x, d.y));
            }

            float segmentMask(vec2 p, vec2 a, vec2 b, float radius) {
                vec2 pa = p - a;
                vec2 ba = b - a;
                float h = clamp(dot(pa, ba) / max(dot(ba, ba), 0.00001), 0.0, 1.0);
                return 1.0 - smoothstep(radius, radius + 0.004, length(pa - ba * h));
            }

            void main() {
                bool isLeft = uEyeSign < 0.0;
                if ((uMode == 3 && !isLeft) || (uMode == 4 && isLeft)) {
                    outColor = vec4(0.0, 0.0, 0.0, 1.0);
                    return;
                }

                vec2 p = vUv;
                vec2 centered = p - vec2(0.5);
                vec3 base = vec3(0.014, 0.020, 0.031);
                if (uMode == 2) {
                    base = isLeft ? vec3(0.28, 0.045, 0.025) : vec3(0.015, 0.16, 0.34);
                }

                float gx = 1.0 - smoothstep(0.0015, 0.004, abs(fract(p.x * 10.0) - 0.5) / 10.0);
                float gy = 1.0 - smoothstep(0.0015, 0.004, abs(fract(p.y * 8.0) - 0.5) / 8.0);
                float grid = max(gx, gy);
                float r = length(centered);
                float rings = 0.0;
                rings = max(rings, 1.0 - smoothstep(0.002, 0.006, abs(r - 0.16)));
                rings = max(rings, 1.0 - smoothstep(0.002, 0.006, abs(r - 0.30)));
                rings = max(rings, 1.0 - smoothstep(0.002, 0.006, abs(r - 0.44)));
                vec3 color = base + vec3(0.12, 0.20, 0.27) * grid * 0.38;
                color += vec3(0.20, 0.34, 0.43) * rings * 0.55;

                float centerCross = max(
                    rectMask(p, vec2(0.5), vec2(0.12, 0.0018)),
                    rectMask(p, vec2(0.5), vec2(0.0018, 0.12))
                );
                color = mix(color, vec3(0.86, 0.91, 0.96), centerCross);

                float targetX = 0.5;
                if (uMode == 5) targetX += isLeft ? 0.055 : -0.055;
                float targetRadius = length(p - vec2(targetX, 0.5));
                float targetRing = 1.0 - smoothstep(0.003, 0.009, abs(targetRadius - 0.035));
                float targetDot = 1.0 - smoothstep(0.012, 0.018, targetRadius);
                color = mix(color, uMode == 5 ? vec3(1.0, 0.78, 0.16) : vec3(0.32, 0.91, 0.68), max(targetRing, targetDot));

                float border = max(
                    max(rectMask(p, vec2(0.5, 0.025), vec2(0.47, 0.002)), rectMask(p, vec2(0.5, 0.975), vec2(0.47, 0.002))),
                    max(rectMask(p, vec2(0.025, 0.5), vec2(0.002, 0.46)), rectMask(p, vec2(0.975, 0.5), vec2(0.002, 0.46)))
                );
                color = mix(color, vec3(0.95, 0.72, 0.18), border);

                if (uMode == 2) {
                    float glyph = 0.0;
                    if (isLeft) {
                        glyph = max(
                            rectMask(p, vec2(0.37, 0.77), vec2(0.018, 0.105)),
                            rectMask(p, vec2(0.425, 0.685), vec2(0.073, 0.018))
                        );
                    } else {
                        glyph = max(
                            rectMask(p, vec2(0.585, 0.75), vec2(0.018, 0.12)),
                            max(
                                max(rectMask(p, vec2(0.635, 0.85), vec2(0.068, 0.018)), rectMask(p, vec2(0.635, 0.75), vec2(0.068, 0.018))),
                                max(segmentMask(p, vec2(0.70, 0.84), vec2(0.70, 0.76), 0.018), segmentMask(p, vec2(0.60, 0.72), vec2(0.69, 0.63), 0.018))
                            )
                        );
                    }
                    color = mix(color, vec3(0.98), glyph);
                }

                outColor = vec4(clamp(color, 0.0, 1.0), 1.0);
            }
        """

        private const val POST_FRAGMENT_SHADER = """
            #version 300 es
            precision highp float;
            in vec2 vUv;
            out vec4 outColor;
            uniform float uEyeSign;
            uniform vec2 uLensCenter;
            uniform vec2 uDistortion;
            uniform float uBlur;
            uniform float uSharpness;
            uniform vec2 uSourceTexel;
            uniform float uAspect;
            uniform float uSwapEyes;
            uniform vec2 uEyeOffset;
            uniform float uVerticalTiltRadians;
            uniform sampler2D uSceneTexture;

            vec3 sampleScene(vec2 uv, float eyeMin, float eyeMax) {
                vec2 bounded = vec2(clamp(uv.x, eyeMin, eyeMax), clamp(uv.y, 0.001, 0.999));
                return texture(uSceneTexture, bounded).rgb;
            }

            void main() {
                vec2 delta = vUv - uLensCenter;
                vec2 radialDelta = delta * vec2(uAspect, 1.0) * 2.0;
                // Google VR's Daydream View v2.0 viewer profile uses the
                // distorted-to-original mapping r = rd + k1*rd^3 + k2*rd^5.
                // This is the polynomial form used by the SDK's pre-warp path.
                float rd2 = dot(radialDelta, radialDelta);
                float radialScale = 1.0 + uDistortion.x * rd2 + uDistortion.y * rd2 * rd2;
                // Positive X/Y values move the displayed source image right/down:
                // X therefore subtracts from the sample coordinate, while the
                // GL texture Y axis is bottom-up and uses the opposite sign.
                vec2 warpedUv = uLensCenter + delta * radialScale + vec2(-uEyeOffset.x, uEyeOffset.y);
                // Pitch the image plane around its horizontal axis. This creates
                // a controlled vertical keystone so the upper/lower edge can be
                // aligned with the headset optics while the user remains wearing it.
                float tiltSin = sin(uVerticalTiltRadians);
                float tiltCos = cos(uVerticalTiltRadians);
                float tiltDenominator = max(0.25, tiltCos - (warpedUv.y - 0.5) * tiltSin);
                warpedUv.y = 0.5 + (warpedUv.y - 0.5) * tiltCos / tiltDenominator;
                if (warpedUv.x < 0.0 || warpedUv.x > 1.0 || warpedUv.y < 0.0 || warpedUv.y > 1.0) {
                    outColor = vec4(0.0, 0.0, 0.0, 1.0);
                    return;
                }

                // uEyeSign identifies the physical half-screen viewport and
                // therefore selects its lens center and user adjustment.
                // Source-eye routing is independent and can be swapped without
                // moving either physical lens center.
                float sourceEyeSign = uSwapEyes > 0.5 ? -uEyeSign : uEyeSign;
                float eyeMin = sourceEyeSign < 0.0 ? 0.0005 : 0.5005;
                float eyeMax = sourceEyeSign < 0.0 ? 0.4995 : 0.9995;
                vec2 sourceUv = vec2((sourceEyeSign < 0.0 ? 0.0 : 0.5) + warpedUv.x * 0.5,
                                     warpedUv.y);
                vec2 stepUv = uSourceTexel * (1.0 + max(uBlur, uSharpness) * 5.0);
                vec3 center = sampleScene(sourceUv, eyeMin, eyeMax);
                vec3 blurColor = center * 0.25;
                blurColor += sampleScene(sourceUv + vec2(stepUv.x, 0.0), eyeMin, eyeMax) * 0.125;
                blurColor += sampleScene(sourceUv - vec2(stepUv.x, 0.0), eyeMin, eyeMax) * 0.125;
                blurColor += sampleScene(sourceUv + vec2(0.0, stepUv.y), eyeMin, eyeMax) * 0.125;
                blurColor += sampleScene(sourceUv - vec2(0.0, stepUv.y), eyeMin, eyeMax) * 0.125;
                blurColor += sampleScene(sourceUv + stepUv, eyeMin, eyeMax) * 0.0625;
                blurColor += sampleScene(sourceUv - stepUv, eyeMin, eyeMax) * 0.0625;
                blurColor += sampleScene(sourceUv + vec2(stepUv.x, -stepUv.y), eyeMin, eyeMax) * 0.0625;
                blurColor += sampleScene(sourceUv + vec2(-stepUv.x, stepUv.y), eyeMin, eyeMax) * 0.0625;

                vec3 color = mix(center, blurColor, uBlur);
                color += (center - blurColor) * uSharpness * 0.85;
                outColor = vec4(clamp(color, 0.0, 1.0), 1.0);
            }
        """
    }
}
