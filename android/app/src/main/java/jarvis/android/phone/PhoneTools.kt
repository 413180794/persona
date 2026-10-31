package jarvis.android.phone

import android.Manifest
import android.annotation.SuppressLint
import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.ClipData
import android.content.ClipboardManager
import android.content.ContentValues
import android.content.Context
import android.content.pm.PackageManager
import android.location.Geocoder
import android.location.Location
import android.location.LocationManager
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager
import android.os.Build
import android.os.Environment
import android.os.StatFs
import android.provider.CalendarContract
import android.provider.ContactsContract
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import jarvis.android.R
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.add
import kotlinx.serialization.json.addJsonObject
import kotlinx.serialization.json.buildJsonArray
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonObject
import java.time.Instant
import java.time.LocalDate
import java.time.OffsetDateTime
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.TimeZone
import kotlin.coroutines.resume

/**
 * phone_* tools (docs/phone-tools.md): Claude calls them through the hub, which sends a WebSocket `tool_call` to
 * the phone that registered last. The names, descriptions and parameters are the iOS app's
 * (apple/Jarvis/PhoneTools), so a call reads the same whichever phone answers, and one phone registering doesn't
 * change what the other's tools look like to Claude. Each data source is off until the user turns it on.
 */
enum class Source(val title: String, val detail: String, val permissions: List<String>) {
    Device("设备信息", "机型、系统版本、电量、存储、网络", emptyList()),
    Clipboard("剪贴板", "读写剪贴板里的文字", emptyList()),
    Notifications("通知", "在手机上弹一条通知", if (Build.VERSION.SDK_INT >= 33) listOf(Manifest.permission.POST_NOTIFICATIONS) else emptyList()),
    Contacts("通讯录", "按姓名、公司、邮箱、电话搜索联系人", listOf(Manifest.permission.READ_CONTACTS)),
    Calendar("日历", "看日程；新建日程前先在手机上确认", listOf(Manifest.permission.READ_CALENDAR, Manifest.permission.WRITE_CALENDAR)),
    Location("位置", "一次性定位和地址", listOf(Manifest.permission.ACCESS_COARSE_LOCATION, Manifest.permission.ACCESS_FINE_LOCATION)),
    Photos("照片", "看最近的照片和截图，按需读出一张给 Claude 看", photoPermissions()),
}

/** Reading the gallery: Android 13+ asks for images and videos apart; older ones for all of storage. */
private fun photoPermissions(): List<String> =
    if (Build.VERSION.SDK_INT >= 33) listOf(Manifest.permission.READ_MEDIA_IMAGES, Manifest.permission.READ_MEDIA_VIDEO)
    else listOf(Manifest.permission.READ_EXTERNAL_STORAGE)

/** What a tool returns: MCP content blocks plus whether it's an error (docs/protocol.md `tool_result`). */
data class ToolOutput(val content: JsonArray, val isError: Boolean = false) {
    companion object {
        fun text(t: String, error: Boolean = false) = ToolOutput(buildJsonArray { addJsonObject { put("type", "text"); put("text", t) } }, error)
        fun json(o: JsonElement) = text(prettyJson.encodeToString(JsonElement.serializer(), o))
        /** A text block and an image the model sees (proto.Content: `data` base64, `mime_type`). */
        fun image(description: JsonElement, jpeg: ByteArray) = ToolOutput(buildJsonArray {
            addJsonObject { put("type", "text"); put("text", prettyJson.encodeToString(JsonElement.serializer(), description)) }
            addJsonObject { put("type", "image"); put("data", android.util.Base64.encodeToString(jpeg, android.util.Base64.NO_WRAP)); put("mime_type", "image/jpeg") }
        })
    }
}

private val prettyJson = kotlinx.serialization.json.Json { prettyPrint = true }

class ToolFailure(message: String) : Exception(message)

class PhoneTool(
    val name: String,
    val description: String,
    val schema: JsonObject,
    val source: Source,
    /** For writes: what the confirmation card says; the tool only runs after the user allows it on the phone. */
    val confirm: ((JsonObject) -> String)? = null,
    val run: suspend (Context, JsonObject) -> ToolOutput,
)

object PhoneTools {
    val all: List<PhoneTool> = listOf(
        PhoneTool(
            "phone_device_info",
            "Describe the phone: model identifier and name, iOS version, battery percentage and charging state, low power mode, free and total storage in bytes, current network type (wifi/cellular/wired/offline), timezone and locale. Cheap and safe to call when you need context about the device.",
            schema(), Source.Device,
        ) { c, _ -> deviceInfo(c) },
        PhoneTool(
            "phone_clipboard_get",
            "Read the phone's clipboard as text. iOS may show an \"Allow Paste?\" prompt the first time, which the user has to confirm. Returns \"text\" and its character count, or text: null when the clipboard is empty or holds something that is not text.",
            schema(), Source.Clipboard,
        ) { c, _ -> clipboardGet(c) },
        PhoneTool(
            "phone_clipboard_set",
            "Replace the phone's clipboard with the given text, for example a link or a draft the user asked for. Overwrites whatever was there.",
            schema("text" to str("Text to place on the clipboard."), required = listOf("text")), Source.Clipboard,
        ) { c, a -> clipboardSet(c, a) },
        PhoneTool(
            "phone_notify",
            "Post a notification on the phone. Use it to tell the user about something that happened while they were not looking (a long task finished, a build failed). delay_seconds schedules it a few seconds or minutes later; omit it to notify immediately. The first call asks for notification permission. Returns whether the notification was scheduled.",
            schema(
                "title" to str("Notification title, short."), "body" to str("Notification body."),
                "delay_seconds" to int("Seconds from now to deliver it. Default 0 (immediately), max 86400.", 0, 86_400),
                required = listOf("title", "body"),
            ), Source.Notifications,
        ) { c, a -> notify(c, a) },
        PhoneTool(
            "phone_contacts_search",
            "Search the phone's contacts by name, company, job title, email or phone number. Returns up to `limit` people with their name, company, job title, phone numbers (with labels such as mobile/work), email addresses and birthday (yyyy-MM-dd). Use this to look up a colleague's number before calling or drafting a message.",
            schema(
                "query" to str("Text to match against name, company, job title, email or phone number. Matching is case-insensitive and looks for the text anywhere in the field; a phone number may contain spaces or dashes."),
                "limit" to int("Maximum number of contacts to return, default 10, max 50.", 1, 50),
                required = listOf("query"),
            ), Source.Contacts,
        ) { c, a -> contactsSearch(c, a) },
        PhoneTool(
            "phone_calendar_events",
            "List calendar events in a time window. Defaults to the next 7 days starting at 00:00 today. Times are ISO 8601 with a timezone offset (e.g. 2026-10-24T14:00:00+08:00). Returns each event's id, title, start, end, all-day flag, calendar name, location, the first 300 characters of notes and the attendees. Use it to check whether the user is free.",
            schema(
                "start" to str("Window start, ISO 8601 (e.g. 2026-10-24T00:00:00+08:00). Default: 00:00 today."),
                "end" to str("Window end, ISO 8601. Default: 7 days after start."),
                "query" to str("Only return events whose title, location or notes contain this text."),
                "limit" to int("Maximum number of events to return, default 50, max 200.", 1, 200),
            ), Source.Calendar,
        ) { c, a -> calendarEvents(c, a) },
        PhoneTool(
            "phone_calendar_create_event",
            "Create a calendar event. `start` and `end` are ISO 8601 with a timezone (e.g. 2026-10-24T14:00:00+08:00). For an all-day event pass all_day=true and dates at 00:00. The user must approve the event on the phone before it is saved. Returns the new event's identifier.",
            schema(
                "title" to str("Event title."), "start" to str("Start time, ISO 8601 with a timezone."),
                "end" to str("End time, ISO 8601 with a timezone. Must be after start."),
                "location" to str("Optional location."), "notes" to str("Optional notes."),
                "all_day" to bool("Mark the event as all-day. Default false."),
                "calendar" to str("Calendar name (title) to put the event in. Default: the default calendar for new events."),
                required = listOf("title", "start", "end"),
            ), Source.Calendar,
            confirm = { a ->
                val fmt = DateTimeFormatter.ofPattern("M月d日 HH:mm")
                val s = runCatching { parseTime(a.s("start")!!).atZone(ZoneId.systemDefault()).format(fmt) }.getOrDefault(a.s("start"))
                val e = runCatching { parseTime(a.s("end")!!).atZone(ZoneId.systemDefault()).format(DateTimeFormatter.ofPattern("HH:mm")) }.getOrDefault(a.s("end"))
                "新建日程「${a.s("title")}」$s → $e" + (a.s("location")?.let { " · $it" } ?: "")
            },
        ) { c, a -> calendarCreate(c, a) },
        PhoneTool(
            "phone_location_current",
            "Get the phone's current location. Returns latitude, longitude, horizontal accuracy in meters, altitude, the reverse-geocoded address (country, province, city, district, street, formatted address), the device timezone and the timestamp of the fix (ISO 8601). Waits at most 15 seconds for a fix. Only call this when the user asks something that needs their location.",
            schema(), Source.Location,
        ) { c, _ -> location(c) },
        PhoneTool(
            "phone_photos_recent",
            "List recent photos and videos, newest first. Each row has the Photos local identifier, creation date (ISO 8601), media type (image/video), pixel width and height, whether it is a screenshot, whether it is a favorite, duration in seconds for videos, and the latitude/longitude where the photo was taken when it is geotagged. Pass an id to phone_photo_get to actually see the image.",
            schema(
                "limit" to int("Maximum number of assets, default 20, max 100.", 1, 100),
                "days" to int("Only assets from the last N days. Default: no limit.", 1, 3650),
                "screenshots_only" to bool("Only screenshots. Default false."),
            ), Source.Photos,
        ) { c, a -> Photos.recent(c, a) },
        PhoneTool(
            "phone_photo_get",
            "Get one photo as an image so you can look at it (read a screenshot, describe a photo, transcribe a whiteboard). Pass the id from phone_photos_recent. The image is JPEG, at most max_size pixels on the long edge (default 1280, max 2048), quality 0.7. Returns the image plus a short text description.",
            schema(
                "id" to str("Photos local identifier from phone_photos_recent."),
                "max_size" to int("Longest edge in pixels, default 1280, max 2048.", 256, 2048),
                required = listOf("id"),
            ), Source.Photos,
        ) { c, a -> Photos.get(c, a) },
    )

    fun byName(name: String) = all.firstOrNull { it.name == name }

    /** MCP tool definitions for `register_phone`: only the sources the user turned on. */
    fun specs(enabled: Set<Source>): JsonArray = buildJsonArray {
        all.filter { it.source in enabled }.forEach { t ->
            addJsonObject { put("name", t.name); put("description", t.description); put("input_schema", t.schema) }
        }
    }

    fun granted(c: Context, s: Source) = s.permissions.all { ContextCompat.checkSelfPermission(c, it) == PackageManager.PERMISSION_GRANTED }
}

// --- schema helpers ---

private fun schema(vararg props: Pair<String, JsonObject>, required: List<String> = emptyList()) = buildJsonObject {
    put("type", "object")
    putJsonObject("properties") { props.forEach { (k, v) -> put(k, v) } }
    if (required.isNotEmpty()) put("required", buildJsonArray { required.forEach { add(it) } })
}
private fun str(d: String) = buildJsonObject { put("type", "string"); put("description", d) }
private fun bool(d: String) = buildJsonObject { put("type", "boolean"); put("description", d) }
private fun int(d: String, min: Int, max: Int) = buildJsonObject { put("type", "integer"); put("description", d); put("minimum", min); put("maximum", max) }

fun JsonObject.s(k: String): String? = (this[k] as? JsonPrimitive)?.takeIf { it.isString }?.content?.takeIf { it.isNotBlank() }
fun JsonObject.i(k: String, default: Int, min: Int, max: Int): Int = ((this[k] as? JsonPrimitive)?.intOrNull ?: default).coerceIn(min, max)
fun JsonObject.b(k: String): Boolean = (this[k] as? JsonPrimitive)?.booleanOrNull ?: false

/** ISO 8601 with an offset (the tools' contract); a bare date means its 00:00 in the phone's zone. */
fun parseTime(s: String): Instant =
    runCatching { OffsetDateTime.parse(s).toInstant() }.getOrNull()
        ?: runCatching { Instant.parse(s) }.getOrNull()
        ?: runCatching { LocalDate.parse(s).atStartOfDay(ZoneId.systemDefault()).toInstant() }.getOrNull()
        ?: throw ToolFailure("not an ISO 8601 time with a timezone: $s")

fun isoLocal(t: Instant): String = t.atZone(ZoneId.systemDefault()).toOffsetDateTime().format(DateTimeFormatter.ISO_OFFSET_DATE_TIME)

internal fun need(c: Context, s: Source) {
    if (!PhoneTools.granted(c, s)) throw ToolFailure("没有权限：${s.title}。请在安卓手机的 Jarvis › 设置 › 手机数据 里授权。\nPermission denied: ${s.name}. Grant access in Jarvis › Settings › Phone data on the Android phone.")
}

// --- tools ---

private fun deviceInfo(c: Context): ToolOutput {
    val bm = c.getSystemService(BatteryManager::class.java)
    val stat = StatFs(Environment.getDataDirectory().path)
    val cm = c.getSystemService(ConnectivityManager::class.java)
    val caps = cm.getNetworkCapabilities(cm.activeNetwork)
    val net = when {
        caps == null -> "offline"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cellular"
        caps.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "wired"
        else -> "other"
    }
    val power = c.getSystemService(android.os.PowerManager::class.java)
    return ToolOutput.json(buildJsonObject {
        put("model", "${Build.MANUFACTURER} ${Build.MODEL}")
        put("system", "Android ${Build.VERSION.RELEASE} (API ${Build.VERSION.SDK_INT})")
        put("battery_percent", bm.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY))
        put("charging", bm.isCharging)
        put("low_power_mode", power.isPowerSaveMode)
        put("storage_free_bytes", stat.availableBytes)
        put("storage_total_bytes", stat.totalBytes)
        put("network", net)
        put("timezone", TimeZone.getDefault().id)
        put("locale", java.util.Locale.getDefault().toLanguageTag())
    })
}

private suspend fun clipboardGet(c: Context): ToolOutput = withContext(Dispatchers.Main) {
    // Android 10+ only lets the app in front read the clipboard: in the background this is empty.
    val cm = c.getSystemService(ClipboardManager::class.java)
    val text = cm.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(c)?.toString()
    ToolOutput.json(buildJsonObject {
        if (text == null) put("text", JsonPrimitive(null as String?)) else { put("text", text); put("characters", text.length) }
        if (text == null) put("note", "Android only lets the app read the clipboard while it is open on screen; ask the user to open Jarvis if this should not be empty.")
    })
}

private suspend fun clipboardSet(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.Main) {
    val text = a.s("text") ?: throw ToolFailure("text is required")
    c.getSystemService(ClipboardManager::class.java).setPrimaryClip(ClipData.newPlainText("Jarvis", text))
    ToolOutput.json(buildJsonObject { put("ok", true); put("characters", text.length) })
}

private const val CH_TOOL = "tool"

@SuppressLint("MissingPermission")
private suspend fun notify(c: Context, a: JsonObject): ToolOutput {
    need(c, Source.Notifications)
    val title = a.s("title") ?: throw ToolFailure("title is required")
    val body = a.s("body") ?: throw ToolFailure("body is required")
    val delay = a.i("delay_seconds", 0, 0, 86_400)
    c.getSystemService(NotificationManager::class.java)
        .createNotificationChannel(NotificationChannel(CH_TOOL, "Claude 发来的通知", NotificationManager.IMPORTANCE_DEFAULT))
    val n = NotificationCompat.Builder(c, CH_TOOL).setSmallIcon(R.drawable.ic_stat).setContentTitle(title).setContentText(body)
        .setStyle(NotificationCompat.BigTextStyle().bigText(body)).setAutoCancel(true).build()
    val id = (title + body + System.nanoTime()).hashCode()
    if (delay == 0) NotificationManagerCompat.from(c).notify(id, n)
    // Kept simple: the foreground service keeps the process alive, so a delayed post is a coroutine, not an alarm.
    else jarvis.android.JarvisApp.instance.scope.launch { delay(delay * 1000L); NotificationManagerCompat.from(c).notify(id, n) }
    return ToolOutput.json(buildJsonObject { put("scheduled", true); put("delay_seconds", delay) })
}

private suspend fun contactsSearch(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.IO) {
    need(c, Source.Contacts)
    val query = a.s("query") ?: throw ToolFailure("query is required")
    val limit = a.i("limit", 10, 1, 50)
    val digits = query.filter { it.isDigit() }
    val cr = c.contentResolver
    // Every contact's data rows once, grouped by contact: fine for a phone's address book, and matches phone numbers
    // with spaces or dashes in them (CONTENT_FILTER_URI does names only).
    val people = linkedMapOf<Long, MutableMap<String, Any>>()
    cr.query(
        ContactsContract.Data.CONTENT_URI,
        arrayOf(ContactsContract.Data.CONTACT_ID, ContactsContract.Data.MIMETYPE, ContactsContract.Data.DATA1, ContactsContract.Data.DATA2, ContactsContract.Data.DATA4, ContactsContract.Contacts.DISPLAY_NAME),
        null, null, null,
    )?.use { cur ->
        while (cur.moveToNext()) {
            val id = cur.getLong(0)
            val p = people.getOrPut(id) { mutableMapOf("name" to (cur.getString(5) ?: ""), "phones" to mutableListOf<Pair<String, String>>(), "emails" to mutableListOf<Pair<String, String>>()) }
            val v = cur.getString(2) ?: continue
            @Suppress("UNCHECKED_CAST")
            when (cur.getString(1)) {
                ContactsContract.CommonDataKinds.Phone.CONTENT_ITEM_TYPE ->
                    (p["phones"] as MutableList<Pair<String, String>>) += ContactsContract.CommonDataKinds.Phone.getTypeLabel(c.resources, cur.getInt(3), "").toString() to v
                ContactsContract.CommonDataKinds.Email.CONTENT_ITEM_TYPE ->
                    (p["emails"] as MutableList<Pair<String, String>>) += ContactsContract.CommonDataKinds.Email.getTypeLabel(c.resources, cur.getInt(3), "").toString() to v
                ContactsContract.CommonDataKinds.Organization.CONTENT_ITEM_TYPE -> { p["company"] = v; cur.getString(4)?.let { p["job_title"] = it } }
                ContactsContract.CommonDataKinds.Event.CONTENT_ITEM_TYPE -> if (cur.getInt(3) == ContactsContract.CommonDataKinds.Event.TYPE_BIRTHDAY) p["birthday"] = v
            }
        }
    }
    @Suppress("UNCHECKED_CAST")
    val hits = people.values.filter { p ->
        val fields = listOfNotNull(p["name"], p["company"], p["job_title"]).map { it.toString() } +
            (p["emails"] as List<Pair<String, String>>).map { it.second }
        fields.any { it.contains(query, ignoreCase = true) } ||
            (digits.length >= 3 && (p["phones"] as List<Pair<String, String>>).any { it.second.filter(Char::isDigit).contains(digits) })
    }.take(limit)
    ToolOutput.json(buildJsonObject {
        put("query", query); put("count", hits.size)
        put("results", buildJsonArray {
            @Suppress("UNCHECKED_CAST")
            hits.forEach { p ->
                addJsonObject {
                    put("name", p["name"].toString())
                    (p["company"] as? String)?.let { put("company", it) }
                    (p["job_title"] as? String)?.let { put("job_title", it) }
                    put("phones", buildJsonArray { (p["phones"] as List<Pair<String, String>>).forEach { (l, n) -> addJsonObject { put("label", l); put("number", n) } } })
                    put("emails", buildJsonArray { (p["emails"] as List<Pair<String, String>>).forEach { (l, e) -> addJsonObject { put("label", l); put("address", e) } } })
                    (p["birthday"] as? String)?.let { put("birthday", it) }
                }
            }
        })
    })
}

private suspend fun calendarEvents(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.IO) {
    need(c, Source.Calendar)
    val zone = ZoneId.systemDefault()
    val start = a.s("start")?.let(::parseTime) ?: LocalDate.now(zone).atStartOfDay(zone).toInstant()
    val end = a.s("end")?.let(::parseTime) ?: start.plusSeconds(7 * 86_400L)
    val limit = a.i("limit", 50, 1, 200)
    val query = a.s("query")?.lowercase()
    val uri = CalendarContract.Instances.CONTENT_URI.buildUpon().also {
        android.content.ContentUris.appendId(it, start.toEpochMilli()); android.content.ContentUris.appendId(it, end.toEpochMilli())
    }.build()
    val rows = mutableListOf<JsonObject>()
    var more = false
    c.contentResolver.query(
        uri,
        arrayOf(CalendarContract.Instances.EVENT_ID, CalendarContract.Instances.TITLE, CalendarContract.Instances.BEGIN, CalendarContract.Instances.END,
            CalendarContract.Instances.ALL_DAY, CalendarContract.Instances.CALENDAR_DISPLAY_NAME, CalendarContract.Instances.EVENT_LOCATION, CalendarContract.Instances.DESCRIPTION),
        null, null, CalendarContract.Instances.BEGIN + " ASC",
    )?.use { cur ->
        while (cur.moveToNext()) {
            val title = cur.getString(1).orEmpty(); val loc = cur.getString(6).orEmpty(); val notes = cur.getString(7).orEmpty()
            if (query != null && listOf(title, loc, notes).none { it.lowercase().contains(query) }) continue
            if (rows.size >= limit) { more = true; break }
            val eventId = cur.getLong(0)
            rows += buildJsonObject {
                put("id", eventId.toString()); put("title", title)
                put("start", isoLocal(Instant.ofEpochMilli(cur.getLong(2)))); put("end", isoLocal(Instant.ofEpochMilli(cur.getLong(3))))
                put("all_day", cur.getInt(4) == 1); put("calendar", cur.getString(5).orEmpty())
                if (loc.isNotBlank()) put("location", loc)
                if (notes.isNotBlank()) put("notes", notes.take(300))
                put("attendees", attendees(c, eventId))
            }
        }
    }
    ToolOutput.json(buildJsonObject {
        put("start", isoLocal(start)); put("end", isoLocal(end)); put("count", rows.size); put("truncated", more)
        put("events", JsonArray(rows))
    })
}

private fun attendees(c: Context, eventId: Long): JsonArray = buildJsonArray {
    c.contentResolver.query(
        CalendarContract.Attendees.CONTENT_URI, arrayOf(CalendarContract.Attendees.ATTENDEE_NAME, CalendarContract.Attendees.ATTENDEE_EMAIL),
        CalendarContract.Attendees.EVENT_ID + " = ?", arrayOf(eventId.toString()), null,
    )?.use { cur -> while (cur.moveToNext()) addJsonObject { cur.getString(0)?.takeIf { it.isNotBlank() }?.let { put("name", it) }; cur.getString(1)?.let { put("email", it) } } }
}

private suspend fun calendarCreate(c: Context, a: JsonObject): ToolOutput = withContext(Dispatchers.IO) {
    need(c, Source.Calendar)
    val title = a.s("title") ?: throw ToolFailure("title is required")
    val start = parseTime(a.s("start") ?: throw ToolFailure("start is required, as ISO 8601 with a timezone"))
    val end = parseTime(a.s("end") ?: throw ToolFailure("end is required, as ISO 8601 with a timezone"))
    if (!end.isAfter(start)) throw ToolFailure("end must be after start")
    // The calendar by name, else the primary writable one.
    val want = a.s("calendar")
    var calId: Long? = null
    c.contentResolver.query(
        CalendarContract.Calendars.CONTENT_URI,
        arrayOf(CalendarContract.Calendars._ID, CalendarContract.Calendars.CALENDAR_DISPLAY_NAME, CalendarContract.Calendars.IS_PRIMARY, CalendarContract.Calendars.CALENDAR_ACCESS_LEVEL),
        CalendarContract.Calendars.CALENDAR_ACCESS_LEVEL + " >= ?", arrayOf(CalendarContract.Calendars.CAL_ACCESS_CONTRIBUTOR.toString()), null,
    )?.use { cur ->
        var first: Long? = null; var primary: Long? = null
        while (cur.moveToNext()) {
            val id = cur.getLong(0); if (first == null) first = id
            if (want != null && cur.getString(1) == want) calId = id
            if (cur.getInt(2) == 1) primary = id
        }
        if (calId == null) calId = primary ?: first
    }
    val cal = calId ?: throw ToolFailure("no writable calendar on this phone")
    val v = ContentValues().apply {
        put(CalendarContract.Events.CALENDAR_ID, cal); put(CalendarContract.Events.TITLE, title)
        put(CalendarContract.Events.DTSTART, start.toEpochMilli()); put(CalendarContract.Events.DTEND, end.toEpochMilli())
        put(CalendarContract.Events.EVENT_TIMEZONE, if (a.b("all_day")) "UTC" else TimeZone.getDefault().id)
        put(CalendarContract.Events.ALL_DAY, if (a.b("all_day")) 1 else 0)
        a.s("location")?.let { put(CalendarContract.Events.EVENT_LOCATION, it) }
        a.s("notes")?.let { put(CalendarContract.Events.DESCRIPTION, it) }
    }
    val uri = c.contentResolver.insert(CalendarContract.Events.CONTENT_URI, v) ?: throw ToolFailure("the calendar did not take the event")
    ToolOutput.json(buildJsonObject { put("id", uri.lastPathSegment.orEmpty()); put("calendar_id", cal.toString()); put("title", title) })
}

@SuppressLint("MissingPermission")
private suspend fun location(c: Context): ToolOutput {
    need(c, Source.Location)
    val lm = c.getSystemService(LocationManager::class.java)
    val provider = listOf(LocationManager.GPS_PROVIDER, LocationManager.NETWORK_PROVIDER).firstOrNull { lm.isProviderEnabled(it) }
        ?: throw ToolFailure("location is turned off on the phone")
    val fix: Location = withTimeoutOrNull(15_000) {
        suspendCancellableCoroutine { cont ->
            if (Build.VERSION.SDK_INT >= 30) {
                lm.getCurrentLocation(provider, null, c.mainExecutor) { l -> if (cont.isActive) cont.resume(l) }
            } else {
                @Suppress("DEPRECATION")
                lm.requestSingleUpdate(provider, { l -> if (cont.isActive) cont.resume(l) }, android.os.Looper.getMainLooper())
            }
        }
    } ?: lm.getLastKnownLocation(provider) ?: throw ToolFailure("no location fix within 15 seconds")
    val address = runCatching {
        @Suppress("DEPRECATION")
        Geocoder(c).getFromLocation(fix.latitude, fix.longitude, 1)?.firstOrNull()
    }.getOrNull()
    return ToolOutput.json(buildJsonObject {
        put("latitude", fix.latitude); put("longitude", fix.longitude)
        put("accuracy_meters", fix.accuracy.toDouble()); put("altitude_meters", fix.altitude)
        put("timestamp", isoLocal(Instant.ofEpochMilli(fix.time))); put("timezone", TimeZone.getDefault().id)
        address?.let { ad ->
            putJsonObject("address") {
                ad.countryName?.let { put("country", it) }; ad.adminArea?.let { put("province", it) }; ad.locality?.let { put("city", it) }
                ad.subLocality?.let { put("district", it) }; ad.thoroughfare?.let { put("street", it) }; ad.getAddressLine(0)?.let { put("formatted", it) }
            }
        }
    })
}
