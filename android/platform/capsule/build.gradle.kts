plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
}
android { namespace = "persona.platform.capsule" }
dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:designsystem"))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.androidx.core)
    implementation(libs.androidx.lifecycle.compose)
    implementation("androidx.lifecycle:lifecycle-viewmodel:${libs.versions.lifecycle.get()}")
    implementation(libs.androidx.lifecycle.process)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
