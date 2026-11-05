package persona.feature.inbox

import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyListScope
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.rememberScrollState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.SpanStyle
import androidx.compose.ui.text.buildAnnotatedString
import androidx.compose.ui.text.withStyle
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.launch
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonObject
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcon
import persona.core.designsystem.icons.PhIcons
import persona.core.navigation.InboxItemUiRoute
import persona.core.navigation.Navigator

// A Feishu document's notice in the inbox (docs/inbox.md「云文档」, #2323): a sheet / Base / docx that @'d the user or
// commented to them. It has no chat behind it, so it opens its own page: what happened, the rows about the user, and
// 「打开文档」 in the app's Feishu document reader. The same page is where an item of a shape this app does not know opens,
// instead of the mail page (which can only fail for it).

/** An item's `doc` block; [reply] is where a reply goes (`reply.label`: 「评论这一行」…), [error] why the hub could not read it. */
@Serializable
data class InboxDocRef(
    val url: String,
    val type: String = "",
    val notice: String = "",
    val rows: List<InboxDocRow> = emptyList(),
    val reply: String? = null,
    val error: String? = null,
)

/** A row about the user: its title, its other fields, and its status columns (as name / value). */
@Serializable
data class InboxDocRow(
    val title: String,
    val table: String? = null,
    val fields: List<InboxDocField> = emptyList(),
    val status: List<InboxDocField> = emptyList(),
)

@Serializable
data class InboxDocField(val name: String, val value: String)

/** The hub's `doc` block; null without one or without a link to open. Unknown fields are ignored. */
fun inboxDocRef(doc: JsonObject?): InboxDocRef? {
    doc ?: return null
    val url = doc.text("url").ifBlank { doc.text("link") }.ifBlank { return null }
    fun JsonObject.list(key: String) = (get(key) as? kotlinx.serialization.json.JsonArray).orEmpty().mapNotNull { it as? JsonObject }
    fun JsonObject.pairs(key: String) = list(key).map { InboxDocField(it.text("name"), it.text("value")) }.filter { it.name.isNotBlank() }
    return InboxDocRef(
        url = url,
        type = doc.text("type"),
        notice = doc.text("notice"),
        rows = doc.list("rows").map { InboxDocRow(it.text("title"), it.cursor("table"), it.pairs("fields"), it.pairs("status")) },
        reply = (doc["reply"] as? JsonObject)?.cursor("label"),
        error = doc.cursor("error"),
    )
}

/** What kind of document, as Feishu names it. */
internal fun docTypeName(type: String?): String = when (type) {
    "sheet" -> "表格"
    "base" -> "多维表格"
    "docx", "doc" -> "文档"
    else -> "云文档"
}

/** The list row's square in place of a chat avatar. */
internal fun docIcon(type: String?): PhIcon = when (type) {
    "sheet" -> PhIcons.SquaresFour
    "base" -> PhIcons.Kanban
    else -> PhIcons.FileText
}

/** @我 or 评论, as the web and iOS tag a document's notice. */
internal fun docKindLabel(doc: InboxDocRef?): String = if (doc?.notice in setOf("comment", "reply")) "评论" else "@我"

/** The item's own page when it is neither a chat, a 公司圈 post nor a mail: a document's notice, or an unknown item. */
@Composable
fun InboxItemDetailScreen(repository: InboxRepository, route: InboxItemDetailRoute, navigator: Navigator) {
    val inbox by repository.snapshot.collectAsStateWithLifecycle()
    val record = inbox.records.firstOrNull { it.item.id == route.itemId }
    val open = LocalLarkLinkOpener.current
    val scope = rememberCoroutineScope()
    var refreshing by remember { mutableStateOf(false) }
    InboxItemExtensionScreen(repository, InboxItemUiRoute(route.itemId, "", "", record?.item?.title.orEmpty()), navigator,
        title = null, loading = false, refreshing = refreshing,
        onRefresh = {
            scope.launch {
                refreshing = true
                try { repository.refreshNow() } catch (e: CancellationException) { throw e } catch (_: Exception) { } finally { refreshing = false }
            }
        }) {
        item("item-extension-notice") { InboxExtensionNotice(route.itemId, route) }
        if (record == null) item("item-missing") { StatusText("没有找到这条事项，可能已经处理或被移除") }
        else inboxItemBody(record) { url -> open?.invoke(url, inbox.contentSource) }
    }
}

internal fun LazyListScope.inboxItemBody(record: InboxRecord, onOpen: (String) -> Unit) {
    val doc = record.doc
    if (doc == null) {
        record.item.preview.takeIf { it.isNotBlank() }?.let { text ->
            item("item-preview") { Text(text, style = PersonaTheme.type.body, color = PersonaTheme.palette.ink) }
        }
        return
    }
    val kind = docTypeName(doc.type)
    item("doc-open") {
        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
            Text(listOf(kind, record.item.preview).filter { it.isNotBlank() }.joinToString(" · "),
                style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink2)
            JButton("打开$kind", { onOpen(doc.url) }, kind = JButtonKind.Secondary, size = JButtonSize.Medium, icon = PhIcons.ArrowSquareOut)
            doc.reply?.let { Text("回复会$it", style = PersonaTheme.type.caption, color = PersonaTheme.palette.ink3) }
        }
    }
    doc.error?.let { error ->
        item("doc-error") { JNotice("读不到文档内容：$error", Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou) }
    }
    if (doc.rows.isEmpty()) return
    item("doc-rows") { JSectionHeader("提到你的行", Modifier.padding(top = Tokens.Space.l)) }
    // A row's table is worth naming when the rows come from more than one, or from a Base's tables.
    val tables = doc.type == "base" || doc.rows.mapNotNull { it.table }.distinct().size > 1
    itemsIndexed(doc.rows, key = { index, _ -> "doc-row-$index" }) { _, row ->
        JCard(Modifier.fillMaxWidth().padding(bottom = Tokens.Space.s), padding = Tokens.Space.m) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                Text(row.title, style = PersonaTheme.type.headline, color = PersonaTheme.palette.ink, modifier = Modifier.weight(1f))
                row.table?.takeIf { tables }?.let { JTag(it) }
            }
            row.fields.forEach { field ->
                Text(buildAnnotatedString {
                    withStyle(SpanStyle(color = PersonaTheme.palette.ink3)) { append(field.name + "  ") }
                    append(field.value)
                }, style = PersonaTheme.type.callout, color = PersonaTheme.palette.ink, maxLines = 3)
            }
            if (row.status.isNotEmpty()) Row(Modifier.horizontalScroll(rememberScrollState()).padding(top = Tokens.Space.xs),
                horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                row.status.forEach { JTag("${it.name}：${it.value.ifBlank { "未设置" }}") }
            }
        }
    }
}
