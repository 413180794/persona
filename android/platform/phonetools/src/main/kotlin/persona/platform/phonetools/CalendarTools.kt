package persona.platform.phonetools

import android.content.ContentUris
import android.content.ContentValues
import android.content.Context
import android.provider.CalendarContract as Calendar
import persona.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*
import java.time.*

internal data class CalendarArguments(
    val title: String, val start: OffsetDateTime, val end: OffsetDateTime,
    val location: String?, val notes: String?, val allDay: Boolean, val calendar: String?,
) {
    val startMillis: Long get() = if (allDay) start.toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() else start.toInstant().toEpochMilli()
    val endMillis: Long get() = if (allDay) end.toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() else end.toInstant().toEpochMilli()
    fun summary() = buildString {
        append("新建日程「$title」\n$start → $end")
        if (allDay) append("\n全天日程，结束日期不包含在内")
        append("\n日历：${calendar ?: "默认可写日历；没有时创建 Jarvis 本地日历"}")
        location?.let { append("\n地点：$it") }; notes?.let { append("\n备注：$it") }
    }
    companion object {
        fun parse(args: JsonObject): CalendarArguments {
            val result = CalendarArguments(
                requireNotNull(args.string("title")).also { require(it.isNotBlank()) { "日程标题不能为空。" } },
                parseTime(requireNotNull(args.string("start"))), parseTime(requireNotNull(args.string("end"))),
                args.string("location"), args.string("notes"), args.bool("all_day"), args.string("calendar"),
            )
            require(result.endMillis > result.startMillis) { "结束时间必须晚于开始时间。" }
            return result
        }
    }
}

internal fun parseTime(value: String): OffsetDateTime = try { OffsetDateTime.parse(value) }
catch (_: Exception) { throw IllegalArgumentException("时间须使用带时区的 ISO 8601 格式。") }
internal fun iso(millis: Long): String = Instant.ofEpochMilli(millis).atZone(ZoneId.systemDefault()).toOffsetDateTime().toString()

internal class CalendarTools(context: Context) {
    private val resolver = context.contentResolver
    fun events(args: JsonObject): PhoneToolResult {
        val start = args.string("start")?.let(::parseTime)?.toInstant()
            ?: LocalDate.now().atStartOfDay(ZoneId.systemDefault()).toInstant()
        val end = args.string("end")?.let(::parseTime)?.toInstant() ?: start.plus(Duration.ofDays(7))
        require(end > start && Duration.between(start, end) <= Duration.ofDays(366)) { "日历查询范围须在一年内，结束时间晚于开始时间。" }
        val uri = Calendar.Instances.CONTENT_URI.buildUpon().also {
            ContentUris.appendId(it, start.toEpochMilli()); ContentUris.appendId(it, end.toEpochMilli())
        }.build()
        val query = args.string("query").orEmpty()
        val rows = buildJsonArray {
            var count = 0
            resolver.query(uri, arrayOf(Calendar.Instances.EVENT_ID, Calendar.Instances.TITLE, Calendar.Instances.BEGIN,
                Calendar.Instances.END, Calendar.Instances.ALL_DAY, Calendar.Instances.CALENDAR_DISPLAY_NAME,
                Calendar.Instances.EVENT_LOCATION, Calendar.Instances.DESCRIPTION), null, null,
                "${Calendar.Instances.BEGIN} ASC, ${Calendar.Instances.EVENT_ID} ASC")?.use { cursor ->
                while (count < args.int("limit", 50) && cursor.moveToNext()) {
                    val title = cursor.getString(1).orEmpty()
                    val notes = cursor.getString(7).orEmpty()
                    if (query.isNotEmpty() && !title.contains(query, true) && !notes.contains(query, true)) continue
                    count++
                    add(buildJsonObject {
                        put("id", cursor.getLong(0).toString()); put("title", title)
                        put("start", iso(cursor.getLong(2))); put("end", iso(cursor.getLong(3)))
                        put("all_day", cursor.getInt(4) != 0); put("calendar", cursor.getString(5).orEmpty())
                        put("location", cursor.getString(6).orEmpty()); put("notes", notes.take(300))
                        put("attendees", attendees(cursor.getLong(0)))
                    })
                }
            } ?: error("系统日历暂不可用。")
        }
        return PhoneToolResult.text(rows.toString())
    }
    private fun attendees(eventId: Long) = buildJsonArray {
        var count = 0
        resolver.query(Calendar.Attendees.CONTENT_URI,
            arrayOf(Calendar.Attendees.ATTENDEE_NAME, Calendar.Attendees.ATTENDEE_EMAIL, Calendar.Attendees.ATTENDEE_STATUS),
            "${Calendar.Attendees.EVENT_ID} = ?", arrayOf(eventId.toString()), null)?.use { cursor ->
            while (count++ < 100 && cursor.moveToNext()) add(buildJsonObject {
                put("name", cursor.getString(0).orEmpty()); put("email", cursor.getString(1).orEmpty()); put("status", cursor.getInt(2))
            })
        }
    }
    fun create(args: JsonObject): PhoneToolResult {
        val event = CalendarArguments.parse(args)
        val id = writableCalendar(event.calendar)
        val values = ContentValues().apply {
            put(Calendar.Events.CALENDAR_ID, id); put(Calendar.Events.TITLE, event.title)
            put(Calendar.Events.DTSTART, event.startMillis); put(Calendar.Events.DTEND, event.endMillis)
            put(Calendar.Events.EVENT_TIMEZONE, if (event.allDay) "UTC" else ZoneId.systemDefault().id)
            put(Calendar.Events.ALL_DAY, if (event.allDay) 1 else 0)
            put(Calendar.Events.EVENT_LOCATION, event.location); put(Calendar.Events.DESCRIPTION, event.notes)
        }
        val uri = resolver.insert(Calendar.Events.CONTENT_URI, values) ?: error("日程未保存。")
        return PhoneToolResult.text(buildJsonObject { put("id", ContentUris.parseId(uri).toString()); put("title", event.title) }.toString())
    }
    private fun writableCalendar(name: String?): Long {
        var first: Long? = null
        resolver.query(Calendar.Calendars.CONTENT_URI, arrayOf(Calendar.Calendars._ID, Calendar.Calendars.CALENDAR_DISPLAY_NAME),
            "${Calendar.Calendars.CALENDAR_ACCESS_LEVEL} >= ?", arrayOf(Calendar.Calendars.CAL_ACCESS_CONTRIBUTOR.toString()),
            "${Calendar.Calendars.IS_PRIMARY} DESC, ${Calendar.Calendars._ID} ASC")?.use { cursor ->
            while (cursor.moveToNext()) {
                if (first == null) first = cursor.getLong(0)
                if (name != null && cursor.getString(1) == name) return cursor.getLong(0)
            }
        } ?: error("系统日历暂不可用。")
        require(name == null) { "未找到指定的可写日历，请换一个日历。" }
        first?.let { return it }
        val uri = Calendar.Calendars.CONTENT_URI.buildUpon()
            .appendQueryParameter(Calendar.CALLER_IS_SYNCADAPTER, "true")
            .appendQueryParameter(Calendar.Calendars.ACCOUNT_NAME, "Jarvis")
            .appendQueryParameter(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL).build()
        val values = ContentValues().apply {
            put(Calendar.Calendars.ACCOUNT_NAME, "Jarvis"); put(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL)
            put(Calendar.Calendars.NAME, "Jarvis"); put(Calendar.Calendars.CALENDAR_DISPLAY_NAME, "Jarvis")
            put(Calendar.Calendars.OWNER_ACCOUNT, "Jarvis"); put(Calendar.Calendars.CALENDAR_ACCESS_LEVEL, Calendar.Calendars.CAL_ACCESS_OWNER)
            put(Calendar.Calendars.CALENDAR_COLOR, 0xff3f51b5.toInt()); put(Calendar.Calendars.VISIBLE, 1)
            put(Calendar.Calendars.SYNC_EVENTS, 1); put(Calendar.Calendars.CALENDAR_TIME_ZONE, ZoneId.systemDefault().id)
        }
        return ContentUris.parseId(resolver.insert(uri, values) ?: error("系统未允许创建本地日历。"))
    }
}
