package jarvis.feature.inbox

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import jarvis.core.designsystem.*
import kotlinx.coroutines.launch

@Composable
internal fun LarkThreadSheet(root: LarkMessage, repository: InboxRepository, onDismiss: () -> Unit) {
    val loader = remember(root.id, root.thread) { root.thread?.let { LarkThreadLoader(root.id, it) } }
    val scope = rememberCoroutineScope()
    val replies = remember(root.id, root.thread, repository) {
        root.thread?.takeIf { it.canReply }?.let { LarkThreadReplyRepository(repository, root.id, it) }
    }
    val composers = rememberReplyComposers(repository)
    val composer = replies?.let { composers.get(it.target, replyRepository = it) }
    val scroll = rememberLazyListState()
    var mediaError by remember(root.id) { mutableStateOf<String?>(null) }
    JSheet(onDismiss = onDismiss, title = "话题") {
        Column(Modifier.fillMaxWidth().fillMaxHeight(.92f)) {

            if (loader == null) {
                JEmptyState("暂时无法查看话题", "当前连接尚未接入话题消息。")
            } else {
                val state by loader.state.collectAsStateWithLifecycle()
                LaunchedEffect(loader) { loader.load() }
                LaunchedEffect(state.revision) { if (state.revision > 0) scroll.scrollToItem(0) }
                if (state.loading) JChip("正在加载话题", status = Status.Working)
                state.error?.let { error ->
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.weight(1f)) { StatusText(error, isError = true) }
                        JButton("重试", enabled = !state.loading, onClick = {
                            scope.launch { loader.load(state.failedOlder) }
                        })
                    }
                }
                mediaError?.let { StatusText(it, isError = true) }
                LazyColumn(Modifier.weight(1f).fillMaxWidth(), state = scroll, reverseLayout = true,
                    contentPadding = PaddingValues(Tokens.Space.l), verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                    if (state.loaded && state.messages.isEmpty()) item("empty") {
                        JEmptyState("暂无回复", "这条消息还没有话题回复。")
                    }
                    items(state.messages.asReversed(), key = { "reply:${it.id}" }) { message ->
                        LarkBubble(message, true, repository, retrying = false, onRetry = {}, onError = { mediaError = it })
                    }
                    if (state.hasOlder) item("older") {
                        JButton(if (state.loading) "正在加载…" else "查看更早的回复",
                            modifier = Modifier.fillMaxWidth(), enabled = !state.loading,
                            onClick = { scope.launch { loader.load(older = true) } })
                    }
                    item("root") {
                        Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                            LarkBubble(root, true, repository, retrying = false, onRetry = {}, onError = { mediaError = it })
                            JDivider()
                            Text("${root.replyCount} 条回复", style = JarvisTheme.type.callout,
                                color = JarvisTheme.palette.ink3)
                        }
                    }
                }
                Box(Modifier.align(Alignment.End)) {
                    if (!state.loading) JumpToLatest(0, onClick = { scope.launch { loader.load() } })
                }
            }
            Column(Modifier.fillMaxWidth().imePadding(), verticalArrangement = Arrangement.spacedBy(Tokens.Space.s)) {
                if (composer == null) {
                    StatusText("当前连接暂不支持话题回复")
                } else {
                    val reply by composer.state.collectAsStateWithLifecycle()
                    LaunchedEffect(reply.sent) { if (reply.sent) loader?.load() }
                    if (reply.sent) StatusText("已发送")
                    ReplyComposerField(reply, composer::update, composer::send, "回复话题")
                }
            }
        }
    }
}
