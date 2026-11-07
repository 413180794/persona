package persona.android

import android.content.res.Resources
import androidx.annotation.StringRes
import androidx.annotation.VisibleForTesting

/**
 * Text for code without a Context of its own: the data layer's exceptions, whose message a page or a toast shows. The
 * application installs its resources, already pinned to Settings › Language by AppLanguages.wrap (docs/android/android.md "Internationalization").
 */
internal object AppText {
    @Volatile private var lookup: ((Int, Array<out Any>) -> String)? = null

    fun install(resources: Resources) { lookup = { id, args -> resources.getString(id, *args) } }

    /** JVM tests, which have no resources, read the strings some other way. */
    @VisibleForTesting fun install(lookup: (Int, Array<out Any>) -> String) { this.lookup = lookup }

    /** The string, or its id when nothing is installed (a JVM test that does not look at the words). */
    fun get(@StringRes id: Int, vararg args: Any): String = lookup?.invoke(id, args) ?: "#$id"
}
