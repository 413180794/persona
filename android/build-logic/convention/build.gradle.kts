plugins {
    `kotlin-dsl`
}

kotlin {
    jvmToolchain(libs.versions.jdk.get().toInt())
}

dependencies {
    implementation(libs.android.gradlePlugin)
    implementation(libs.kotlin.gradlePlugin)
    implementation(libs.kotlin.composePlugin)
    implementation(libs.kotlin.serializationPlugin)
    implementation(libs.hilt.gradlePlugin)
    implementation(libs.ksp.gradlePlugin)
}
