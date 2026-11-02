plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "jarvis.feature.settings" }

dependencies {
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.core)
    implementation(libs.zxing)
    implementation(libs.okhttp)
    implementation(libs.compose.icons)
    implementation(libs.compose.preview)
    testImplementation(libs.junit)
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
}
