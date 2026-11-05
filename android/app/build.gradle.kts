plugins {
    id("jarvis.android.application")
    id("jarvis.compose")
    id("jarvis.hilt")
    id("org.jetbrains.kotlin.plugin.serialization")
}

// The application id is personal (docs/config.md): scripts/android.sh passes it from ~/.config/jarvis/config.json.
val appId = providers.gradleProperty("personaApplicationId")
    .orElse(providers.gradleProperty("jarvisApplicationId"))
    .getOrElse("com.example.jarvis")
// A different label lets two builds sit side by side on one phone (with different application ids).
val appLabel = providers.gradleProperty("personaAppLabel")
    .orElse(providers.gradleProperty("jarvisAppLabel"))
    .getOrElse("Persona")
// Gmail's Google sign-in: the redirect scheme of the hub's Google OAuth client (docs/mail.md「Android」).
val googleOAuthScheme = providers.gradleProperty("personaGoogleOAuthScheme")
    .orElse(providers.gradleProperty("jarvisGoogleOAuthScheme"))
    .orNull?.takeIf { it.isNotBlank() }
    ?: System.getenv("PERSONA_GOOGLE_OAUTH_SCHEME")?.takeIf { it.isNotBlank() }
    ?: System.getenv("JARVIS_GOOGLE_OAUTH_SCHEME")?.takeIf { it.isNotBlank() }
    ?: "persona.oauth.unset"

android {
    namespace = "persona.android"
    lint {
        baseline = file("lint-baseline.xml")
    }
    defaultConfig {
        applicationId = appId
        versionCode = 1
        versionName = "0.1"
        manifestPlaceholders["appLabel"] = appLabel
        manifestPlaceholders["googleOAuthScheme"] = googleOAuthScheme
    }
    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }
}

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:terminal"))
    implementation(project(":core:navigation"))
    implementation(project(":core:hub"))
    implementation(project(":core:data"))
    implementation(project(":core:chatlist"))
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.material3.adaptive)
    implementation(libs.androidx.material3.adaptive.layout)
    implementation(libs.androidx.material3.adaptive.navigation3)
    implementation(project(":feature:chat"))
    implementation(project(":feature:agents"))
    implementation(project(":feature:assistant"))
    implementation(project(":feature:inbox"))
    implementation(project(":feature:settings"))
    implementation(project(":feature:terminal"))
    implementation(project(":feature:machines"))
    implementation(project(":feature:files"))
    implementation(project(":feature:knowledge"))
    implementation(project(":feature:voice"))
    implementation(project(":platform:phonetools"))
    implementation(project(":core:demo"))
    implementation(libs.room.runtime)
    implementation(project(":platform:notify"))
    implementation(project(":platform:shortcuts"))
    implementation(project(":platform:capsule"))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.compose.preview)
    debugImplementation(libs.compose.tooling)

    implementation(libs.androidx.work)
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.androidx.navigation3.ui)
    implementation(libs.androidx.lifecycle.navigation3)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.okhttp)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(libs.turbine)
}
