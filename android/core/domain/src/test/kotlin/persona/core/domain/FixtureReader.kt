package persona.core.domain

import persona.core.contracts.JsonCodec
import java.io.File
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonArray
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.int

/** Shared fixtures stay outside Android and can arrive in a later merge. */
object FixtureReader {
    data class Case(val file: String, val value: JsonObject)

    fun read(domain: String): List<Case> {
        val root = System.getProperty("persona.repo.root")?.let(::File) ?: generateSequence(File(".").canonicalFile) { it.parentFile }
            .firstOrNull { File(it, ".git").exists() && File(it, "android").isDirectory }
            ?: error("Cannot locate the repository root; set persona.repo.root")
        val directory = File(root, "spec/fixtures/$domain")
        if (!directory.isDirectory) {
            println("Skipping spec/fixtures/$domain: directory is not present")
            return emptyList()
        }
        val files = directory.listFiles { file -> file.isFile && file.extension == "json" }!!.sortedBy { it.name }
        check(files.isNotEmpty()) { "No JSON fixtures in spec/fixtures/$domain" }
        return files.flatMap { file ->
            val fixture = JsonCodec.parseToJsonElement(file.readText()).jsonObject
            check(fixture.getValue("version").jsonPrimitive.int == 1) { "Unsupported fixture version: ${file.name}" }
            val cases = fixture.getValue("cases").jsonArray
            check(cases.isNotEmpty()) { "No cases in ${file.name}" }
            cases.map { Case(file.name, it.jsonObject) }
        }.also { cases ->
            val names = cases.map { it.value.getValue("name").jsonPrimitive.content }
            check(names.distinct().size == names.size) { "Duplicate fixture case names" }
        }
    }
}
