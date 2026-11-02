plugins {
    id("jarvis.android.library")
}

android {
    namespace = "jarvis.core.demo"
    sourceSets.getByName("test").resources.srcDir("src/main/assets")
}

dependencies {
    api(project(":core:contracts"))
    implementation(project(":core:hub"))
    testImplementation(libs.junit)
    testImplementation(libs.coroutines.test)
}
