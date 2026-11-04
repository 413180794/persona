package persona.feature.inbox

import android.net.Uri
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import coil3.compose.AsyncImage
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import persona.core.designsystem.*
import persona.core.designsystem.Tokens
import persona.core.designsystem.icons.PhIcons

data class LarkPersonRef(
    val id: String,
    val name: String,
    val avatar: AuthenticatedLarkImage? = null,
    val feedId: String? = null,
)

data class LarkParsedProfile(
    val displayName: String,
    val enName: String? = null,
    val alias: String? = null,
    val groupNickname: String? = null,
    val avatarUrl: String? = null,
    val coverUrl: String? = null,
    val description: String? = null,
    val userType: String? = null,
    val isExternal: Boolean = false,
    val statusText: String? = null,
    val fields: List<FieldItem> = emptyList(),
) {
    data class FieldItem(
        val key: String,
        val label: String,
        val value: String,
        val leaderId: String? = null,
        val linkUrl: String? = null,
    )
}

fun parseProfile(json: JsonObject, fallbackName: String): LarkParsedProfile {
    val alias = json.cursor("alias")
    val displayName = alias ?: json.cursor("displayName") ?: json.cursor("display_name") ?: fallbackName
    val enName = json.cursor("enName") ?: json.cursor("en_name")
    val groupNickname = json.cursor("groupNickname") ?: json.cursor("group_nickname")
    val avatarUrl = json.cursor("avatarUrl") ?: json.cursor("avatar_url")
    val coverUrl = json.cursor("coverUrl") ?: json.cursor("cover_url")
    val description = json.cursor("description")
    val userType = json.cursor("userType") ?: json.cursor("user_type")

    val relation = json["relation"] as? JsonObject
    val isExternal = relation?.flag("external") == true

    val customStatus = json["customStatus"] as? JsonObject
    val workStatus = json["workStatus"] as? JsonObject
    val statusText = when (workStatus?.number("status")?.toInt()) {
        1 -> "请假中"
        2 -> "会议中"
        else -> customStatus?.cursor("title")
    }

    val rawFields = (json["fields"] as? JsonArray).orEmpty().mapNotNull { it as? JsonObject }
    val fields = rawFields.mapNotNull { f ->
        val key = f.text("key")
        val value = (f["value"] as? JsonPrimitive)?.content ?: return@mapNotNull null
        if (value.isBlank()) return@mapNotNull null
        val label = when (key) {
            "B-DEPARTMENT" -> "部门"
            "B-LEADER" -> "直属上级"
            "B-EMPLOYEETYPE" -> "人员类型"
            else -> f.cursor("label") ?: key
        }
        val url = f.cursor("url")
        val leaderId = if (url?.startsWith("lark://client/profile") == true) {
            Regex("[?&]uid=([^&#]+)").find(url)?.groupValues?.get(1)
                ?: runCatching { Uri.parse(url).getQueryParameter("uid") }.getOrNull()
        } else null
        val linkUrl = if (url?.startsWith("http") == true) url else null
        LarkParsedProfile.FieldItem(key, label, value, leaderId, linkUrl)
    }

    return LarkParsedProfile(
        displayName = displayName,
        enName = enName,
        alias = alias,
        groupNickname = groupNickname,
        avatarUrl = avatarUrl,
        coverUrl = coverUrl,
        description = description,
        userType = userType,
        isExternal = isExternal,
        statusText = statusText,
        fields = fields
    )
}

/** Sender profile sheet displaying user profile, direct supervisor, department, etc. */
@Composable
fun LarkProfileSheet(
    person: LarkPersonRef,
    repository: InboxRepository,
    onDismiss: () -> Unit,
    onDirectChat: ((String) -> Unit)? = null,
) {
    val stack = remember(person) { mutableStateListOf(person) }
    val current = stack.last()

    BackHandler(enabled = stack.size > 1) {
        stack.removeAt(stack.size - 1)
    }

    JSheet(
        onDismiss = onDismiss,
        title = "名片",
        skipPartiallyExpanded = false
    ) {
        LarkProfileCardContent(
            person = current,
            repository = repository,
            onOpenPerson = { stack.add(it) },
            onDirectChat = onDirectChat
        )
    }
}

@Composable
private fun LarkProfileCardContent(
    person: LarkPersonRef,
    repository: InboxRepository,
    onOpenPerson: (LarkPersonRef) -> Unit,
    onDirectChat: ((String) -> Unit)?,
) {
    val palette = PersonaTheme.palette
    val clipboard = LocalClipboardManager.current
    var profileData by remember(person) { mutableStateOf<LarkParsedProfile?>(null) }
    var loading by remember(person) { mutableStateOf(true) }
    var error by remember(person) { mutableStateOf<String?>(null) }

    LaunchedEffect(person) {
        loading = true
        error = null
        try {
            val json = repository.userProfile(person.id, person.feedId)
            if (json != null) {
                profileData = parseProfile(json, person.name)
            } else {
                profileData = LarkParsedProfile(person.name)
            }
        } catch (e: Exception) {
            error = e.message ?: "加载资料失败"
            profileData = LarkParsedProfile(person.name)
        } finally {
            loading = false
        }
    }

    val profile = profileData ?: LarkParsedProfile(person.name)

    Column(
        Modifier
            .fillMaxWidth()
            .verticalScroll(rememberScrollState())
            .padding(bottom = Tokens.Space.xl)
    ) {
        // Banner and Avatar
        Box(Modifier.fillMaxWidth().height(160.dp)) {
            // Cover
            if (!profile.coverUrl.isNullOrBlank()) {
                AsyncImage(
                    model = profile.coverUrl,
                    contentDescription = "封面",
                    modifier = Modifier.fillMaxWidth().height(120.dp)
                )
            } else {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(120.dp)
                        .background(
                            Brush.linearGradient(
                                listOf(palette.accent.copy(alpha = 0.5f), palette.accent.copy(alpha = 0.2f))
                            )
                        )
                )
            }

            // Avatar overlapping cover
            Box(
                Modifier
                    .padding(start = Tokens.Space.l)
                    .align(Alignment.BottomStart)
                    .size(72.dp)
                    .clip(CircleShape)
                    .background(palette.surface)
                    .padding(3.dp)
            ) {
                LarkAvatar(profile.displayName, person.avatar)
            }
        }

        // Info Body
        Column(
            Modifier.padding(horizontal = Tokens.Space.l, vertical = Tokens.Space.m),
            verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)
        ) {
            // Names & Tags
            Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.xs)) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(Tokens.Space.s)
                ) {
                    Text(
                        profile.displayName,
                        style = PersonaTheme.type.headline,
                        color = palette.ink,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                    if (profile.userType == "bot") {
                        JTag("机器人")
                    }
                    if (profile.isExternal) {
                        JTag("外部")
                    }
                }

                // Subtitle
                val subtitleParts = listOfNotNull(
                    profile.enName,
                    profile.alias?.takeIf { it != profile.displayName },
                    profile.groupNickname?.let { "群昵称：$it" }
                ).filter { it.isNotBlank() }

                if (subtitleParts.isNotEmpty()) {
                    Text(
                        subtitleParts.joinToString(" · "),
                        style = PersonaTheme.type.caption,
                        color = palette.ink3
                    )
                }

                if (!profile.statusText.isNullOrBlank()) {
                    Box(
                        Modifier
                            .clip(RoundedCornerShape(Tokens.Radius.badge))
                            .background(palette.accent.copy(alpha = 0.12f))
                            .padding(horizontal = Tokens.Space.s, vertical = 2.dp)
                    ) {
                        Text(
                            profile.statusText,
                            style = PersonaTheme.type.micro,
                            color = palette.accent
                        )
                    }
                }
            }

            // Signature / Bio
            if (!profile.description.isNullOrBlank()) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .clip(RoundedCornerShape(Tokens.Radius.control))
                        .background(palette.ink.copy(alpha = 0.04f))
                        .padding(Tokens.Space.m)
                ) {
                    Text(
                        profile.description,
                        style = PersonaTheme.type.callout,
                        color = palette.ink2
                    )
                }
            }

            // Direct Chat Button
            if (profile.userType != "bot" && onDirectChat != null) {
                JButton(
                    "发消息",
                    onClick = { onDirectChat(person.id) },
                    modifier = Modifier.fillMaxWidth()
                )
            }

            // Profile Fields
            if (profile.fields.isNotEmpty()) {
                JCard(Modifier.fillMaxWidth(), padding = Tokens.Space.m) {
                    Column(verticalArrangement = Arrangement.spacedBy(Tokens.Space.m)) {
                        profile.fields.forEachIndexed { index, field ->
                            if (index > 0) JDivider()
                            Row(
                                Modifier
                                    .fillMaxWidth()
                                    .clickable {
                                        if (field.leaderId != null) {
                                            onOpenPerson(LarkPersonRef(field.leaderId, field.value))
                                        } else {
                                            clipboard.setText(AnnotatedString(field.value))
                                        }
                                    }
                                    .padding(vertical = Tokens.Space.xs),
                                horizontalArrangement = Arrangement.SpaceBetween,
                                verticalAlignment = Alignment.CenterVertically
                            ) {
                                Text(
                                    field.label,
                                    style = PersonaTheme.type.callout,
                                    color = palette.ink3,
                                    modifier = Modifier.width(80.dp)
                                )
                                Text(
                                    field.value,
                                    style = PersonaTheme.type.callout,
                                    color = if (field.leaderId != null) palette.accent else palette.ink,
                                    maxLines = 2,
                                    overflow = TextOverflow.Ellipsis,
                                    modifier = Modifier.weight(1f)
                                )
                                if (field.leaderId != null) {
                                    JIcon(PhIcons.CaretRight, modifier = Modifier.size(16.dp), tint = palette.accent)
                                }
                            }
                        }
                    }
                }
            }

            if (loading) {
                Box(Modifier.fillMaxWidth().padding(Tokens.Space.l), contentAlignment = Alignment.Center) {
                    JLoading()
                }
            }

            if (error != null) {
                JNotice(error ?: "加载失败", status = Status.NeedsYou)
            }
        }
    }
}
