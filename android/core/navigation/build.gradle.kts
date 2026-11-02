plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android { namespace = "jarvis.core.navigation" }

dependencies {
    api(libs.androidx.navigation3.runtime)
    implementation(libs.serialization.json)
    testImplementation(libs.junit)
}
