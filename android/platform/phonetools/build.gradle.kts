plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android { namespace = "persona.platform.phonetools" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:navigation"))
    implementation(project(":core:designsystem"))
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.compose.preview)
    implementation(libs.coroutines.android)
    implementation(libs.datastore.preferences)
    implementation(libs.androidx.work)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
