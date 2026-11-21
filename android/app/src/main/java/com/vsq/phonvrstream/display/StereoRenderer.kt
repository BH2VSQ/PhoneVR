package com.vsq.phonvrstream.display

import android.opengl.GLES30
import android.opengl.GLES11Ext
import android.graphics.SurfaceTexture
import android.os.Handler
import android.os.Looper
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
    private var videoTextureId = 0
    private var videoSurfaceTexture: SurfaceTexture? = null
    private var videoSurface: Surface? = null
    private val videoFramePending = AtomicBoolean(false)
    @Volatile private var videoSurfaceListener: ((Surface) -> Unit)? = null
    private var hasVideoFrame = false
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
        val blur: Int, val sharpness: Int, val sourceTexel: Int, val aspect: Int, val sceneTexture: Int,
    )
    private lateinit var scene: SceneLocations
    private lateinit var post: PostLocations

    fun setVideoSurfaceListener(listener: (Surface) -> Unit) {
        videoSurfaceListener = listener
        videoSurface?.let(listener)
    }

    private fun createVideoInputSurface() {
        videoSurface?.release()
        videoSurfaceTexture?.release()
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
        val texture = SurfaceTexture(videoTextureId).apply {
            setDefaultBufferSize(VIDEO_WIDTH, VIDEO_HEIGHT)
            setOnFrameAvailableListener({ videoFramePending.set(true) }, Handler(Looper.getMainLooper()))
        }
        val surface = Surface(texture)
        videoSurfaceTexture = texture
        videoSurface = surface
        videoFramePending.set(false)
        hasVideoFrame = false
        videoSurfaceListener?.invoke(surface)
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
            } catch (_: RuntimeException) {
                hasVideoFrame = false
            }
        }

        // Pass 1: use the live decoded SBS stream when available. Before the
        // first complete decoder frame, retain the built-in test scene.
        GLES30.glBindFramebuffer(GLES30.GL_FRAMEBUFFER, framebuffer)
        GLES30.glViewport(0, 0, width, height)
        GLES30.glClear(GLES30.GL_COLOR_BUFFER_BIT)
        if (hasVideoFrame && videoProgram != 0 && videoTextureId != 0) {
            GLES30.glUseProgram(videoProgram)
            bindPosition(video.position)
            GLES30.glActiveTexture(GLES30.GL_TEXTURE0)
            GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, videoTextureId)
            GLES30.glUniform1i(video.texture, 0)
            GLES30.glUniformMatrix4fv(video.transform, 1, false, videoTextureMatrix, 0)
            GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
            GLES30.glDisableVertexAttribArray(video.position)
            GLES30.glBindTexture(GLES11Ext.GL_TEXTURE_EXTERNAL_OES, 0)
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
        GLES30.glUniform2f(post.distortion, settings.distortionK1, settings.distortionK2)
        GLES30.glUniform1f(post.blur, settings.blur)
        GLES30.glUniform1f(post.sharpness, settings.sharpness)

        GLES30.glViewport(0, 0, halfWidth, height)
        GLES30.glUniform1f(post.eyeSign, -1f)
        GLES30.glUniform2f(post.lensCenter, leftLensCenterX, settings.lensCenterY)
        GLES30.glDrawArrays(GLES30.GL_TRIANGLE_STRIP, 0, 4)
        GLES30.glViewport(halfWidth, 0, width - halfWidth, height)
        GLES30.glUniform1f(post.eyeSign, 1f)
        GLES30.glUniform2f(post.lensCenter, rightLensCenterX, settings.lensCenterY)
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
                vec2 warpedUv = uLensCenter + delta * radialScale;
                if (warpedUv.x < 0.0 || warpedUv.x > 1.0 || warpedUv.y < 0.0 || warpedUv.y > 1.0) {
                    outColor = vec4(0.0, 0.0, 0.0, 1.0);
                    return;
                }

                float eyeMin = uEyeSign < 0.0 ? 0.0005 : 0.5005;
                float eyeMax = uEyeSign < 0.0 ? 0.4995 : 0.9995;
                vec2 sourceUv = vec2((uEyeSign < 0.0 ? 0.0 : 0.5) + warpedUv.x * 0.5,
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
