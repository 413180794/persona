package persona.feature.files

import org.junit.Assert.*
import org.junit.Test
import java.time.Instant
import java.time.ZoneId
import java.time.ZoneOffset

class FilesPathsTest {
    @Test fun pathsFromAConversationResolveAgainstTheSessionFolder() {
        val cwd = "/Users/me/code/weather"
        assertEquals("/Users/me/code/weather/Sources/App.swift", FilesPaths.resolve("Sources/App.swift", cwd))
        assertEquals("/Users/me/code/weather/README.md", FilesPaths.resolve("./README.md", cwd))
        assertEquals("/Users/me/code/other/a.txt", FilesPaths.resolve("../other/a.txt", cwd))
        assertEquals("/etc/hosts", FilesPaths.resolve("/etc//hosts", cwd))
        // "~" is the machine's home: only the hub knows it, so it travels as written (folded, never joined to cwd).
        assertEquals("~/.jarvis/personas", FilesPaths.resolve("~/.jarvis/personas/", cwd))
        assertEquals("~", FilesPaths.resolve("~", cwd))
        assertEquals("~", FilesPaths.resolve("~/x/..", cwd))
        assertEquals("/", FilesPaths.resolve("/../..", cwd))
        assertEquals(cwd, FilesPaths.resolve("  ", cwd))
        assertEquals("/Users/me/code/weather/Sources", FilesPaths.resolve("Sources/", cwd))
    }

    @Test fun aTrailingSlashOrTheHomeNamesAFolder() {
        assertTrue(FilesPaths.namesDirectory("Sources/"))
        assertTrue(FilesPaths.namesDirectory("~"))
        assertTrue(FilesPaths.namesDirectory("/"))
        assertFalse(FilesPaths.namesDirectory("Makefile"))
        assertFalse(FilesPaths.namesDirectory("~/notes.txt"))
    }

    @Test fun parentsGoUpToTheTop() {
        assertEquals("/Users/me/code", FilesPaths.parent("/Users/me/code/weather"))
        assertEquals("/Users/me/code", FilesPaths.parent("/Users/me/code/weather/"))
        assertEquals("/", FilesPaths.parent("/Users"))
        assertNull(FilesPaths.parent("/"))
        assertEquals("~", FilesPaths.parent("~/code"))
        assertNull(FilesPaths.parent("~"))
        assertEquals("weather", FilesPaths.name("/Users/me/code/weather/"))
        assertEquals("/", FilesPaths.name("/"))
        assertEquals("~", FilesPaths.name("~"))
    }

    @Test fun crumbsRunFromTheTopToTheFolderShown() {
        assertEquals(listOf("weather" to "/r/weather"), FilesPaths.crumbs("/r/weather", "/r/weather"))
        assertEquals(listOf("weather" to "/r/weather", "Sources" to "/r/weather/Sources", "App" to "/r/weather/Sources/App"),
            FilesPaths.crumbs("/r/weather", "/r/weather/Sources/App"))
        assertEquals(listOf("/" to "/", "Users" to "/Users", "me" to "/Users/me"), FilesPaths.crumbs("/", "/Users/me"))
        assertEquals(listOf("~" to "~", "code" to "~/code"), FilesPaths.crumbs("~", "~/code"))
        // A folder outside the top (a sibling with the same prefix) is its own single crumb.
        assertEquals(listOf("weather-site" to "/r/weather-site"), FilesPaths.crumbs("/r/weather", "/r/weather-site"))
        assertTrue(FilesPaths.isWithin("/r/weather/a", "/r/weather"))
        assertFalse(FilesPaths.isWithin("/r/weather-site", "/r/weather"))
        assertTrue(FilesPaths.isWithin("/anything", "/"))
    }

    @Test fun joiningNeverDoublesOrLosesTheSlash() {
        assertEquals("/a", joinRemotePath("/", "a"))
        assertEquals("/r/a", joinRemotePath("/r", "a"))
        assertEquals("/r/a", joinRemotePath("/r/", "a"))
        assertEquals("~/a", joinRemotePath("~", "a"))
    }

    @Test fun nodeTimesWithTheMachinesOffsetAreRead() {
        // Shapes as a node sends them: RFC 3339 with the machine's own offset, nanoseconds on file times.
        assertEquals(Instant.parse("2026-10-31T05:58:40.205893083Z"), fileTime("2026-10-31T13:58:40.205893083+08:00"))
        assertEquals(Instant.parse("2026-10-31T05:58:40Z"), fileTime("2026-10-31T13:58:40+08:00"))
        assertEquals(Instant.parse("2026-10-31T05:58:40Z"), fileTime("2026-10-31T05:58:40Z"))
        assertEquals(Instant.parse("2026-10-31T19:58:40Z"), fileTime("2026-10-31T13:58:40-06:00"))
        // Go's zero time is "none", not year 1.
        assertNull(fileTime("0001-01-01T00:00:00Z"))
        assertNull(fileTime("0001-01-01T08:05:43+08:05"))
        assertNull(fileTime(""))
        assertNull(fileTime(null))
        assertNull(fileTime("yesterday"))
    }

    @Test fun listTimesAreRelativeThenDates() {
        val now = Instant.parse("2026-11-02T12:00:00Z")
        val zone = ZoneOffset.UTC
        assertEquals("刚刚", relativeTime(now.minusSeconds(30), now, zone))
        assertEquals("5 分钟前", relativeTime(now.minusSeconds(300), now, zone))
        assertEquals("3 小时前", relativeTime(now.minusSeconds(3 * 3600), now, zone))
        assertEquals("2 天前", relativeTime(now.minusSeconds(2 * 86_400), now, zone))
        assertEquals("10月1日", relativeTime(Instant.parse("2026-10-01T09:00:00Z"), now, zone))
        assertEquals("2025年12月30日", relativeTime(Instant.parse("2025-12-30T09:00:00Z"), now, zone))
        assertEquals("3 小时前", fileDate("2026-11-02T17:00:00+08:00", now, zone))
        assertNull(fileDate("0001-01-01T00:00:00Z", now, zone))
    }

    @Test fun aCommitsWholeDateIsInThePhonesZone() {
        assertEquals("2026年10月31日 13:58", fullDate("2026-10-31T05:58:40Z", ZoneId.of("Asia/Shanghai")))
        assertEquals("2026年10月31日 13:58", fullDate("2026-10-31T13:58:40+08:00", ZoneId.of("Asia/Shanghai")))
        assertNull(fullDate("0001-01-01T00:00:00Z", ZoneOffset.UTC))
    }

    @Test fun sizesUseBinaryUnits() {
        assertEquals("0 B", fileSize(0))
        assertEquals("1023 B", fileSize(1023))
        assertEquals("1 KB", fileSize(1024))
        assertEquals("1.5 KB", fileSize(1536))
        assertEquals("165 KB", fileSize(165 * 1024 + 100))
        assertEquals("2 MB", fileSize(2L shl 20))
    }
}
