import SwiftUI

// Feishu documents in the inbox (docs/inbox.md「云文档」): a sheet / Base / docx that @'d the user or commented to
// them. The rows about the user show with their status columns, each a menu that moves the status in the document;
// the AI 分身's draft can propose moves, made when it is sent.

/// A status column's value as a chip; tapping lists the values and moving to one writes it to the document.
struct DocStatusMenu: View {
    var item: InboxItem
    var row: InboxItem.DocRow
    var status: InboxItem.DocStatus
    /// The column's name before the value (the detail page; the list row has no room).
    var showName = false
    private var store: InboxStore { .shared }

    var body: some View {
        let moving = store.moving.contains(item.id + "|" + row.ref + "|" + status.field)
        Menu {
            Section(status.name) {
                ForEach(status.options, id: \.name) { o in
                    Button {
                        store.setDocStatus(item, row: row, status: status, to: o.name)
                    } label: {
                        if o.name == status.value { Label(o.name, systemImage: "checkmark") } else { Text(o.name) }
                    }
                }
            }
        } label: {
            HStack(spacing: 4) {
                if moving {
                    ProgressView().controlSize(.mini)
                } else {
                    Circle().fill(DocStatusColor.dot(status, in: status.value)).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                }
                Text((showName ? status.name + "：" : "") + (status.value.isEmpty ? "未设置" : status.value))
                Image(systemName: "chevron.up.chevron.down").font(.caption2.weight(.semibold)).foregroundStyle(.secondary)
            }
            .font(.footnote.weight(.medium))
            .lineLimit(1)
            .foregroundStyle(.primary)
            .padding(.horizontal, 9)
            .padding(.vertical, 4)
            .background(DocStatusColor.fill(status, in: status.value), in: .capsule)
        }
        .tint(.primary) // the label keeps its own colors, not the app's accent
        .disabled(moving || item.isDone)
        .accessibilityLabel("\(status.name)：\(status.value.isEmpty ? "未设置" : status.value)，点按修改")
    }
}

/// The document's own option colors are pastel fills (fine in light mode, too bright on dark): the chip takes the
/// hue faintly, the dot takes it whole.
enum DocStatusColor {
    static func color(_ status: InboxItem.DocStatus, in value: String) -> Color? {
        guard let hex = status.options.first(where: { $0.name == value })?.color, let c = parse(hex) else { return nil }
        return c
    }

    static func dot(_ status: InboxItem.DocStatus, in value: String) -> Color {
        color(status, in: value).map { $0.mix(with: .black, by: 0.25) } ?? Color.secondary.opacity(0.5)
    }

    static func fill(_ status: InboxItem.DocStatus, in value: String) -> Color {
        color(status, in: value).map { $0.opacity(0.35) } ?? Color.secondary.opacity(0.12)
    }

    private static func parse(_ hex: String) -> Color? {
        var s = hex.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("#") { s.removeFirst() }
        guard s.count == 6, let v = UInt32(s, radix: 16) else { return nil }
        return Color(hex: v)
    }
}

/// Under an inbox row: the first row about the user with its status, and how many more there are.
struct InboxDocRowsPreview: View {
    var item: InboxItem

    var body: some View {
        if let rows = item.doc?.rows, let first = rows.first {
            HStack(spacing: 6) {
                Image(systemName: "arrow.turn.down.right").font(.caption2).foregroundStyle(.secondary)
                Text(first.title).font(.footnote).foregroundStyle(.secondary).lineLimit(1)
                    .layoutPriority(-1)
                ForEach((first.status ?? []).prefix(1)) { st in
                    DocStatusMenu(item: item, row: first, status: st)
                }
                if rows.count > 1 {
                    Text("+\(rows.count - 1) 行").font(.caption).foregroundStyle(.secondary).lineLimit(1).fixedSize()
                }
                Spacer(minLength: 0)
            }
        }
    }
}

/// The 40pt square in place of a chat avatar: what kind of document.
struct InboxDocIcon: View {
    var doc: InboxItem.Doc

    var body: some View {
        RoundedRectangle(cornerRadius: 10, style: .continuous)
            .fill(tint.opacity(0.15))
            .scaledFrame(width: Theme.Size.avatar, height: Theme.Size.avatar)
            .overlay(Image(systemName: doc.symbol).font(.body.weight(.semibold)).foregroundStyle(tint))
    }

    private var tint: Color {
        switch doc.type {
        case "sheet": Color(hex: 0x2EA121) // Feishu's sheet green
        case "base": Color(hex: 0x7F3BF5)  // Base purple
        default: Color(hex: 0x3370FF)      // docs blue
        }
    }
}

/// An opened document item: what happened, the rows about the user (each field, each status a menu), and the
/// document itself one tap away.
struct InboxDocDetailView: View {
    var item: InboxItem
    @Environment(\.feishuOpen) private var feishuOpen

    var body: some View {
        let doc = item.doc
        List {
            Section {
                HStack(alignment: .top, spacing: 12) {
                    if let doc { InboxDocIcon(doc: doc) }
                    VStack(alignment: .leading, spacing: 4) {
                        Text(item.title).font(.headline).lineLimit(2)
                        Text([doc?.typeName, item.preview, LarkTime.short(item.updatedAt)].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · "))
                            .font(.footnote).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                .padding(.vertical, 4)
                if let doc, let url = URL(string: doc.url) {
                    Button {
                        feishuOpen?(.doc(url: url.absoluteString, title: item.title))
                    } label: {
                        Label("打开\(doc.typeName)", systemImage: "arrow.up.forward.square").lineLimit(1)
                    }
                }
                if let error = doc?.error, !error.isEmpty {
                    Label("读不到文档内容：\(error)", systemImage: "exclamationmark.triangle")
                        .font(.footnote).foregroundStyle(Theme.danger)
                }
            }
            if let rows = doc?.rows, !rows.isEmpty {
                Section {
                    ForEach(rows) { row in rowCard(row) }
                } header: {
                    Text("提到你的行").textCase(nil)
                } footer: {
                    Text("改状态会直接写进\(doc?.typeName ?? "文档")。").textCase(nil)
                }
            }
            if let reply = doc?.reply {
                Section {
                    Label("回复会\(reply.label)", systemImage: "text.bubble").font(.footnote).foregroundStyle(.secondary)
                }
            }
        }
        .listStyle(.insetGrouped)
        .themedPage()
    }

    private func rowCard(_ row: InboxItem.DocRow) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(spacing: 6) {
                Text(row.title).font(.body.weight(.semibold)).lineLimit(2)
                Spacer(minLength: 4)
                if let t = row.table, !t.isEmpty, (item.doc?.rows?.contains { $0.table != t } ?? false) || item.doc?.type == "base" {
                    Chip(text: t, look: .neutral)
                }
            }
            ForEach(row.fields ?? [], id: \.name) { f in
                (Text(f.name + "  ").foregroundStyle(.secondary) + Text(f.value))
                    .font(.subheadline).lineLimit(3)
            }
            if let status = row.status, !status.isEmpty {
                FlowLayoutRow(spacing: 6) {
                    ForEach(status) { st in DocStatusMenu(item: item, row: row, status: st, showName: true) }
                }
            }
        }
        .padding(.vertical, 4)
    }
}

/// Chips in a row that wrap when they do not fit (a row can have several status columns).
struct FlowLayoutRow: Layout {
    var spacing: CGFloat = 6

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? .greatestFiniteMagnitude
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for s in subviews {
            let size = s.sizeThatFits(.unspecified)
            if x > 0 && x + size.width > width { y += line + spacing; x = 0; line = 0 }
            x += size.width + spacing
            line = max(line, size.height)
            widest = max(widest, x - spacing)
        }
        return CGSize(width: min(widest, width), height: y + line)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        var x = bounds.minX, y = bounds.minY, line: CGFloat = 0
        for s in subviews {
            let size = s.sizeThatFits(.unspecified)
            if x > bounds.minX && x + size.width > bounds.maxX { y += line + spacing; x = bounds.minX; line = 0 }
            s.place(at: CGPoint(x: x, y: y), proposal: ProposedViewSize(size))
            x += size.width + spacing
            line = max(line, size.height)
        }
    }
}

/// The AI 分身's proposed moves, as lines ("状态：进行中 → 已完成 · 写文档"), for the draft card and sheet.
enum DocChangeText {
    static func line(_ change: InboxItem.DocChange, in doc: InboxItem.Doc?) -> String {
        guard let (row, st) = doc?.describe(change) else { return change.value }
        let from = st.value.isEmpty ? "未设置" : st.value
        return "\(st.name)：\(from) → \(change.value) · \(row.title)"
    }
}
