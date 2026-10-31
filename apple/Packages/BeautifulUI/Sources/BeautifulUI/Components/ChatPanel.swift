import SwiftUI

/// One agent reply in a chat thread.
public struct ChatReply: Identifiable, Sendable {
    public var id: String
    /// What the step was, e.g. "Sales History".
    public var label: String
    /// Where it came from, e.g. "Flavor Data".
    public var source: String
    /// How long it took, e.g. "4s".
    public var duration: String
    public var body: String
    /// Still being worked on: drawn faded, slightly blurred and shrunk.
    public var isResolving: Bool

    public init(id: String? = nil, label: String, source: String, duration: String, body: String, isResolving: Bool = false) {
        self.id = id ?? label
        self.label = label
        self.source = source
        self.duration = duration
        self.body = body
        self.isResolving = isResolving
    }
}

/// A fixed-height chat panel: tabs and actions on top, the thread (a right-aligned user
/// bubble, then agent replies that fade up) and a composer at the bottom.
public struct ChatPanel: View {
    public enum Action: String, CaseIterable, Sendable {
        case new, history, more

        var glyph: BUIGlyph {
            switch self {
            case .new: .plus
            case .history: BUIGlyph(BUIGlyph.circle(12, 12, 9) + "M12 7v5l3 2")
            case .more: BUIGlyph(BUIGlyph.circle(5, 12, 1.8) + BUIGlyph.circle(12, 12, 1.8) + BUIGlyph.circle(19, 12, 1.8), filled: true)
            }
        }

        var label: String {
            switch self {
            case .new: "New chat"
            case .history: "History"
            case .more: "More"
            }
        }
    }

    var tabs: [String]
    var externalTab: Binding<String>?
    var prompt: String?
    var replies: [ChatReply]
    var placeholder: String
    var height: CGFloat
    var onSend: ((String) -> Void)?
    var onAction: ((Action) -> Void)?

    @State private var internalTab: String
    @State private var draft = ""
    @FocusState private var focused: Bool
    @Environment(\.buiTheme) private var theme

    /// - Parameters:
    ///   - prompt: the user's message; `nil` leaves the thread empty.
    ///   - replies: replies so far; new ones fade up.
    public init(
        tabs: [String] = [],
        selectedTab: Binding<String>? = nil,
        prompt: String?,
        replies: [ChatReply],
        placeholder: String = "Prompt or tag a flavor with @",
        height: CGFloat = 288,
        onSend: ((String) -> Void)? = nil,
        onAction: ((Action) -> Void)? = nil
    ) {
        self.tabs = tabs
        self.externalTab = selectedTab
        self.prompt = prompt
        self.replies = replies
        self.placeholder = placeholder
        self.height = height
        self.onSend = onSend
        self.onAction = onAction
        _internalTab = State(initialValue: tabs.first ?? "")
    }

    private var tab: String { externalTab?.wrappedValue ?? internalTab }
    private var canSend: Bool { !draft.trimmingCharacters(in: .whitespaces).isEmpty }

    public var body: some View {
        VStack(spacing: 0) {
            header
            thread
            composer
        }
        .frame(maxWidth: 380)
        .frame(height: height)
        .buiSurface(radius: theme.radii.window, elevation: .card)
    }

    private var header: some View {
        HStack {
            HStack(spacing: 0) {
                ForEach(tabs, id: \.self) { item in
                    let on = item == tab
                    Button {
                        withAnimation(.easeOut(duration: 0.1)) {
                            if let externalTab { externalTab.wrappedValue = item } else { internalTab = item }
                        }
                    } label: {
                        Text(item)
                            .buiFont(.body)
                            .foregroundStyle(theme.colors.ink)
                            .padding(.horizontal, 8)
                            .padding(.vertical, 3)
                            .background(on ? theme.colors.field : .clear, in: .rect(cornerRadius: 6))
                            .opacity(on ? 1 : 0.5)
                    }
                    .buttonStyle(.plain)
                    .accessibilityAddTraits(on ? .isSelected : [])
                }
            }
            Spacer(minLength: 8)
            HStack(spacing: 4) {
                ForEach(Action.allCases, id: \.self) { action in
                    Button {
                        onAction?(action)
                    } label: {
                        BUIIcon(action.glyph, size: 15)
                    }
                    .buttonStyle(IconButtonStyle(size: 24))
                    .accessibilityLabel(action.label)
                }
            }
        }
        .padding(6)
        .overlay(alignment: .bottom) {
            Rectangle().fill(theme.colors.line).frame(height: 1)
        }
    }

    private var thread: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 10) {
                if let prompt {
                    HStack {
                        Spacer(minLength: 56)
                        Text(prompt)
                            .buiFont(BUITextStyle(size: 13, lineHeight: 1.4))
                            .foregroundStyle(theme.colors.ink)
                            .padding(.horizontal, 12)
                            .padding(.vertical, 6)
                            .background(theme.colors.field, in: .rect(cornerRadius: 12, style: .continuous))
                    }
                    .id(prompt)
                    .transition(.offset(y: 10).combined(with: .opacity))
                }
                ForEach(replies) { reply in
                    ReplySection(reply: reply)
                }
            }
            .padding(.horizontal, 12)
            .padding(.top, 10)
            .padding(.bottom, 4)
            .animation(BUICurve.outStrong.animation(duration: 0.3), value: prompt)
        }
        .scrollIndicators(.hidden)
        // Short threads sit at the top like the original; growing ones follow the newest reply.
        .defaultScrollAnchor(.top, for: .alignment)
        .defaultScrollAnchor(.bottom, for: .sizeChanges)
        .frame(maxHeight: .infinity)
    }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 8) {
            TextField(placeholder, text: $draft)
                .buiFont(BUITextStyle(size: 13, lineHeight: 1.4))
                .foregroundStyle(theme.colors.ink)
                .focused($focused)
                .submitLabel(.send)
                .onSubmit(send)
                .accessibilityLabel("Chat prompt")
            HStack {
                Spacer()
                Button(action: send) {
                    BUIIcon(.arrowUp, size: 16, lineWidth: 2.4)
                        .foregroundStyle(canSend ? theme.colors.surface : theme.colors.ink2)
                        .frame(width: 28, height: 28)
                        .background(canSend ? theme.colors.ink : theme.colors.lineStrong, in: .rect(cornerRadius: 8, style: .continuous))
                        .animation(.easeInOut(duration: 0.2), value: canSend)
                }
                .buttonStyle(.plain)
                .disabled(!canSend)
                .accessibilityLabel("Send")
            }
        }
        .padding(10)
        .background(theme.colors.field, in: .rect(cornerRadius: theme.radii.control, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: theme.radii.control, style: .continuous)
                .strokeBorder(focused ? theme.colors.lineStrong : theme.colors.line, lineWidth: 1)
        }
        .shadow(color: .black.opacity(0.035), radius: 1, y: 1)
        .contentShape(.rect)
        .onTapGesture { focused = true }
        .padding(6)
    }

    private func send() {
        guard canSend else { return }
        onSend?(draft.trimmingCharacters(in: .whitespaces))
        draft = ""
    }
}

private struct ReplySection: View {
    let reply: ChatReply
    @Environment(\.buiTheme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 4) {
                Text(reply.label).fontWeight(.medium).foregroundStyle(theme.colors.ink)
                Text(reply.source).foregroundStyle(theme.colors.ink2)
                Text("for \(reply.duration)").foregroundStyle(theme.colors.ink)
            }
            .buiFont(BUITextStyle(size: 12, lineHeight: 1.3))
            .lineLimit(1)
            Text(reply.body)
                .buiFont(BUITextStyle(size: 13, lineHeight: 1.5))
                .foregroundStyle(theme.colors.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .opacity(reply.isResolving ? 0.55 : 1)
        .blur(radius: reply.isResolving ? 0.5 : 0)
        .scaleEffect(reply.isResolving ? 0.985 : 1, anchor: .topLeading)
        .animation(BUICurve.outStrong.animation(duration: 0.4), value: reply.isResolving)
        .buiEntrance(.fadeUp, duration: 0.4)
    }
}
