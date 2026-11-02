plugins {
    id("jarvis.android.library")
    id("jarvis.hilt")
}

android {
    namespace = "jarvis.platform.notify"
    testOptions.unitTests.all {
        it.systemProperty("jarvis.repo.root", rootProject.projectDir.parentFile.absolutePath)
    }
}

dependencies {
    implementation(project(":core:contracts"))
    implementation(project(":core:domain"))
    implementation(project(":core:designsystem"))
    implementation(platform(libs.compose.bom))
    implementation(project(":core:navigation"))
    implementation(libs.androidx.core)
    implementation(libs.androidx.work)
    implementation(libs.coroutines.android)
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
    testImplementation(libs.serialization.json)
}
