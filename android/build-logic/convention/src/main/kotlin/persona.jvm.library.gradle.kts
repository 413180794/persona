import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    id("org.jetbrains.kotlin.jvm")
    `java-library`
}

val libs = extensions.getByType<VersionCatalogsExtension>().named("libs")
kotlin {
    jvmToolchain(libs.findVersion("jdk").get().requiredVersion.toInt())
    compilerOptions {
        jvmTarget.set(JvmTarget.fromTarget(libs.findVersion("jvm").get().requiredVersion))
    }
}
java {
    sourceCompatibility = JavaVersion.toVersion(libs.findVersion("jvm").get().requiredVersion)
    targetCompatibility = sourceCompatibility
}
dependencies {
    "testImplementation"(libs.findLibrary("junit").get())
}
tasks.withType<Test>().configureEach {
    useJUnit()
    val repositoryRoot = providers.gradleProperty("personaRepoRoot")
        .getOrElse(rootProject.layout.projectDirectory.dir("..").asFile.absolutePath)
    systemProperty("persona.repo.root", repositoryRoot)
    inputs.files(fileTree(File(repositoryRoot, "spec/fixtures")) { include("**/*.json") })
        .withPropertyName("sharedFixtures")
        .withPathSensitivity(PathSensitivity.RELATIVE)
    testLogging {
        events("failed", "skipped")
        showStandardStreams = true
    }
}
