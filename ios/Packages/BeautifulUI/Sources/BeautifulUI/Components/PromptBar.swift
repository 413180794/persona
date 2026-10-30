import Observation
import SwiftUI

/// Something `@` can pull into the prompt: a data source, an app, or the attach action.
public struct PromptSource: Identifiable, Sendable {
    public enum Icon: Sendable {
        case glyph(BUIGlyph)
        case mark(BUIAvatar, size: CGSize)
    }

    public enum Kind: Sendable {
        case source
        /// Picking it asks the host to add an attachment instead of inserting a mention.
        case attach
        /// Shows a Connect / Connected action.
        case connectable
    }

    public var id: String
    public var name: String
    public var detail: String
    public var icon: Icon
    public var kind: Kind

    public init(id: String, name: String, detail: String, icon: Icon, kind: Kind = .source) {
        self.id = id
        self.name = name
        self.detail = detail
        self.icon = icon
        self.kind = kind
    }
}

/// A `/` command.
public struct PromptCommand: Identifiable, Sendable {
    public var id: String { name }
    /// Including the slash, e.g. "/compare".
    public var name: String
    public var detail: String

    public init(_ name: String, detail: String) {
        self.name = name
        self.detail = detail
    }
}

/// A model in the picker.
public struct PromptModel: Identifiable, Sendable {
    public var id: String
    public var name: String
    public var tag: String

    public init(id: String, name: String, tag: String) {
        self.id = id
        self.name = name
        self.tag = tag
    }
}

/// The `@word` or `/word` being typed at the end of a draft.
public struct PromptToken: Equatable, Sendable {
    public enum Kind: Sendable { case mention, command }
    public var kind: Kind
    public var query: String
    /// Offset of the `@` or `/` in the draft.
    public var start: Int

    /// Matches `(^|\s)([@/])([\w-]*)$` like the original.
    public static func parse(_ draft: String) -> PromptToken? {
        let chars = Array(draft)
        var i = chars.count
        while i > 0, chars[i - 1].isLetter || chars[i - 1].isNumber || chars[i - 1] == "_" || chars[i - 1] == "-" {
            i -= 1
        }
        guard i > 0, chars[i - 1] == "@" || chars[i - 1] == "/" else { return nil }
        let triggerAt = i - 1
        guard triggerAt == 0 || chars[triggerAt - 1].isWhitespace else { return nil }
        return PromptToken(
            kind: chars[triggerAt] == "@" ? .mention : .command,
            query: String(chars[i...]).lowercased(),
            start: triggerAt
        )
    }
}

/// Everything a prompt bar changes. Own one per composer; drive it from your app (or a
/// script) and the bar renders whatever it holds.
@MainActor
@Observable
public final class PromptBarModel {
    public var text: String
    public var attachments: [String]
    public var selectedModelID: String
    public var isListening = false
    public var connectedSourceIDs: Set<String> = []
    /// The row the menu highlight sits on; `nil` hides the highlight.
    public var highlightedRow: Int?
    public var isModelMenuOpen = false
    /// The `+` button's source menu, open without typing `@`.
    public var isSourceMenuOpen = false
    /// Set by Escape-like dismissal; cleared by the next edit.
    public var isMenuDismissed = false

    public init(text: String = "", attachments: [String] = [], selectedModelID: String) {
        self.text = text
        self.attachments = attachments
        self.selectedModelID = selectedModelID
    }

    public var token: PromptToken? { isMenuDismissed ? nil : PromptToken.parse(text) }

    public var canSend: Bool {
        !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !attachments.isEmpty
    }
}

/// A composer with attach, `@` sources, `/` commands, a model picker, dictation and send.
/// Menus grow up from the composer's top edge; a long draft moves above the controls.
/// Picking `sweepModelID` plays a one-shot rainbow sweep across the composer.
public struct PromptBar: View {
    public enum Shape: String, CaseIterable, Sendable {
        /// 14 pt corners.
        case rounded
        /// Fully round, easing to 24 pt when the draft wraps or attachments show.
        case pill
    }

    @Bindable var model: PromptBarModel
    var shape: Shape
    var sources: [PromptSource]
    var commands: [PromptCommand]
    var models: [PromptModel]
    var placeholder: String
    var sweepModelID: String?
    var onSend: ((String) -> Void)?
    var onAttach: (() -> Void)?

    @State private var sweepID = 0
    @State private var containerWidth: CGFloat = 0
    @State private var draftWidth: CGFloat = 0
    @State private var modelButtonWidth: CGFloat = 0
    @FocusState private var inputFocused: Bool
    @Environment(\.buiTheme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(
        model: PromptBarModel,
        shape: Shape = .rounded,
        sources: [PromptSource] = [],
        commands: [PromptCommand] = [],
        models: [PromptModel] = [],
        placeholder: String = "Write a message…",
        sweepModelID: String? = nil,
        onSend: ((String) -> Void)? = nil,
        onAttach: (() -> Void)? = nil
    ) {
        self.model = model
        self.shape = shape
        self.sources = sources
        self.commands = commands
        self.models = models
        self.placeholder = placeholder
        self.sweepModelID = sweepModelID
        self.onSend = onSend
        self.onAttach = onAttach
    }

    private enum Menu { case mention, command }

    private var menu: Menu? {
        if model.isSourceMenuOpen { return .mention }
        switch model.token?.kind {
        case .mention: return .mention
        case .command: return .command
        case nil: return nil
        }
    }

    private var query: String { model.isSourceMenuOpen ? "" : model.token?.query ?? "" }

    private var sourceRows: [PromptSource] {
        query.isEmpty ? sources : sources.filter { $0.name.lowercased().contains(query) }
    }

    private var commandRows: [PromptCommand] {
        commands.filter { $0.name.dropFirst().lowercased().hasPrefix(query) }
    }

    private var rowCount: Int { menu == .mention ? sourceRows.count : menu == .command ? commandRows.count : 0 }

    /// Wrapped text moves above the controls: when it has a newline or no longer fits inline.
    private var wide: Bool {
        let inline = containerWidth - 12 - 28 * 3 - modelButtonWidth - 4 * 4
        return model.text.contains("\n") || draftWidth + 8 > inline
    }

    private var currentModel: PromptModel? { models.first { $0.id == model.selectedModelID } }

    private var composerRadius: CGFloat {
        switch shape {
        case .rounded: 14
        case .pill: (wide || !model.attachments.isEmpty) ? 24 : 100
        }
    }

    public var body: some View {
        VStack(spacing: 0) {
            // Menus hang above the composer: a zero-height, bottom-aligned slot lets them
            // overflow upward without taking layout space, 8 pt over the composer's top edge.
            ZStack(alignment: .bottomLeading) {
                if let menu {
                    sourceOrCommandMenu(menu)
                        .transition(.scale(scale: 0.95, anchor: .bottom).combined(with: .opacity))
                }
                if model.isModelMenuOpen {
                    modelMenu
                        .padding(.leading, modelMenuX)
                        .transition(.scale(scale: 0.95, anchor: .bottomLeading).combined(with: .opacity))
                }
            }
            .fixedSize(horizontal: false, vertical: true)
            .padding(.bottom, 8)
            .frame(maxWidth: .infinity, alignment: .bottomLeading)
            .frame(height: 0, alignment: .bottom)
            .zIndex(1)
            composer
        }
        .animation(BUICurve.outStrong.animation(duration: 0.18), value: menu)
        .animation(BUICurve.outStrong.animation(duration: 0.18), value: model.isModelMenuOpen)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { containerWidth = $0 }
        .onChange(of: model.selectedModelID) { _, id in
            if id == sweepModelID, !reduceMotion { sweepID += 1 }
        }
        .onChange(of: model.text) { model.isMenuDismissed = false }
        .onChange(of: menu) { model.highlightedRow = nil }
    }

    /// `w-44` plus 8 pt, since SF Pro sets model names wider than Inter.
    private static let modelMenuWidth: CGFloat = 184

    /// The model menu sits over its trigger but stays inside the composer's width.
    private var modelMenuX: CGFloat {
        let triggerX = wide ? 6 + 28 + 4 : containerWidth - 6 - 28 * 2 - 4 * 2 - modelButtonWidth
        return max(0, min(triggerX, containerWidth - Self.modelMenuWidth))
    }

    // MARK: Composer

    private var composer: some View {
        let radius = composerRadius
        let pill = shape == .pill
        return VStack(alignment: .leading, spacing: 6) {
            if !model.attachments.isEmpty {
                BUIFlowLayout(spacing: 6) {
                    ForEach(Array(model.attachments.enumerated()), id: \.offset) { index, file in
                        attachmentChip(file, at: index, pill: pill)
                    }
                }
                .padding(.top, 2)
                .padding(.horizontal, pill ? 4 : 2)
            }
            controls
        }
        .padding(6)
        .background {
            RainbowSweep(trigger: sweepID)
        }
        .clipShape(.rect(cornerRadius: radius, style: .continuous))
        .background {
            RoundedRectangle(cornerRadius: radius, style: .continuous)
                .fill(theme.colors.surface)
                .shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 1, y: 1)
                .shadow(color: Color(light: 0, dark: 0, lightAlpha: 0.035, darkAlpha: 0.2), radius: 10, y: 5)
        }
        .overlay {
            RoundedRectangle(cornerRadius: radius, style: .continuous)
                .strokeBorder(inputFocused ? theme.colors.lineStrong : theme.colors.line, lineWidth: 1)
        }
        .animation(BUIMotion.standard, value: radius)
        .background(alignment: .topLeading) {
            // Measures the draft on one line to decide when it moves above the controls.
            Text(model.text.isEmpty ? " " : model.text)
                .buiFont(BUITextStyle(size: 13, lineHeight: 18 / 13))
                .fixedSize()
                .hidden()
                .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { draftWidth = $0 }
        }
    }

    @ViewBuilder
    private var controls: some View {
        if wide {
            VStack(alignment: .leading, spacing: 6) {
                input
                HStack(alignment: .bottom, spacing: 4) {
                    plusButton
                    modelButton
                    Spacer(minLength: 0)
                    micButton
                    sendButton
                }
            }
        } else {
            HStack(alignment: .bottom, spacing: 4) {
                plusButton
                input
                modelButton
                micButton
                sendButton
            }
        }
    }

    private var input: some View {
        TextField(model.isListening ? "Listening…" : placeholder, text: $model.text, axis: .vertical)
            .buiFont(BUITextStyle(size: 13, lineHeight: 18 / 13))
            .foregroundStyle(theme.colors.ink)
            .lineLimit(1...5)
            .focused($inputFocused)
            .padding(.horizontal, 4)
            .padding(.vertical, 5)
            .frame(maxWidth: .infinity, minHeight: 28, alignment: .leading)
            .accessibilityLabel("Prompt")
    }

    private var controlShape: AnyShape {
        shape == .pill ? AnyShape(Capsule()) : AnyShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
    }

    private var plusButton: some View {
        Button {
            model.isModelMenuOpen = false
            model.isSourceMenuOpen.toggle()
            inputFocused = true
        } label: {
            BUIIcon(.plus, size: 16)
                .frame(width: 28, height: 28)
                .contentShape(controlShape)
        }
        .buttonStyle(ControlButtonStyle(shape: controlShape, active: model.isSourceMenuOpen))
        .accessibilityLabel("Add attachments and sources")
    }

    private var modelButton: some View {
        Button {
            model.isSourceMenuOpen = false
            model.isModelMenuOpen.toggle()
        } label: {
            HStack(spacing: 4) {
                Text(currentModel?.name ?? "Model")
                    .buiFont(BUITextStyle(size: 12, weight: .medium))
                    .foregroundStyle(theme.colors.ink2)
                BUIIcon(.chevronDown, size: 11, lineWidth: 2.4)
                    .foregroundStyle(theme.colors.ink3)
            }
            .padding(.horizontal, 6)
            .frame(height: 28)
            .contentShape(controlShape)
        }
        .buttonStyle(ControlButtonStyle(shape: controlShape, active: false))
        .fixedSize()
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { modelButtonWidth = $0 }
        .accessibilityLabel("Choose model")
        .accessibilityValue(currentModel?.name ?? "")
    }

    private var micButton: some View {
        Button {
            model.isListening.toggle()
        } label: {
            Group {
                if model.isListening {
                    LevelBars().foregroundStyle(theme.colors.accentInk)
                } else {
                    BUIIcon(BUIGlyph("M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3zM19 10v2a7 7 0 0 1-14 0v-2M12 19v3"), size: 15)
                }
            }
            .frame(width: 28, height: 28)
            .background(model.isListening ? theme.colors.accentTint : .clear, in: controlShape)
            .contentShape(controlShape)
        }
        .buttonStyle(ControlButtonStyle(shape: controlShape, active: false))
        .accessibilityLabel(model.isListening ? "Stop dictation" : "Start dictation")
    }

    private var sendButton: some View {
        let enabled = model.canSend
        return Button {
            send()
        } label: {
            BUIIcon(.arrowUp, size: 16, lineWidth: 2.4)
                .foregroundStyle(enabled ? theme.colors.surface : theme.colors.ink2)
                .frame(width: 28, height: 28)
                .background(enabled ? theme.colors.ink : theme.colors.lineStrong, in: controlShape)
                .animation(.easeInOut(duration: 0.2), value: enabled)
        }
        .buttonStyle(ShrinkStyle())
        .disabled(!enabled)
        .accessibilityLabel("Send")
    }

    private func attachmentChip(_ file: String, at index: Int, pill: Bool) -> some View {
        let chipShape = pill ? AnyShape(Capsule()) : AnyShape(RoundedRectangle(cornerRadius: theme.radii.chip, style: .continuous))
        return HStack(spacing: 6) {
            BUIIcon(ToolCall.Kind.read.glyph, size: 12)
            Text(file)
                .lineLimit(1)
                .frame(maxWidth: 144, alignment: .leading)
                .fixedSize()
            Button {
                _ = withAnimation(BUIMotion.standard) { model.attachments.remove(at: index) }
            } label: {
                BUIIcon(.close, size: 10, lineWidth: 2.5)
                    .foregroundStyle(theme.colors.ink3)
                    .frame(width: 24, height: 24)
                    .contentShape(chipShape)
            }
            .buttonStyle(.plain)
            .padding(.vertical, -4)
            .accessibilityLabel("Remove \(file)")
        }
        .buiFont(BUITextStyle(size: 11.5))
        .foregroundStyle(theme.colors.ink2)
        .padding(.leading, 6)
        .padding(.trailing, 4)
        .frame(height: 26)
        .background(theme.colors.field, in: chipShape)
        .overlay(chipShape.stroke(theme.colors.line, lineWidth: 1))
        .buiEntrance(.popIn, duration: 0.2)
    }

    // MARK: Menus

    private func sourceOrCommandMenu(_ menu: Menu) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            ZStack(alignment: .top) {
                if let row = model.highlightedRow, row < rowCount {
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(theme.colors.hover)
                        .frame(height: 36)
                        .offset(y: CGFloat(row) * 36)
                        .frame(maxHeight: .infinity, alignment: .top)
                        .transition(.opacity)
                }
                VStack(spacing: 0) {
                    if menu == .mention {
                        ForEach(Array(sourceRows.enumerated()), id: \.element.id) { index, source in
                            menuRow(index: index, name: source.name, detail: source.detail) {
                                pick(source: source)
                            } leading: {
                                sourceIcon(source.icon)
                            } trailing: {
                                if source.kind == .connectable {
                                    let connected = model.connectedSourceIDs.contains(source.id)
                                    Button(connected ? "Connected" : "Connect") {
                                        if connected { model.connectedSourceIDs.remove(source.id) } else { model.connectedSourceIDs.insert(source.id) }
                                    }
                                    .buttonStyle(.plain)
                                    .buiFont(BUITextStyle(size: 12, weight: .medium))
                                    .foregroundStyle(connected ? theme.colors.green : theme.colors.accentInk)
                                }
                            }
                        }
                    } else {
                        ForEach(Array(commandRows.enumerated()), id: \.element.id) { index, command in
                            menuRow(index: index, name: command.name, detail: command.detail) {
                                pick(command: command)
                            } leading: {
                                EmptyView()
                            } trailing: {
                                EmptyView()
                            }
                        }
                    }
                    if rowCount == 0 {
                        Text("No matches for “\(query)”")
                            .buiFont(.caption)
                            .foregroundStyle(theme.colors.ink3)
                            .padding(.horizontal, 8)
                            .frame(maxWidth: .infinity, minHeight: 36, alignment: .leading)
                    }
                }
            }
            .animation(BUICurve.outStrong.animation(duration: 0.22), value: model.highlightedRow)
            Text(menu == .mention ? "Type to search sources & files" : "Type to search commands")
                .buiFont(BUITextStyle(size: 11))
                .foregroundStyle(theme.colors.ink3)
                .padding(.horizontal, 8)
                .padding(.top, 6)
                .padding(.bottom, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
                .overlay(alignment: .top) { Rectangle().fill(theme.colors.line).frame(height: 1) }
                .padding(.top, 4)
        }
        .padding(4)
        .buiSurface(radius: theme.radii.card, elevation: .raised)
    }

    private func menuRow<Leading: View, Trailing: View>(
        index: Int,
        name: String,
        detail: String,
        action: @escaping () -> Void,
        @ViewBuilder leading: () -> Leading,
        @ViewBuilder trailing: () -> Trailing
    ) -> some View {
        Button(action: action) {
            HStack(spacing: 10) {
                leading()
                Text(name)
                    .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                    .foregroundStyle(theme.colors.ink)
                    .fixedSize()
                Text(detail)
                    .buiFont(.caption)
                    .foregroundStyle(theme.colors.ink3)
                    .lineLimit(1)
                    .frame(maxWidth: .infinity, alignment: .leading)
                trailing()
            }
            .padding(.horizontal, 8)
            .frame(height: 36)
            .contentShape(.rect(cornerRadius: 6))
        }
        .buttonStyle(MenuRowStyle(index: index, highlighted: $model.highlightedRow))
    }

    @ViewBuilder
    private func sourceIcon(_ icon: PromptSource.Icon) -> some View {
        switch icon {
        case .glyph(let glyph):
            BUIIcon(glyph, size: 15, lineWidth: 1.8)
                .foregroundStyle(theme.colors.ink2)
                .frame(width: 22, height: 22)
        case .mark(let avatar, let size):
            avatar.view(width: size.width, height: size.height)
                .frame(width: 22, height: 22)
        }
    }

    private var modelMenu: some View {
        VStack(spacing: 0) {
            ForEach(Array(models.enumerated()), id: \.element.id) { index, option in
                Button {
                    model.selectedModelID = option.id
                    model.isModelMenuOpen = false
                } label: {
                    HStack(spacing: 8) {
                        Text(option.name)
                            .buiFont(BUITextStyle(size: 12.5, weight: .medium))
                            .foregroundStyle(theme.colors.ink)
                            .lineLimit(1)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        Text(option.tag)
                            .buiFont(BUITextStyle(size: 11))
                            .foregroundStyle(theme.colors.ink3)
                        BUIIcon(.check, size: 13, lineWidth: 2.5)
                            .foregroundStyle(theme.colors.ink)
                            .opacity(option.id == model.selectedModelID ? 1 : 0)
                    }
                    .padding(.horizontal, 8)
                    .frame(height: 30)
                    .contentShape(.rect(cornerRadius: 6))
                }
                .buttonStyle(PressHighlightStyle(radius: 6, color: theme.colors.hover))
                .accessibilityAddTraits(option.id == model.selectedModelID ? .isSelected : [])
                .id(index)
            }
        }
        .padding(4)
        .frame(width: Self.modelMenuWidth)
        .buiSurface(radius: theme.radii.card, elevation: .raised)
    }

    // MARK: Actions

    private func pick(source: PromptSource) {
        let prefix = model.token.map { String(model.text.prefix($0.start)) } ?? model.text
        switch source.kind {
        case .attach:
            onAttach?()
            if model.token != nil { model.text = prefix }
        case .source, .connectable:
            model.text = "\(prefix)@\(source.name) "
        }
        model.isSourceMenuOpen = false
        inputFocused = true
    }

    private func pick(command: PromptCommand) {
        let prefix = model.token.map { String(model.text.prefix($0.start)) } ?? model.text
        model.text = "\(prefix)\(command.name) "
        inputFocused = true
    }

    private func send() {
        guard model.canSend else { return }
        onSend?(model.text.trimmingCharacters(in: .whitespacesAndNewlines))
        model.text = ""
        model.attachments = []
        model.isSourceMenuOpen = false
        model.isModelMenuOpen = false
    }
}

/// Composer controls: `ink-3`, filling `hover` and turning `ink` while pressed or active,
/// shrinking to 94 %.
private struct ControlButtonStyle: ButtonStyle {
    var shape: AnyShape
    var active: Bool
    @Environment(\.buiTheme) private var theme

    func makeBody(configuration: Configuration) -> some View {
        let on = configuration.isPressed || active
        configuration.label
            .foregroundStyle(on ? theme.colors.ink : theme.colors.ink3)
            .background(on ? theme.colors.hover : .clear, in: shape)
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

private struct ShrinkStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.94 : 1)
            .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
    }
}

/// Moves the menu's gliding highlight to the row under the finger.
private struct MenuRowStyle: ButtonStyle {
    var index: Int
    @Binding var highlighted: Int?

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .onChange(of: configuration.isPressed) { _, pressed in
                if pressed { highlighted = index }
            }
    }
}

/// Dictation level meter: three 2.5 pt bars bouncing (`eq-bounce`, 900 ms, staggered 150 ms).
private struct LevelBars: View {
    @State private var start = Date()
    private static let bounce = BUIKeyframes([(0, 0.35), (0.5, 1), (1, 0.35)], curve: .easeInOut)

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30)) { context in
            let t = context.date.timeIntervalSince(start)
            HStack(spacing: 2.5) {
                ForEach(0..<3, id: \.self) { i in
                    Capsule()
                        .frame(width: 2.5, height: 14)
                        .scaleEffect(y: Self.bounce.value(elapsed: t, duration: 0.9, delay: Double(i) * 0.15, rest: 1))
                }
            }
        }
        .frame(height: 14)
    }
}

/// A one-shot rainbow band sweeping left to right behind the composer's controls
/// (the original runs a WebGL shader for this).
private struct RainbowSweep: View {
    var trigger: Int
    @State private var progress: CGFloat = 0
    @State private var visible = false

    private static let colors: [Color] = [
        Color(hex: 0xFF453A), Color(hex: 0xFF9F0A), Color(hex: 0xFFD60A), Color(hex: 0x32D74B),
        Color(hex: 0x64D2FF), Color(hex: 0x0A84FF), Color(hex: 0xBF5AF2),
    ]

    var body: some View {
        GeometryReader { proxy in
            let w = proxy.size.width
            LinearGradient(colors: Self.colors, startPoint: .leading, endPoint: .trailing)
                .frame(width: w * 0.7)
                .blur(radius: 14)
                .offset(x: -w * 0.7 + (w * 1.7) * progress)
                .opacity(visible ? 0.55 : 0)
        }
        .allowsHitTesting(false)
        .onChange(of: trigger) {
            progress = 0
            visible = true
            // easeOutExpo over 570 ms, then an 80 ms outro.
            withAnimation(BUICurve(0.16, 1, 0.3, 1).animation(duration: 0.57)) { progress = 1 }
            withAnimation(.easeOut(duration: 0.08).delay(0.5)) { visible = false }
        }
    }
}
