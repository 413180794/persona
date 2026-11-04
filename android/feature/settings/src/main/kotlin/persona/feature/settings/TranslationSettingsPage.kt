package persona.feature.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.role
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.semantics.stateDescription
import androidx.compose.ui.text.style.TextOverflow
import kotlinx.coroutines.CancellationException
import persona.core.contracts.api.*
import persona.core.designsystem.*
import persona.core.designsystem.icons.PhIcons

/**
 * 设置 › 翻译 (docs/android.md「翻译」, the iOS TranslationSettingsView, the web's TranslationSettings): on / off, the
 * translation model, the target language, and what gets translated: mail (on), and the Feishu chats picked here (none:
 * Feishu has too many messages to translate them all). Kept on this phone and sent to the hub, which translates new mail
 * ahead of time for it.
 */
@Composable
internal fun TranslationSettingsPage(
    translation: TranslationSettings?,
    models: ModelConfiguration?,
    connectors: ConnectorsManagement?,
    onBack: () -> Unit,
    onModel: () -> Unit,
    /** Opens the chat picker at once (captures). */
    pickingAtStart: Boolean = false,
) {
    BackHandler(onBack = onBack)
    if (translation == null) {
        JListPage(title = "翻译", onBack = onBack) { item { JNotice("体验模式不翻译内容。") } }
        return
    }
    val prefs by translation.prefs.collectAsState()
    // Feishu comes from the lark-connector extension: without it on the hub there are no chats to pick (picked ones stay kept).
    val lark = LocalLarkFeatures.current.chats
    var model by remember { mutableStateOf<String?>(null) }
    var choosingTarget by remember { mutableStateOf(false) }
    var picking by remember { mutableStateOf(pickingAtStart) }
    LaunchedEffect(models) {
        model = try { models?.purpose("translate")?.let { d -> d.purpose.summaryParts(d.providers).joinToString("，").ifEmpty { "未配置" } } }
        catch (error: CancellationException) { throw error } catch (_: Exception) { null }
    }
    JListPage(title = "翻译", onBack = onBack) {
        item("enabled") {
            JGroupItem(GroupPosition.of(0, 2), Modifier.padding(top = Tokens.Space.m)) {
                JSwitchRow("自动翻译", prefs.enabled, { on -> translation.update { it.copy(enabled = on) } },
                    Modifier.testTag("translate-enabled"), leadingIcon = PhIcons.Translate)
            }
        }
        item("model") {
            JGroupItem(GroupPosition.of(1, 2)) {
                JRow("翻译模型", leadingIcon = PhIcons.Brain, subtitle = model, subtitleLines = 1, onClick = onModel)
            }
        }
        item("about") {
            SettingsCaption("内容不是目标语言时自动翻译，原文随时可以切回来。翻译由「模型配置」中的翻译模型处理。",
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
        }
        if (prefs.enabled) {
            item("target") {
                JGroupItem(GroupPosition.Single) {
                    JRow("目标语言", leadingIcon = PhIcons.GlobeSimple, value = TranslationPrefs.languageName(prefs.target),
                        onClick = { choosingTarget = true })
                }
            }
            item("scope-header") { JSectionHeader("翻译范围") }
            item("mail") {
                JGroupItem(GroupPosition.of(0, if (lark) 2 else 1)) {
                    JSwitchRow("邮件", prefs.mail, { on -> translation.update { it.copy(mail = on) } },
                        Modifier.testTag("translate-mail"), leadingIcon = PhIcons.EnvelopeSimple)
                }
            }
            if (lark) {
                item("lark") {
                    JGroupItem(GroupPosition.of(1, 2)) {
                        JRow("飞书会话", leadingIcon = PhIcons.ChatCircleDots,
                            value = TranslationText.chats(prefs.chats.size), onClick = { picking = true })
                    }
                }
                item("lark-about") {
                    SettingsCaption("飞书消息很多，默认不翻译；只翻译这里选中的会话里别人发的消息。",
                        Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.s))
                }
            }
            if (lark && prefs.chats.isNotEmpty()) {
                item("chats-header") { JSectionHeader("要翻译的飞书会话", count = prefs.chats.size) }
                itemsIndexed(prefs.chats, key = { _, c -> "chat-${c.id}" }) { index, chat ->
                    JGroupItem(GroupPosition.of(index, prefs.chats.size)) {
                        JRow(chat.name, leading = { JAvatar(chat.name, icon = PhIcons.ChatsCircle) },
                            trailing = {
                                JIconButton(PhIcons.X, "不再翻译「${chat.name}」",
                                    { translation.update { p -> p.copy(chats = p.chats.filterNot { it.id == chat.id }) } })
                            })
                    }
                }
            }
        }
        item("note") {
            SettingsCaption("这些设置存在这台手机上，并会发给 hub：hub 按它提前翻好新邮件，打开时直接显示译文。",
                Modifier.padding(horizontal = pagePadding, vertical = Tokens.Space.l))
        }
    }
    if (choosingTarget) JActionSheet(
        TranslationPrefs.languages.map { language ->
            JAction(language.name, checked = language.code == prefs.target) {
                translation.update { it.copy(target = language.code) }
                choosingTarget = false
            }
        },
        onDismiss = { choosingTarget = false }, title = "目标语言",
    )
    if (picking) TranslationChatPicker(connectors, prefs.chats, onDismiss = { picking = false }) { chats ->
        translation.update { it.copy(chats = chats) }
        picking = false
    }
}

/**
 * Picks the Feishu chats to translate: the picked ones first (so one no longer recent can still be unpicked), then the
 * pinned and recent ones, searchable by name.
 */
@Composable
private fun TranslationChatPicker(
    connectors: ConnectorsManagement?,
    selected: List<TranslateChat>,
    onDismiss: () -> Unit,
    onSave: (List<TranslateChat>) -> Unit,
) {
    var picked by remember { mutableStateOf(selected) }
    var choices by remember { mutableStateOf<List<LarkChatChoice>?>(null) }
    var error by remember { mutableStateOf<String?>(null) }
    var query by remember { mutableStateOf("") }
    LaunchedEffect(connectors) {
        choices = try { connectors?.larkChatChoices().orEmpty() }
        catch (e: CancellationException) { throw e }
        catch (e: Exception) { error = e.message?.takeIf(String::isNotBlank) ?: "读不到飞书会话"; emptyList() }
    }
    // Scrolls as a whole: the pinned and recent chats are at most a few dozen.
    JSheet(onDismiss, title = "要翻译的会话", scrollable = true) {
        TranslationChatPickerContent(selected, choices, error, query, { query = it }, picked) { chat ->
            picked = if (picked.any { it.id == chat.id }) picked.filterNot { it.id == chat.id } else picked + chat
        }
        Row(Modifier.fillMaxWidth().padding(top = Tokens.Space.m), horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s, Alignment.End)) {
            JButton("取消", onDismiss, kind = JButtonKind.Secondary)
            JButton("完成", { onSave(picked) }, Modifier.testTag("translate-chats-done"))
        }
    }
}

/** The picker's search field and rows (inside its sheet; drawn on its own for captures). */
@Composable
internal fun TranslationChatPickerContent(
    selected: List<TranslateChat>,
    choices: List<LarkChatChoice>?,
    error: String?,
    query: String,
    onQuery: (String) -> Unit,
    picked: List<TranslateChat>,
    onToggle: (TranslateChat) -> Unit,
) {
    val rows = remember(selected, choices, query) { TranslationText.pickerRows(selected, choices.orEmpty(), query) }
    Column(Modifier.fillMaxWidth()) {
        JSearchField(query, onQuery, "搜索会话")
        error?.let { JNotice(it, Modifier.padding(top = Tokens.Space.s), status = Status.NeedsYou) }
        Column(Modifier.fillMaxWidth().padding(top = Tokens.Space.s)) {
            when {
                choices == null && selected.isEmpty() -> JLoading(text = "正在读飞书会话")
                rows.isEmpty() -> JEmptyState(if (query.isBlank()) "没有飞书会话" else "没有匹配的会话", "置顶和最近的飞书会话会列在这里。",
                    icon = PhIcons.ChatCircleDots)
                else -> rows.forEachIndexed { index, chat ->
                    key(chat.id) {
                        val on = picked.any { it.id == chat.id }
                        val choice = choices?.firstOrNull { it.id == chat.id }
                        JGroupItem(GroupPosition.of(index, rows.size)) {
                            JRow(
                                Modifier.semantics {
                                    role = Role.Checkbox
                                    stateDescription = if (on) "已选" else "未选"
                                },
                                trailing = { JCheckbox(on, null) },
                                onClick = { onToggle(chat) },
                            ) {
                                JAvatar(chat.name,
                                    icon = if (choice?.isGroup == false) null else PhIcons.ChatsCircle)
                                Spacer(Modifier.width(Tokens.Space.m))
                                Text(chat.name, Modifier.weight(1f), style = PersonaTheme.type.body, color = PersonaTheme.palette.ink,
                                    maxLines = 1, overflow = TextOverflow.Ellipsis)
                            }
                        }
                    }
                }
            }
            // The picked ones show at once; the hub's list is still on its way.
            if (choices == null && selected.isNotEmpty()) JLoading(text = "正在读飞书会话")
        }
    }
}

/** Captures (`debug.translateChats 1`): the page opens with its chat picker up, once. */
object TranslationPreview {
    @Volatile var picking: Boolean = false
    fun take(): Boolean = picking.also { picking = false }
}

internal object TranslationText {
    fun chats(count: Int): String = if (count == 0) "不翻译" else "$count 个会话"

    /** The picked chats first (under the names kept for them), then the others offered, filtered by name. */
    fun pickerRows(selected: List<TranslateChat>, choices: List<LarkChatChoice>, query: String): List<TranslateChat> {
        val all = (selected + choices.map { TranslateChat(it.id, it.name) }).distinctBy { it.id }
        val q = query.trim().lowercase()
        return if (q.isEmpty()) all else all.filter { it.name.lowercase().contains(q) }
    }
}
