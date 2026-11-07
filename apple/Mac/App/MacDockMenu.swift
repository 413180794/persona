import AppKit

/// Right-click on the Dock icon: the Agents that need you and the ones just done (the menu bar list's first and last
/// sections, `MacMenuBar.sections`), each opening its chat, then New Agent Chat. The system adds Options / Show All Windows / Quit.
@MainActor final class MacDockMenu: NSObject {
    static let shared = MacDockMenu()

    func menu(_ model: AppModel) -> NSMenu {
        // Same quiet-task filter (#1731) as the menu bar: its agents are the main conversation's to tell.
        menu(MacMenuBar.sections(MacMenuBar.menuSessions(model.allSessions, quiet: { model.isQuiet($0) }),
                                 muted: { model.isMuted($0) }),
             machineName: { id in model.machines.first { $0.id == id }?.name ?? id }, paired: model.isPaired)
    }

    func menu(_ s: MacMenuBar.Sections, machineName: (String) -> String, paired: Bool) -> NSMenu {
        let menu = NSMenu()
        menu.autoenablesItems = false
        add(s.needsYou, header: StateName.needsYou.text, to: menu, machineName: machineName)
        add(s.unread, header: StateName.unread.text, to: menu, machineName: machineName)
        if !s.needsYou.isEmpty || !s.unread.isEmpty { menu.addItem(.separator()) }
        let new = NSMenuItem(title: String(localized: "New Agent Chat…"), action: #selector(newAgent), keyEquivalent: "")
        new.target = self
        new.isEnabled = paired
        menu.addItem(new)
        return menu
    }

    private func add(_ sessions: [Session], header: String, to menu: NSMenu, machineName: (String) -> String) {
        guard !sessions.isEmpty else { return }
        menu.addItem(.sectionHeader(title: header))
        for s in sessions.prefix(8) {
            let machine = machineName(s.machineID)
            let item = NSMenuItem(title: "\(s.displayTitle) · \(machine)", action: #selector(open(_:)), keyEquivalent: "")
            item.target = self
            item.representedObject = s.key
            menu.addItem(item)
        }
    }

    @objc private func open(_ item: NSMenuItem) {
        guard let key = item.representedObject as? SessionKey else { return }
        MacShellState.shared.showMain() // the main window may have been closed
        MacShellState.shared.open(key, AppModel.shared)
    }

    @objc private func newAgent() {
        MacShellState.shared.showMain()
        MacShellState.shared.newAgent = true
    }
}
