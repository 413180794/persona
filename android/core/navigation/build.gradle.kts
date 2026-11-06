plugins {
    id("persona.android.library")
    id("persona.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android { namespace = "persona.core.navigation" }

dependencies {
    api(libs.androidx.navigation3.runtime)
    implementation(libs.serialization.json)
    testImplementation(libs.junit)
}
