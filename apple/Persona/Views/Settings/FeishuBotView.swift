import SwiftUI

/// 设置 › 连接器 › 飞书机器人 (docs/bots.md): talk to Persona from Feishu. The user makes a custom app with a bot on the
/// open platform, enters its id and secret here, pairs by sending the bot a code, and switches on what it may do.
struct FeishuBotView: View {
    @State private var model = FeishuBotModel()
    @State private var confirmUnpair = false

    var body: some View {
        SettingsForm {
            ChannelLinkSection(channelID: "feishu")
            statusSection
            credentialsSection
            if model.status?.hasSecret == true {
                pairingSection
                featuresSection
            }
            if let agents = model.status?.agents, !agents.isEmpty {
                Section("飞书里聊过的 Agent") {
                    ForEach(agents, id: \.sessionID) { a in
                        LabeledContent(a.title) { Text(a.machineID).lineLimit(1) }
                    }
                }
            }
            setupSection
        }
        .themedPage()
        .detailTitle("飞书机器人")
        .navigationBarTitleDisplayMode(.inline)
        .task { await model.load() }
        .refreshable { await model.load() }
        .task(id: model.status?.pairing?.code) { // waiting for the code: look again until it is used
            guard model.status?.pairing != nil else { return }
            while !Task.isCancelled, model.status?.pairing != nil, model.status?.owner == nil {
                try? await Task.sleep(for: .seconds(3))
                await model.load()
            }
        }
        .confirmationDialog("解除配对？", isPresented: $confirmUnpair, titleVisibility: .visible) {
            Button("解除配对", role: .destructive) { Task { await model.unpair() } }
        } message: {
            Text("机器人会不再回应现在的飞书账号，直到重新配对。")
        }
    }

    // MARK: Sections

    private var statusSection: some View {
        Section {
            HStack(spacing: Theme.Space.m) {
                BrandMark(Theme.Brand.lark, side: Theme.Size.wellM)
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(model.status?.botName ?? "飞书机器人").font(Theme.Font.heading).lineLimit(1)
                    HStack(spacing: Theme.Space.sm) {
                        Circle().fill(tint).scaledFrame(width: Theme.Size.dotS, height: Theme.Size.dotS)
                        Text(model.stateText).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(2)
                    }
                }
                Spacer(minLength: Theme.Space.s)
                if model.loading || model.saving { ProgressView() }
            }
            .padding(.vertical, Theme.Space.xs)
            Toggle("打开飞书机器人", isOn: Binding(get: { model.status?.enabled ?? false }, set: { on in Task { await model.setEnabled(on) } }))
                .disabled(model.status == nil || model.saving)
            if let at = model.status?.lastEventAt, at > 0 {
                LabeledContent("最近收到") {
                    TimelineView(.periodic(from: .now, by: 30)) { ctx in
                        Text(Fmt.relative(Date(timeIntervalSince1970: TimeInterval(at)), now: ctx.date)).lineLimit(1)
                    }
                }
            }
            if let e = model.error, !e.isEmpty {
                LoadProblem(text: e, labeled: false)
            }
        } footer: {
            Text("在飞书里私聊这个机器人，就是在和 Persona 说话。机器人只回应配对过的你，群里不回应。")
        }
    }

    private var credentialsSection: some View {
        Section {
            #if os(macOS)
            // A Mac form puts the title on the left and the field on the right: the title stays the field's name, what
            // to type goes in the prompt (#1221).
            TextField("App ID", text: $model.appID, prompt: Text("必填")).autocorrectionDisabled()
            SecureField("App Secret", text: $model.appSecret, prompt: Text(model.status?.hasSecret == true ? "已保存，留空不改" : "必填"))
            #else
            TextField("App ID", text: $model.appID)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .font(Theme.Font.code)
            SecretField(model.status?.hasSecret == true ? "App Secret（已保存，留空不改）" : "App Secret", filled: "已填写，点验证并保存",
                        text: $model.appSecret, identifier: "bot.appSecret")
            #endif
            Picker("平台", selection: $model.domain) {
                Text("飞书").tag("feishu")
                Text("Lark").tag("lark")
                if model.domain != "feishu" && model.domain != "lark" { Text(model.domain).tag(model.domain) }
            }
            #if os(macOS)
            // A standard button at the group's trailing end, not a bar the width of the form (#1221).
            HStack {
                Spacer(minLength: 0)
                Button { Task { await model.saveCredentials() } } label: { Text("验证并保存").lineLimit(1) }
                    .formButton(prominent: true)
                    .disabled(!model.credentialsChanged || model.appID.isEmpty || model.saving)
            }
            #else
            Button {
                Task { await model.saveCredentials() }
            } label: {
                Text("验证并保存").lineLimit(1).frame(maxWidth: .infinity)
            }
            .disabled(!model.credentialsChanged || model.appID.isEmpty || model.saving)
            #endif
        } header: {
            Text("应用凭证")
        } footer: {
            if model.status?.fromConfig == true {
                Text("现在用的是 hub.json 里的凭证；在这里保存会改用这里的。")
            } else {
                Text("App Secret 加密存在 hub 上，不会再显示出来。")
            }
        }
    }

    @ViewBuilder private var pairingSection: some View {
        Section {
            if let owner = model.status?.owner {
                LabeledContent("已配对") { Text(owner.name.isEmpty ? "飞书用户" : owner.name).lineLimit(1) }
                if let target = model.status?.target, !target.isEmpty {
                    LabeledContent("消息现在发给") { Text(target).lineLimit(1) }
                }
                Button {
                    Task { await model.test() }
                } label: {
                    Label("发一条测试消息", systemImage: Theme.Icon.sendMail).lineLimit(1)
                }
                .disabled(model.saving || model.status?.connected != true)
                Button(role: .destructive) { confirmUnpair = true } label: {
                    Label("解除配对", systemImage: Theme.Icon.close).lineLimit(1)
                }
            } else if let p = model.status?.pairing, Date(timeIntervalSince1970: TimeInterval(p.expiresAt)) > .now {
                VStack(alignment: .leading, spacing: Theme.Space.s) {
                    Text("在飞书里把这串数字发给机器人").font(Theme.Font.secondary).foregroundStyle(.secondary)
                    Text(p.code).font(Theme.Font.display).monospacedDigit().textSelection(.enabled)
                    TimelineView(.periodic(from: .now, by: 1)) { ctx in
                        let left = max(0, Int(Date(timeIntervalSince1970: TimeInterval(p.expiresAt)).timeIntervalSince(ctx.date)))
                        Text("\(left / 60) 分 \(left % 60) 秒内有效").font(Theme.Font.label).foregroundStyle(.tertiary).monospacedDigit()
                    }
                }
                .padding(.vertical, Theme.Space.xs)
                Button { UIPasteboard.general.string = p.code; Haptics.success() } label: {
                    Label("拷贝配对码", systemImage: Theme.Icon.copy).lineLimit(1)
                }
            } else {
                Button {
                    Task { await model.pair() }
                } label: {
                    Label("配对", systemImage: Theme.Icon.pairing).lineLimit(1)
                }
                .disabled(model.saving || model.status?.enabled != true)
            }
        } header: {
            Text("配对")
        } footer: {
            if model.status?.owner == nil {
                Text(model.status?.enabled == true ? "点「配对」拿一个 6 位数字，10 分钟内在飞书里发给机器人。" : "先打开飞书机器人再配对。")
            }
        }
    }

    private var featuresSection: some View {
        Section {
            feature(\.main, Theme.Icon.jarvis, "和 Persona 对话", "消息发进主对话（/new、/list 换一段），回答发回飞书")
            feature(\.cards, Theme.Icon.needsYou, "提问和批准", "Persona 和飞书里驱动的 Agent 要你回答、批准时，在飞书里发卡片，点一下就行")
            feature(\.allCards, Theme.Icon.notifications, "所有 Agent 的提问", "各台电脑上所有 Agent 的提问和批准都发到飞书")
                .disabled(!features.cards)
            feature(\.pushes, Theme.Icon.unmute, "推送也发到飞书", "Persona 的通知、定时任务的结果也发一份到飞书")
            feature(\.sync, Theme.Icon.inProgress, "同步主对话", "在 App 里、定时任务在主对话里的回答，也发到飞书")
            feature(\.agents, Theme.Icon.agents, "驱动 Agent", "/agents 选各台电脑上的 Claude Code / Codex，在飞书里和它聊；/start 开新的")
        } header: {
            Text("能做什么")
        } footer: {
            Text("主对话能用你的全部数据（飞书、邮件、手机里的日历和位置……），机器人发出的消息会留在飞书里。替你出面的操作照常先问你。")
        }
    }

    private var setupSection: some View {
        Section {
            step(1, "在飞书开放平台建一个企业自建应用，开启「机器人」能力")
            step(2, "权限里开通：读取与发送单聊消息（im:message、im:message:send_as_bot）、获取单聊中发给机器人的消息、上传和下载消息里的资源；能读通讯录名字更好（contact:user.base:readonly）")
            step(3, "「事件与回调」里选「使用长连接接收事件」，订阅「接收消息」和「卡片回传交互」")
            step(4, "发布版本（公司里可能要管理员审核），把 App ID、App Secret 填到上面")
        } header: {
            Text("怎么建机器人")
        } footer: {
            Text("长连接由 hub 主动连出去，hub 不需要公网地址。")
        }
    }

    // MARK: Pieces

    private var features: FeishuBotStatus.Features { model.features }

    private var tint: Color {
        guard let st = model.status, st.enabled else { return .secondary }
        if !st.connected { return Theme.danger }
        return st.owner == nil ? Theme.caution : Theme.positive
    }

    private func feature(_ key: WritableKeyPath<FeishuBotStatus.Features, Bool>, _ symbol: String, _ title: String, _ detail: String) -> some View {
        Toggle(isOn: Binding(get: { model.features[keyPath: key] }, set: { on in Task { await model.set(key, on) } })) {
            Label {
                VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                    Text(title)
                    Text(detail).font(Theme.Font.caption).foregroundStyle(.secondary)
                }
            } icon: {
                Image(systemName: symbol).foregroundStyle(Theme.accent)
            }
        }
        .disabled(model.saving)
    }

    private func step(_ n: Int, _ text: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: Theme.Space.m) {
            Text("\(n)").font(Theme.Font.captionStrong).foregroundStyle(Theme.accent).monospacedDigit()
            Text(text).font(Theme.Font.caption).foregroundStyle(.secondary)
        }
        .padding(.vertical, Theme.Space.xxs)
    }
}
