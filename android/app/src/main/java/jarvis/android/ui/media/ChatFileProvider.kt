package jarvis.android.ui.media

import androidx.core.content.FileProvider

/**
 * The chat's pictures shared / copied from the viewer (`cache/shared`, [MediaShare]). Its own subclass so another
 * FileProvider declared in the manifest later doesn't collide with it.
 */
class ChatFileProvider : FileProvider()
