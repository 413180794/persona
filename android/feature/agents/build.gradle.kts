plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "jarvis.feature.agents" }

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":feature:chat"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    implementation(libs.compose.icons)
    implementation(libs.androidx.lifecycle.compose)
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:${libs.versions.lifecycle.get()}")
    implementation(libs.androidx.activity)
    implementation(libs.datastore.preferences)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
