plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

// The application id is personal (docs/config.md): scripts/android.sh passes it from ~/.config/jarvis/config.json.
val appId = providers.gradleProperty("jarvisApplicationId").getOrElse("com.example.jarvis")

android {
    namespace = "jarvis.android"
    compileSdk = 34

    defaultConfig {
        applicationId = appId
        minSdk = 26
        targetSdk = 34
        versionCode = 1
        versionName = "0.1"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
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
}

dependencies {
    val composeBom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(composeBom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material:material-icons-extended")
    implementation("androidx.compose.ui:ui-tooling-preview")
    debugImplementation("androidx.compose.ui:ui-tooling")

    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.lifecycle:lifecycle-runtime-compose:2.8.7")
    implementation("androidx.lifecycle:lifecycle-process:2.8.7")
    implementation("androidx.navigation:navigation-compose:2.8.5")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.7.3")
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
    // Algorithmic darkening for pages in the link sheet (dark mode without a white page).
    implementation("androidx.webkit:webkit:1.12.1")
    // QR pairing (jarvis://pair?...) without Google Play services, which many Android phones in China do not have.
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    // The home-screen widget (widget/JarvisWidget.kt); the last version that builds against compileSdk 34.
    implementation("androidx.glance:glance-appwidget:1.1.1")

    testImplementation("junit:junit:4.13.2")
    // REST calls against a local HTTP server (same version as okhttp).
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
