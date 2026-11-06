package persona.android.platform

import java.io.File
import javax.xml.parsers.DocumentBuilderFactory
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import org.w3c.dom.Element

class WorkerNamesTest {
    @Test fun manifestsDeclareEachCanonicalComponentOnceAndLaunchTheRealActivity() {
        val root = generateSequence(File(System.getProperty("user.dir")).absoluteFile) { it.parentFile }
            .first { File(it, "android/app/src/main/AndroidManifest.xml").isFile }
        val namespace = "http://schemas.android.com/apk/res/android"
        fun manifest(relative: String) = DocumentBuilderFactory.newInstance().apply { isNamespaceAware = true }
            .newDocumentBuilder().parse(File(root, "android/$relative/src/main/AndroidManifest.xml"))
        for (module in listOf("app", "platform/notify")) {
            val document = manifest(module)
            for (tag in listOf("activity", "activity-alias", "receiver", "service")) {
                val nodes = document.getElementsByTagName(tag)
                val names = (0 until nodes.length).map { (nodes.item(it) as Element).getAttributeNS(namespace, "name") }
                assertEquals("$module $tag", names.size, names.distinct().size)
            }
        }
        val app = manifest("app")
        assertEquals(0, app.getElementsByTagName("activity-alias").length)
        val launchers = app.getElementsByTagName("category").let { nodes ->
            (0 until nodes.length).map { nodes.item(it) as Element }
                .filter { it.getAttributeNS(namespace, "name") == "android.intent.category.LAUNCHER" }
        }
        assertEquals(1, launchers.size)
        val activity = launchers.single().parentNode.parentNode as Element
        assertEquals("activity", activity.tagName)
        assertEquals(".MainActivity", activity.getAttributeNS(namespace, "name"))
        val routes = app.getElementsByTagName("data").let { nodes ->
            (0 until nodes.length).map { nodes.item(it) as Element }
                .map { it.getAttributeNS(namespace, "scheme") to it.getAttributeNS(namespace, "host") }
                .filter { it.first.isNotEmpty() }
        }
        assertEquals(listOf("persona" to "pair", "persona" to "open"), routes)
        assertTrue(activity.getAttributeNS(namespace, "exported").toBoolean())
    }

    @Test fun queuedWorkersUseCanonicalNames() {
        assertEquals("persona.android.platform.ShareRetryWorker", ShareRetryWorker::class.java.name)
        assertEquals(ShareRetryWorker::class.java, Class.forName("persona.android.platform.ShareRetryWorker"))
        assertEquals("persona.platform.phonetools.PhoneNotificationWorker", persona.platform.phonetools.PhoneNotificationWorker::class.java.name)
        assertEquals(persona.platform.phonetools.PhoneNotificationWorker::class.java,
            Class.forName("persona.platform.phonetools.PhoneNotificationWorker"))
    }
}
