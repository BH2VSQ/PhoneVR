plugins {
    id("com.android.application")
}

val phoneVrReleaseKeystore = System.getenv("PHONEVR_RELEASE_KEYSTORE")
val phoneVrReleaseStorePassword = System.getenv("PHONEVR_RELEASE_STORE_PASSWORD")
val phoneVrReleaseKeyAlias = System.getenv("PHONEVR_RELEASE_KEY_ALIAS")
val phoneVrReleaseKeyPassword = System.getenv("PHONEVR_RELEASE_KEY_PASSWORD")
val phoneVrReleaseSigningEnabled = listOf(
    phoneVrReleaseKeystore,
    phoneVrReleaseStorePassword,
    phoneVrReleaseKeyAlias,
    phoneVrReleaseKeyPassword
).all { !it.isNullOrBlank() }

android {
    namespace = "com.vsq.phonvrstream"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.vsq.phonvrstream"
        minSdk = 26
        targetSdk = 36
        versionCode = 36
        versionName = "0.5.1"
    }

    signingConfigs {
        if (phoneVrReleaseSigningEnabled) {
            create("phoneVrRelease") {
                storeFile = file(phoneVrReleaseKeystore!!)
                storePassword = phoneVrReleaseStorePassword
                keyAlias = phoneVrReleaseKeyAlias
                keyPassword = phoneVrReleaseKeyPassword
            }
        }
    }

    buildTypes {
        release {
            if (phoneVrReleaseSigningEnabled) {
                signingConfig = signingConfigs.getByName("phoneVrRelease")
            }
            isMinifyEnabled = false
            proguardFiles(
                getDefaultProguardFile("proguard-android-optimize.txt"),
                "proguard-rules.pro"
            )
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    testOptions {
        unitTests.isReturnDefaultValues = true
    }
}

dependencies {
    testImplementation("junit:junit:4.13.2")
    // Use a real JVM implementation for local unit tests. The Android SDK
    // org.json stubs return null/default values under the mockable android.jar.
    testImplementation("org.json:json:20240303")
}
