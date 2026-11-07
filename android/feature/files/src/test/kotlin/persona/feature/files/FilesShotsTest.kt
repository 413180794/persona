package persona.feature.files

import android.graphics.Bitmap
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.asAndroidBitmap
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.test.captureToImage
import androidx.compose.ui.test.junit4.createComposeRule
import androidx.compose.ui.test.onNodeWithTag
import androidx.compose.ui.test.onNodeWithText
import androidx.compose.ui.test.performClick
import androidx.compose.ui.unit.dp
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Rule
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.annotation.Config
import org.robolectric.annotation.GraphicsMode
import persona.core.contracts.api.*
import persona.core.designsystem.AppStyle
import persona.core.designsystem.PersonaTheme
import persona.core.navigation.ChatRoute
import java.io.File
import java.time.Instant
import java.time.ZoneOffset

/** A repository on a machine for the pictures: a few files and folders, two changes, three commits. */
private class ShotMachine : SessionFiles {
    private val now = Instant.now()
    private fun ago(seconds: Long) = now.minusSeconds(seconds).atOffset(ZoneOffset.ofHours(8)).toString()
    private val root = "/Users/me/code/weather"

    override suspend fun directory(machineId: String, cwd: String, path: String) = SessionDirectory(root, listOf(
        SessionFileEntry("Sources", dir = true, mtime = ago(300)),
        SessionFileEntry("docs", dir = true, mtime = ago(3 * 86_400)),
        SessionFileEntry("README.md", size = 1536, mtime = ago(30)),
        SessionFileEntry("Package.swift", size = 1023, mtime = ago(2 * 3600)),
        SessionFileEntry("logo.bin", size = 2L shl 20, mtime = ago(40 * 86_400)),
        SessionFileEntry("latest", symlink = true, size = 12, mtime = ago(400 * 86_400)),
    ))

    override suspend fun file(machineId: String, cwd: String, path: String) =
        SessionFileContent(path, FilesPaths.name(path), size = 2L shl 20, mime = "application/octet-stream")

    override suspend fun status(machineId: String, cwd: String) = SessionGitStatus(true, root = root, branch = "main",
        head = SessionGitCommit("a1b2c3d4e5f6", "a1b2c3d", "Show the forecast for the next week", author = "Dev", date = ago(1200), refs = "HEAD -> main, origin/main"),
        upstream = "origin/main", ahead = 2, stashes = 1,
        files = listOf(SessionGitFile("Sources/App/Forecast.swift", index = "M", worktree = "M", added = 12, deleted = 3),
            SessionGitFile("docs/notes.md", index = "?", worktree = "?", added = 8)),
        commits = listOf(
            SessionGitCommit("a1b2c3d4e5f6", "a1b2c3d", "Show the forecast for the next week", author = "Dev", date = ago(1200), refs = "HEAD -> main, origin/main"),
            SessionGitCommit("b2c3d4e5f6a1", "b2c3d4e", "Cache the last response", author = "Dev", date = ago(2 * 86_400), refs = "tag: v1.2"),
            SessionGitCommit("c3d4e5f6a1b2", "c3d4e5f", "First version", author = "Dev", date = ago(60 * 86_400)),
        ))

    override suspend fun diff(machineId: String, cwd: String, path: String, staged: Boolean) = SessionGitDiff(path, "")
    override suspend fun log(machineId: String, cwd: String, skip: Int, limit: Int) = SessionGitLog()
    override suspend fun show(machineId: String, cwd: String, hash: String) = SessionGitShow(
        SessionGitCommit(hash, hash.take(7), "Cache the last response", body = "Keeps the forecast when offline.", author = "Dev", date = ago(2 * 86_400)),
        files = listOf(SessionGitFile("Sources/App/Cache.swift", index = "A", added = 40), SessionGitFile("Package.swift", index = "M", added = 1, deleted = 1)),
        diff = "diff --git a/Package.swift b/Package.swift\n@@ -1 +1 @@\n-a\n+b",
    )
}

/**
 * The files pane (browser, Git, a commit, a file it can't preview) in each of the app's languages, light and dark
 * (docs/protocol/i18n.md). Written as PNGs to `build/i18n-shots/` (or `-Dpersona.shots=<dir>`) to look at.
 */
@RunWith(AndroidJUnit4::class)
@GraphicsMode(GraphicsMode.Mode.NATIVE)
class FilesShotsTest {
    @get:Rule val rule = createComposeRule()

    private fun out(): File = File(System.getProperty("persona.shots") ?: "build/i18n-shots").also { it.mkdirs() }

    @Config(sdk = [35], qualifiers = "zh-w400dp-h800dp-xhdpi")
    @Test fun simplifiedChinese() = shots("zh", git = "改动")

    @Config(sdk = [35], qualifiers = "en-w400dp-h800dp-xhdpi")
    @Test fun english() = shots("en", git = "Changes")

    @Config(sdk = [35], qualifiers = "ja-w400dp-h800dp-xhdpi")
    @Test fun japanese() = shots("ja", git = "変更")

    private fun shots(language: String, git: String) {
        var style by mutableStateOf(AppStyle.Glass)
        var dark by mutableStateOf(false)
        var page by mutableStateOf("browser")
        val machine = ShotMachine()
        val cwd = "/Users/me/code/weather"
        rule.setContent {
            PersonaTheme(darkTheme = dark, reduceMotion = true, style = style) {
                Box(Modifier.testTag("shot").size(400.dp, 800.dp).background(PersonaTheme.palette.background)) {
                    key(page) {
                        SessionFilesScreen(ChatRoute("mac", "s1"), cwd, machine, onBackToChat = {}, onInsertPath = {},
                            initialPane = if (page == "browser") FilesPane.Files else FilesPane.Git,
                            initialPath = if (page == "preview") "$cwd/logo.bin" else null)
                    }
                }
            }
        }
        fun capture(name: String) {
            rule.waitForIdle()
            val bitmap = rule.onNodeWithTag("shot").captureToImage().asAndroidBitmap()
            File(out(), "files-$name-$language-${style.name.lowercase()}-${if (dark) "dark" else "light"}.png").outputStream()
                .use { bitmap.compress(Bitmap.CompressFormat.PNG, 100, it) }
        }
        for (s in listOf(AppStyle.Glass, AppStyle.entries.last())) for (d in listOf(false, true)) {
            style = s; dark = d
            page = "browser"; capture("browser")
            page = "git"; rule.waitForIdle()
            rule.onNodeWithText(git).assertExists()
            capture("git")
            rule.onNodeWithText("Cache the last response").performClick()
            rule.waitForIdle()
            rule.onNodeWithText("Keeps the forecast when offline.").assertExists()
            capture("commit")
            page = "preview"; capture("preview")
        }
    }
}
