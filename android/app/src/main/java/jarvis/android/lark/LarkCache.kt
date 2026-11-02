package jarvis.android.lark

import jarvis.android.data.JsonCodec
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import kotlinx.serialization.builtins.ListSerializer
import java.io.File
import java.security.MessageDigest

/**
 * The last page of each Feishu chat on disk (`cache/lark-chats`, iOS `StateCache "lark-chat-<id>"`): a chat opened again
 * paints at once, offline too, and the network only brings it up to date. 设置 › 存储空间 counts it as 飞书.
 */
class LarkCache(private val dir: File) {
    private fun file(feedId: String): File {
        val h = MessageDigest.getInstance("SHA-1").digest(feedId.toByteArray()).joinToString("") { "%02x".format(it) }
        return File(dir, "$h.json")
    }

    suspend fun page(feedId: String): List<LarkMessage>? = withContext(Dispatchers.IO) {
        val f = file(feedId)
        if (!f.exists()) return@withContext null
        runCatching { JsonCodec.decodeFromString(ListSerializer(LarkMessage.serializer()), f.readText()) }.getOrNull()
    }

    suspend fun save(feedId: String, messages: List<LarkMessage>) = withContext(Dispatchers.IO) {
        runCatching {
            dir.mkdirs()
            val f = file(feedId)
            val tmp = File(dir, f.name + ".tmp")
            tmp.writeText(JsonCodec.encodeToString(ListSerializer(LarkMessage.serializer()), messages.takeLast(KEEP)))
            if (!tmp.renameTo(f)) tmp.delete()
        }
        Unit
    }

    private companion object {
        const val KEEP = 80
    }
}
