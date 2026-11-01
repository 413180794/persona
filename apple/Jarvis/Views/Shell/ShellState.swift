import SwiftUI

/// The ChatGPT-style shell on an iPhone (docs/ios.md「新外壳」): 助手 (the Jarvis assistant) and Agents (the Agents list)
/// under one top switch, the drawer on the left (收件箱, 定时任务, 知识库, 机器, the conversations, 设置), 设置 as a sheet.
/// `旧布局` (AppModel.oldLayout) brings the four tabs back unchanged.
///
/// The pages read this from the environment (`\.shell`): nil means they are in the old layout, on an iPad or on the
/// Mac, and look exactly as before.
@MainActor @Observable final class ShellState {
    /// The drawer is the shell's (it used to be the Jarvis tab's own): it slides both pages aside.
    let drawer = AssistantDrawer()
    /// 助手 and Agents side by side: a drag sideways moves from one to the other (`ShellPager`).
    let pager = ShellPager()
    /// What the drawer or the home page asked the 助手 page to show; `serial` makes the same request count twice.
    private(set) var request: (page: ShellPage, serial: Int)?
    /// Each page says whether it is at its root: the drawer's drag only opens there (pushed pages swipe back).
    var chatAtRoot = true
    var workAtRoot = true

    func show(_ page: ShellPage) {
        request = (page, (request?.serial ?? 0) + 1)
    }
}

/// Pages the 助手 page pushes for the shell.
enum ShellPage: Hashable, Sendable {
    case newConversation, profile, inbox, routines, knowledge, machines
}

/// What the 助手 page pushes by value for the shell (the inbox and the machines push values of their own on top).
enum ShellRoute: Hashable, Sendable {
    case inbox, machines
}

extension EnvironmentValues {
    @Entry var shell: ShellState?
}
