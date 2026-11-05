import Observation
import SwiftUI
import UserNotifications

/// 保存在 AppModel 的系统入口。动作先领取再发送，外观和窗口重建只重画，不重放写操作。
@Observable final class ExtensionSystemOpen: Identifiable {
    let id = UUID()
    let link: ExtensionSystemLink
    let action: ExtensionNotifyAction?
    let values: [String: String]
    private(set) var consumed = false

    init(link: ExtensionSystemLink, action: ExtensionNotifyAction? = nil, values: [String: String] = [:]) {
        self.link = link
        self.action = action
        self.values = values
    }

    func start(_ page: ExtensionViewModel) async -> ExtensionViewModel.Outcome {
        var outcome = ExtensionViewModel.Outcome()
        if !consumed, let action {
            consumed = true
            outcome = await page.perform(ExtensionAction(id: action.id), with: values)
        }
        if page.view == nil, !page.popped, !Task.isCancelled { await page.load() }
        return outcome
    }
}

extension AppModel {
    @discardableResult func openExtension(_ link: ExtensionSystemLink, action: ExtensionNotifyAction? = nil,
                                         values: [String: String] = [:]) -> Bool {
        guard api(link.group) != nil else {
            toast = Toast(String(localized: "这个扩展所属的 hub 已断开，未执行操作"), style: .error)
            return false
        }
        extensionOpen = ExtensionSystemOpen(link: link, action: action, values: values)
        return true
    }
}

/// 共用普通扩展页面及其链接、push/pop、失效刷新，不另造通知专用渲染器。
private struct ExtensionSystemPresentation: ViewModifier {
    @Environment(AppModel.self) private var app

    func body(content: Content) -> some View {
        if let route = app.extensionOpen {
            NavigationStack {
                Group {
                    if let ui = route.link.ui {
                        ExtensionPageView(plugin: route.link.owner, ui: ui, title: String(localized: "扩展"),
                                          state: route.link.state, group: route.link.group, initial: route)
                    } else {
                        EmptyStateView(symbol: Theme.Icon.plugins, title: String(localized: "没有可打开的页面"),
                                       message: String(localized: "这个扩展没有提供完整页面。"))
                    }
                }
                .toolbar {
                    ToolbarItem(placement: .confirmationAction) {
                        Button("完成") { app.extensionOpen = nil }.lineLimit(1)
                    }
                }
            }
            .environment(app)
        } else { content }
    }
}

extension View {
    func extensionSystemPresentation() -> some View { modifier(ExtensionSystemPresentation()) }
}

@MainActor enum ExtensionNotificationHandler {
    /// 只保留短期的回调去重；相同推送的另一个按钮仍是独立的用户操作。
    private struct DeliveryKey: Hashable {
        var group: String
        var notification: String
        var action: String?
    }
    private static var handled: [DeliveryKey: Date] = [:]

    struct Deps {
        var group: (ExtensionNotificationEvent) -> String?
        var api: (String) -> APIClient?
        var open: (ExtensionSystemLink, ExtensionNotifyAction?, [String: String]) -> Void
        var report: (String, String, String) async -> Void
    }

    static func live(_ model: AppModel) -> Deps {
        Deps(group: { event in
            var mapping: [String: String] = [:]
            if let id = event.hubID, let group = model.registry.group(hubID: id) { mapping[id] = group }
            return event.group(hubGroups: mapping, groups: model.groupIDs)
        }, api: { model.api($0) }, open: { link, action, values in
            model.openExtension(link, action: action, values: values)
        }, report: { id, title, body in
            let content = UNMutableNotificationContent()
            content.title = title
            content.body = body
            try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "extension-result-" + id,
                                                                                   content: content, trigger: nil))
        })
    }

    static func handle(_ event: ExtensionNotificationEvent, deps: Deps, deduplicate: Bool = true) async {
        guard !Task.isCancelled, !event.dismissed, event.valid else { return }
        guard let group = deps.group(event), let api = deps.api(group) else {
            await deps.report(event.notificationID, String(localized: "未执行扩展操作"), String(localized: "找不到通知所属的 hub，请重新连接后在 App 中操作。"))
            return
        }
        let key = DeliveryKey(group: group, notification: event.notificationID, action: event.action?.id)
        if deduplicate {
            handled = handled.filter { $0.value > Date.now.addingTimeInterval(-300) }
            guard handled[key] == nil else { return }
            handled[key] = .now
        }
        let route = ExtensionSystemLink(owner: event.payload.extension, ui: event.payload.ui,
                                        group: group, state: event.payload.state)
        if event.action == nil || event.action?.foreground == true {
            deps.open(route, event.action, event.values)
            return
        }
        guard let ui = event.payload.ui, let action = event.action else { return }
        do {
            let result = try await api.pluginUI(event.payload.extension, ui, context: .here(), state: event.payload.state,
                                                action: action.id, values: event.values.isEmpty ? nil : event.values)
            guard !Task.isCancelled else { return }
            // 后台不处理 view、open_url、push 或 pop；只有文字反馈作为本地通知。
            if let toast = result.toast, !toast.isEmpty { await deps.report(event.notificationID, String(localized: "扩展操作已完成"), toast) }
        } catch {
            if !error.isCancellation {
                await deps.report(event.notificationID, String(localized: "扩展操作未完成"), error.localizedDescription)
            }
        }
    }
}
