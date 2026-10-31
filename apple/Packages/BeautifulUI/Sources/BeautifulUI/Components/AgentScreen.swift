import SwiftUI

/// Watch an agent work: a framed capture of its screen that opens into a full-screen viewer,
/// where you can teach it a task (recording with a running timer) and end the recording.
/// Pass any view as the screen — a stream, an image — or use the built-in placeholder window.
///
/// The original reveals its Open button on hover; here it appears while the card is pressed.
public struct AgentScreen<Screen: View>: View {
    var agentName: String
    var isLoading: Bool
    var screen: Screen
    var onRecordingChange: ((Bool) -> Void)?

    @State private var open: Bool
    @State private var recording: Bool
    @State private var startedAt = Date()
    @Environment(\.buiTheme) private var theme

    /// Capture aspect ratio (2964 × 1856), so the card shows the whole desktop uncropped.
    static var aspect: CGFloat { 2964 / 1856 }

    public init(
        agentName: String = "Agent",
        isLoading: Bool = false,
        isOpen: Bool = false,
        isRecording: Bool = false,
        onRecordingChange: ((Bool) -> Void)? = nil,
        @ViewBuilder screen: () -> Screen
    ) {
        self.agentName = agentName
        self.isLoading = isLoading
        self.screen = screen()
        self.onRecordingChange = onRecordingChange
        _open = State(initialValue: isOpen)
        _recording = State(initialValue: isRecording)
    }

    /// `mm:ss`.
    nonisolated static func clock(_ seconds: Int) -> String {
        String(format: "%02d:%02d", seconds / 60, seconds % 60)
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Button {
                present(true)
            } label: {
                // The capture's aspect sizes the card; the screen fills it and is cropped.
                Color.clear
                    .aspectRatio(Self.aspect, contentMode: .fit)
                    .overlay {
                        if isLoading { ConnectingScreen() } else { screen }
                    }
                .overlay(alignment: .topLeading) {
                    if !isLoading {
                        GeometryReader { proxy in
                            AgentCursor().offset(x: proxy.size.width * 0.42, y: proxy.size.height * 0.53)
                        }
                    }
                }
                .clipShape(.rect(cornerRadius: theme.radii.window, style: .continuous))
                .background(theme.colors.inset, in: .rect(cornerRadius: theme.radii.window, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: theme.radii.window, style: .continuous)
                        .strokeBorder(BUIElevation.card.ring(theme.colors) ?? .clear, lineWidth: 1)
                }
            }
            .buttonStyle(RevealOpenStyle())
            .disabled(isLoading)
            .shadow(color: .black.opacity(0.04), radius: 8, y: 4)
            .accessibilityLabel("Open \(agentName)'s screen")

            Text("\(agentName)'s screen")
                .buiFont(.label)
                .foregroundStyle(theme.colors.ink)
                .lineLimit(1)
                .padding(.horizontal, 2)
        }
        .frame(maxWidth: 340)
        .buiEntrance(.fadeUp, duration: 0.38)
        .fullScreenCoverIfAvailable(isPresented: $open) {
            viewer
        }
        .onChange(of: recording) { _, now in onRecordingChange?(now) }
    }

    private var viewer: some View {
        ZStack {
            Color.black.opacity(0.6)
                .ignoresSafeArea()
                .onTapGesture { present(false) }
                .buiEntrance(.fadeIn, duration: 0.18, curve: .easeOut)
            VStack(spacing: 0) {
                HStack(spacing: 12) {
                    HStack(spacing: 8) {
                        Text(agentName)
                            .buiFont(BUITextStyle(size: 13, weight: .semibold, lineHeight: 1.19))
                            .foregroundStyle(theme.colors.ink)
                        if recording {
                            RecordingBadge(startedAt: startedAt)
                        }
                    }
                    Spacer(minLength: 0)
                    if recording {
                        Button {
                            withAnimation(BUIMotion.standard) { recording = false }
                        } label: {
                            HStack(spacing: 6) {
                                RoundedRectangle(cornerRadius: 2).fill(.white).frame(width: 10, height: 10)
                                Text("End")
                            }
                            .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1))
                            .foregroundStyle(.white)
                            .padding(.leading, 10)
                            .padding(.trailing, 12)
                            .frame(height: 27)
                            .background(theme.colors.red, in: Capsule())
                        }
                        .buttonStyle(.plain)
                    } else {
                        Button {
                            startedAt = Date()
                            withAnimation(BUIMotion.standard) { recording = true }
                        } label: {
                            HStack(spacing: 4) {
                                BUIIcon(BUIGlyph(BUIGlyph.circle(12, 12, 9)), size: 15)
                                    .overlay { Circle().frame(width: 15 * 7 / 24, height: 15 * 7 / 24) }
                                Text("Teach a task")
                            }
                        }
                        .buttonStyle(.bui(.secondary, horizontalPadding: 10))
                    }
                    Button {
                        present(false)
                    } label: {
                        BUIIcon(BUIGlyph("M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7"), size: 15)
                    }
                    .buttonStyle(IconButtonStyle())
                    .accessibilityLabel("Collapse")
                }
                .padding(.horizontal, 6)
                .frame(height: 44)
                Color.clear
                    .aspectRatio(Self.aspect, contentMode: .fit)
                    .overlay {
                        if isLoading { ConnectingScreen() } else { screen }
                    }
                .clipShape(.rect(cornerRadius: 8, style: .continuous))
                .background(theme.colors.inset, in: .rect(cornerRadius: 8, style: .continuous))
            }
            .padding(.horizontal, 8)
            .padding(.bottom, 8)
            .frame(maxWidth: 960)
            .buiSurface(radius: 16, elevation: .overlay)
            .padding(16)
            .buiEntrance(.popIn, duration: 0.24)
        }
        .presentationBackgroundIfAvailable()
    }
}

extension AgentScreen {
    /// Presents without the cover's slide; the viewer runs its own fade and pop-in.
    private func present(_ show: Bool) {
        var transaction = Transaction()
        transaction.disablesAnimations = true
        withTransaction(transaction) { open = show }
    }
}

public extension AgentScreen where Screen == AgentScreenPlaceholder {
    init(agentName: String = "Agent", isLoading: Bool = false, isOpen: Bool = false, isRecording: Bool = false, onRecordingChange: ((Bool) -> Void)? = nil) {
        self.init(agentName: agentName, isLoading: isLoading, isOpen: isOpen, isRecording: isRecording, onRecordingChange: onRecordingChange) {
            AgentScreenPlaceholder()
        }
    }
}

/// Red pill with a pulsing dot and the elapsed `mm:ss`, ticking each second.
private struct RecordingBadge: View {
    var startedAt: Date
    @State private var pulse = false
    @Environment(\.buiTheme) private var theme

    var body: some View {
        HStack(spacing: 6) {
            Circle()
                .fill(theme.colors.red)
                .frame(width: 8, height: 8)
                .scaleEffect(pulse ? 1 : 0.8)
                .opacity(pulse ? 1 : 0.35)
                .onAppear { withAnimation(.easeInOut(duration: 0.55).repeatForever()) { pulse = true } }
            TimelineView(.periodic(from: startedAt, by: 1)) { context in
                Text(AgentScreen<EmptyView>.clock(Int(context.date.timeIntervalSince(startedAt))))
                    .monospacedDigit()
            }
        }
        .buiFont(BUITextStyle(size: 11.5, weight: .medium, lineHeight: 1.19))
        .foregroundStyle(theme.colors.red)
        .padding(.leading, 6)
        .padding(.trailing, 8)
        .padding(.vertical, 2)
        .background(theme.colors.redTint, in: Capsule())
    }
}

/// While pressed: dim the capture and show the accent "Open" pill.
private struct RevealOpenStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .overlay {
                ZStack {
                    Color(red: 17 / 255, green: 19 / 255, blue: 24 / 255).opacity(configuration.isPressed ? 0.18 : 0)
                    HStack(spacing: 6) {
                        BUIIcon(BUIGlyph("M15 3h6v6M9 21H3v-6M21 3l-7 7M3 21l7-7"), size: 14)
                        Text("Open")
                    }
                    .buiFont(BUITextStyle(size: 13, weight: .medium, lineHeight: 1))
                    .foregroundStyle(.white)
                    .padding(.horizontal, 12)
                    .frame(height: 27)
                    .background(Color(light: 0x0285FF, dark: 0x3D9AFF), in: Capsule())
                    .offset(y: configuration.isPressed ? 0 : 4)
                    .opacity(configuration.isPressed ? 1 : 0)
                }
                .animation(.easeOut(duration: 0.15), value: configuration.isPressed)
                .clipShape(.rect(cornerRadius: 14, style: .continuous))
                .allowsHitTesting(false)
            }
    }
}

/// The macOS-style pointer resting on the capture.
private struct AgentCursor: View {
    var body: some View {
        let path = SVGPath.path("M4.037 4.688a.495.495 0 0 1 .651-.651l16 6.5a.5.5 0 0 1-.063.947l-6.124 1.58a2 2 0 0 0-1.438 1.435l-1.579 6.126a.5.5 0 0 1-.947.063z")
            .applying(CGAffineTransform(scaleX: 30 / 24, y: 30 / 24))
        ZStack {
            path.fill(Color(hex: 0x111318))
            path.stroke(.white, style: StrokeStyle(lineWidth: 1.4 * 30 / 24, lineJoin: .round))
        }
        .frame(width: 30, height: 30)
        .shadow(color: .black.opacity(0.35), radius: 1.5, y: 1)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

/// "Connecting to agent's screen": a white ring spinner on black.
private struct ConnectingScreen: View {
    @State private var turning = false

    var body: some View {
        ZStack {
            Color.black
            ZStack {
                Circle().stroke(Color.white.opacity(0.18), lineWidth: 2)
                Circle().trim(from: 0, to: 0.28).stroke(.white, style: StrokeStyle(lineWidth: 2, lineCap: .round))
                    .rotationEffect(.degrees(turning ? 360 : 0))
            }
            .frame(width: 24, height: 24)
            .onAppear { withAnimation(.linear(duration: 1.1).repeatForever(autoreverses: false)) { turning = true } }
            Text("Connecting to agent's screen")
                .buiFont(BUITextStyle(size: 12.5, weight: .medium, lineHeight: 1.19))
                .foregroundStyle(.white.opacity(0.7))
                .offset(y: 28 + 8)
        }
    }
}

/// The original's faux browser window, used when no screen is supplied.
public struct AgentScreenPlaceholder: View {
    @Environment(\.buiTheme) private var theme

    public init() {}

    public var body: some View {
        TopPinned { window }
    }

    private var window: some View {
        let c = theme.colors
        return VStack(spacing: 0) {
            HStack(spacing: 6) {
                HStack(spacing: 4) {
                    ForEach([c.red, c.orange, c.green], id: \.self) { Circle().fill($0).frame(width: 8, height: 8) }
                }
                HStack(spacing: 6) {
                    RoundedRectangle(cornerRadius: 3).fill(c.accentTint).frame(width: 8, height: 8)
                    Capsule().fill(c.lineStrong).frame(width: 56, height: 6)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 4)
                .background(c.surface, in: UnevenRoundedRectangle(topLeadingRadius: 6, topTrailingRadius: 6))
                .padding(.leading, 4)
                Spacer()
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .background(c.inset)
            .overlay(alignment: .bottom) { Rectangle().fill(c.line).frame(height: 1) }
            HStack(spacing: 8) {
                BUIIcon(BUIGlyph("M15 18l-6-6 6-6"), size: 12)
                BUIIcon(BUIGlyph("M9 6l6 6-6 6"), size: 12)
                Text("hunter.io/try/search/ugly.cash")
                    .font(.system(size: 9, design: .monospaced))
                    .lineLimit(1)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(c.field, in: Capsule())
            }
            .foregroundStyle(c.ink3)
            .padding(.horizontal, 10)
            .padding(.vertical, 6)
            .overlay(alignment: .bottom) { Rectangle().fill(c.line).frame(height: 1) }
            VStack(alignment: .leading, spacing: 10) {
                HStack(spacing: 8) {
                    Text("h").font(.system(size: 8, weight: .bold)).foregroundStyle(c.accent)
                        .frame(width: 16, height: 16).background(c.accentTint, in: .rect(cornerRadius: 4))
                    Capsule().fill(c.lineStrong).frame(width: 48, height: 6)
                    Spacer()
                    Capsule().fill(c.inset).frame(width: 48, height: 16).overlay(Capsule().strokeBorder(c.lineStrong, lineWidth: 1))
                }
                HStack(spacing: 8) {
                    Capsule().fill(c.surface).frame(height: 12).overlay(Capsule().strokeBorder(c.line, lineWidth: 1))
                    Capsule().fill(c.accent).frame(width: 36, height: 12)
                }
                .padding(8)
                .background(c.inset, in: .rect(cornerRadius: 8))
                ForEach([0.4, 0.5, 0.33, 0.4], id: \.self) { fraction in
                    HStack(spacing: 10) {
                        Circle().fill(c.accentTint).frame(width: 24, height: 24)
                        GeometryReader { proxy in
                            VStack(alignment: .leading, spacing: 6) {
                                Capsule().fill(c.lineStrong).frame(width: proxy.size.width * fraction, height: 6)
                                Capsule().fill(c.line).frame(width: proxy.size.width * 0.6, height: 6)
                            }
                            .frame(maxHeight: .infinity, alignment: .center)
                        }
                        .frame(height: 18)
                    }
                }
                Spacer(minLength: 0)
            }
            .padding(12)
        }
        .background(c.surface)
    }
}

/// Pins the placeholder to the top of whatever frame it gets, so a short frame crops the
/// bottom rows rather than the window chrome (a flexible frame would grow to the content).
private struct TopPinned<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        Color.clear
            .overlay(alignment: .top) { content }
            .clipped()
    }
}

private extension View {
    @ViewBuilder
    func fullScreenCoverIfAvailable<V: View>(isPresented: Binding<Bool>, @ViewBuilder content: @escaping () -> V) -> some View {
        #if os(iOS)
        fullScreenCover(isPresented: isPresented, content: content)
        #else
        sheet(isPresented: isPresented, content: content)
        #endif
    }

    @ViewBuilder
    func presentationBackgroundIfAvailable() -> some View {
        presentationBackground(.clear)
    }
}
