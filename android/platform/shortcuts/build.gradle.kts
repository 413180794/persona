plugins {
    id("persona.android.library")
    id("persona.compose")
    id("persona.hilt")
}

android {
    namespace = "persona.platform.shortcuts"
    testOptions { unitTests { isIncludeAndroidResources = true } }
}

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(project(":core:navigation"))
    // HubFailure, to say why loading or answering failed.
    implementation(project(":core:hub"))
    // FleetFeed: the hub's fleet, shared with the ongoing notification.
    implementation(project(":platform:notify"))
    implementation(libs.androidx.core)
    implementation(libs.androidx.activity)
    implementation(libs.androidx.lifecycle.compose)
    implementation(libs.coroutines.android)
    implementation(libs.glance.appwidget)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(platform(libs.compose.bom))
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.compose.ui:ui-test-manifest")
    testImplementation("org.robolectric:robolectric:4.17")
    testImplementation("androidx.test.ext:junit:1.3.0")
}
