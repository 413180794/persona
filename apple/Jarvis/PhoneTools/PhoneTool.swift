import Foundation

/// A phone capability Claude can call through the hub (`phone_*` MCP tools).
struct PhoneTool {
    var spec: ToolSpec
    var source: PhoneDataSource
    /// Writes (creating events, reminders, …) ask the user in the app before running.
    var confirmation: ((JSONValue) -> String)?
    var run: (JSONValue) async throws -> [ToolContent]
}

/// Groups of tools the user can switch on or off in Settings › 连接器 › 这台 iPhone (was 手机数据).
enum PhoneDataSource: String, CaseIterable, Identifiable, Codable {
    case contacts, calendar, reminders, location, health, photos, device, clipboard, notifications

    var id: String { rawValue }

    /// The sources this device has tools for, in order (设置 › 手机数据).
    static var onThisDevice: [PhoneDataSource] {
        #if os(macOS)
        allCases.filter { $0 != .health } // no health data on a Mac (PhoneToolRegistry.healthTools)
        #else
        allCases
        #endif
    }

    var title: String {
        switch self {
        case .contacts: "通讯录"
        case .calendar: "日历"
        case .reminders: "提醒事项"
        case .location: "位置"
        case .health: "健康"
        case .photos: "照片"
        case .device: "设备信息"
        case .clipboard: "剪贴板"
        case .notifications: "通知"
        }
    }

    var symbol: String {
        switch self {
        case .contacts: "person.crop.circle"
        case .calendar: "calendar"
        case .reminders: "checklist"
        case .location: "location"
        case .health: "heart"
        case .photos: "photo.on.rectangle"
        case .device: "iphone"
        case .clipboard: "doc.on.clipboard"
        case .notifications: "bell.badge"
        }
    }

    var detail: String {
        switch self {
        case .contacts: "查找联系人的电话、邮箱、公司"
        case .calendar: "查看日程，经你确认后新建日程"
        case .reminders: "查看和新建提醒事项"
        case .location: "当前位置和地址"
        case .health: "步数、心率、睡眠、活动能量等汇总"
        case .photos: "最近的照片和截图"
        case .device: "电量、存储、系统版本、网络"
        case .clipboard: "读取和写入剪贴板"
        case .notifications: "让 Claude 给你发本地通知"
        }
    }

    private var defaultsKey: String { "phoneSource.\(rawValue)" }

    var isEnabled: Bool {
        get { UserDefaults.standard.object(forKey: defaultsKey) as? Bool ?? true }
        nonmutating set { UserDefaults.standard.set(newValue, forKey: defaultsKey) }
    }
}

enum PhoneToolError: LocalizedError {
    case denied(String)
    case invalidArgument(String)
    case unavailable(String)

    var errorDescription: String? {
        switch self {
        case .denied(let s): "没有权限：\(s)。请在 iPhone 的 Jarvis › 设置 › 连接器 › 这台 iPhone 里授权。"
        case .invalidArgument(let s): "参数错误：\(s)"
        case .unavailable(let s): s
        }
    }
}
