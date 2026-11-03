plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
}

android { namespace = "persona.feature.files" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:hub"))
    implementation(project(":core:navigation"))
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.activity)
    implementation(libs.coroutines.android)
    implementation(libs.coil.compose)
    implementation(libs.markdown)
    implementation(libs.androidx.core)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
