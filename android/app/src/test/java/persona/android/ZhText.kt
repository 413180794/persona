package persona.android

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.w3c.dom.Element

/**
 * The app's Simplified Chinese strings for JVM tests, which have no Android resources: AppText reads them from
 * values-zh, so tests keep asserting the words the app shows (docs/protocol/i18n.md: unit tests run in Chinese).
 */
internal object ZhText {
    fun install() {
        val names = R.string::class.java.fields.associate { it.getInt(null) to it.name }
        val document = DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(File("src/main/res/values-zh/strings.xml"))
        val nodes = document.getElementsByTagName("string")
        val texts = (0 until nodes.length).map { nodes.item(it) as Element }
            .associate { it.getAttribute("name") to it.textContent.replace("\\n", "\n").replace("\\'", "'").replace("\\\"", "\"") }
        AppText.install { id, args -> String.format(requireNotNull(texts[names[id]]) { "no string $id" }, *args) }
    }
}
