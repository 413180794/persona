import SwiftUI

/// A Mac's VNC on the whole screen (#1343), where it can be operated: its picture, the touches as the mode says (Mouse /
/// Trackpad), the clipboard both ways, the keyboard with a row of the Mac's shortcuts, Help, and Picture in Picture back to picture in
/// picture. When the agent whose chat opened it waits on the user, 「我完成了」 hands the Mac back (closing does too).
struct VNCPage: View {
    @Environment(AppModel.self) private var model
    let ref: MachineRef
    var unlock = false
    var from: SessionKey?
    var onClose: () -> Void
    @State private var session: RFBSession?
    @State private var mode: VNCMode = .mouse
    @State private var help = false
    @State private var typing = false
    @State private var unlocked = false
    #if os(iOS) || os(visionOS)
    @State private var screenView: VNCScreenUIView?
    @State private var modifiers = VNCModifiers()
    @State private var pipLog = false
    #endif

    private var name: String { model.machine(ref)?.name ?? ref.id }
    /// The agent that opened it waits on the user: there is something to hand back.
    private var waiting: Bool { from.flatMap { model.session($0) }?.state == .needsYou }

    var body: some View {
        VStack(spacing: 0) {
            topBar
            TimelineView(.periodic(from: .now, by: 1)) { ctx in
                if let a = model.computerActivities[ref], a.shown(at: ctx.date) { stepLine(a) }
            }
            ZStack {
                Color.black
                if let s = session { content(s) }
            }
            #if os(iOS) || os(visionOS)
            // Over the keyboard while it is up (the bars ride above it).
            if typing, let s = session { VNCShortcutBar(session: s, modifiers: modifiers) }
            #endif
            bottomBar
        }
        .background(Color.black.ignoresSafeArea())
        .environment(\.colorScheme, .dark)
        .onAppear {
            session = VNC.hold(ref)
            mode = VNCMode.of(ref)
        }
        .onDisappear {
            #if os(iOS)
            VNCPiP.shared.pageGone(ref)
            #endif
            VNC.release(ref)
        }
        .onChange(of: session?.phase) { _, phase in
            #if os(iOS)
            // The small window ready (#1839): Picture in Picture starts it in the tap, and going home starts it by itself.
            if phase == .live { VNCPiP.shared.prepare(ref, from: from, automatic: true) }
            #endif
            if phase == .live, unlock, !unlocked {
                unlocked = true
                session?.unlockWithKeptPassword()
            }
        }
        .sheet(isPresented: $help) { VNCHelp() }
        #if os(iOS)
        .sheet(isPresented: $pipLog) { VNCPiPDiagnostics() }
        #endif
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("screen-drawer")
    }

    // MARK: Bars

    private var topBar: some View {
        HStack(spacing: Theme.Space.s) {
            Button { close() } label: {
                Image(systemName: Theme.Icon.close).font(Theme.Font.labelStrong).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
            }
            .themedButton()
            .accessibilityLabel("关闭画面")
            .accessibilityIdentifier("screen-close")
            Spacer(minLength: Theme.Space.s)
            HStack(spacing: Theme.Space.xs) {
                Circle().fill(dot).scaledFrame(width: Theme.Size.dot, height: Theme.Size.dot)
                Text(name).font(Theme.Font.bodyStrong).lineLimit(1)
                Text(phaseLabel).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
            .accessibilityElement(children: .combine)
            .accessibilityIdentifier("screen-phase")
            Spacer(minLength: Theme.Space.s)
            Button { help = true } label: { Image(systemName: Theme.Icon.help).font(Theme.Font.labelStrong) }
                .themedButton()
                .accessibilityLabel("帮助")
                .accessibilityIdentifier("vnc-help")
            moreMenu
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.s)
    }

    private var moreMenu: some View {
        Menu {
            #if os(iOS) || os(visionOS)
            // On the Mac updateNSView never reads the mode; the picker would only change the iPhone's setting (#3893).
            Picker("模式", selection: Binding(get: { mode }, set: { mode = $0; VNCMode.set($0, for: ref) })) {
                Label("鼠标模式", systemImage: Theme.Icon.pointer).tag(VNCMode.mouse)
                Label("触控板模式", systemImage: Theme.Icon.trackpad).tag(VNCMode.trackpad)
            }
            #endif
            Button { centerPointer() } label: { Label("指针居中", systemImage: Theme.Icon.pointerCenter) }
            #if os(iOS) || os(visionOS)
            if VNCPiP.supported {
                Button { toPictureInPicture() } label: { Label("小窗", systemImage: Theme.Icon.pictureInPicture) }
            }
            Button { pipLog = true } label: { Label("小窗诊断", systemImage: Theme.Icon.help) }
            #else
            Button { toPictureInPicture() } label: { Label("小窗", systemImage: Theme.Icon.pictureInPicture) }
            #endif
            #if DEBUG
            if let s = session { Text(String(format: "%.0f 帧/秒", s.fps) + (s.latency.map { String(format: " · %.0f 毫秒", $0) } ?? "")) } // i18n-ignore
            #endif
        } label: {
            Image(systemName: Theme.Icon.more).font(Theme.Font.labelStrong)
        }
        .themedButton()
        .accessibilityLabel("更多")
        .accessibilityIdentifier("vnc-more")
    }

    private var bottomBar: some View {
        HStack(spacing: Theme.Space.m) {
            Menu {
                Button { Task { await pasteFromPhone(andPaste: true) } } label: { Label(Self.fromPhone, systemImage: Theme.Icon.paste) }
                Button { Task { await pasteFromPhone(andPaste: false) } } label: { Label("只放进 Mac 的剪贴板", systemImage: Theme.Icon.clipboard) }
                Button { Task { await copyToPhone() } } label: { Label(Self.toPhone, systemImage: Theme.Icon.copy) }
            } label: {
                Image(systemName: Theme.Icon.clipboard).font(Theme.Font.heading).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
            }
            .themedButton()
            .accessibilityLabel("剪贴板")
            .accessibilityIdentifier("vnc-clipboard")
            Spacer(minLength: Theme.Space.s)
            if waiting {
                Button { handBack() } label: {
                    Label("我完成了", systemImage: Theme.Icon.handBack).font(Theme.Font.labelStrong).lineLimit(1)
                }
                .themedButton(prominent: true)
                .accessibilityIdentifier("vnc-done")
                Spacer(minLength: Theme.Space.s)
            }
            #if os(iOS) || os(visionOS)
            Button { typing.toggle() } label: {
                Image(systemName: Theme.Icon.keyboard).font(Theme.Font.heading).scaledFrame(width: Theme.Size.icon, height: Theme.Size.icon)
            }
            .themedButton(prominent: typing)
            .accessibilityLabel("键盘")
            .accessibilityIdentifier("vnc-keyboard")
            #endif
        }
        .padding(.horizontal, Theme.Space.page)
        .padding(.vertical, Theme.Space.s)
        #if os(iOS) || os(visionOS)
        .background { if let s = session { VNCKeyCatcher(session: s, modifiers: modifiers, typing: $typing).frame(width: Theme.Size.hairline, height: Theme.Size.hairline) } }
        #endif
    }

    #if os(iOS) || os(visionOS)
    static let fromPhone = String(localized: "从 iPhone 粘贴"), toPhone = String(localized: "复制到 iPhone")
    #else
    static let fromPhone = String(localized: "从这台 Mac 粘贴"), toPhone = String(localized: "复制到这台 Mac")
    #endif

    private func stepLine(_ a: ComputerActivity) -> some View {
        Text("正在：\(a.step)").font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Theme.Space.page)
            .padding(.bottom, Theme.Space.xs)
            .accessibilityIdentifier("screen-step")
    }

    // MARK: Content

    @ViewBuilder private func content(_ s: RFBSession) -> some View {
        switch s.phase {
        case .live:
            #if os(iOS) || os(visionOS)
            VNCScreen(session: s, mode: mode, interactive: true) { v in DispatchQueue.main.async { screenView = v } }
            #else
            VNCScreen(session: s, mode: mode, interactive: true)
            #endif
        case .signIn(let failed):
            VNCSignIn(name: name, account: Self.account(model.machine(ref)?.home), failed: failed) { account, password, remember in
                s.signIn(account: account, password: password, remember: remember)
            }
        case .connecting:
            VStack(spacing: Theme.Space.s) {
                ProgressView()
                Text("正在连接 \(name) 的屏幕…").font(Theme.Font.secondary).foregroundStyle(.secondary)
            }
        case .closed(let why):
            VStack(spacing: Theme.Space.m) {
                Text(why ?? String(localized: "已断开。")).font(Theme.Font.secondary).foregroundStyle(.secondary).multilineTextAlignment(.center)
                Button("重新连接") { s.start() }.themedButton(prominent: true).lineLimit(1)
            }
            .padding(Theme.Space.xl)
        }
    }

    /// The Mac account's short name, from its home directory (/Users/<name>).
    static func account(_ home: String?) -> String {
        guard let home, home.hasPrefix("/Users/") else { return "" }
        return String(home.dropFirst("/Users/".count).split(separator: "/").first ?? "")
    }

    private var dot: Color {
        switch session?.phase {
        case .live: Theme.positive
        case .closed: Theme.danger
        default: Theme.caution
        }
    }

    private var phaseLabel: String {
        switch session?.phase {
        case .live: String(localized: "已连接")
        case .signIn: String(localized: "要登录")
        case .closed: String(localized: "没连上")
        default: String(localized: "连接中")
        }
    }

    // MARK: Actions

    private func centerPointer() {
        #if os(iOS) || os(visionOS)
        screenView?.centerPointer()
        #else
        if let s = session { s.move(to: CGPoint(x: s.size.width / 2, y: s.size.height / 2)) }
        #endif
    }

    private func toPictureInPicture() {
        VNCPiP.shared.start(ref, from: from)
        #if os(macOS)
        onClose()
        #endif
        // iOS: the page goes once the small window has started (VNCPiP), or stays, saying why it could not.
    }

    private func handBack() {
        guard let from else { return }
        Task { _ = await model.send(from, text: String(localized: "我在电脑上操作完了，你继续吧。")) }
        onClose()
    }

    private func close() {
        if waiting { handBack() } else { onClose() }
    }

    private func pasteFromPhone(andPaste: Bool) async {
        #if os(iOS) || os(visionOS)
        let text = UIPasteboard.general.string
        #else
        let text = NSPasteboard.general.string(forType: .string)
        #endif
        guard let text, !text.isEmpty, let s = session else {
            model.toast = Toast(String(localized: "剪贴板里没有文字"), style: .error)
            return
        }
        let ok = andPaste ? await s.paste(text) : await s.setMacClipboard(text)
        model.toast = ok ? Toast(andPaste ? String(localized: "已粘贴到 \(name)") : String(localized: "已放进 \(name) 的剪贴板"))
            : Toast(andPaste ? String(localized: "没能写进 \(name) 的剪贴板，没有粘贴") : String(localized: "没能写进 \(name) 的剪贴板"), style: .error)
    }

    private func copyToPhone() async {
        guard let s = session, let text = await s.readMacClipboard(), !text.isEmpty else {
            model.toast = Toast(String(localized: "\(name) 的剪贴板里没有文字"), style: .error)
            return
        }
        #if os(iOS) || os(visionOS)
        UIPasteboard.general.string = text
        #else
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #endif
        model.toast = Toast(String(localized: "已复制 \(name) 的剪贴板"))
    }
}

/// The Mac's sign-in for its Screen Sharing, when this device keeps none (or the kept one was refused).
private struct VNCSignIn: View {
    let name: String
    @State var account: String
    let failed: String?
    let submit: (String, String, Bool) -> Void
    @State private var password = ""
    @State private var remember = true

    init(name: String, account: String, failed: String?, submit: @escaping (String, String, Bool) -> Void) {
        self.name = name
        _account = State(initialValue: account)
        self.failed = failed
        self.submit = submit
    }

    var body: some View {
        VStack(alignment: .leading, spacing: Theme.Space.m) {
            Text("登录这台 Mac 的屏幕共享").font(Theme.Font.heading)
            Text(remember ? String(localized: "用 \(name) 上的 macOS 账号和密码。只发给那台 Mac；记住的话存在这台设备的钥匙串里，不上传。")
                 : String(localized: "用 \(name) 上的 macOS 账号和密码。只发给那台 Mac，不保存。"))
                .font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            if let failed {
                Text(failed).font(Theme.Font.label).foregroundStyle(Theme.danger).fixedSize(horizontal: false, vertical: true)
                    .accessibilityIdentifier("vnc-signin-failed")
            }
            TextField("账号", text: $account)
                .textContentType(.username)
                #if os(iOS) || os(visionOS)
                .textInputAutocapitalization(.never)
                #endif
                .autocorrectionDisabled()
                .textFieldStyle(.roundedBorder)
                .accessibilityIdentifier("vnc-account")
            SecureField("密码", text: $password)
                .textContentType(.password)
                .textFieldStyle(.roundedBorder)
                .onSubmit(go)
                .accessibilityIdentifier("vnc-password")
            Toggle(ScreenKeep.label, isOn: $remember).font(Theme.Font.secondary).accessibilityIdentifier("vnc-remember")
            Button(action: go) { Text("连接").font(Theme.Font.labelStrong).lineLimit(1).frame(maxWidth: .infinity) }
                .themedButton(prominent: true)
                .disabled(account.trimmingCharacters(in: .whitespaces).isEmpty || password.isEmpty)
                .accessibilityIdentifier("vnc-connect")
        }
        .padding(Theme.Space.l)
        .card()
        .padding(Theme.Space.l)
        .frame(maxWidth: Theme.Size.box380)
    }

    private func go() {
        let a = account.trimmingCharacters(in: .whitespaces)
        guard !a.isEmpty, !password.isEmpty else { return }
        submit(a, password, remember)
        password = ""
    }
}

/// 「在这台手机上记住」 on the phone, 「在这台 Mac 上记住」 on the Mac.
enum ScreenKeep {
    #if os(macOS)
    static let label = String(localized: "在这台 Mac 上记住")
    #else
    static let label = String(localized: "在这台手机上记住")
    #endif
}

/// Help: how the touches, the keyboard and the clipboard work, and handing the Mac back.
struct VNCHelp: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            List {
                Section("移动操作") {
                    #if !os(macOS)
                    row(Theme.Icon.pointer, String(localized: "鼠标模式（默认）"), String(localized: "轻点在那里点击；单指拖动就是拖拽；两指拖动滚动；两指轻点或按住不放是右键。"))
                    row(Theme.Icon.search, String(localized: "放大"), String(localized: "双指开合放大、缩小。放大以后，两指拖动是移动画面，不再滚动。"))
                    #else
                    row(Theme.Icon.pointer, String(localized: "鼠标"), String(localized: "点按就是左键；右键点按（或按住 Control 再点按）是右键；按住鼠标键不放再移动就是拖拽。"))
                    row(Theme.Icon.trackpad, String(localized: "滚动"), String(localized: "拨鼠标滚轮，或在妙控鼠标、触控板上双指滑动来滚动画面。"))
                    row(Theme.Icon.search, String(localized: "放大"), String(localized: "Mac 上没有放大手势，画面始终完整显示。"))
                    #endif
                }
                Section("输入和剪贴板") {
                    #if !os(macOS)
                    row(Theme.Icon.keyboard, String(localized: "打字"), String(localized: "点底栏的键盘。键盘上面一排是 Mac 的快捷键：⌘ ⌥ ⌃ ⇧ 点一下按住、再点松开，还有 Esc、Tab、方向键和 ⌘C ⌘V ⌘Z ⌘Tab ⌘空格。中文照常输入，上屏的字经剪贴板粘贴过去。"))
                    #else
                    row(Theme.Icon.keyboard, String(localized: "打字"), String(localized: "直接用这台 Mac 的键盘输入，文字、⌘ ⌥ ⌃ ⇧ 和快捷键都原样送到画面里的 Mac。"))
                    #endif
                    row(Theme.Icon.clipboard, String(localized: "复制和粘贴"), String(localized: "点底栏的剪贴板：「\(VNCPage.fromPhone)」把这边的文字放进 Mac 的剪贴板并粘贴；「\(VNCPage.toPhone)」把 Mac 剪贴板里的文字拿过来。"))
                }
                #if !os(macOS)
                Section("何时用触控板模式") {
                    row(Theme.Icon.trackpad, String(localized: "触控板模式"), String(localized: "从右上角「…」里打开。手指在屏幕上移动指针；轻点在指针处点击；轻点后马上拖动是拖拽。指针跑偏了用「指针居中」。"))
                }
                #endif
                Section("小窗") {
                    row(Theme.Icon.pictureInPicture, String(localized: "小窗"), String(localized: "「…」里的「小窗」把画面放进画中画，切到别的 App 或回到桌面也浮着；点小窗回到这里。小窗只能看，关掉小窗就断开。"))
                }
                Section("交还电脑") {
                    row(Theme.Icon.handBack, String(localized: "我完成了"), String(localized: "Agent 等你在电脑上操作时，做完点底栏的「我完成了」，它就接着做。直接关掉画面也算交还。"))
                }
            }
            .navigationTitle("使用 VNC")
            #if os(iOS) || os(visionOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button { dismiss() } label: { Image(systemName: Theme.Icon.close) }.accessibilityIdentifier("vnc-help-close")
                }
            }
        }
        .accessibilityIdentifier("vnc-help-page")
    }

    private func row(_ icon: String, _ title: String, _ text: String) -> some View {
        Label {
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(title).font(Theme.Font.body)
                Text(text).font(Theme.Font.label).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
            }
        } icon: {
            Image(systemName: icon).foregroundStyle(Theme.accent)
        }
    }
}

extension RFBSession {
    /// One-Tap Unlock (#1507 a-2): a Shift wakes the lock screen and puts the cursor in its password field, then the kept
    /// password and Return; the hub only notes that it was unlocked.
    func unlockWithKeptPassword() {
        guard let kept = ScreenCredentials.load(ref) else { return }
        press(Self.shift)
        Task {
            try? await Task.sleep(for: .milliseconds(800))
            for ch in kept.password.unicodeScalars { press(Self.keysym(ch)) }
            press(VNCKeys.enter)
            _ = try? await AppModel.shared.api(ref.group)?.computerJob(machine: ref.id, "unlocking")
        }
    }
}
