package persona.core.contracts

/**
 * What the hub offers of Feishu through lark-connector (#1530; iOS `LarkFeatures`, web `larkOn`). That path is an extension's:
 * the hub has `/api/v1/lark/…` only while the extension is installed, and the extension adds `lark` (the chats, the native
 * readers of Feishu documents and company-feed posts, 设置 › 连接器 › 飞书, every `/api/v1/lark/…` route) and
 * `lark_moments` (the company feed) to the hello's features. Without them nothing that needs those routes shows:
 * Feishu links open as ordinary web pages. Inbox items with `lark` / `doc` / `moment` blocks come from the extension only,
 * so they are drawn as they come.
 *
 * Unknown is today's behaviour, everything shown: no hello yet (or the demo), and a hub from before the extension, which
 * served Feishu itself and said so with `lark_attachments` but not `lark`.
 */
data class LarkFeatures(
    /** `lark`: the chats, the Feishu account in 设置, the native document reader. */
    val chats: Boolean,
    /** `lark_moments`: company-feed posts open in the native reader (only with [chats]). */
    val moments: Boolean,
) {
    companion object {
        val All = LarkFeatures(chats = true, moments = true)
        val None = LarkFeatures(chats = false, moments = false)

        /** From the hub's hello features. */
        fun of(features: Collection<String>): LarkFeatures = when {
            features.isEmpty() -> All // no hello yet
            "lark" !in features && "lark_attachments" in features -> All // a hub that serves Feishu itself
            else -> LarkFeatures(chats = "lark" in features, moments = "lark" in features && "lark_moments" in features)
        }
    }
}
