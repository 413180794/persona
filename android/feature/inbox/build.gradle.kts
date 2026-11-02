plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
    id("org.jetbrains.kotlin.plugin.serialization")
}

android { namespace = "jarvis.feature.inbox" }

dependencies {
    implementation(project(":core:chatlist"))
    implementation(project(":core:contracts"))
    implementation(project(":core:hub"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.activity)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.coil.compose)
    implementation(libs.compose.preview)
    implementation(libs.compose.icons)
    implementation("org.jsoup:jsoup:1.18.3")
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
