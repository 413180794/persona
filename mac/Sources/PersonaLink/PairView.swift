import SwiftUI

/// 配对 / 修改连接: choose how this Mac and the hub find each other. Active (我去连 hub): paste the link `jarvisd hub
/// pair` printed, and look at or add the hub's addresses. Passive (让 hub 来连): make the link the hub's admin page takes.
struct PairView: View {
    let model: LinkModel
    @State private var link = ""
    @State private var machine = ""
    @State private var result: (ok: Bool, message: String)?
    @State private var busy = false
    @State private var newAddress = ""
    @State private var addressError: String?
    /// Which mode the window is showing (not yet switched to: that happens on pairing, or with the switch-back button).
    @State private var tab: String
    @State private var listen: String
    @State private var invite: (link: String, note: String)?
    @State private var inviteError: String?
    @State private var copied = false

    init(model: LinkModel, link: String = "", machine: String = "", result: (ok: Bool, message: String)? = nil,
         newAddress: String = "", addressError: String? = nil, tab: String? = nil, invite: (link: String, note: String)? = nil) {
        self.model = model
        _link = State(initialValue: link)
        _machine = State(initialValue: machine)
        _result = State(initialValue: result)
        _newAddress = State(initialValue: newAddress)
        _addressError = State(initialValue: addressError)
        _tab = State(initialValue: tab ?? (model.passive ? "passive" : "active"))
        _listen = State(initialValue: model.passivePairing?.listen ?? ":18663")
        _invite = State(initialValue: invite)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Picker("", selection: $tab) {
                Text("我去连 hub（主动）").tag("active")
                Text("让 hub 来连（被动）").tag("passive")
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            if tab == "passive" {
                passivePairing
            } else {
                pairing
                Divider()
                addresses
            }
        }
        .padding(20)
        .frame(width: 480)
    }

    /// The other mode is on but this one was paired before: back to it without pairing again.
    @ViewBuilder private func switchBack(to mode: String, paired: Bool, what: String) -> some View {
        if paired && (mode == "passive") != model.passive {
            HStack(spacing: 10) {
                Image(systemName: "arrow.uturn.backward.circle").foregroundStyle(.secondary)
                Text(what).font(.callout).foregroundStyle(.secondary).fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 8)
                Button {
                    Task { _ = await model.setMode(mode) }
                } label: {
                    Text("改用这个").lineLimit(1)
                }
            }
            .padding(10)
            .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 8))
        }
    }

    private var passivePairing: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text("让 hub 来连这台电脑").font(.title3.weight(.semibold))
            Text("这台电脑在本机监听一个端口，hub 按配对时拿到的指纹连进来。适合 hub 能直接连到的电脑：固定地址的台式机、服务器。笔记本换网络后 hub 连不到，选「我去连 hub」。")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            if let p = model.passivePairing, p.paired {
                Label {
                    Text("已经和 hub" + (p.hubName.map { "「\($0)」" } ?? "") + "配对" + (p.machine.map { "，这台是上面的「\($0)」" } ?? "")
                         + "。再生成链接、换一台 hub 配对，原来的就连不进来了。")
                        .fixedSize(horizontal: false, vertical: true)
                } icon: {
                    Image(systemName: "checkmark.seal").foregroundStyle(.green)
                }
                .font(.callout)
            }
            switchBack(to: "passive", paired: model.passivePairing?.paired == true, what: "现在是「我去连 hub」。被动模式的配对还在，不用重新配对。")
            HStack(spacing: 10) {
                Text("监听").font(.callout).foregroundStyle(.secondary)
                TextField(":18663", text: $listen)
                    .textFieldStyle(.roundedBorder)
                    .font(.system(.callout, design: .monospaced))
                    .frame(width: 140)
                Spacer(minLength: 8)
                Button {
                    Task { await makeInvite() }
                } label: {
                    Text(busy ? "生成中…" : "生成配对链接").lineLimit(1)
                }
                .keyboardShortcut(.defaultAction)
                .disabled(busy)
            }
            if let invite {
                VStack(alignment: .leading, spacing: 8) {
                    Text("把这条链接粘贴到 hub 管理界面「电脑 › 添加电脑 › hub 来连」，10 分钟内有效，只能用一次：")
                        .font(.callout)
                        .fixedSize(horizontal: false, vertical: true)
                    HStack(alignment: .top, spacing: 10) {
                        Text(invite.link)
                            .font(.system(.caption, design: .monospaced))
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                            .padding(8)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 8))
                        Button {
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString(invite.link, forType: .string)
                            copied = true
                        } label: {
                            Label(copied ? "已复制" : "复制", systemImage: copied ? "checkmark" : "doc.on.doc").lineLimit(1)
                        }
                    }
                    if !invite.note.isEmpty {
                        Text(invite.note)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            if let inviteError {
                Label {
                    Text(inviteError).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                } icon: {
                    Image(systemName: "xmark.octagon.fill").foregroundStyle(.red)
                }
                .font(.callout)
            }
        }
    }

    private func makeInvite() async {
        busy = true
        copied = false
        let r = await model.pairListen(listen: listen)
        busy = false
        if let l = r.link {
            invite = (l, Self.inviteNote(link: l, stderr: r.message))
            inviteError = nil
        } else {
            invite = nil
            inviteError = r.message
        }
    }

    /// Beside the link: which addresses are in it, and jarvis-link's line about the firewall.
    static func inviteNote(link: String, stderr: String) -> String {
        var lines: [String] = []
        let addrs = URLComponents(string: link)?.queryItems?.filter { $0.name == "addr" }.compactMap(\.value) ?? []
        if !addrs.isEmpty {
            lines.append("hub 按顺序试这些地址：\(addrs.joined(separator: "、"))；都连不到时在 hub 管理界面里改地址。")
        }
        lines += stderr.split(separator: "\n").map(String.init).filter { $0.hasPrefix("防火墙") }
        return lines.joined(separator: "\n")
    }

    private var pairing: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(model.pairedMachine == nil ? "配对这台电脑" : "重新配对").font(.title3.weight(.semibold))
            switchBack(to: "active", paired: model.pairedMachine != nil, what: "现在是「让 hub 来连」。主动模式的配对还在，不用重新配对。")
            Text("在 hub 所在的机器上运行 `jarvisd hub pair --machine <这台的 id>`，把它打出的链接粘贴到下面。链接 10 分钟内有效，只能用一次。")
                .font(.callout)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            TextField("jarvis://link?…", text: $link)
                .textFieldStyle(.roundedBorder)
                .font(.system(.callout, design: .monospaced))
                .truncationMode(.middle)
            HStack(spacing: 10) {
                TextField("机器 id（链接里带了就不用填）", text: $machine)
                    .textFieldStyle(.roundedBorder)
                Button {
                    Task { await pair() }
                } label: {
                    Text(busy ? "配对中…" : "配对").lineLimit(1).frame(minWidth: 56)
                }
                .keyboardShortcut(.defaultAction)
                .disabled(busy || link.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            if let result {
                Label {
                    Text(result.message).fixedSize(horizontal: false, vertical: true).textSelection(.enabled)
                } icon: {
                    Image(systemName: result.ok ? "checkmark.circle.fill" : "xmark.octagon.fill")
                        .foregroundStyle(result.ok ? Color.green : Color.red)
                }
                .font(.callout)
            }
        }
    }

    private var addresses: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .firstTextBaseline) {
                Text("hub 的地址").font(.headline)
                Text("按这个顺序尝试；连上以后 hub 会更新这份列表").font(.caption).foregroundStyle(.secondary)
            }
            if model.endpoints.isEmpty {
                Text("还没有：配对以后会从链接和 hub 那里拿到。").font(.callout).foregroundStyle(.secondary)
            } else {
                VStack(spacing: 0) {
                    ForEach(model.endpoints) { e in
                        HStack(spacing: 10) {
                            Text(LinkText.kinds[e.kind] ?? e.kind)
                                .font(.caption.weight(.medium))
                                .foregroundStyle(.secondary)
                                .frame(width: 64, alignment: .leading)
                            Text(LinkText.host(e.url))
                                .font(.system(.callout, design: .monospaced))
                                .lineLimit(1)
                                .truncationMode(.middle)
                                .textSelection(.enabled)
                            Spacer(minLength: 8)
                            if model.status?.state == "connected" && model.status?.address == e.url {
                                Text("在用").font(.caption.weight(.semibold)).foregroundStyle(.green).lineLimit(1)
                            }
                            if e.manual == true {
                                Button("移除") { Task { await model.removeAddress(e.url) } }
                                    .buttonStyle(.link)
                                    .lineLimit(1)
                            }
                        }
                        .padding(.vertical, 6)
                        .padding(.horizontal, 10)
                        if e.id != model.endpoints.last?.id { Divider().padding(.leading, 84) }
                    }
                }
                .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 8))
            }
            HStack(spacing: 10) {
                TextField("再加一个地址，比如 hub.example.com", text: $newAddress)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit { Task { await add() } }
                Button {
                    Task { await add() }
                } label: {
                    Text("添加").lineLimit(1).frame(minWidth: 40)
                }
                .disabled(model.pairedMachine == nil || newAddress.trimmingCharacters(in: .whitespaces).isEmpty)
            }
            if let addressError {
                Text(addressError).font(.caption).foregroundStyle(.red)
            }
        }
    }

    private func pair() async {
        busy = true
        result = await model.pair(link: link, machine: machine)
        busy = false
        if result?.ok == true { link = "" }
    }

    private func add() async {
        let r = await model.addAddress(newAddress)
        addressError = r.ok ? nil : r.message
        if r.ok { newAddress = "" }
    }
}
