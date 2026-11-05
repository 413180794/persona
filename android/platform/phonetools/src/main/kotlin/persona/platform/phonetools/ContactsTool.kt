package persona.platform.phonetools

import android.content.Context
import android.provider.ContactsContract.Contacts
import android.provider.ContactsContract.Data
import android.provider.ContactsContract.CommonDataKinds.*
import persona.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*

/** phone_contacts_search, answering as the iOS app's ContactsTool does (keys, empty-result note, address book order). */
internal class ContactsTool(private val context: Context) {
    private val resolver = context.contentResolver

    fun search(args: JsonObject): PhoneToolResult {
        val query = args.text("query").orEmpty()
        val limit = args.int("limit", 10, 1, 50)
        val ids = if (query.isEmpty()) firstContacts(limit) else matches(query, limit)
        val people = ids.map(::contact)
        return PhoneToolResult.text(buildJsonObject {
            put("query", query); put("count", people.size); put("results", JsonArray(people))
            if (people.isEmpty()) put("note", "No contacts matched. Try a shorter query, or the company or phone number instead.")
        }.toString())
    }

    /** The address book's own order (its sort key), like an empty search on iOS. */
    private fun firstContacts(limit: Int): List<Long> = buildList {
        resolver.query(Contacts.CONTENT_URI, arrayOf(Contacts._ID), null, null, "${Contacts.SORT_KEY_PRIMARY} ASC")?.use { cursor ->
            while (size < limit && cursor.moveToNext()) add(cursor.getLong(0))
        } ?: unavailable("通讯录暂不可用 / The contacts provider is not available on this phone.")  // i18n-ignore: tool result for the model
    }

    /**
     * Name, nickname, company, job title, phone number or email containing [query] (case-insensitive); a query of at least
     * three digits also matches phone numbers written with spaces, dashes or brackets.
     */
    private fun matches(query: String, limit: Int): List<Long> {
        val escaped = query.replace("\\", "\\\\").replace("%", "\\%").replace("_", "\\_")
        val digits = query.filter(Char::isDigit).takeIf { it.length >= 3 }
        val types = listOf(StructuredName.CONTENT_ITEM_TYPE, Nickname.CONTENT_ITEM_TYPE, Organization.CONTENT_ITEM_TYPE,
            Phone.CONTENT_ITEM_TYPE, Email.CONTENT_ITEM_TYPE)
        val like = "LIKE ? ESCAPE '\\'"
        val number = "REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(${Data.DATA1}, ' ', ''), '-', ''), '(', ''), ')', ''), '.', '')"
        val selection = "${Data.MIMETYPE} IN (${types.joinToString { "?" }}) AND (${Data.DISPLAY_NAME} $like OR ${Data.DATA1} $like" +
            " OR (${Data.MIMETYPE} = ? AND ${Organization.TITLE} $like)" +
            (if (digits != null) " OR (${Data.MIMETYPE} = ? AND $number LIKE ?)" else "") + ")"
        val pattern = "%$escaped%"
        val values = types + listOf(pattern, pattern, Organization.CONTENT_ITEM_TYPE, pattern) +
            (if (digits != null) listOf(Phone.CONTENT_ITEM_TYPE, "%$digits%") else emptyList())
        val found = linkedSetOf<Long>()
        resolver.query(Data.CONTENT_URI, arrayOf(Data.CONTACT_ID), selection, values.toTypedArray(), null)?.use { cursor ->
            while (found.size < MAX_CANDIDATES && cursor.moveToNext()) found += cursor.getLong(0)
        } ?: unavailable("通讯录暂不可用 / The contacts provider is not available on this phone.")  // i18n-ignore: tool result for the model
        if (found.isEmpty()) return emptyList()
        // Back in the address book's order, as iOS enumerates it.
        return buildList {
            resolver.query(Contacts.CONTENT_URI, arrayOf(Contacts._ID), "${Contacts._ID} IN (${found.joinToString(",")})", null,
                "${Contacts.SORT_KEY_PRIMARY} ASC")?.use { cursor ->
                while (size < limit && cursor.moveToNext()) add(cursor.getLong(0))
            }
        }
    }

    private fun contact(id: Long): JsonObject {
        var name = ""; var nickname = ""; var company = ""; var job = ""; var birthday: String? = null
        val phones = mutableListOf<JsonElement>(); val emails = mutableListOf<JsonElement>()
        resolver.query(Data.CONTENT_URI, arrayOf(Data.DISPLAY_NAME, Data.MIMETYPE, Data.DATA1, Data.DATA2, Data.DATA3, Data.DATA4),
            "${Data.CONTACT_ID} = ?", arrayOf(id.toString()), null)?.use { cursor ->
            while (cursor.moveToNext()) {
                name = cursor.getString(0).orEmpty()
                val value = cursor.getString(2).orEmpty()
                when (cursor.getString(1)) {
                    Phone.CONTENT_ITEM_TYPE -> phones += buildJsonObject {
                        put("label", Phone.getTypeLabel(context.resources, cursor.getInt(3), cursor.getString(4)).toString())
                        put("number", value)
                    }
                    Email.CONTENT_ITEM_TYPE -> emails += buildJsonObject {
                        put("label", Email.getTypeLabel(context.resources, cursor.getInt(3), cursor.getString(4)).toString())
                        put("email", value)
                    }
                    Nickname.CONTENT_ITEM_TYPE -> if (nickname.isEmpty()) nickname = value
                    Organization.CONTENT_ITEM_TYPE -> { company = value; job = cursor.getString(5).orEmpty() }
                    Event.CONTENT_ITEM_TYPE -> if (cursor.getInt(3) == Event.TYPE_BIRTHDAY) birthday = value
                }
            }
        }
        // Keys only when there is something to say, as iOS drops empty fields.
        return buildJsonObject {
            put("id", id.toString())
            if (name.isNotEmpty()) put("name", name)
            if (nickname.isNotEmpty()) put("nickname", nickname)
            if (company.isNotEmpty()) put("company", company)
            if (job.isNotEmpty()) put("job_title", job)
            if (phones.isNotEmpty()) put("phones", JsonArray(phones))
            if (emails.isNotEmpty()) put("emails", JsonArray(emails))
            birthday?.takeIf { it.isNotEmpty() }?.let { put("birthday", it) }
        }
    }

    private companion object { const val MAX_CANDIDATES = 500 }
}
