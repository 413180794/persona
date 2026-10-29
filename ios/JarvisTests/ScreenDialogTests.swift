import Foundation
import Testing
@testable import Jarvis

struct ScreenDialogTests {
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
}
