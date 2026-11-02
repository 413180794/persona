package jarvis.android.lark

import java.time.Duration
import java.time.Instant
import java.time.ZoneId
import java.time.ZonedDateTime
import java.time.format.DateTimeFormatter

/** Message positions are decimal strings that can outgrow Long in theory; compare them as numbers (iOS `LarkPosition`). */
object LarkPosition {
    fun less(a: String, b: String): Boolean {
        val x = a.trim()
        val y = b.trim()
        val nx = x.startsWith("-")
        val ny = y.startsWith("-")
        if (nx != ny) return nx
        val ax = if (nx) x.drop(1) else x
        val ay = if (ny) y.drop(1) else y
        val r = if (ax.length != ay.length) ax.length < ay.length else ax < ay
        return if (nx) !r && ax != ay else r
    }

    fun next(p: String): String = p.trim().toLongOrNull()?.let { (it + 1).toString() } ?: p

    fun previous(p: String): String = p.trim().toLongOrNull()?.let { (it - 1).toString() } ?: p
}

/** The chat page's pure rules (iOS `LarkChatView` statics), kept here so they are unit-tested. */
object LarkChatRules {
    /** Same sender within five minutes: no second avatar / name. */
    fun continues(m: LarkMessage, prev: LarkMessage?): Boolean {
        if (prev == null || m.isSystem || prev.isSystem || prev.senderName != m.senderName || prev.isMine != m.isMine) return false
        val a = prev.createdAt ?: return false
        val b = m.createdAt ?: return false
        return Duration.between(a, b).seconds < 300
    }

    /** A time line above [m]: a new day, or more than three hours since the previous message. */
    fun startsDay(m: LarkMessage, prev: LarkMessage?, zone: ZoneId = ZoneId.systemDefault()): Boolean {
        val d = m.createdAt ?: return false
        val p = prev?.createdAt ?: return true
        return d.atZone(zone).toLocalDate() != p.atZone(zone).toLocalDate() || Duration.between(p, d).seconds > 3 * 3600
    }

    private val hm = DateTimeFormatter.ofPattern("HH:mm")

    /** 15:04 / 昨天 15:04 / 10月21日 15:04 / 2025年10月21日 15:04. */
    fun dayLabel(d: Instant?, now: Instant = Instant.now(), zone: ZoneId = ZoneId.systemDefault()): String {
        d ?: return ""
        val t = d.atZone(zone)
        val today = now.atZone(zone).toLocalDate()
        val time = t.format(hm)
        return when {
            t.toLocalDate() == today -> time
            t.toLocalDate() == today.minusDays(1) -> "昨天 $time"
            t.year == today.year -> "${t.monthValue}月${t.dayOfMonth}日 $time"
            else -> "${t.year}年${t.monthValue}月${t.dayOfMonth}日 $time"
        }
    }

    /** The time under a message (24-hour). */
    fun clock(d: Instant?, zone: ZoneId = ZoneId.systemDefault()): String = d?.let { ZonedDateTime.ofInstant(it, zone).format(hm) } ?: ""

    /** The first message after [anchor] (the read position the chat was opened at) gets 以下为新消息 above it. */
    fun isFirstUnread(m: LarkMessage, prev: LarkMessage?, anchor: String?): Boolean {
        anchor ?: return false
        if (!LarkPosition.less(anchor, m.position)) return false
        prev ?: return true
        return !LarkPosition.less(anchor, prev.position)
    }

    /** Unread messages from others that have not been on screen yet (the 「N 条新消息」 on the jump button). */
    fun unseenNew(messages: List<LarkMessage>, seen: Set<String>, anchor: String?, unread: Int): Int {
        anchor ?: return 0
        val seenUnread = messages.count { !it.isMine && !it.isSystem && it.position in seen && LarkPosition.less(anchor, it.position) }
        return maxOf(0, unread - seenUnread)
    }

    /**
     * What to report as read: the newest message seen past [cursor], with every unread message from others up to it.
     * Null when nothing new was on screen (opening a chat alone never reports anything).
     */
    fun readReport(messages: List<LarkMessage>, seen: Set<String>, cursor: String?): LarkReadReport? {
        val c = cursor ?: "0"
        val top = messages.filter { it.position in seen && LarkPosition.less(c, it.position) }
            .maxWithOrNull { a, b -> if (LarkPosition.less(a.position, b.position)) -1 else if (a.position == b.position) 0 else 1 }
            ?: return null
        val ids = messages.filter { m ->
            !m.isMine && m.isRead != true && !m.isSystem && LarkPosition.less(c, m.position) && !LarkPosition.less(top.position, m.position)
        }.map { it.id }
        return LarkReadReport(top.position, top.badgeCount, ids.ifEmpty { null })
    }

    /**
     * Folds the latest page into what is shown: changed messages (cards, reactions, recalls) update in place, newer
     * ones append. Returns the new list and how many were appended.
     */
    fun mergeLatest(current: List<LarkMessage>, latest: List<LarkMessage>): Pair<List<LarkMessage>, Int> {
        val byId = latest.associateBy { it.id }
        val updated = current.map { byId[it.id] ?: it }
        val have = current.mapTo(HashSet()) { it.id }
        val last = current.lastOrNull()?.position
        val fresh = latest.filter { it.id !in have && (last == null || LarkPosition.less(last, it.position)) }
        return (updated + fresh) to fresh.size
    }

    /** Older page in front of what is shown, without duplicates. */
    fun prependOlder(current: List<LarkMessage>, older: List<LarkMessage>): List<LarkMessage> {
        val have = current.mapTo(HashSet()) { it.id }
        return older.filter { it.id !in have } + current
    }

    fun appendNewer(current: List<LarkMessage>, newer: List<LarkMessage>): List<LarkMessage> {
        val have = current.mapTo(HashSet()) { it.id }
        return current + newer.filter { it.id !in have }
    }
}

/** The message with my reaction [key] toggled (lark-connector's optimistic update: remove if mine, else add). */
fun LarkMessage.togglingReaction(key: String, me: String): Pair<LarkMessage, Boolean> {
    val list = (reactions ?: emptyList()).toMutableList()
    val i = list.indexOfFirst { it.key.equals(key, ignoreCase = true) }
    if (i >= 0) {
        val r = list[i]
        if (r.mine) {
            val next = r.copy(count = r.count - 1, mine = false, users = r.users?.filter { it != me })
            if (next.count <= 0) list.removeAt(i) else list[i] = next
            return copy(reactions = list) to true
        }
        list[i] = r.copy(count = r.count + 1, mine = true, users = (r.users ?: emptyList()) + me)
    } else {
        list += LarkReaction(key = key, count = 1, mine = true, users = listOf(me))
    }
    return copy(reactions = list) to false
}

/** removed share links (`…/client/moments/detail?postId=…`). */
object LarkMoments {
    fun postId(link: String?): String? {
        link ?: return null
        val uri = runCatching { java.net.URI(link) }.getOrNull() ?: return null
        val path = uri.path ?: return null
        if (!path.endsWith("/client/moments/detail") && !path.endsWith("/moments/detail")) return null
        val id = (uri.rawQuery ?: return null).split('&').firstNotNullOfOrNull { kv ->
            val i = kv.indexOf('=')
            if (i > 0 && kv.substring(0, i) == "postId") java.net.URLDecoder.decode(kv.substring(i + 1), "UTF-8") else null
        } ?: return null
        return id.takeIf { it.isNotEmpty() && it.all(Char::isDigit) }
    }
}

/** lark-connector's rich-text tree: consecutive leaves sharing a block ancestor share the block (port of `rich-blocks.ts`). */
sealed interface LarkRichNode {
    data class Leaf(val part: LarkPart) : LarkRichNode
    data class Block(val block: LarkRichBlock, val children: List<LarkRichNode>) : LarkRichNode

    companion object {
        private class Box(val block: LarkRichBlock) {
            val children = ArrayList<Any>()
        }

        fun tree(parts: List<LarkPart>): List<LarkRichNode> {
            val root = ArrayList<Any>()
            val open = ArrayList<Box>()
            fun add(x: Any) {
                if (open.isEmpty()) root += x else open.last().children += x
            }
            for (part in parts) {
                val path = part.blocks
                var shared = 0
                while (shared < open.size && shared < path.size && open[shared].block.id == path[shared].id) shared++
                while (open.size > shared) open.removeAt(open.size - 1)
                for (b in path.drop(shared)) {
                    val box = Box(b)
                    add(box)
                    open += box
                }
                add(part)
            }
            fun convert(items: List<Any>): List<LarkRichNode> = items.map { item ->
                if (item is Box) Block(item.block, convert(item.children)) else Leaf(item as LarkPart)
            }
            return adoptNestedLists(convert(root))
        }

        /** Feishu nests a sub-list as a sibling of the items (`OL > LI, OL, LI`); move it into the item before it. */
        fun adoptNestedLists(nodes: List<LarkRichNode>): List<LarkRichNode> = nodes.map { node ->
            if (node !is Block) return@map node
            val inner = adoptNestedLists(node.children)
            if (!node.block.type.endsWith("-list")) return@map Block(node.block, inner)
            val out = ArrayList<LarkRichNode>()
            for (child in inner) {
                val last = out.lastOrNull()
                if (child is Block && child.block.type.endsWith("-list") && last is Block && last.block.type == "list-item") {
                    out[out.size - 1] = Block(last.block, last.children + child)
                } else {
                    out += child
                }
            }
            Block(node.block, out)
        }

        /** The marker of item [index] in list [b] (1. / a. / A. / iv. / IV. / • ◦ ▪). */
        fun marker(b: LarkRichBlock, index: Int): String {
            if (b.type != "ordered-list") return listOf("•", "◦", "▪", "")[b.listType.coerceIn(0, 3)]
            val n = b.start + index
            return when (b.listType) {
                1 -> (if (n in 1..26) ('a' + n - 1).toString() else "$n") + "."
                2 -> (if (n in 1..26) ('A' + n - 1).toString() else "$n") + "."
                3 -> roman(n).lowercase() + "."
                4 -> roman(n) + "."
                else -> "$n."
            }
        }

        fun roman(n: Int): String {
            val table = listOf(1000 to "M", 900 to "CM", 500 to "D", 400 to "CD", 100 to "C", 90 to "XC", 50 to "L", 40 to "XL",
                10 to "X", 9 to "IX", 5 to "V", 4 to "IV", 1 to "I")
            var v = n
            val sb = StringBuilder()
            for ((k, s) in table) while (v >= k) { sb.append(s); v -= k }
            return sb.toString()
        }
    }
}

object LarkParts {
    /** Parts that draw their own chrome (cards, pictures, stickers, files, video, removed cards) and need no bubble. */
    fun isBare(parts: List<LarkPart>): Boolean {
        val visible = parts.filter { !(it.kind == "text" && (it.text ?: "").isBlank()) }
        if (visible.isEmpty()) return false
        if (visible.size == 1) {
            val p = visible[0]
            return p.kind in setOf("card", "image", "file", "media") || (p.kind == "emotion" && p.url != null) ||
                (p.kind == "link" && LarkMoments.postId(p.url) != null)
        }
        return visible.all { it.kind == "card" }
    }

    /** Every picture of a message, so tapping one pages through all of them. */
    fun imageUrls(parts: List<LarkPart>): List<String> = parts.filter { it.kind == "image" }.mapNotNull { it.url }

    /** A picture's size in dp: its own aspect ratio inside [maxW] × [maxH], never blown up; 200 × 150 when unknown. */
    fun displaySize(width: Double, height: Double, maxW: Double = 240.0, maxH: Double = 300.0): Pair<Float, Float> {
        if (width <= 0 || height <= 0) return 200f to 150f
        val s = minOf(maxW / width, maxH / height, 1.0)
        return maxOf(width * s, 60.0).toFloat() to maxOf(height * s, 40.0).toFloat()
    }

    /** Cards and links stretch to the full width; other bare parts keep their own size. */
    fun fillsWidth(parts: List<LarkPart>): Boolean = parts.any { it.kind == "card" || it.kind == "link" }
}

/** Card `list` element markers: numbering restarts per nesting level. */
fun larkCardListMarkers(items: List<kotlinx.serialization.json.JsonElement>): List<String> {
    val counters = HashMap<Int, Int>()
    return items.map { item ->
        val level = item["level"].int ?: 0
        counters.keys.filter { it > level }.forEach { counters.remove(it) }
        if (item["type"].str != "ol") return@map listOf("•", "◦", "▪")[level.coerceIn(0, 2)]
        val n = (counters[level] ?: 0) + 1
        counters[level] = n
        "$n."
    }
}

/** Hub thumbnails come in steps of 24 px up to 624 (lark-connector's `MEDIA_THUMBNAIL_EDGES`); larger is the original. */
fun larkThumbEdge(px: Int): Int? {
    val edge = ((px + 23) / 24) * 24
    return if (edge in 1..624) edge else null
}
