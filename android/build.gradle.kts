plugins {
    alias(libs.plugins.kotlin.serialization) apply false
}

tasks.wrapper {
    gradleVersion = libs.versions.gradle.get()
    distributionType = Wrapper.DistributionType.BIN
    networkTimeout = 60_000
    retries = 3
}
