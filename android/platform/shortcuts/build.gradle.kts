plugins {
    id("jarvis.android.library")
    id("jarvis.compose")
    id("jarvis.hilt")
}

android { namespace = "persona.platform.shortcuts" }

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
}
