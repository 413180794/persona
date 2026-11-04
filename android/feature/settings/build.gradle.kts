plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android {
    namespace = "persona.feature.settings"
    // Plugins' views are rendered on the JVM (Robolectric's native graphics) for their pixel checks and screenshots.
    testOptions { unitTests { isIncludeAndroidResources = true } }
    defaultConfig {
        manifestPlaceholders["googleOAuthScheme"] = "persona.oauth.unset"
    }
}

dependencies {
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.androidx.core)
    implementation(libs.zxing)
    implementation(libs.okhttp)
    implementation(libs.compose.icons)
    implementation(libs.compose.preview)
    testImplementation(libs.junit)
    testImplementation(project(":core:hub"))
    // An inbox item's page in pictures: the inbox's header over a plugin's body (InboxItemShotsTest).
    testImplementation(project(":feature:inbox"))
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
}
