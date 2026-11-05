package persona.core.domain.sort

import persona.core.domain.zhAny

import persona.core.contracts.Session
import org.junit.Assert.*
import org.junit.Test

class SessionRenameTest {
    private val session = Session("machine", "session", title = "旧会话", paneId = "pane", slotName = "旧标签")

    @Test fun onlyChangedTrimmedNonemptyFieldsAreSent() {
        assertFalse(SessionRename.changes(session, " 旧会话 ", " 旧标签 ").changed)
        assertFalse(SessionRename.changes(session, "  \n", "").changed)
        assertFalse(SessionRename.changes(session, null, null).changed)
        assertEquals(SessionRename(title = "新会话"), SessionRename.changes(session, " 新会话\n", "旧标签"))
        assertEquals(SessionRename(tabName = "新标签"), SessionRename.changes(session, "", " 新标签 "))
        assertFalse(SessionRename.changes(session.copy(paneId = null, tabId = "tab"), "", "新标签").changed)
        assertFalse(SessionRename.changes(session.copy(paneId = " "), "", "新标签").changed)
        val edit = SessionRename.changes(session, " 新会话 ", " 新标签 ")
        assertEquals("新会话", (edit.apply(session).displayTitle).zhAny())
        assertEquals("新标签", (edit.apply(session).slotName).zhAny())
        assertTrue(edit.matches(edit.apply(session)))
        assertFalse(edit.matches(session))
    }
}
