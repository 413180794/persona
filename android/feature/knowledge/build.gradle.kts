plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android { namespace = "persona.feature.knowledge" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:${libs.versions.lifecycle.get()}")
    implementation(libs.androidx.navigation3.runtime)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.markdown)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
