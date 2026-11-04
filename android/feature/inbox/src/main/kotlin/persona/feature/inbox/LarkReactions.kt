package persona.feature.inbox

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.grid.GridCells
import androidx.compose.foundation.lazy.grid.GridItemSpan
import androidx.compose.foundation.lazy.grid.LazyVerticalGrid
import androidx.compose.foundation.lazy.grid.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import persona.core.contracts.LarkReaction
import persona.core.designsystem.*
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons

val DEFAULT_QUICK_REACTIONS = listOf(
    "THUMBSUP", "OK", "DONE", "HEART", "SMILE", "LAUGH", "SURPRISED"
)

val DEFAULT_EMOJI_GROUPS = listOf(
    "默认表情" to listOf(
        "THUMBSUP", "OK", "DONE", "HEART", "SMILE", "LAUGH", "SURPRISED",
        "CRY", "CLAP", "FIRE", "PARTY", "THINKING", "MUSCLE", "SOB",
        "WAVE", "JIAYI", "FIST", "SHAKE", "ROSE", "BEER", "CAKE", "COFFEE"
    )
)

/** Reaction chip underneath a message. */
@Composable
fun LarkReactionChip(
    reaction: LarkReaction,
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val palette = PersonaTheme.palette
    val isMine = reaction.mine == true
    val symbol = LarkEmojiHelper.symbol(reaction.key)
    val label = LarkEmojiHelper.label(reaction.key)
    val shape = RoundedCornerShape(Tokens.Radius.badge)

    val bg = if (isMine) palette.accent.copy(alpha = 0.15f) else palette.ink.copy(alpha = 0.05f)
    val borderModifier = if (isMine) Modifier.border(1.dp, palette.accent.copy(alpha = 0.4f), shape) else Modifier

    Row(
        modifier
            .clip(shape)
            .background(bg)
            .then(borderModifier)
            .clickable(onClick = onClick)
            .padding(horizontal = Tokens.Space.s, vertical = Tokens.Space.xs)
            .semantics(mergeDescendants = true) {
                contentDescription = "$label ${reaction.count}"
            },
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
    ) {
        Text(symbol, fontSize = 14.sp)
        val text = when {
            reaction.users != null && reaction.users!!.isNotEmpty() && reaction.users!!.size <= 3 ->
                reaction.users!!.joinToString("、")
            reaction.count > 0 -> "${reaction.count}"
            else -> ""
        }
        if (text.isNotEmpty()) {
            Text(
                text,
                style = PersonaTheme.type.micro,
                color = if (isMine) palette.accent else palette.ink2,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
        }
    }
}

/** Row of reaction chips below a message. */
@OptIn(ExperimentalLayoutApi::class)
@Composable
fun LarkReactionRow(
    reactions: List<LarkReaction>,
    onToggleReaction: (String, Boolean) -> Unit,
    onOpenPicker: () -> Unit,
    modifier: Modifier = Modifier,
) {
    if (reactions.isEmpty()) return
    val palette = PersonaTheme.palette

    FlowRow(
        modifier = modifier.fillMaxWidth(),
        horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
        verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)
    ) {
        reactions.forEach { reaction ->
            LarkReactionChip(
                reaction = reaction,
                onClick = { onToggleReaction(reaction.key, reaction.mine == true) }
            )
        }
        // Button to open reaction picker
        Box(
            Modifier
                .size(24.dp)
                .clip(CircleShape)
                .background(palette.ink.copy(alpha = 0.05f))
                .clickable(onClick = onOpenPicker)
                .semantics { contentDescription = "表情回复" },
            contentAlignment = Alignment.Center
        ) {
            JIcon(PhIcons.Plus, modifier = Modifier.size(12.dp), tint = palette.ink3)
        }
    }
}

/** Long press action menu with quick reaction bar and actions. */
@Composable
fun LarkMessageActionMenu(
    message: LarkMessage,
    expanded: Boolean,
    onDismiss: () -> Unit,
    onReact: (String) -> Unit,
    onOpenPicker: () -> Unit,
    onOpenProfile: (() -> Unit)? = null,
    onCopy: () -> Unit,
) {
    val palette = PersonaTheme.palette
    val clipboard = LocalClipboardManager.current

    DropdownMenu(
        expanded = expanded,
        onDismissRequest = onDismiss,
        modifier = Modifier.background(palette.surfaceStrong).border(1.dp, palette.ink.copy(alpha = 0.08f), RoundedCornerShape(Tokens.Radius.card))
    ) {
        // Quick reaction bar
        Row(
            Modifier.padding(horizontal = Tokens.Space.s, vertical = Tokens.Space.xs),
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
            verticalAlignment = Alignment.CenterVertically
        ) {
            for (key in DEFAULT_QUICK_REACTIONS) {
                Box(
                    Modifier
                        .size(32.dp)
                        .clip(CircleShape)
                        .clickable {
                            onDismiss()
                            onReact(key)
                        }
                        .semantics { contentDescription = LarkEmojiHelper.label(key) },
                    contentAlignment = Alignment.Center
                ) {
                    Text(LarkEmojiHelper.symbol(key), fontSize = 18.sp)
                }
            }
            Box(
                Modifier
                    .size(32.dp)
                    .clip(CircleShape)
                    .clickable {
                        onDismiss()
                        onOpenPicker()
                    }
                    .semantics { contentDescription = "更多表情" },
                contentAlignment = Alignment.Center
            ) {
                JIcon(PhIcons.Plus, modifier = Modifier.size(16.dp), tint = palette.accent)
            }
        }

        HorizontalDivider(color = palette.ink.copy(alpha = 0.08f))

        // Copy
        DropdownMenuItem(
            text = { Text("拷贝", style = PersonaTheme.type.callout, color = palette.ink) },
            leadingIcon = { JIcon(PhIcons.Copy, modifier = Modifier.size(16.dp), tint = palette.ink) },
            onClick = {
                onDismiss()
                onCopy()
            }
        )

        // View Sender Profile
        if (onOpenProfile != null && message.senderId.isNotBlank()) {
            DropdownMenuItem(
                text = { Text("查看名片", style = PersonaTheme.type.callout, color = palette.ink) },
                leadingIcon = { JIcon(PhIcons.User, modifier = Modifier.size(16.dp), tint = palette.ink) },
                onClick = {
                    onDismiss()
                    onOpenProfile()
                }
            )
        }
    }
}

/** Full reaction picker bottom sheet. */
@Composable
fun LarkReactionPicker(
    message: LarkMessage,
    onPick: (String) -> Unit,
    onDismiss: () -> Unit,
    customEmojis: JsonObject? = null,
) {
    val palette = PersonaTheme.palette
    val mine = remember(message) {
        message.reactions.filter { it.mine == true }.map { it.key.lowercase() }.toSet()
    }

    // Parse custom groups if available, or fallback to default
    val groups = remember(customEmojis) {
        val parsed = mutableListOf<Pair<String, List<String>>>()
        if (customEmojis != null) {
            val rawGroups = customEmojis["groups"] as? JsonArray
            rawGroups?.mapNotNull { it as? JsonObject }?.forEach { g ->
                val title = (g["title"] as? JsonPrimitive)?.content ?: "表情"
                val keys = (g["keys"] as? JsonArray)?.mapNotNull { (it as? JsonPrimitive)?.content }.orEmpty()
                if (keys.isNotEmpty()) parsed.add(title to keys)
            }
        }
        if (parsed.isEmpty()) DEFAULT_EMOJI_GROUPS else parsed
    }

    JSheet(
        onDismiss = onDismiss,
        title = "表情回应",
        skipPartiallyExpanded = false
    ) {
        LazyVerticalGrid(
            columns = GridCells.Fixed(7),
            modifier = Modifier.fillMaxWidth().heightIn(max = 380.dp),
            contentPadding = PaddingValues(Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.s),
            horizontalArrangement = Arrangement.spacedBy(Tokens.Space.xs),
        ) {
            // Quick reactions row
            items(DEFAULT_QUICK_REACTIONS, key = { "q:$it" }) { key ->
                val selected = key.lowercase() in mine
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(44.dp)
                        .clip(RoundedCornerShape(Tokens.Radius.control))
                        .background(if (selected) palette.accent.copy(alpha = 0.15f) else Color.Transparent)
                        .clickable { onPick(key); onDismiss() }
                        .semantics { contentDescription = LarkEmojiHelper.label(key) },
                    contentAlignment = Alignment.Center
                ) {
                    Text(LarkEmojiHelper.symbol(key), fontSize = 22.sp)
                }
            }

            for ((title, keys) in groups) {
                item(span = { GridItemSpan(maxLineSpan) }, key = "header:$title") {
                    Text(
                        title,
                        style = PersonaTheme.type.callout,
                        color = palette.ink3,
                        modifier = Modifier.padding(top = Tokens.Space.m, bottom = Tokens.Space.xs)
                    )
                }
                items(items = keys, key = { key -> "$title:$key" }) { key ->
                    val selected = key.lowercase() in mine
                    Box(
                        Modifier
                            .fillMaxWidth()
                            .height(44.dp)
                            .clip(RoundedCornerShape(Tokens.Radius.control))
                            .background(if (selected) palette.accent.copy(alpha = 0.15f) else Color.Transparent)
                            .clickable { onPick(key); onDismiss() }
                            .semantics { contentDescription = LarkEmojiHelper.label(key) },
                        contentAlignment = Alignment.Center
                    ) {
                        Text(LarkEmojiHelper.symbol(key), fontSize = 22.sp)
                    }
                }
            }
        }
    }
}
