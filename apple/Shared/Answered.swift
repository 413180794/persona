import Foundation

// Shared answered interaction formatting and status handling (docs/protocol.md, #1688).
// Aligns with web/src/lib/answered.ts and Android HubInteractionCenter.

nonisolated struct ResolveResult: Codable, Sendable {
    var ok: Bool?
    var already: Bool?
    var status: String?
    var resolution: Resolution?
}

nonisolated struct AnswerLine: Hashable, Sendable {
    var label: String
    var answer: String
}

nonisolated enum Answered {
    static var modeLabels: [String: String] {
        [
            "acceptEdits": String(localized: "自动接受编辑"),
            "bypassPermissions": String(localized: "全部放行"),
            "default": String(localized: "每步确认")
        ]
    }

    static func answerLines(_ ix: Interaction, _ r: Resolution) -> [AnswerLine] {
        if r.dismiss == true { return [AnswerLine(label: "", answer: String(localized: "交回电脑处理"))] }
        switch ix.kind {
        case .question:
            let answers = r.answers ?? [:]
            return (ix.questions ?? []).map { q in
                AnswerLine(label: q.question, answer: answers[q.question] ?? String(localized: "（未回答）"))
            }
        case .plan:
            if r.approve == true {
                let mode = r.mode ?? ""
                let text = modeLabels[mode].map { String(localized: "批准，\($0)", comment: "Plan approved, with the permission mode") } ?? String(localized: "批准")
                return [AnswerLine(label: "", answer: text)]
            }
            if let feedback = r.feedback, !feedback.isEmpty {
                return [AnswerLine(label: "", answer: String(localized: "打回：\(feedback)"))]
            }
            return [AnswerLine(label: "", answer: String(localized: "打回"))]
        case .permission:
            if r.allow == true {
                return [AnswerLine(label: "", answer: r.always == true ? String(localized: "总是允许") : String(localized: "允许"))]
            }
            if r.allow == false {
                return [AnswerLine(label: "", answer: String(localized: "拒绝"))]
            }
            return [AnswerLine(label: "", answer: String(localized: "已处理"))]
        default:
            let options = ix.dialog?.options ?? []
            func labelOf(_ key: String) -> String {
                options.first(where: { $0.key == key })?.label ?? key
            }
            let isSecret = ix.secret == true || r.text == "<redacted>"
            let typed = (r.text?.isEmpty == false) ? (isSecret ? String(localized: "（已输入）") : r.text!) : nil
            var picked: [String] = []
            if let opts = r.options {
                picked = opts.map(labelOf)
            } else if let opt = r.option {
                picked = [labelOf(opt)]
            }
            let all = (picked + (typed.map { [$0] } ?? [])).filter { !$0.isEmpty }
            let combined = all.joined(separator: String(localized: "，", comment: "Separator between items in a list"))
            return [AnswerLine(label: "", answer: combined.isEmpty ? String(localized: "已处理") : combined)]
        }
    }

    static func endedText(_ status: String?) -> String {
        switch status {
        case "expired":
            return String(localized: "提问已过期，回答没有送到")
        case "cancelled":
            return String(localized: "电脑上已撤回该提问，回答没有送到")
        case "dismissed":
            return String(localized: "提问已交回电脑处理，回答没有送到")
        default:
            return String(localized: "提问已结束，回答没有送到")
        }
    }
}
