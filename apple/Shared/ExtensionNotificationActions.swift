import Foundation
import UserNotifications

/// 通知服务扩展与 App 共用的按钮定义，只解码与注册，不读取凭据或执行动作。
nonisolated enum ExtensionNotificationActions {
    static func payload(_ info: [AnyHashable: Any]) -> ExtensionNotification? {
        guard let raw = info["extension_notification"] as? [String: Any],
              let data = try? JSONSerialization.data(withJSONObject: raw),
              (try? JSONDecoder().decode(Shape.self, from: data)) != nil,
              let payload = try? JSONDecoder().decode(ExtensionNotification.self, from: data),
              segment(payload.extension), payload.ui.map(segment) ?? true else { return nil }
        return payload
    }

    static func category(identifier: String, payload: ExtensionNotification) -> UNNotificationCategory? {
        let prefix = "ext." + payload.extension + "."
        guard identifier.hasPrefix(prefix), identifier.count > prefix.count, identifier.utf8.count <= 256,
              let actions = payload.actions, !actions.isEmpty, actions.count <= 3,
              Set(actions.map(\.id)).count == actions.count else { return nil }
        var buttons: [UNNotificationAction] = []
        for action in actions {
            guard !action.id.isEmpty, action.id.count <= 128,
                  ![UNNotificationDefaultActionIdentifier, UNNotificationDismissActionIdentifier].contains(action.id),
                  !action.title.isEmpty, action.title.count <= 40,
                  action.role == nil || ["normal", "destructive"].contains(action.role!) else { return nil }
            var options: UNNotificationActionOptions = []
            if action.role == "destructive" { options.insert(.destructive) }
            if action.foreground == true { options.formUnion([.foreground, .authenticationRequired]) }
            if let input = action.input {
                buttons.append(UNTextInputNotificationAction(identifier: action.id, title: action.title, options: options,
                    textInputButtonTitle: String(localized: "发送"), textInputPlaceholder: input.placeholder ?? String(localized: "输入内容")))
            } else {
                buttons.append(UNNotificationAction(identifier: action.id, title: action.title, options: options))
            }
        }
        return UNNotificationCategory(identifier: identifier, actions: buttons, intentIdentifiers: [], options: [])
    }

    static func register(content: UNNotificationContent) async {
        guard let payload = payload(content.userInfo), category(identifier: content.categoryIdentifier, payload: payload) != nil else { return }
        await ExtensionNotificationCategoryRegistry.shared.register(payload: payload, identifier: content.categoryIdentifier)
    }

    static func preserveAndRegister(_ make: @escaping @Sendable () -> Set<UNNotificationCategory>) async {
        await ExtensionNotificationCategoryRegistry.shared.mergeMade(make)
    }

    static func action(_ id: String, payload: ExtensionNotification) -> ExtensionNotifyAction? {
        guard id != UNNotificationDefaultActionIdentifier, id != UNNotificationDismissActionIdentifier else { return nil }
        return payload.actions?.first { $0.id == id }
    }

    /// 生成类型对新增字段采用宽容解码；通知的执行标志必须严格验类型，不能把错误的 foreground 当作 false。
    private struct Shape: Decodable {
        var `extension`: String
        var ui: String?
        var state: String?
        var actions: [ActionShape]?
    }
    private struct ActionShape: Decodable {
        var id: String
        var title: String
        var role: String?
        var foreground: Bool?
        var input: InputShape?
    }
    private struct InputShape: Decodable { var placeholder: String? }

    private static func segment(_ value: String) -> Bool {
        let characters = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-")
        return !value.isEmpty && value.count <= 128 && value != "." && value != ".."
            && value.unicodeScalars.allSatisfy(characters.contains)
    }
}

/// 合并操作在单个进程内串行；保留系统已注册的静态类别和其他扩展的类别。
private actor ExtensionNotificationCategoryRegistry {
    static let shared = ExtensionNotificationCategoryRegistry()
    private var known: Set<UNNotificationCategory>?

    func register(payload: ExtensionNotification, identifier: String) async {
        guard let category = ExtensionNotificationActions.category(identifier: identifier, payload: payload) else { return }
        await merge([category])
    }

    func mergeMade(_ make: @Sendable () -> Set<UNNotificationCategory>) async {
        await merge(make())
    }

    private func merge(_ additions: Set<UNNotificationCategory>) async {
        // SDK 对象只在 actor 内取得和创建，不把通知中心或类别从调用方转移进来。
        let center = UNUserNotificationCenter.current()
        let existing = await center.notificationCategories()
        var categories: [String: UNNotificationCategory] = [:]
        for category in known ?? [] { categories[category.identifier] = category }
        // 再读系统集合，接住通知服务扩展刚注册的类别；同名项以本次定义为准。
        for category in existing { categories[category.identifier] = category }
        for category in additions { categories[category.identifier] = category }
        let merged = Set(categories.values)
        known = merged
        center.setNotificationCategories(merged)
    }
}
