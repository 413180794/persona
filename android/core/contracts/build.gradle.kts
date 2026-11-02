plugins {
    id("jarvis.jvm.library")
    id("org.jetbrains.kotlin.plugin.serialization")
}

dependencies {
    api(libs.serialization.json)
    api(libs.coroutines.core)
}
