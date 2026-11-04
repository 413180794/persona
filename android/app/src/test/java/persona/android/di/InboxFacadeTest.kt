package persona.android.di

import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File

/**
 * Verifies that the InboxRepository facade in InboxModule explicitly forwards
 * every method defined on InboxRepository to avoid silent fallback to default interface implementations.
 * (Fixes #1619)
 */
class InboxFacadeTest {
    @Test
    fun facadeOverridesAllInboxRepositoryMethods() {
        val rootDir = File(".").canonicalFile
        val moduleFile = File(rootDir, "android/app/src/main/java/persona/android/di/InboxModule.kt")
        assertTrue("InboxModule.kt must exist at ${moduleFile.path}", moduleFile.exists())

        val content = moduleFile.readText()
        val facadeBlock = content.substringAfter("val inbox = object : persona.feature.inbox.InboxRepository {")
            .substringBefore("val assistant = object : AssistantConversations {")

        val requiredMethods = listOf(
            "momentsPost",
            "momentsComments",
            "urlPreview",
            "react",
            "cardAction",
            "userProfile",
            "quickReactions",
            "emojis",
            "refresh",
            "refreshNow",
            "loadOlder",
            "refreshConversation",
            "loadNewer",
            "latestConversation",
            "markVisible",
            "setDone",
            "readDraft",
            "saveDraft",
            "reply",
            "downloadFile",
            "replyStatus",
            "verifyReply",
            "askPersona",
            "personaContext",
            "draftReply",
            "handOff"
        )

        for (method in requiredMethods) {
            assertTrue(
                "InboxRepository facade in InboxModule.kt must explicitly override $method to forward to active inbox",
                facadeBlock.contains("override suspend fun $method") ||
                facadeBlock.contains("override fun $method") ||
                facadeBlock.contains("override val $method")
            )
        }
    }
}
