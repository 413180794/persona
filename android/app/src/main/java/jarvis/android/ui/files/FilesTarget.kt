package jarvis.android.ui.files

/**
 * What [FilesScreen] opens: [path] on [machineId] (absolute, "~", or relative to [cwd], a session's folder), as the
 * browser, one file, or the Git page. [Mode.AUTO] guesses from the path.
 */
data class FilesTarget(val machineId: String, val path: String, val cwd: String? = null, val mode: Mode = Mode.AUTO) {
    enum class Mode { AUTO, DIR, FILE, GIT }

    /** What to show when [mode] says nothing definite. */
    val resolvedMode: Mode
        get() {
            if (mode != Mode.AUTO) return mode
            // A file read of a directory still lists it (FileViewer), so anything with a dot is tried as a file.
            return if (FilesPaths.looksLikeDir(path)) Mode.DIR else Mode.FILE
        }
}
