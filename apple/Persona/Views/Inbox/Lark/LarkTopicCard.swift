import SwiftUI

/// One topic of a Feishu topic group, drawn the way lark-connector draws a topic group (#1087): a card per topic with the
/// poster's avatar, name and time on top and the topic under them, folded behind 展开 past `Theme.Size.box320`.
/// A tap on the topic (not on its links or a card's buttons) opens its thread; a long press has the chat's menu.
/// Topics are never run together like a chat's messages (`LarkChatLayout.continues`): where a bot posts a topic a
/// minute, each one still has its own card, name and time. A recalled or edited topic follows 风灵月影 (防撤回, 编辑历史)
/// as a chat's row does (`LarkMessageDisplay`).
struct LarkTopicCard: View {
    var topic: LarkMessage
    /// The topic group, named under the thread's title.
    var chatName: String
    var feedID: String?
    @Environment(\.larkOpenProfile) private var openProfile
    @Environment(\.larkReact) private var react
    @Environment(\.larkAddPin) private var addPin
    @Environment(\.larkDisplay) private var display
    /// The open thread, and whether its box is focused (回复).
    @State private var threadOpen: LarkTopicThreadOpening?
    @State private var picking = false
    @State private var showHistory = false

    var body: some View {
        let m = topic
        VStack(alignment: .leading, spacing: Theme.Space.s) {
            header
            let fold = LarkTopicFold { LarkTopicContent(message: m) }.onTapGesture { openThread() }
            // A recalled topic whose body is not shown has no menu, as a chat's row has none: 拷贝 would copy what it
            // no longer shows.
            if display.showsBody(of: m) { fold.contextMenu { menu } } else { fold }
            if m.olk == nil { LarkTranslationLine(message: m, feedID: feedID) }
            footer
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .card(padding: Theme.Space.m)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("lark.topic")
        .sheet(isPresented: $picking) { LarkReactionPicker(message: m) { react?(m, $0) } }
        .sheet(isPresented: $showHistory) { LarkEditHistoryView(message: m) }
        .sheet(item: $threadOpen) { t in
            LarkThreadView(root: m, isGroup: true, feedID: feedID, focusBox: t.focus, chatName: chatName)
        }
    }

    private var menu: some View {
        LarkMessageMenu(message: topic, react: react, reply: canOpen ? { openThread(focus: true) } : nil,
                        pickReaction: { picking = true }, addPin: addPin, showProfile: openProfile == nil ? nil : { showProfile() })
    }

    /// Avatar, then the name (with 机器人 for a bot) over the time. Avatar and name open the poster's card.
    private var header: some View {
        let m = topic
        return HStack(spacing: Theme.Space.s) {
            LarkAvatarImage(name: m.senderName, url: m.senderAvatar, size: Theme.Size.well)
                .onTapGesture { showProfile() }
                .accessibilityAddTraits(.isButton)
                .accessibilityLabel("\(m.senderName) 的资料")
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                HStack(spacing: Theme.Space.xs) {
                    Text(m.senderName).font(Theme.Font.secondaryMedium).lineLimit(1)
                        .onTapGesture { showProfile() }
                    if m.senderType == "bot" { LarkNameTag(text: "机器人") }
                }
                Text(LarkChatLayout.dayLabel(m.createdAt)).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
            }
        }
    }

    /// Reactions, then how many replies (opens the thread) and 已编辑; none of them on a recalled topic, which says
    /// 已撤回 when its body shows (as a chat's row).
    @ViewBuilder private var footer: some View {
        let m = topic
        let reactions = m.reactions ?? []
        if !reactions.isEmpty && !m.recalled {
            LarkFlowLayout(spacing: Theme.Space.sm, lineSpacing: Theme.Space.sm) {
                ForEach(reactions, id: \.key) { r in
                    Button { react?(m, r.key) } label: { LarkReactionChip(reaction: r) }
                        .buttonStyle(.plain)
                        .disabled(react == nil)
                }
                if react != nil {
                    Button { picking = true } label: {
                        Image(systemName: Theme.Icon.react).font(Theme.Font.secondary).foregroundStyle(.secondary)
                            .scaledFrame(width: Theme.Size.well, height: Theme.Size.iconL)
                            .background(Color.primary.opacity(0.05), in: .capsule)
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("表情回复")
                }
            }
        }
        let replies = m.recalled ? 0 : max(m.threadReplyCount ?? 0, m.replyCount)
        let edited = m.edited == true && display.showsBody(of: m)
        if m.recalled && display.showsBody(of: m) {
            Text("已撤回").font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        if replies > 0 || edited {
            HStack(spacing: Theme.Space.s) {
                if replies > 0 {
                    Button { openThread() } label: {
                        Label("\(replies) 条回复", systemImage: Theme.Icon.reply).foregroundStyle(LarkColor.blue)
                    }
                    .buttonStyle(.plain)
                }
                if edited { editedMark }
            }
            .font(Theme.Font.label)
            .foregroundStyle(.secondary)
            .lineLimit(1)
        }
    }

    /// 已编辑: with 编辑历史 on and old versions kept, a button to them (`LarkEditHistoryView`).
    @ViewBuilder private var editedMark: some View {
        if display.showsHistory(of: topic) {
            Button("已编辑") { showHistory = true }.buttonStyle(.plain).lineLimit(1)
                .accessibilityLabel("查看编辑历史").accessibilityIdentifier("lark.editHistory.\(topic.id)")
        } else {
            Text("已编辑")
        }
    }

    /// Nothing to open for a recalled topic or one not in Feishu yet (`LarkReply.route`).
    private var canOpen: Bool { LarkReply.route(for: topic, inTopicGroup: true, inThread: false) != nil }

    private func openThread(focus: Bool = false) {
        guard canOpen else { return }
        threadOpen = LarkTopicThreadOpening(focus: focus)
    }

    private func showProfile() {
        guard let id = topic.senderID, !id.isEmpty else { return }
        openProfile?(LarkPersonRef(id: id, name: topic.senderName, avatar: topic.senderAvatar, feedID: feedID))
    }
}

/// A topic's thread opening from its card, with 回复's focus in it. An item, not a flag beside a separate focus flag:
/// the sheet's content was built from the state before the tap that opened it, so the box was never focused.
private struct LarkTopicThreadOpening: Identifiable {
    let id = UUID()
    var focus: Bool
}

/// A topic's own content on its card: what a chat bubble holds, without the bubble (the card is its frame). A recalled
/// topic shows what it said only with 防撤回 on and its content kept (`LarkMessageDisplay.showsBody`).
private struct LarkTopicContent: View {
    var message: LarkMessage
    @Environment(\.larkDisplay) private var display

    var body: some View {
        let m = message
        let parts = m.parts ?? []
        VStack(alignment: .leading, spacing: Theme.Space.sm) {
            if !display.showsBody(of: m) {
                Text(m.recallNotice).font(Theme.Font.caption).foregroundStyle(.tertiary).italic()
            } else {
                if let reply = m.replyTo { LarkReplyQuote(reply: reply) }
                if let encryption = m.olk {
                    LarkEncryptedContent(encryption: encryption)
                } else if parts.isEmpty {
                    Text(m.text.isEmpty ? "[\(m.type.lowercased())]" : m.text).font(Theme.Font.body)
                } else {
                    LarkPartsView(parts: parts)
                        .environment(\.larkContext, LarkRenderContext(messageID: m.recalled ? nil : m.id, images: LarkPartsView.imageURLs(parts)))
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A topic taller than the fold shows its top under a fade with 展开 over it; open, 收起 under it (lark-connector folds a
/// topic at 320 px). It is laid out no taller than the fold from the start and measured as it is, so a long topic never
/// shows whole first and then jumps shut. What the fold hides takes no taps.
private struct LarkTopicFold<Content: View>: View {
    @ViewBuilder var content: Content
    @Environment(\.dynamicTypeSize) private var typeSize
    /// The content's own height.
    @State private var height: CGFloat = 0
    @State private var open = false

    var body: some View {
        let fold = Theme.Size.box320 * Theme.sizeScale(typeSize)
        let folded = height > fold && !open
        VStack(spacing: Theme.Space.s) {
            content
                .fixedSize(horizontal: false, vertical: true)
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
                .frame(maxHeight: open ? nil : fold, alignment: .top)
                .clipped()
                .mask {
                    if folded {
                        LinearGradient(stops: [.init(color: .black, location: 0.8), .init(color: .clear, location: 1)],
                                       startPoint: .top, endPoint: .bottom)
                    } else {
                        Rectangle()
                    }
                }
                .contentShape(.rect)
                .overlay(alignment: .bottom) { if folded { toggle } }
            if open && height > fold { toggle }
        }
    }

    private var toggle: some View {
        Button {
            withAnimation(Theme.Motion.layout) { open.toggle() }
        } label: {
            Text(open ? "收起" : "展开")
                .font(Theme.Font.labelMedium)
                .lineLimit(1)
                .padding(.horizontal, Theme.Space.l)
                .padding(.vertical, Theme.Space.xs)
                .surface(interactive: true, in: .capsule)
                // The pill stays small, its tap target does not: a finger a little off it must not open the topic. The
                // pill keeps to the topic's edge: at the foot of the fold, right under the open topic.
                .scaledFrame(minHeight: Theme.Size.tapTarget, alignment: open ? .top : .bottom)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("lark.topic.fold")
    }
}
