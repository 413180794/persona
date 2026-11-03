import AVFoundation
import Contacts
import CoreLocation
import EventKit
import Foundation
import HealthKit
import Photos
import UserNotifications

// MARK: - Authorization

/// What the user has allowed for one group of phone data.
enum PhoneAuthStatus: String, Sendable {
    case notDetermined, granted, denied, limited, notNeeded

    var isUsable: Bool { self == .granted || self == .limited || self == .notNeeded }

    var title: String {
        switch self {
        case .notDetermined: "未授权"
        case .granted: "已授权"
        case .denied: "已拒绝"
        case .limited: "部分授权"
        case .notNeeded: "无需授权"
        }
    }
}

/// Everything the registry knows about permissions, so the settings screen and
/// the tools agree on one answer.
enum PhoneAuthorization {
    static func status(_ source: PhoneDataSource) -> PhoneAuthStatus {
        switch source {
        case .contacts: ContactsAccess.status()
        case .calendar: CalendarAccess.status()
        case .reminders: CalendarAccess.reminderStatus()
        case .location: LocationAccess.status()
        case .health: HealthAccess.cachedStatus()
        case .photos: PhotosAccess.status()
        case .notifications: NotificationsAccess.cachedStatus()
        case .device, .clipboard: .notNeeded
        }
    }

    static func request(_ source: PhoneDataSource) async -> PhoneAuthStatus {
        switch source {
        case .contacts: await ContactsAccess.request()
        case .calendar: await CalendarAccess.requestEvents()
        case .reminders: await CalendarAccess.requestReminders()
        case .location: await LocationAccess.request()
        case .health: await HealthAccess.request()
        case .photos: await PhotosAccess.request()
        case .notifications: await NotificationsAccess.request()
        case .device, .clipboard: .notNeeded
        }
    }
}

// MARK: - Registry

/// The catalogue of `phone_*` tools Claude can call, and the single entry point
/// the WebSocket layer uses to run one.
enum PhoneToolRegistry {
    /// Every tool, whatever the user's toggles say. Tests walk this list.
    static var all: [PhoneTool] {
        ContactsTool.tools
            + CalendarTool.tools
            + RemindersTool.tools
            + LocationTool.tools
            + healthTools
            + PhotosTool.tools
            + DeviceTool.tools
            + ClipboardTool.tools
            + NotificationsTool.tools
    }

    /// A Mac has HealthKit's API but no health data: no health tools there.
    private static var healthTools: [PhoneTool] {
        #if os(iOS)
        HealthTool.tools
        #else
        []
        #endif
    }

    /// The tool definitions announced to the hub in `register_phone`.
    static func enabledSpecs() -> [ToolSpec] {
        all.filter { $0.source.isActive }.map(\.spec)
    }

    /// Current permission state of one data source, for the settings screen.
    static func authorizationStatus(_ source: PhoneDataSource) -> PhoneAuthStatus {
        PhoneAuthorization.status(source)
    }

    /// Asks the system for access; the settings screen calls this from its
    /// toggle or button.
    static func requestAccess(_ source: PhoneDataSource) async -> PhoneAuthStatus {
        await PhoneAuthorization.request(source)
    }

    /// Runs one tool call. Never throws: failures come back as an error result
    /// with a message meant for Claude and for the transcript on the phone.
    static func call(
        name: String,
        args: JSONValue,
        confirm: (String) async -> Bool
    ) async -> (content: [ToolContent], isError: Bool) {
        guard let tool = all.first(where: { $0.spec.name == name }) else {
            return ([.text("未知工具 / Unknown tool: \(name). 可用工具 / Available tools: \(enabledSpecs().map(\.name).joined(separator: ", ")).")], true)
        }
        guard tool.source.isEnabled else {
            return ([.text("“\(tool.source.title)” 已在手机上关闭 / The \(tool.source.rawValue) data source is turned off on the phone. "
                + "The user can enable it in Persona › 设置 › 连接器 › 这台 iPhone.")], true)
        }
        if let summary = tool.confirmation?(args) {
            let allowed = await confirm(summary)
            guard allowed else {
                return ([.text("The user declined on the phone.")], true)
            }
        }
        do {
            let content = try await tool.run(args)
            return (content.isEmpty ? [.text("完成 / Done.")] : content, false)
        } catch {
            return ([.text(ToolErrors.describe(error))], true)
        }
    }
}
