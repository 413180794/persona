package persona.feature.settings

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.api.LarkMessageDisplay
import persona.core.contracts.api.MessagesChat
import persona.core.contracts.api.MessagesMessage
import persona.core.contracts.api.MessagesPerson
import persona.core.contracts.api.MessagesStatus
import persona.core.designsystem.Status
import java.time.ZoneOffset

class ParityInboxTest {
    @Test fun messagesStateWordsFollowTheStatus() {
        assertEquals(R.string.settings_src_checking, messagesStateText(null, loading = true))
        assertEquals(R.string.settings_src_status_unreadable, messagesStateText(null, loading = false))
        assertEquals(R.string.settings_src_messages_state_ok, messagesStateText(MessagesStatus(status = "ok"), false))
        assertEquals(R.string.settings_src_messages_state_no_permission, messagesStateText(MessagesStatus(status = "no_permission"), false))
        assertEquals(R.string.settings_src_messages_state_failed, messagesStateText(MessagesStatus(status = "error"), false))
        assertEquals(Status.Idle, messagesStatusDot(MessagesStatus(status = "ok")))
        assertEquals(Status.NeedsYou, messagesStatusDot(MessagesStatus(status = "no_permission")))
        assertEquals(Status.Offline, messagesStatusDot(MessagesStatus(status = "not_configured")))
    }

    @Test fun chatSearchMatchesNamesAndHandles() {
        val chats = listOf(
            MessagesChat("1", "Alex", members = listOf(MessagesPerson("+1 555 0100", "Alex"))),
            MessagesChat("2", "Trip", group = true, members = listOf(MessagesPerson("sam@example.com", "Sam"))),
        )
        assertEquals(chats, filterMessagesChats(chats, "  "))
        assertEquals(listOf("1"), filterMessagesChats(chats, "alex").map { it.id })
        assertEquals(listOf("1"), filterMessagesChats(chats, "0100").map { it.id })
        assertEquals(listOf("2"), filterMessagesChats(chats, "sam").map { it.id })
    }

    @Test fun linesShowTimeAfterAPauseSenderInGroupsAndServiceChanges() {
        val alex = MessagesPerson("a", "Alex")
        val sam = MessagesPerson("s", "Sam")
        val lines = messagesLines(listOf(
            MessagesMessage("1", "2026-01-01T08:00:00Z", sender = alex, text = "hi", service = "iMessage"),
            MessagesMessage("2", "2026-01-01T08:05:00Z", sender = alex, text = "again", service = "iMessage"),
            MessagesMessage("3", "2026-01-01T08:06:00Z", sender = sam, text = "yo", service = "SMS"),
            MessagesMessage("4", "2026-01-01T10:00:00Z", fromMe = true, text = "late", service = "SMS"),
        ), group = true, services = true, zone = ZoneOffset.UTC)
        assertEquals(listOf(true, false, false, true), lines.map { it.time != null })
        assertEquals(listOf("Alex", null, "Sam", null), lines.map { it.sender })
        assertEquals(listOf("iMessage", null, "SMS", null), lines.map { it.service })
    }

    @Test fun outlookSignsInInsteadOfAPassword() {
        val form = MailAccountForm(MailAccountForm.Kind.OUTLOOK, "me@example.com", "pw")
        assertFalse(form.canConnect(gmailPasswordAllowed = true))
        assertEquals("outlook", form.request().provider)
    }

    @Test fun microsoftErrorsKeepTheirDescription() {
        val r = GoogleSignIn.parse(null, "s", "invalid_request", "admin consent required")
        assertEquals(GoogleSignIn.Result.Failed("invalid_request", "s", "admin consent required"), r)
        assertEquals(GoogleSignIn.Result.Cancelled, GoogleSignIn.parse(null, "s", "access_denied", "user cancelled"))
    }

    @Test fun messageDisplayKeysNameTheirSwitch() {
        val d = LarkMessageDisplay("acct", antiRecall = true, showEditHistory = false)
        assertEquals(true, d.value("anti_recall"))
        assertEquals(false, d.value("show_edit_history"))
        assertNull(d.value("unsupported_setting"))
        assertNull(d.value("unknown"))
    }
}
