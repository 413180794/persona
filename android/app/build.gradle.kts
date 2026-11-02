plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
    id("app.cash.paparazzi")
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
        // Gmail's Google sign-in: the redirect scheme of the hub's Google OAuth client (its reversed client ID, docs/mail.md
        // 「Android」). Personal, so it comes from the config like the application id: scripts/android.sh passes
        // android.google_oauth_scheme; JARVIS_GOOGLE_OAUTH_SCHEME works too. Without it the app offers app passwords.
        manifestPlaceholders["googleOAuthScheme"] = providers.gradleProperty("jarvisGoogleOAuthScheme").orNull?.takeIf { it.isNotBlank() }
            ?: System.getenv("JARVIS_GOOGLE_OAUTH_SCHEME")?.takeIf { it.isNotBlank() }
            ?: "jarvis.oauth.unset"
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

// The screenshot tests (src/test/java/jarvis/android/shots, Paparazzi against scripts/mock_hub.py) run only when asked:
// ./scripts/android.sh shots (docs/testing.md「安卓截图」); `test` stays the plain unit tests.
tasks.withType<Test>().configureEach {
    if (!project.hasProperty("shots")) exclude("jarvis/android/shots/**")
    // DesignTokensTest writes its baseline anew (after counts went down): -PrecordDesignBaseline.
    if (project.hasProperty("recordDesignBaseline")) systemProperty("recordDesignBaseline", "true")
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
    // Where a foldable's hinge is (ui/shell/Fold.kt): the inner screen's two panes meet there.
    implementation("androidx.window:window:1.3.0")
    // The home-screen widget (widget/JarvisWidget.kt); the last version that builds against compileSdk 34.
    implementation("androidx.glance:glance-appwidget:1.1.1")
    // phone_health_summary (phone/Health.kt). 1.1.0-alpha08 is the last one that builds against compileSdk 34
    // (alpha09 needs 35, 1.1.0 needs 36 and AGP 8.9).
    implementation("androidx.health.connect:connect-client:1.1.0-alpha08")

    testImplementation("junit:junit:4.13.2")
    // REST calls against a local HTTP server (same version as okhttp).
    testImplementation("com.squareup.okhttp3:mockwebserver:4.12.0")
}
