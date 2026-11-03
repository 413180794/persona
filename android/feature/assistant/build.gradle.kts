plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "persona.feature.assistant" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:data"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(project(":feature:chat"))
    implementation(libs.compose.icons)
    implementation(libs.compose.preview)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
