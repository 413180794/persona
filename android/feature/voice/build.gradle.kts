plugins {
    id("persona.android.library")
    id("persona.compose")
}

android {
    namespace = "persona.feature.voice"
    // The page's words are resources (docs/protocol/i18n.md): tests read them, and its pictures are drawn, under Robolectric.
    testOptions { unitTests { isIncludeAndroidResources = true } }
}

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
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
}
