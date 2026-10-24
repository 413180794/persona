import SwiftUI

/// Agent state dot: breathes while working, pulses red when it needs you, solid green when finished-unseen,
/// muted gray once seen (docs/agent-states.md).
struct StatusDot: View {
    var state: AgentState
    var size: CGFloat = 9
    @State private var pulse = false

    var body: some View {
        Circle()
            .fill(state.color)
            .frame(width: size, height: size)
            .overlay {
                if state.animates {
                    Circle()
                        .stroke(state.color.opacity(0.6), lineWidth: 1.5)
                        .scaleEffect(pulse ? 2.2 : 1)
                        .opacity(pulse ? 0 : 1)
                }
            }
            .onAppear { animate() }
            .onChange(of: state) { _, _ in animate() }
            .accessibilityLabel(state.label)
    }

    private func animate() {
        pulse = false
        guard state.animates else { return }
        withAnimation(.easeOut(duration: state == .needsYou ? 1.0 : 1.4).repeatForever(autoreverses: false)) { pulse = true }
    }
}

/// Machine glyph inside a soft tinted squircle with the state dot in the corner.
struct SessionAvatar: View {
    var machine: Machine?
    var state: AgentState
    var size: CGFloat = 44

    var body: some View {
        let tint: Color = state == .idle || state == .offline || state == .unknown ? .secondary : state.color
        ZStack(alignment: .bottomTrailing) {
            RoundedRectangle(cornerRadius: size * 0.3, style: .continuous)
                .fill(tint.opacity(state == .offline ? 0.06 : 0.12))
                .frame(width: size, height: size)
                .overlay {
                    Image(systemName: machine?.symbol ?? "terminal")
                        .font(.system(size: size * 0.42, weight: .medium))
                        .foregroundStyle(tint)
                        .symbolEffect(.pulse, options: .repeating, isActive: state == .working)
                }
            if state != .offline {
                StatusDot(state: state, size: size * 0.24)
                    .padding(2.5)
                    .background(Circle().fill(Color(.systemBackground)))
                    .offset(x: 3, y: 3)
            }
        }
    }
}

struct Chip: View {
    var text: String
    var symbol: String?
    var tint: Color = .secondary

    var body: some View {
        HStack(spacing: 3) {
            if let symbol { Image(systemName: symbol).font(.system(size: 9, weight: .semibold)) }
            Text(text).lineLimit(1)
        }
        .font(.caption2.weight(.medium))
        .foregroundStyle(tint)
        .padding(.horizontal, 6)
        .padding(.vertical, 2.5)
        .background(tint.opacity(0.12), in: .capsule)
    }
}

struct StatusLabel: View {
    var state: AgentState

    var body: some View {
        HStack(spacing: 5) {
            StatusDot(state: state, size: 7)
            Text(state.label)
                .font(.caption.weight(.semibold))
                .foregroundStyle(state == .idle ? Color.secondary : state.color)
        }
    }
}

struct EmptyStateView: View {
    var symbol: String
    var title: String
    var message: String
    var action: (title: String, run: () -> Void)?

    var body: some View {
        VStack(spacing: 14) {
            Image(systemName: symbol)
                .font(.system(size: 44, weight: .light))
                .foregroundStyle(Theme.accent.gradient)
                .symbolEffect(.bounce, value: title)
            Text(title).font(.title3.weight(.semibold))
            Text(message)
                .font(.subheadline)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
            if let action {
                Button(action.title, action: action.run)
                    .buttonStyle(.glassProminent)
                    .padding(.top, 4)
            }
        }
        .padding(32)
        .frame(maxWidth: .infinity)
    }
}

struct ConnectionBanner: View {
    @Environment(AppModel.self) private var model

    var body: some View {
        switch model.live.state {
        case .connected, .idle:
            EmptyView()
        case .connecting:
            banner(symbol: "antenna.radiowaves.left.and.right", text: "正在连接 hub…", tint: Theme.attention, spinning: true)
        case .failed(let msg):
            Button {
                model.live.ensureConnected()
                Task { await model.refresh() }
            } label: {
                banner(symbol: "wifi.exclamationmark", text: "连不上 hub：\(msg)", tint: Theme.danger, spinning: false)
            }
            .buttonStyle(.plain)
        }
    }

    private func banner(symbol: String, text: String, tint: Color, spinning: Bool) -> some View {
        HStack(spacing: 8) {
            if spinning { ProgressView().controlSize(.small) } else { Image(systemName: symbol).foregroundStyle(tint) }
            Text(text).font(.footnote.weight(.medium)).lineLimit(2)
            Spacer(minLength: 0)
            if !spinning { Text("重试").font(.footnote.weight(.semibold)).foregroundStyle(tint) }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(tint.opacity(0.1), in: .rect(cornerRadius: 14, style: .continuous))
    }
}

extension View {
    /// Rounded card surface used across lists.
    func card(padding: CGFloat = 14, radius: CGFloat = 18) -> some View {
        self.padding(padding)
            .background(Color(.secondarySystemGroupedBackground), in: .rect(cornerRadius: radius, style: .continuous))
            .overlay(RoundedRectangle(cornerRadius: radius, style: .continuous).stroke(Theme.cardStroke, lineWidth: 0.5))
    }
}
