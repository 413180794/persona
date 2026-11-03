plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "persona.feature.chat" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:chatlist"))
    implementation(project(":core:navigation"))
    implementation(project(":feature:inbox"))
    implementation(libs.androidx.activity)
    implementation(libs.androidx.webkit)
    implementation(libs.compose.ui)
    implementation(libs.compose.material3)
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.core)
    implementation(libs.androidx.navigation3.runtime)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.markdown)
    implementation(libs.coil.compose)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
