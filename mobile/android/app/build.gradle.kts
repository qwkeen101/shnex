plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// Version lives in gradle.properties (shnexVersion); -PshnexVersion=x.y.z overrides it.
val shnexVersion = (project.findProperty("shnexVersion") as String?)?.trim()
    ?: error("shnexVersion is not set in gradle.properties")
val shnexVersionCode = shnexVersion.split('.').map { it.toInt() }.let { parts ->
    require(parts.size == 3 && parts[1] < 100 && parts[2] < 100) { "shnexVersion must look like x.y.z: $shnexVersion" }
    parts[0] * 10000 + parts[1] * 100 + parts[2]
}

android {
    namespace = "dev.shnex.app"
    compileSdk = 35

    defaultConfig {
        applicationId = "dev.shnex.app"
        minSdk = 26
        targetSdk = 35
        versionCode = shnexVersionCode
        versionName = shnexVersion

        ndk {
            abiFilters += listOf("arm64-v8a")
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            // Sign release preview with Android standard debug key if no release keystore is configured yet,
            // so the produced APK installs directly on any Android phone out of the box.
            signingConfig = signingConfigs.getByName("debug")
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

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }

    // Bundled terminal font is stored uncompressed so Typeface.createFromAsset can map it directly.
    androidResources {
        noCompress += "ttf"
    }

    packaging {
        resources {
            excludes += "/META-INF/{AL2.0,LGPL2.1}"
        }
    }
}

dependencies {
    // In-process Go Single-Core Engine (.aar produced by gomobile bind from core/mobilebridge)
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar", "*.jar"))))

    // 100% Apache-2.0 Official Jetpack Compose Material 3 & AndroidX Libraries (ZERO GPL, ZERO WebView)
    val composeBom = platform("androidx.compose:compose-bom:2024.11.00")
    implementation(composeBom)
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.8.7")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-graphics")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")

    // Apache-2.0 HTTP + Binary WebSocket Client for 127.0.0.1 Core Bridge
    implementation("com.squareup.okhttp3:okhttp:4.12.0")

    // Apache-2.0 Pure-Java Vector SVG Renderer for Go core/texmath Native Math Formulas (Zero WebView)
    implementation("com.caverock:androidsvg-aar:1.4")
}
