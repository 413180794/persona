import SwiftUI

/// An install, upgrade or removal on the extension's market page (#3307): what is going on, the hub's steps with the
/// one it is at (a bar while it downloads, when the hub knows the size), and for one that failed the step it stopped
/// at, why, and 重试. While it waits for the user's yes: 查看权限 opens the preview again, 取消 drops it. An older hub
/// has no steps: the section says what it is doing and for how long, so the page never looks stuck.
struct ExtensionOperationSection: View {
    var operation: ExtensionOperation
    var name: String
    var retry: () -> Void
    var review: () -> Void
    var dismiss: () -> Void

    private var failed: Bool { operation.state == .failed }
    private var waiting: Bool { operation.state == .needsConsent }
    /// Still downloading or checking (the consent not reached): the hub can stop it.
    private var cancelable: Bool {
        operation.isRunning && !operation.isLocal && operation.steps.contains { $0.id == "consent" && $0.state == .pending }
    }

    var body: some View {
        Section {
            headline
            // What to do (why it stopped and 重试, or the permissions to agree to) right under the title, before the
            // steps: eight of them push it off the screen.
            if failed { failure }
            if waiting { consent }
            if operation.steps.isEmpty {
                if operation.isRunning {
                    Text(ExtensionOperationText.waitingNote(operation.kind))
                        .font(Theme.Font.label).foregroundStyle(.secondary)
                        .accessibilityIdentifier("extension-operation-waiting")
                }
            } else {
                ForEach(operation.shownSteps) {
                    ExtensionOperationStepRow(step: $0, kind: operation.kind, current: $0.id == operation.current?.id)
                }
            }
            if cancelable {
                Button(action: dismiss) {
                    Text("取消").lineLimit(1).formActionWidth()
                }
                .themedButton()
                .padding(.vertical, Theme.Space.xs)
                .accessibilityIdentifier("extension-operation-cancel")
            }
        } header: {
            SettingsHeading(failed ? String(localized: "没有完成") : waiting ? String(localized: "等你同意") : String(localized: "进行中"))
        }
    }

    private var headline: some View {
        HStack(spacing: Theme.Space.m) {
            Group {
                if failed {
                    Image(systemName: Theme.Icon.errorFilled).resizable().scaledToFit().foregroundStyle(Theme.danger)
                } else if waiting {
                    Image(systemName: Theme.Icon.infoFilled).resizable().scaledToFit().foregroundStyle(Theme.caution)
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .scaledFrame(width: Theme.Size.iconS, height: Theme.Size.iconS)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(ExtensionOperationText.title(operation) + " " + name).font(Theme.Font.bodyStrong).lineLimit(1)
                let line = [ExtensionOperationText.versions(operation), ExtensionOperationText.progress(operation),
                            ExtensionOperationText.actor(operation)].compactMap { $0 }
                if !line.isEmpty {
                    Text(line.joined(separator: " · ")).font(Theme.Font.label).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            Spacer(minLength: Theme.Space.s)
            if operation.isRunning, let start = operation.startedAt {
                TimelineView(.periodic(from: start, by: 1)) { context in
                    Text(ExtensionOperationText.elapsed(since: start, now: context.date))
                        .font(Theme.Font.label).foregroundStyle(.secondary).monospacedDigit().lineLimit(1)
                }
            }
        }
        .padding(.vertical, Theme.Space.xxs)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("extension-operation")
        .accessibilityValue(operation.state.rawValue)
    }

    @ViewBuilder private var failure: some View {
        VStack(alignment: .leading, spacing: Theme.Space.xxs) {
            if let step = operation.failedStep {
                Text("停在「\(ExtensionOperationText.step(step.id, kind: operation.kind))」")
                    .font(Theme.Font.labelStrong).foregroundStyle(Theme.danger)
            }
            if let e = operation.error, !e.isEmpty {
                Text(e).font(Theme.Font.label).foregroundStyle(Theme.danger).textSelection(.enabled)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("extension-operation-error")
        buttons(primary: String(localized: "重试"), id: "extension-operation-retry", action: retry,
                secondary: String(localized: "关闭"), secondaryID: "extension-operation-dismiss")
    }

    @ViewBuilder private var consent: some View {
        Text("已经下载并检查好了，看一下它要的权限，同意后接着安装。")
            .font(Theme.Font.label).foregroundStyle(.secondary)
        buttons(primary: String(localized: "查看权限"), id: "extension-operation-review", action: review,
                secondary: String(localized: "取消"), secondaryID: "extension-operation-cancel")
    }

    /// Two buttons in the card (a FormAction's clear row would cut the card in two).
    private func buttons(primary: String, id: String, action: @escaping () -> Void,
                         secondary: String, secondaryID: String) -> some View {
        HStack(spacing: Theme.Space.s) {
            Button(action: action) {
                Text(primary).lineLimit(1).formActionWidth()
            }
            .themedButton(prominent: true)
            .accessibilityIdentifier(id)
            Button(action: dismiss) {
                Text(secondary).lineLimit(1).formActionWidth()
            }
            .themedButton()
            .accessibilityIdentifier(secondaryID)
        }
        .padding(.vertical, Theme.Space.xs)
    }
}

/// One step: done, going (with its bar), waiting or where it failed.
private struct ExtensionOperationStepRow: View {
    var step: ExtensionOperationStep
    var kind: ExtensionOperation.Kind
    var current: Bool

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Space.m) {
            mark.scaledFrame(width: Theme.Size.markM, height: Theme.Size.markM)
            VStack(alignment: .leading, spacing: Theme.Space.xxs) {
                Text(ExtensionOperationText.step(step.id, kind: kind))
                    .font(current ? Theme.Font.secondaryMedium : Theme.Font.secondary)
                    .foregroundStyle(step.state == .pending ? Color.secondary : Color.primary)
                    .lineLimit(1)
                if step.state == .running, step.id == "download" || (step.id == "tools" && step.bytes != nil) {
                    // A size the hub does not know: a bar that cannot say how far.
                    Group {
                        if let f = step.fraction { ProgressView(value: f) } else { ProgressView() }
                    }
                    .progressViewStyle(.linear)
                    .tint(Theme.accent)
                    .accessibilityLabel(ExtensionOperationText.step(step.id, kind: kind))
                }
                if let d = ExtensionOperationText.detail(step) {
                    Text(d).font(Theme.Font.label).foregroundStyle(.secondary).monospacedDigit().lineLimit(2)
                }
                // The tools step's programs (#4507), a line each: which one downloads, which use the machine's own.
                ForEach(step.tools ?? []) { tool in
                    ExtensionOperationToolLine(tool: tool)
                }
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("extension-operation-step-\(step.id)")
        .accessibilityValue(step.state.rawValue)
    }

    private var mark: some View { ExtensionOperationMark(state: step.state) }
}

/// One program of the tools step: its state's mark, its name and what it is doing.
private struct ExtensionOperationToolLine: View {
    var tool: ExtensionOperationStep.Tool

    var body: some View {
        HStack(spacing: Theme.Space.s) {
            ExtensionOperationMark(state: tool.state)
                .scaledFrame(width: Theme.Size.markS, height: Theme.Size.markS)
            Text(tool.name).font(Theme.Font.code).lineLimit(1)
            if let note = ExtensionOperationText.tool(tool) {
                Text(note).font(Theme.Font.label).foregroundStyle(tool.state == .failed ? Theme.danger : Color.secondary).lineLimit(1)
            }
            Spacer(minLength: 0)
        }
        .accessibilityIdentifier("extension-operation-tool-\(tool.name)")
    }
}

/// A step's (or a program's) mark: done, going, failed or waiting.
private struct ExtensionOperationMark: View {
    var state: ExtensionOperationStep.State

    var body: some View {
        switch state {
        case .done:
            Image(systemName: Theme.Icon.successFilled).resizable().scaledToFit().foregroundStyle(Theme.positive)
        case .running:
            ProgressView().controlSize(.mini)
        case .failed:
            Image(systemName: Theme.Icon.errorFilled).resizable().scaledToFit().foregroundStyle(Theme.danger)
        case .pending, .skipped:
            Image(systemName: Theme.Icon.unchecked).resizable().scaledToFit().foregroundStyle(.tertiary)
        }
    }
}
