package jarvis.platform.phonetools

import jarvis.core.contracts.api.PhoneToolSpec
import kotlinx.serialization.json.*

enum class PhoneSource(val title: String, val detail: String) {
    Contacts("通讯录", "搜索姓名、公司、电话和邮箱"),
    Calendar("日历", "查看日程，新建日程须逐次确认"),
    Location("位置", "仅在前台获取位置，不依赖 Google 服务"),
    Photos("照片", "读取照片和视频，可只授权部分照片"),
    Clipboard("剪贴板", "只能在 App 打开时读写"),
    Device("设备信息", "系统、电量、存储和网络"),
    Notifications("通知", "发送本地通知，查看 Jarvis 当前通知"),
}

internal data class ToolDefinition(
    val spec: PhoneToolSpec, val source: PhoneSource, val foregroundOnly: Boolean = false,
    val confirmation: Boolean = false,
)

internal object PhoneToolCatalog {
    private fun string(max: Int = 4000) = buildJsonObject { put("type", "string"); put("maxLength", max) }
    private fun integer(min: Int, max: Int) = buildJsonObject {
        put("type", "integer"); put("minimum", min); put("maximum", max)
    }
    private val boolean = buildJsonObject { put("type", "boolean") }
    private fun tool(
        name: String, description: String, source: PhoneSource,
        properties: Map<String, JsonObject> = emptyMap(), required: List<String> = emptyList(),
        foreground: Boolean = false, confirmation: Boolean = false,
    ) = ToolDefinition(PhoneToolSpec(name, description, buildJsonObject {
        put("type", "object"); put("properties", JsonObject(properties)); put("additionalProperties", false)
        put("required", JsonArray(required.map(::JsonPrimitive)))
    }), source, foreground, confirmation)

    val all = listOf(
        tool("phone_contacts_search", "搜索 Android 通讯录的姓名、公司、职位、电话和邮箱。", PhoneSource.Contacts,
            mapOf("query" to string(256), "limit" to integer(1, 100)), listOf("query")),
        tool("phone_calendar_events", "读取日历事件，默认今天至七天后；时间使用带时区的 ISO 8601。", PhoneSource.Calendar,
            mapOf("start" to string(64), "end" to string(64), "query" to string(256), "limit" to integer(1, 100))),
        tool("phone_calendar_create_event", "在手机确认后新建日程；没有可写日历时创建 Jarvis 本地日历。", PhoneSource.Calendar,
            mapOf("title" to string(200), "start" to string(64), "end" to string(64), "location" to string(500),
                "notes" to string(), "all_day" to boolean, "calendar" to string(200)),
            listOf("title", "start", "end"), confirmation = true),
        tool("phone_location_current", "仅在前台获取当前位置；标明坐标系，地址可能不可用。", PhoneSource.Location, foreground = true),
        tool("phone_photos_recent", "读取已授权照片和视频的元数据，可能只有部分照片；不读取媒体位置。", PhoneSource.Photos,
            mapOf("limit" to integer(1, 100), "days" to integer(1, 3650), "screenshots_only" to boolean)),
        tool("phone_photo_get", "按最近照片返回的 id 获取缩放 JPEG；视频返回预览图。", PhoneSource.Photos,
            mapOf("id" to string(100), "max_size" to integer(1, 2048)), listOf("id")),
        tool("phone_device_info", "读取 Android 系统、电量、存储、网络、省电与热状态。", PhoneSource.Device),
        tool("phone_clipboard_get", "仅在前台读取剪贴板文字，部分系统另有剪贴板权限。", PhoneSource.Clipboard, foreground = true),
        tool("phone_clipboard_set", "仅在前台覆盖剪贴板文字。", PhoneSource.Clipboard,
            mapOf("text" to string(16000)), listOf("text"), foreground = true),
        tool("phone_notify", "发送 Jarvis 本地通知，可延迟至一天后；系统调度不保证准点。", PhoneSource.Notifications,
            mapOf("title" to string(200), "body" to string(2000), "delay_seconds" to integer(0, 86400)), listOf("title", "body")),
        tool("phone_notifications_list", "只列出 Jarvis 当前显示的通知，不读取其他 App 的通知或系统提醒事项。", PhoneSource.Notifications,
            mapOf("limit" to integer(1, 100))),
    )

    fun validate(tool: ToolDefinition, args: JsonObject) {
        val schema = tool.spec.inputSchema
        val properties = schema.getValue("properties").jsonObject
        require(args.keys.all { it in properties }) { "包含不支持的参数。" }
        schema.getValue("required").jsonArray.forEach { key -> require(args.containsKey(key.jsonPrimitive.content)) { "缺少参数：${key.jsonPrimitive.content}" } }
        args.forEach { (key, value) ->
            val definition = properties.getValue(key).jsonObject
            val primitive = value as? JsonPrimitive ?: error("参数 $key 格式不正确。")
            val valid = when (definition.getValue("type").jsonPrimitive.content) {
                "string" -> primitive.isString && primitive.content.length <= definition.getValue("maxLength").jsonPrimitive.int
                "integer" -> !primitive.isString && primitive.intOrNull?.let {
                    it in definition.getValue("minimum").jsonPrimitive.int..definition.getValue("maximum").jsonPrimitive.int
                } == true
                "boolean" -> !primitive.isString && primitive.booleanOrNull != null
                else -> false
            }
            require(valid) { "参数 $key 格式或范围不正确。" }
        }
    }
}

internal fun JsonObject.string(key: String): String? = (get(key) as? JsonPrimitive)?.contentOrNull
internal fun JsonObject.int(key: String, default: Int) = (get(key) as? JsonPrimitive)?.intOrNull ?: default
internal fun JsonObject.bool(key: String) = (get(key) as? JsonPrimitive)?.booleanOrNull ?: false
