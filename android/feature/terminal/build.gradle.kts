plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "persona.feature.terminal" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:terminal"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:${libs.versions.lifecycle.get()}")
    implementation(libs.androidx.navigation3.runtime)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
