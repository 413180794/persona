package persona.feature.chat

import android.content.Context
import android.content.Intent
import android.net.Uri

/**
 * The intent a link for another app may start, or null to drop it (the old Android app's rules): `intent:` URIs lose
 * their component, selector, clip data and every URI-grant flag (a page could otherwise make the app grant another app
 * its files, or start one of its own screens that are not exported); data that is the app's own (`persona:`), another
 * intent, a local file or provider, and this app's own package are refused.
 */
internal fun externalIntent(url: String, ownPackage: String): Intent? {
    val link = url.trim()
    if (ChatLinks.kind(link) != ChatLinkKind.External) return null
    val intent = runCatching {
        if (link.substringBefore(':').equals("intent", ignoreCase = true)) Intent.parseUri(link, Intent.URI_INTENT_SCHEME)
        else Intent(Intent.ACTION_VIEW, Uri.parse(link))
    }.getOrNull() ?: return null
    intent.component = null
    intent.selector = null
    intent.clipData = null
    intent.flags = intent.flags and GRANT_FLAGS.inv()
    if (!ChatLinks.allowedIntent(intent.data?.scheme, intent.`package`, ownPackage)) return null
    intent.addCategory(Intent.CATEGORY_BROWSABLE).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    return intent
}

/** Starts the app for [url] once the user said so; false when it was refused or no app takes it. */
internal fun openExternal(context: Context, url: String): Boolean {
    val intent = externalIntent(url, context.packageName) ?: return false
    return runCatching { context.startActivity(intent) }.isSuccess
}

private const val GRANT_FLAGS = Intent.FLAG_GRANT_READ_URI_PERMISSION or Intent.FLAG_GRANT_WRITE_URI_PERMISSION or
    Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION or Intent.FLAG_GRANT_PREFIX_URI_PERMISSION
