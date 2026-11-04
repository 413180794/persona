package persona.feature.agents

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import persona.core.contracts.api.HubCompat
import persona.core.contracts.api.HubNotice
import persona.core.contracts.api.HubRelease
import persona.core.contracts.api.HubUpdateStatus

/** The line over the Agents list about the hub (#1838): which notice, its words, and closing it. */
class HubNoticeRowTest {
    private val newer = HubRelease(version = "1.1.0", build = 110, newer = true)

    @Test fun textMappingMatchesCopy() {
        assertEquals("App 版本太旧，配不上这个 hub：请到 TestFlight 更新", HubNotice.text(HubNotice.AppTooOld))
        assertEquals("hub 版本太旧，部分功能用不了", HubNotice.text(HubNotice.HubTooOld))
        assertEquals("hub 正在更新：正在下载", HubNotice.text(HubNotice.Updating("正在下载")))
        assertEquals("起不来", HubNotice.text(HubNotice.RolledBack("起不来", 123)))
        assertEquals("hub 会在空闲时自动更新", HubNotice.text(HubNotice.AutoOn))
        assertEquals("hub 有新版本 1.5.0", HubNotice.text(HubNotice.Available("1.5.0", 150)))
        assertEquals("hub 有新镜像 1.5.0", HubNotice.text(HubNotice.NewImage("1.5.0")))
    }

    @Test fun theMostPressingComesFirstAndAClosedOneStaysClosed() {
        val status = HubUpdateStatus(channel = "release", canUpdate = true, latest = newer, autoNotice = true)
        assertEquals(HubNotice.AutoOn, HubNotice.of(status))
        val available = HubNotice.of(status.copy(autoNotice = false))
        assertEquals(HubNotice.Available("1.1.0", 110), available)
        assertNull(HubNotice.of(status.copy(autoNotice = false), dismissed = setOf(available!!.dismissKey!!)))
        assertEquals(HubNotice.Updating("正在下载"), HubNotice.of(status.copy(state = "downloading")))
    }

    @Test fun anAppTooOldPreemptsTheRest() {
        val status = HubUpdateStatus(channel = "release", canUpdate = true, latest = newer, autoNotice = true)
        assertEquals(HubNotice.AppTooOld, HubNotice.of(status, HubCompat.AppTooOld))
        assertEquals(HubCompat.AppTooOld, HubCompat.of(protocolVersion = 1, minAppBuild = 200, appBuild = 150))
        assertEquals(HubCompat.Ok, HubCompat.of(protocolVersion = 1, minAppBuild = 200, appBuild = 150, connected = false))
    }
}
