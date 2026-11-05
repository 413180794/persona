package persona.core.contracts

import persona.core.contracts.api.*
import org.junit.Assert.*
import org.junit.Test

class HubNoticeTest {
    @Test
    fun compatResolvesCorrectly() {
        // Not connected: Ok (nothing to say)
        assertEquals(HubCompat.Ok, HubCompat.of(protocolVersion = null, minAppBuild = null, appBuild = 10, connected = false))

        // Connected and meets constraints: Ok
        assertEquals(HubCompat.Ok, HubCompat.of(protocolVersion = 1, minAppBuild = 0, appBuild = 10, connected = true))
        assertEquals(HubCompat.Ok, HubCompat.of(protocolVersion = 2, minAppBuild = 10, appBuild = 10, connected = true))

        // App too old takes precedence
        assertEquals(HubCompat.AppTooOld, HubCompat.of(protocolVersion = 0, minAppBuild = 20, appBuild = 10))
        assertEquals(HubCompat.AppTooOld, HubCompat.of(protocolVersion = 1, minAppBuild = 100, appBuild = 99))

        // Hub too old when protocol is less than MinHubProtocol (1) or absent on a legacy connected hub
        assertEquals(HubCompat.HubTooOld, HubCompat.of(protocolVersion = 0, minAppBuild = 0, appBuild = 10))
        assertEquals(HubCompat.HubTooOld, HubCompat.of(protocolVersion = null, minAppBuild = 0, appBuild = 10, connected = true))
    }

    // A hub's hello decides; before any hello (it went by before the page was there) nothing is said (#2157).
    @Test
    fun compatFromTheHello() {
        assertEquals(HubCompat.Ok, HubCompat.of(hello = null, appBuild = 10, connected = true))
        assertEquals(HubCompat.Ok, HubCompat.of(Hello(protocol = 1, minAppBuild = 0), appBuild = 10))
        assertEquals(HubCompat.HubTooOld, HubCompat.of(Hello(protocol = null), appBuild = 10))
        assertEquals(HubCompat.AppTooOld, HubCompat.of(Hello(protocol = 1, minAppBuild = 20), appBuild = 10))
    }

    @Test
    fun priorityAndCandidateOrder() {
        val base = HubUpdateStatus(version = "1.0.0", build = 100, canUpdate = true)
        val newer = HubRelease(version = "1.1.0", build = 110, newer = true)

        // 1. App too old: only AppTooOld is returned, cannot be dismissed
        val appOldCandidates = HubNotice.candidates(base.copy(state = "downloading", latest = newer), HubCompat.AppTooOld)
        assertEquals(listOf(HubNotice.AppTooOld), appOldCandidates)
        assertNull(HubNotice.AppTooOld.dismissKey)

        // 2. Hub too old: first candidate is HubTooOld
        val hubOldCandidates = HubNotice.candidates(base.copy(state = "downloading"), HubCompat.HubTooOld)
        assertEquals(HubNotice.HubTooOld, hubOldCandidates.first())
        assertEquals("hubTooOld", (HubNotice.HubTooOld.dismissKey).zhAny())

        // 3. Updating: stateTitle mapping
        val updating = base.copy(state = "waiting_idle", busy = listOf(HubUpdateBusy(), HubUpdateBusy()))
        val noticeUpdating = HubNotice.of(updating)
        assertEquals("等 2 个在忙的结束", ((noticeUpdating as HubNotice.Updating).what.zh()).zhAny())
        assertNull(noticeUpdating?.dismissKey)

        // 4. Rolled back: needs error and latest.build
        val rolledBack = base.copy(state = "rolled_back", error = "起不来", latest = newer)
        val noticeRolledBack = HubNotice.of(rolledBack)
        assertEquals(HubNotice.RolledBack("起不来", 110), noticeRolledBack)
        assertEquals("rolledBack:110", (noticeRolledBack?.dismissKey).zhAny())

        // 5. AutoOn notice
        val autoNotice = base.copy(autoNotice = true)
        val noticeAuto = HubNotice.of(autoNotice)
        assertEquals(HubNotice.AutoOn, noticeAuto)
        assertNull(noticeAuto?.dismissKey)

        // 6. Available vs Docker NewImage
        val releaseAvailable = base.copy(auto = false, latest = newer)
        val noticeAvailable = HubNotice.of(releaseAvailable)
        assertEquals(HubNotice.Available("1.1.0", 110), noticeAvailable)
        assertEquals("build:110", (noticeAvailable?.dismissKey).zhAny())

        val dockerAvailable = base.copy(channel = "docker", latest = newer)
        val noticeDocker = HubNotice.of(dockerAvailable)
        assertEquals(HubNotice.NewImage("1.1.0"), noticeDocker)
        assertEquals("image:1.1.0", (noticeDocker?.dismissKey).zhAny())
    }

    @Test
    fun dismissalSkipsDismissedAndShowsNextCandidate() {
        val base = HubUpdateStatus(
            version = "1.0.0",
            build = 100,
            canUpdate = true,
            autoNotice = true,
            latest = HubRelease(version = "1.1.0", build = 110, newer = true),
        )

        // Hub is too old, but user dismissed "hubTooOld"
        val notice = HubNotice.of(base, HubCompat.HubTooOld, dismissed = setOf("hubTooOld"))
        // Next candidate in priority is AutoOn!
        assertEquals(HubNotice.AutoOn, notice)

        // Available dismissed by build
        val availableStatus = base.copy(autoNotice = false, auto = false)
        val dismissedNotice = HubNotice.of(availableStatus, HubCompat.Ok, dismissed = setOf("build:110"))
        assertNull(dismissedNotice)
    }
}
