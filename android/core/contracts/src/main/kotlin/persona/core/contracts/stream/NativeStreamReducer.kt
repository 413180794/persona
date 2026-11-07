package persona.core.contracts.stream

import kotlinx.serialization.json.*
import persona.core.contracts.JsonCodec
import persona.core.contracts.generated.*

/** An immutable native tree. Raw presence is canonical; generated models are a display projection only. */
class NativeStreamTree internal constructor(
    val snapshot: ExtensionUIStreamSnapshot,
    val raw: JsonObject,
    val nodes: Map<String, JsonObject>,
) {
    val view: ExtensionView get() = snapshot.view

    /** A callback ID only selects a saved node/event slot; the server must authorize it independently. */
    fun callback(key: String, action: String, event: String): Boolean = event in callbackEvents(key, action)

    /** Event kind belongs to the saved native slot; Action.submit only gathers form values. */
    fun callbackEvent(key: String, action: String, preferred: String? = null): String? {
        val events = callbackEvents(key, action)
        return preferred?.takeIf { it in events } ?: events.singleOrNull()
    }

    internal fun submits(key: String, action: String): Boolean {
        val node = nodes[key] ?: return false
        fun search(value: JsonElement): Boolean = when (value) {
            is JsonObject -> if (value !== node && value["key"] != null) false else
                value["id"] == JsonPrimitive(action) && value["submit"] == JsonPrimitive(true) || value.values.any(::search)
            is JsonArray -> value.any(::search)
            else -> false
        }
        return search(node)
    }

    private fun callbackEvents(key: String, action: String): Set<String> {
        val node = nodes[key] ?: return emptySet()
        val review = node["review"] as? JsonObject
        val buttons = if (node["type"] == JsonPrimitive("composer")) node["buttons"] else review?.get("buttons")
        val submission = (buttons as? JsonArray)?.any { it.jsonObject["action"]?.jsonObject?.get("id") == JsonPrimitive(action) } == true
        val events = linkedSetOf<String>()
        fun search(value: JsonElement, slot: String, ownerType: String) {
            if (value is JsonObject) {
                // A disabled control does not disable the row's independent primary/menu slots.
                if (slot == "control" && value["disabled"] == JsonPrimitive(true)) return
                // A nested keyed tile owns its actions, not the containing component.
                if (value !== node && value["key"] != null) return
                if (slot == "action" || slot in setOf("load", "expand", "comment", "context", "draft", "resolve", "mark_viewed", "reply", "discard")) {
                    if (value["id"] == JsonPrimitive(action)) events.add(when {
                        submission -> "submit"
                        ownerType in setOf("segmented", "control", "search", "filter", "tabs") -> "change"
                        else -> "press"
                    })
                }
                value.forEach { (name, child) -> search(child, name,
                    if (name in setOf("control", "search", "filter", "tabs")) name else ownerType) }
            } else if (value is JsonArray) value.forEach { search(it, slot, ownerType) }
        }
        search(node, "", node["type"]?.jsonPrimitive?.content.orEmpty())
        return events
    }
}

sealed interface NativeStreamUpdate {
    data class Applied(val tree: NativeStreamTree) : NativeStreamUpdate
    /** No state is committed on any failure. Recovery is read-only, never an interaction replay. */
    data object SnapshotRequired : NativeStreamUpdate
}

/** Pure, transport-independent reducer of generated stream payloads. */
class NativeStreamReducer(
    private val supportedComponents: Set<String> = ExtensionComponent.TYPES.filter { it != "html" }.toSet(),
    private val acceptsView: (JsonObject) -> Boolean = { true },
) {
    var tree: NativeStreamTree? = null
        private set

    fun clear() { tree = null }

    fun snapshot(text: String): NativeStreamUpdate = transaction {
        materialize(NativeStreamSchema.parse(text, "UIStreamSnapshot"))
    }

    fun delta(text: String): NativeStreamUpdate = transaction {
        val current = checkNotNull(tree)
        val raw = NativeStreamSchema.parse(text, "UIStreamDelta")
        val delta = JsonCodec.decodeFromJsonElement(ExtensionUIStreamDelta.serializer(), raw)
        val saved = current.snapshot
        require(delta.session == saved.session && delta.epoch == saved.epoch && delta.baseVersion == saved.version)
        require(if (delta.operations.isEmpty()) delta.version == saved.version || delta.version == saved.version + 1 else delta.version > saved.version)
        var staged = current.raw.getValue("view").jsonObject
        delta.operations.forEachIndexed { index, op ->
            staged = apply(staged, saved.rootKey, op, raw.getValue("operations").jsonArray[index].jsonObject)
            // Also reject transient duplicates/oversized trees before resolving the next key.
            topology(staged, saved.rootKey)
        }
        materialize(JsonObject(current.raw + ("version" to raw.getValue("version")) + ("view" to staged)))
    }

    private inline fun transaction(block: () -> NativeStreamTree): NativeStreamUpdate = try {
        val next = block()
        tree = next
        NativeStreamUpdate.Applied(next)
    } catch (_: IllegalArgumentException) { NativeStreamUpdate.SnapshotRequired }
      catch (_: IllegalStateException) { NativeStreamUpdate.SnapshotRequired }
      catch (_: kotlinx.serialization.SerializationException) { NativeStreamUpdate.SnapshotRequired }
      catch (_: NoSuchElementException) { NativeStreamUpdate.SnapshotRequired }

    private fun materialize(raw: JsonObject): NativeStreamTree {
        // Bound the final tree as well as incoming frames (an insert can grow an otherwise small frame).
        NativeStreamSchema.parse(raw.toString(), "UIStreamSnapshot")
        require(acceptsView(raw.getValue("view").jsonObject))
        val snapshot = JsonCodec.decodeFromJsonElement(ExtensionUIStreamSnapshot.serializer(), raw)
        val nodes = topology(raw.getValue("view").jsonObject, snapshot.rootKey)
        require(snapshot.view.sections.all { section -> section.items.all {
            it !is ExtensionComponent.Unknown && it !is ExtensionHtmlComponent && it.type in supportedComponents
        } })
        return NativeStreamTree(snapshot, raw, nodes)
    }

    private fun topology(view: JsonObject, root: String): Map<String, JsonObject> {
        val nodes = linkedMapOf(root to view)
        fun add(node: JsonObject) {
            val key = node.getValue("key").jsonPrimitive.content
            require(key.codePointCount(0, key.length) in 1..128 && nodes.put(key, node) == null)
            require(nodes.size <= 300)
        }
        require(view["state"]?.jsonPrimitive?.content?.toByteArray(Charsets.UTF_8)?.size?.let { it <= 16384 } != false)
        view.getValue("sections").jsonArray.forEach { s ->
            val section = s.jsonObject
            add(section)
            section.getValue("items").jsonArray.forEach { i ->
                val item = i.jsonObject
                add(item)
                require(item["type"] != JsonPrimitive("html"))
                if (item["type"]?.jsonPrimitive?.content in setOf("grid", "carousel")) {
                    item.getValue("tiles").jsonArray.forEach { add(it.jsonObject) }
                }
            }
        }
        return nodes
    }

    private fun apply(view: JsonObject, root: String, op: ExtensionUIStreamOperation, raw: JsonObject): JsonObject {
        val sections = view.getValue("sections").jsonArray.map { it.jsonObject }.toMutableList()
        fun sectionIndex(key: String) = sections.indexOfFirst { it["key"] == JsonPrimitive(key) }
        fun itemLocation(key: String): Pair<Int, Int>? {
            sections.forEachIndexed { s, section ->
                val i = section.getValue("items").jsonArray.indexOfFirst { it.jsonObject["key"] == JsonPrimitive(key) }
                if (i >= 0) return s to i
            }
            return null
        }
        fun items(s: Int) = sections[s].getValue("items").jsonArray.map { it.jsonObject }.toMutableList()
        fun save(s: Int, items: List<JsonObject>) { sections[s] = JsonObject(sections[s] + ("items" to JsonArray(items))) }
        fun insert(parent: String, before: String?, node: JsonObject, isSection: Boolean) {
            fun destination(list: List<JsonObject>): Int = if (before == null) list.size else
                list.indexOfFirst { it["key"] == JsonPrimitive(before) }.also { require(it >= 0) }
            if (isSection) {
                require(parent == root)
                sections.add(destination(sections), node)
            } else {
                val s = sectionIndex(parent)
                require(s >= 0)
                val children = items(s)
                children.add(destination(children), node)
                save(s, children)
            }
        }
        when (op) {
            is ExtensionUIStreamReplace -> {
                val s = sectionIndex(op.key)
                val location = itemLocation(op.key)
                when {
                    op.key == root -> return raw.getValue("view").jsonObject.also { require(op.section == null && op.item == null) }
                    s >= 0 -> {
                        require(op.section != null && op.view == null && op.item == null)
                        val node = raw.getValue("section").jsonObject
                        require(node["key"] == JsonPrimitive(op.key))
                        sections[s] = node
                    }
                    location != null -> {
                        require(op.item != null && op.view == null && op.section == null)
                        val node = raw.getValue("item").jsonObject
                        require(node["key"] == JsonPrimitive(op.key))
                        val children = items(location.first)
                        children[location.second] = node
                        save(location.first, children)
                    }
                    else -> error("Unknown replacement key")
                }
            }
            is ExtensionUIStreamInsert -> insert(op.parent, op.before,
                raw.getValue(if (op.section != null) "section" else "item").jsonObject, op.section != null)
            is ExtensionUIStreamRemove, is ExtensionUIStreamMove -> {
                val key = when (op) { is ExtensionUIStreamRemove -> op.key; is ExtensionUIStreamMove -> op.key; else -> error("Unreachable") }
                require(key != root)
                val s = sectionIndex(key)
                val location = itemLocation(key)
                val node = when {
                    s >= 0 -> sections.removeAt(s)
                    location != null -> {
                        val children = items(location.first)
                        children.removeAt(location.second).also { save(location.first, children) }
                    }
                    else -> error("Unknown key or tile operation")
                }
                if (op is ExtensionUIStreamMove) {
                    require(op.before != key && op.parent != key)
                    insert(op.parent, op.before, node, s >= 0)
                }
            }
            is ExtensionUIStreamOperation.Unknown -> error("Unknown operation")
        }
        return JsonObject(view + ("sections" to JsonArray(sections)))
    }
}
