package persona.platform.phonetools

import android.content.ContentUris
import android.content.ContentValues
import android.content.Context
import android.content.res.Resources
import persona.core.designsystem.LocalizedFormats
import android.provider.CalendarContract as Calendar
import persona.core.contracts.api.PhoneToolResult
import kotlinx.serialization.json.*
import java.time.*
import java.time.format.DateTimeFormatter
import java.time.temporal.ChronoUnit
import java.util.Locale

internal data class CalendarArguments(
    val title: String, val start: Instant, val end: Instant,
    val location: String?, val notes: String?, val allDay: Boolean, val calendar: String?,
) {
    /** All-day events are stored as UTC midnights of the local dates, the end date exclusive (CalendarContract). */
    fun startMillis(zone: ZoneId = ZoneId.systemDefault()): Long =
        if (allDay) start.atZone(zone).toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() else start.toEpochMilli()
    fun endMillis(zone: ZoneId = ZoneId.systemDefault()): Long =
        if (allDay) end.atZone(zone).toLocalDate().atStartOfDay(ZoneOffset.UTC).toInstant().toEpochMilli() else end.toEpochMilli()

    /**
     * What the phone shows before saving: the times read in the phone's own zone and written in its language
     * (LocalizedFormats), not as ISO strings.
     */
    fun summary(res: Resources, zone: ZoneId = ZoneId.systemDefault(), today: LocalDate = LocalDate.now(zone)): String {
        val locale = res.configuration.locales[0]
        val from = start.atZone(zone)
        val to = end.atZone(zone)
        fun day(date: ZonedDateTime) = LocalizedFormats.formatter(
            if (date.year == today.year) LocalizedFormats.MONTH_DAY else LocalizedFormats.DATE, locale, zone).format(date)
        fun time(date: ZonedDateTime) = LocalizedFormats.formatter(LocalizedFormats.TIME, locale, zone).format(date)
        val span = buildString {
            if (allDay) {
                // The end date is exclusive: an event on Nov 1 ends at Nov 2 00:00.
                val last = to.minusDays(1).takeIf { it.toLocalDate().isAfter(from.toLocalDate()) }
                append(day(from))
                last?.let { append(" → ").append(day(it)) }
            } else {
                append(day(from)).append(' ').append(time(from))
                append(" → ")
                if (to.toLocalDate() != from.toLocalDate()) append(day(to)).append(' ')
                append(time(to))
            }
        }
        return listOfNotNull(
            res.getString(R.string.phonetools_calendar_new, title),
            if (allDay) res.getString(R.string.phonetools_calendar_all_day, span) else span,
            location?.let { res.getString(R.string.phonetools_calendar_location, it) },
            calendar?.let { res.getString(R.string.phonetools_calendar_calendar, it) },
            notes?.let { res.getString(R.string.phonetools_calendar_notes, if (it.length > 200) it.take(200) + "…" else it) },
        ).joinToString("\n")
    }

    companion object {

        fun parse(args: JsonObject, zone: ZoneId = ZoneId.systemDefault()): CalendarArguments {
            val title = args.text("title") ?: invalid("title is required")
            val start = parseTime(args.text("start"), zone) ?: invalid("start is required, as ISO 8601 with a timezone")
            val end = parseTime(args.text("end"), zone) ?: invalid("end is required, as ISO 8601 with a timezone")
            if (!end.isAfter(start)) invalid("end must be after start")
            return CalendarArguments(title, start, end, args.text("location"), args.text("notes"), args.bool("all_day"), args.text("calendar"))
        }
    }
}

/**
 * An ISO 8601 time with an offset, or a bare `yyyy-MM-dd` / `yyyy-MM-dd HH:mm` / `yyyy-MM-ddTHH:mm` read in the phone's
 * zone (iOS ToolDate.parse). Null for a missing value; an unreadable one is an argument error.
 */
internal fun parseTime(raw: String?, zone: ZoneId = ZoneId.systemDefault()): Instant? {
    val text = raw?.trim()?.takeIf { it.isNotEmpty() } ?: return null
    runCatching { return OffsetDateTime.parse(text).toInstant() }
    runCatching { return Instant.parse(text) }
    runCatching { return LocalDate.parse(text).atStartOfDay(zone).toInstant() }
    for (pattern in listOf("yyyy-MM-dd HH:mm", "yyyy-MM-dd'T'HH:mm", "yyyy-MM-dd'T'HH:mm:ss")) {
        runCatching { return LocalDateTime.parse(text, DateTimeFormatter.ofPattern(pattern, Locale.ROOT)).atZone(zone).toInstant() }
    }
    invalid("cannot read the time \"$raw\"; use ISO 8601 such as 2026-10-24T14:00:00+08:00")
}

/** ISO 8601 in the phone's zone with seconds and offset, as every tool documents (2026-10-24T14:00:00+08:00). */
internal fun iso(instant: Instant, zone: ZoneId = ZoneId.systemDefault()): String =
    instant.truncatedTo(ChronoUnit.SECONDS).atZone(zone).format(DateTimeFormatter.ISO_OFFSET_DATE_TIME)

internal fun iso(millis: Long): String = iso(Instant.ofEpochMilli(millis))

internal class CalendarTools(context: Context) {
    private val resolver = context.contentResolver

    fun events(args: JsonObject): PhoneToolResult {
        val zone = ZoneId.systemDefault()
        val start = parseTime(args.text("start"), zone) ?: LocalDate.now(zone).atStartOfDay(zone).toInstant()
        val end = parseTime(args.text("end"), zone) ?: start.atZone(zone).plusDays(7).toInstant()
        if (!end.isAfter(start)) invalid("end must be after start")
        val limit = args.int("limit", 50, 1, 200)
        val query = args.text("query")?.lowercase()
        val uri = Calendar.Instances.CONTENT_URI.buildUpon().also {
            ContentUris.appendId(it, start.toEpochMilli()); ContentUris.appendId(it, end.toEpochMilli())
        }.build()
        val rows = mutableListOf<JsonObject>()
        var truncated = false
        resolver.query(uri, arrayOf(Calendar.Instances.EVENT_ID, Calendar.Instances.TITLE, Calendar.Instances.BEGIN,
            Calendar.Instances.END, Calendar.Instances.ALL_DAY, Calendar.Instances.CALENDAR_DISPLAY_NAME,
            Calendar.Instances.EVENT_LOCATION, Calendar.Instances.DESCRIPTION, Calendar.Instances.OWNER_ACCOUNT), null, null,
            "${Calendar.Instances.BEGIN} ASC, ${Calendar.Instances.EVENT_ID} ASC")?.use { cursor ->
            while (cursor.moveToNext()) {
                val title = cursor.getString(1).orEmpty()
                val location = cursor.getString(6).orEmpty()
                val notes = cursor.getString(7).orEmpty()
                if (query != null && listOf(title, location, notes).none { it.lowercase().contains(query) }) continue
                if (rows.size >= limit) { truncated = true; break }
                val allDay = cursor.getInt(4) != 0
                rows += buildJsonObject {
                    put("id", cursor.getLong(0).toString())
                    if (title.isNotEmpty()) put("title", title)
                    put("start", eventTime(cursor.getLong(2), allDay, zone)); put("end", eventTime(cursor.getLong(3), allDay, zone))
                    put("all_day", allDay)
                    cursor.getString(5)?.takeIf { it.isNotEmpty() }?.let { put("calendar", it) }
                    if (location.isNotEmpty()) put("location", location)
                    if (notes.isNotEmpty()) put("notes", notes.take(300))
                    attendees(cursor.getLong(0), cursor.getString(8)).takeIf { it.isNotEmpty() }?.let { put("attendees", it) }
                }
            }
        } ?: unavailable("系统日历暂不可用 / The calendar provider is not available on this phone.")  // i18n-ignore: tool result for the model
        return PhoneToolResult.text(buildJsonObject {
            put("start", iso(start, zone)); put("end", iso(end, zone))
            put("count", rows.size); put("truncated", truncated); put("events", JsonArray(rows))
        }.toString())
    }

    /** All-day instances are UTC midnights; report them as the local date's midnight, as the iOS app does. */
    private fun eventTime(millis: Long, allDay: Boolean, zone: ZoneId): String =
        if (!allDay) iso(Instant.ofEpochMilli(millis), zone)
        else iso(Instant.ofEpochMilli(millis).atZone(ZoneOffset.UTC).toLocalDate().atStartOfDay(zone).toInstant(), zone)

    private fun attendees(eventId: Long, owner: String?) = buildJsonArray {
        var count = 0
        resolver.query(Calendar.Attendees.CONTENT_URI,
            arrayOf(Calendar.Attendees.ATTENDEE_NAME, Calendar.Attendees.ATTENDEE_EMAIL, Calendar.Attendees.ATTENDEE_RELATIONSHIP,
                Calendar.Attendees.ATTENDEE_TYPE, Calendar.Attendees.ATTENDEE_STATUS),
            "${Calendar.Attendees.EVENT_ID} = ?", arrayOf(eventId.toString()), null)?.use { cursor ->
            while (count++ < 100 && cursor.moveToNext()) add(buildJsonObject {
                cursor.getString(0)?.takeIf { it.isNotBlank() }?.let { put("name", it) }
                val email = cursor.getString(1)?.takeIf { it.isNotBlank() }
                email?.let { put("email", it) }
                put("role", attendeeRole(cursor.getInt(2), cursor.getInt(3)))
                put("status", attendeeStatus(cursor.getInt(4)))
                put("is_current_user", email != null && owner != null && email.equals(owner, ignoreCase = true))
            })
        }
    }

    fun create(args: JsonObject): PhoneToolResult {
        val event = CalendarArguments.parse(args)
        val (id, name) = writableCalendar(event.calendar)
        val values = ContentValues().apply {
            put(Calendar.Events.CALENDAR_ID, id); put(Calendar.Events.TITLE, event.title)
            put(Calendar.Events.DTSTART, event.startMillis()); put(Calendar.Events.DTEND, event.endMillis())
            put(Calendar.Events.EVENT_TIMEZONE, if (event.allDay) "UTC" else ZoneId.systemDefault().id)
            put(Calendar.Events.ALL_DAY, if (event.allDay) 1 else 0)
            put(Calendar.Events.EVENT_LOCATION, event.location); put(Calendar.Events.DESCRIPTION, event.notes)
        }
        val uri = resolver.insert(Calendar.Events.CONTENT_URI, values)
            ?: unavailable("日程没有保存 / The calendar did not take the event.")  // i18n-ignore: tool result for the model
        return PhoneToolResult.text(buildJsonObject {
            put("id", ContentUris.parseId(uri).toString()); put("title", event.title)
            put("start", iso(event.start)); put("end", iso(event.end))
            name?.takeIf { it.isNotEmpty() }?.let { put("calendar", it) }
        }.toString())
    }

    /**
     * The calendar named [wanted] (exact, then partial match, ignoring case), else the primary writable one, else the
     * first writable one. A phone with none gets a local Persona calendar instead of losing the event.
     */
    private fun writableCalendar(wanted: String?): Pair<Long, String?> {
        val calendars = mutableListOf<Pair<Long, String>>()
        resolver.query(Calendar.Calendars.CONTENT_URI, arrayOf(Calendar.Calendars._ID, Calendar.Calendars.CALENDAR_DISPLAY_NAME),
            "${Calendar.Calendars.CALENDAR_ACCESS_LEVEL} >= ?", arrayOf(Calendar.Calendars.CAL_ACCESS_CONTRIBUTOR.toString()),
            "${Calendar.Calendars.IS_PRIMARY} DESC, ${Calendar.Calendars._ID} ASC")?.use { cursor ->
            while (cursor.moveToNext()) calendars += cursor.getLong(0) to cursor.getString(1).orEmpty()
        } ?: unavailable("系统日历暂不可用 / The calendar provider is not available on this phone.")  // i18n-ignore: tool result for the model
        if (wanted != null) {
            val needle = wanted.lowercase()
            val match = calendars.firstOrNull { it.second.lowercase() == needle } ?: calendars.firstOrNull { it.second.lowercase().contains(needle) }
            return match ?: invalid("no calendar named \"$wanted\"; available calendars: ${calendars.map { it.second }.sorted().joinToString(", ")}")
        }
        calendars.firstOrNull()?.let { return it }
        val uri = Calendar.Calendars.CONTENT_URI.buildUpon()
            .appendQueryParameter(Calendar.CALLER_IS_SYNCADAPTER, "true")
            .appendQueryParameter(Calendar.Calendars.ACCOUNT_NAME, "Persona")
            .appendQueryParameter(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL).build()
        val values = ContentValues().apply {
            put(Calendar.Calendars.ACCOUNT_NAME, "Persona"); put(Calendar.Calendars.ACCOUNT_TYPE, Calendar.ACCOUNT_TYPE_LOCAL)
            put(Calendar.Calendars.NAME, "Persona"); put(Calendar.Calendars.CALENDAR_DISPLAY_NAME, "Persona")
            put(Calendar.Calendars.OWNER_ACCOUNT, "Persona"); put(Calendar.Calendars.CALENDAR_ACCESS_LEVEL, Calendar.Calendars.CAL_ACCESS_OWNER)
            put(Calendar.Calendars.CALENDAR_COLOR, LOCAL_CALENDAR_COLOR); put(Calendar.Calendars.VISIBLE, 1)
            put(Calendar.Calendars.SYNC_EVENTS, 1); put(Calendar.Calendars.CALENDAR_TIME_ZONE, ZoneId.systemDefault().id)
        }
        val created = resolver.insert(uri, values)
            ?: unavailable("这台手机上没有可写入的日历 / No writable calendar is configured on this phone.")  // i18n-ignore: tool result for the model
        return ContentUris.parseId(created) to "Persona"
    }

    private companion object {
        /** The local calendar's colour in the system calendar app (data, not app UI). */
        const val LOCAL_CALENDAR_COLOR = 0xff3f51b5.toInt()
    }
}

/** EventKit's participant roles (iOS): the organizer chairs; optional and resource attendees keep their kind. */
internal fun attendeeRole(relationship: Int, type: Int): String = when {
    relationship == Calendar.Attendees.RELATIONSHIP_ORGANIZER -> "chair"
    type == Calendar.Attendees.TYPE_OPTIONAL -> "optional"
    type == Calendar.Attendees.TYPE_RESOURCE -> "non_participant"
    type == Calendar.Attendees.TYPE_REQUIRED -> "required"
    else -> "unknown"
}

internal fun attendeeStatus(status: Int): String = when (status) {
    Calendar.Attendees.ATTENDEE_STATUS_ACCEPTED -> "accepted"
    Calendar.Attendees.ATTENDEE_STATUS_DECLINED -> "declined"
    Calendar.Attendees.ATTENDEE_STATUS_TENTATIVE -> "tentative"
    Calendar.Attendees.ATTENDEE_STATUS_INVITED -> "pending"
    else -> "unknown"
}
