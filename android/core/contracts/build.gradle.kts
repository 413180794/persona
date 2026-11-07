import java.util.Properties

plugins {
    id("persona.jvm.library")
    id("org.jetbrains.kotlin.plugin.serialization")
}

dependencies {
    api(libs.serialization.json)
    api(libs.coroutines.core)
}

// The interface languages come from resources, not enum cases. This generated source is never checked in.
val languageFiles = rootProject.fileTree("i18n") { include("*.properties") }
abstract class GenerateLanguages : DefaultTask() {
    @get:InputFiles @get:PathSensitive(PathSensitivity.RELATIVE)
    abstract val languages: ConfigurableFileCollection
    @get:OutputDirectory abstract val outputDirectory: DirectoryProperty

    @TaskAction fun generate() {
        fun quote(s: String) = "\"" + s.replace("\\", "\\\\").replace("\"", "\\\"").replace("$", "\\$") + "\""
        val entries = languages.files.sortedWith(compareBy({ when (it.nameWithoutExtension) {
            "zh-Hans" -> 0; "en" -> 1; else -> 2
        } }, { it.name })).map { file ->
            val meta = Properties().apply { file.reader(Charsets.UTF_8).use(::load) }
            val matches = meta.getProperty("matches").split(',').map { quote(it.trim()) }.joinToString(", ")
            "        AppLanguage(${quote(file.nameWithoutExtension)}, ${quote(meta.getProperty("name"))}, listOf($matches))"
        }
        val output = outputDirectory.get().file("persona/core/contracts/GeneratedLanguages.kt").asFile
        output.parentFile.mkdirs()
        output.writeText("package persona.core.contracts\n\ninternal object GeneratedLanguages {\n    val entries = listOf(\n" +
            entries.joinToString(",\n") + "\n    )\n}\n")
    }
}
val languageSource = layout.buildDirectory.dir("generated/languages/kotlin")
val generateLanguages by tasks.registering(GenerateLanguages::class) {
    languages.from(languageFiles)
    outputDirectory.set(languageSource)
}
kotlin.sourceSets.named("main") { kotlin.srcDir(languageSource) }
tasks.withType<org.jetbrains.kotlin.gradle.tasks.KotlinCompile>().configureEach { dependsOn(generateLanguages) }

// Package the canonical schemas at build time, never a second handwritten client contract.
val streamSchemas by tasks.registering(Sync::class) {
    from(rootProject.file("../schema/extension")) {
        include("stream.schema.json", "view.schema.json", "protocol.schema.json")
    }
    into(layout.buildDirectory.dir("generated/stream-schemas/extension-schema"))
}
sourceSets.named("main") { resources.srcDir(layout.buildDirectory.dir("generated/stream-schemas")) }
tasks.named("processResources") { dependsOn(streamSchemas) }
