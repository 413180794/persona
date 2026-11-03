plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
}

android { namespace = "persona.core.chatlist" }

dependencies {
    api(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
}
