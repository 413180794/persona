package persona.android

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

/** #4989: allowBackup=false no longer blocks device-to-device transfer on API 31+, so the rules must exclude everything. */
class BackupRulesTest {
    private val android = "http://schemas.android.com/apk/res/android"
    private val domains = setOf(
        "root", "file", "database", "sharedpref", "external",
        "device_root", "device_file", "device_database", "device_sharedpref",
    )

    private fun parse(path: String) = DocumentBuilderFactory.newInstance()
        .apply { isNamespaceAware = true }.newDocumentBuilder().parse(File(path))

    @Test
    fun manifestReferencesExtractionRulesThatExcludeEveryDomain() {
        val app = parse("src/main/AndroidManifest.xml").getElementsByTagName("application").item(0) as Element
        assertEquals("false", app.getAttributeNS(android, "allowBackup"))
        assertEquals("@xml/data_extraction_rules", app.getAttributeNS(android, "dataExtractionRules"))

        val rules = parse("src/main/res/xml/data_extraction_rules.xml").documentElement
        for (section in listOf("cloud-backup", "device-transfer")) {
            val node = rules.getElementsByTagName(section).item(0) as Element?
            assertTrue("missing <$section>", node != null)
            assertEquals("$section includes", 0, node!!.getElementsByTagName("include").length)
            val excludes = node.getElementsByTagName("exclude")
            val excluded = (0 until excludes.length).map { excludes.item(it) as Element }
                .filter { it.getAttribute("path") == "." }.map { it.getAttribute("domain") }.toSet()
            assertEquals("$section excludes", domains, excluded)
        }
    }
}
