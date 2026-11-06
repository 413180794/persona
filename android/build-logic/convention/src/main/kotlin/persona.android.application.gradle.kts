plugins {
    id("com.android.application")
}

val libs = extensions.getByType<VersionCatalogsExtension>().named("libs")
android {
    buildToolsVersion = libs.findVersion("buildTools").get().requiredVersion
    compileSdk = libs.findVersion("compileSdk").get().requiredVersion.toInt()
    defaultConfig {
        minSdk = libs.findVersion("minSdk").get().requiredVersion.toInt()
        targetSdk = libs.findVersion("targetSdk").get().requiredVersion.toInt()
    }
    // The core modules' words in Simplified Chinese for code with no resources at hand (CoreTextFallback).
    sourceSets.getByName("test").resources.srcDir(rootProject.file("core/designsystem/src/main/res/values-zh"))
    compileOptions {
        sourceCompatibility = JavaVersion.toVersion(libs.findVersion("jvm").get().requiredVersion)
        targetCompatibility = JavaVersion.toVersion(libs.findVersion("jvm").get().requiredVersion)
    }
}
