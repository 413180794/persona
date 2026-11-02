package jarvis.platform.phonetools

import android.content.Context
import android.provider.ContactsContract.Data
import android.provider.ContactsContract.CommonDataKinds.*
import jarvis.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*

internal class ContactsTool(private val context: Context) {
    private val resolver = context.contentResolver
    fun search(args: JsonObject): PhoneToolResult {
        val query = requireNotNull(args.string("query")).trim()
        require(query.isNotEmpty()) { "请输入联系人搜索词。" }
        val escaped = query.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
        val number = query.filter(Char::isDigit)
        val ids = linkedSetOf<Long>()
        val mimeTypes = listOf(StructuredName.CONTENT_ITEM_TYPE, Organization.CONTENT_ITEM_TYPE, Phone.CONTENT_ITEM_TYPE, Email.CONTENT_ITEM_TYPE)
        val fields = listOf(Data.DISPLAY_NAME, Data.DATA1, Data.DATA4)
        val clauses = fields.joinToString(" OR ") { "$it LIKE ? ESCAPE '\\'" }
        val numericClause = if (number.isNotEmpty()) " OR (${Data.MIMETYPE} = ? AND REPLACE(REPLACE(REPLACE(REPLACE(${Data.DATA1}, ' ', ''), '-', ''), '(', ''), ')', '') LIKE ?)" else ""
        val selection = "${Data.MIMETYPE} IN (${mimeTypes.joinToString { "?" }}) AND ($clauses$numericClause)"
        val values = mimeTypes + fields.map { "%$escaped%" } + if (number.isNotEmpty()) listOf(Phone.CONTENT_ITEM_TYPE, "%$number%") else emptyList()
        resolver.query(Data.CONTENT_URI, arrayOf(Data.CONTACT_ID), selection, values.toTypedArray(), "${Data.CONTACT_ID} ASC")?.use { cursor ->
            while (ids.size < args.int("limit", 20) && cursor.moveToNext()) ids.add(cursor.getLong(0))
        } ?: error("通讯录暂不可用。")
        return PhoneToolResult.text(JsonArray(ids.map(::contact)).toString())
    }
    private fun contact(id: Long): JsonObject {
        var name = ""; var company = ""; var job = ""; var birthday: String? = null
        val phones = mutableListOf<JsonElement>(); val emails = mutableListOf<JsonElement>()
        resolver.query(Data.CONTENT_URI, arrayOf(Data.DISPLAY_NAME, Data.MIMETYPE, Data.DATA1, Data.DATA2, Data.DATA3, Data.DATA4),
            "${Data.CONTACT_ID} = ?", arrayOf(id.toString()), null)?.use { cursor ->
            while (cursor.moveToNext()) {
                name = cursor.getString(0).orEmpty()
                val value = cursor.getString(2).orEmpty()
                when (cursor.getString(1)) {
                    Phone.CONTENT_ITEM_TYPE -> phones.add(buildJsonObject {
                        put("number", value); put("label", Phone.getTypeLabel(context.resources, cursor.getInt(3), cursor.getString(4)).toString())
                    })
                    Email.CONTENT_ITEM_TYPE -> emails.add(buildJsonObject {
                        put("address", value); put("label", Email.getTypeLabel(context.resources, cursor.getInt(3), cursor.getString(4)).toString())
                    })
                    Organization.CONTENT_ITEM_TYPE -> { company = value; job = cursor.getString(5).orEmpty() }
                    Event.CONTENT_ITEM_TYPE -> if (cursor.getInt(3) == Event.TYPE_BIRTHDAY) birthday = value
                }
            }
        }
        return buildJsonObject {
            put("id", id.toString()); put("name", name); put("company", company); put("job_title", job)
            put("phones", JsonArray(phones)); put("emails", JsonArray(emails)); birthday?.let { put("birthday", it) }
        }
    }
}
