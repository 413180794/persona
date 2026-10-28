import Foundation
import Testing
@testable import Jarvis

struct ScreenDialogTests {
    @Test func readsAnAskUserQuestionDialog() throws {
        let screen = """
         Ran 1 shell command
        ────────────────────────────────────────────────
         ☐ 取消发布单

         │沙箱镜像第一次构建失败：SES 用 garble 混淆编译，而我选的 hertz-c
         │编译通过，修复也已推送（a7553e20）。

        ❯ 1. 同意取消 37997 (Recommended)
             用 example-cli release delivery cancel 37997 取消，然后用修复后的 commit
             重新发一次。
          2. 先不取消
             我停在这里；镜像、Nacos 和 e2e 都先不动。
          3. Type something.
        ────────────────────────────────────────────────
          4. Chat about this

         Enter to select · ↑/↓ to navigate · Esc to cancel
        """
        let d = try #require(ScreenParser.dialog(screen))
        #expect(d.header == "取消发布单")
        #expect(d.question == "沙箱镜像第一次构建失败：SES 用 garble 混淆编译，而我选的 hertz-c编译通过，修复也已推送（a7553e20）。")
        #expect(d.options.map(\.key) == ["1", "2", "3", "4"])
        #expect(d.options[0].label == "同意取消 37997 (Recommended)")
        #expect(d.options[0].detail == "用 example-cli release delivery cancel 37997 取消，然后用修复后的 commit重新发一次。")
        #expect(d.options[1].detail == "我停在这里；镜像、Nacos 和 e2e 都先不动。")
        #expect(d.options[2].kind == .typeSomething)
        #expect(d.options[2].detail == nil)
        #expect(d.options[3].kind == .chat)
    }

    @Test func readsAPermissionPrompt() throws {
        let screen = """
        ────────────────────────────────────────────────
         Bash command

           git push origin master
           Push the release commit

         Do you want to proceed?
         ❯ 1. Yes
           2. Yes, and don't ask again for git push commands in /code/app
           3. No, and tell Claude what to do differently (esc)
        """
        let d = try #require(ScreenParser.dialog(screen))
        #expect(d.header == nil)
        #expect(d.question == "Do you want to proceed?")
        #expect(d.detail?.contains("git push origin master") == true)
        #expect(d.options.count == 3)
        #expect(d.options.allSatisfy { $0.detail == nil })
    }

    @Test func noDialogWithoutNumberedOptions() {
        #expect(ScreenParser.dialog("> hello\n  some output") == nil)
    }
}
