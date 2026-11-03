plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
}

android { namespace = "persona.feature.voice" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(project(":core:hub"))
    testImplementation(libs.coroutines.test)
}
