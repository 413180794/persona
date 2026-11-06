import Foundation
import Testing
@testable import Persona

struct ScreenDialogTests {
    /// Several questions: the TUI's tab bar is steps, not text above the question (it showed as a grey code box).
    @Test func readsTheTabBarOfSeveralQuestions() throws {
        let screen = """
        ────────────────────────────────────────────────
         ←  ☐ 审核策略  ☒ 推送代码  ✔ Submit  →

        这次「发布」想怎么处理审核？

        ❯ 1. 回复问题 (Recommended)
             先起草回复，确认后才发送。
          2. 直接重新提交
          3. Type something.
        ────────────────────────────────────────────────
          4. Chat about this
        """
        let d = try #require(ScreenParser.dialog(screen))
        #expect(d.steps == [.init(label: "审核策略", done: false), .init(label: "推送代码", done: true)])
        #expect(d.detail == nil)
        #expect(d.question == "这次「发布」想怎么处理审核？")
        // The hub's reading of the same dialog comes through `ScreenDialog.parsed`: the same steps.
        let hub = ScreenDialog(header: nil, question: d.question, detail: "←  ☐ 审核策略  ☒ 推送代码  ✔ Submit  →", options: [], rev: "x")
        #expect(hub.parsed.steps.count == 2)
        #expect(hub.parsed.detail == nil)
        #expect(ScreenParser.steps(in: "$ rm -rf build").0.isEmpty)
    }

    @Test func readsAnAskUserQuestionDialog() throws {
        let screen = """
         Ran 1 shell command
        ────────────────────────────────────────────────
         ☐ 发布方式

         │第一次构建失败：镜像里缺一个依赖，补上以后
         │编译通过，修复也已推送（a1b2c3d）。

        ❯ 1. 取消这次发布 (Recommended)
             用修复后的 commit 重新发一次，
             旧的那次先撤掉。
          2. 先不取消
             我停在这里，什么都不动。
          3. Type something.
        ────────────────────────────────────────────────
          4. Chat about this

         Enter to select · ↑/↓ to navigate · Esc to cancel
        """
        let d = try #require(ScreenParser.dialog(screen))
        #expect(d.header == "发布方式")
        #expect(d.question == "第一次构建失败：镜像里缺一个依赖，补上以后编译通过，修复也已推送（a1b2c3d）。")
        #expect(d.options.map(\.key) == ["1", "2", "3", "4"])
        #expect(d.options[0].label == "取消这次发布 (Recommended)")
        #expect(d.options[0].detail == "用修复后的 commit 重新发一次，旧的那次先撤掉。")
        #expect(d.options[1].detail == "我停在这里，什么都不动。")
        #expect(d.options[2].kind == .typeSomething)
        #expect(d.options[2].detail == nil)
        #expect(d.options[3].kind == .chat)
    }

    @Test func readsAPermissionPrompt() throws {
        let screen = """
        ────────────────────────────────────────────────
         Bash command

           git push origin main
           Push the release commit

         Do you want to proceed?
         ❯ 1. Yes
           2. Yes, and don't ask again for git push commands in /code/app
           3. No, and tell Claude what to do differently (esc)
        """
        let d = try #require(ScreenParser.dialog(screen))
        #expect(d.header == nil)
        #expect(d.question == "Do you want to proceed?")
        #expect(d.detail?.contains("git push origin main") == true)
        #expect(d.options.count == 3)
        #expect(d.options.allSatisfy { $0.detail == nil })
    }

    @Test func noDialogWithoutNumberedOptions() {
        #expect(ScreenParser.dialog("> hello\n  some output") == nil)
    }

    /// A multi-select question as the hub reads it (Claude Code 2.1.316): boxes ticked at the computer, the free text,
    /// the Submit / Next row, the tab bar's questions; an older hub's JSON without them still decodes.
    @Test func decodesAMultiSelectDialog() throws {
        let json = #"""
        {"header":"","question":"哪些平台要一起发？","options":[
          {"key":"1","label":"手机","detail":"iPhone 版本。","keys":["1"],"checked":true},
          {"key":"2","label":"手表","keys":["2"]},
          {"key":"3","label":"Type something","kind":"type_something","keys":["3"],"checked":true,"text":"电脑"},
          {"key":"4","label":"Chat about this","kind":"chat","keys":["4"]}],
         "multi":true,"submit":"Next","steps":[{"label":"环境","done":true},{"label":"平台","done":false}],"rev":"abc"}
        """#
        let d = try JSONDecoder().decode(ScreenDialog.self, from: Data(json.utf8)).parsed
        #expect(d.multi && d.submit == "Next" && !d.review)
        #expect(d.steps == [.init(label: "环境", done: true), .init(label: "平台", done: false)])
        #expect(d.options.map(\.checked) == [true, false, true, false])
        #expect(d.options[2].kind == .typeSomething && d.options[2].text == "电脑")
        #expect(ScreenForm.eyebrow(d) == "可多选")
        let old = try JSONDecoder().decode(ScreenDialog.self, from: Data(#"{"question":"q","options":[{"key":"1","label":"a"}],"rev":"x"}"#.utf8)).parsed
        #expect(!old.multi && old.submit == nil && !old.options[0].checked)
        // What the card sends: the ticks by key, the text beside them.
        let r = try JSONSerialization.jsonObject(with: JSONEncoder().encode(Resolution(text: "电脑", options: ["1"]))) as? [String: Any]
        #expect(r?["options"] as? [String] == ["1"] && r?["text"] as? String == "电脑")
    }
}
