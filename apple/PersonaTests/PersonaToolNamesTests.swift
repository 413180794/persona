import Foundation
import Testing
@testable import Persona

/// Persona's own MCP tools (server `persona`) read as Persona in a run of steps, never as the raw tool name (#2494).
@MainActor
struct PersonaToolNamesTests {
    @Test func commonToolsGetAVerb() {
        #expect(ToolPresentation.verb("mcp__persona__agent_read") == "读 Agent 对话")
        #expect(ToolPresentation.verb("mcp__persona__agent_send") == "给 Agent 发消息")
        #expect(ToolPresentation.verb("mcp__persona__send_to_agent") == "给 Agent 发消息")
        #expect(ToolPresentation.verb("mcp__persona__kb_search") == "查知识库")
        #expect(ToolPresentation.verb("mcp__persona__kb_update") == "更新知识库")
        #expect(ToolPresentation.verb("mcp__persona__agent_start") == "派任务")
        #expect(ToolPresentation.verb("mcp__persona__ask_user") == "问你")
        #expect(ToolPresentation.verb("mcp__persona__schedule_followup") == "提醒")
    }

    @Test func theRestReadLikeTheWeb() {
        #expect(ToolPresentation.verb("mcp__persona__github_read") == "调用 Persona · github read")
        #expect(ToolPresentation.verb("mcp__persona__imessage_search") == "调用 Persona · imessage search")
        #expect(ToolPresentation.verb("mcp__persona__some_new_tool") == "调用 Persona · some new tool")
    }

    @Test func toolsTheGenericTableNamesKeepTheirVerb() {
        #expect(ToolPresentation.verb("mcp__persona__conversation_read") == "读对话")
        #expect(ToolPresentation.verb("mcp__persona__computer_state") == "看电脑")
        #expect(ToolPresentation.verb("mcp__persona__browser_look") == "看浏览器")
        #expect(ToolPresentation.verb("mcp__persona__phone_calendar_events") == "iPhone")
        #expect(ToolPresentation.verb("mcp__persona__mail_send") == "邮件")
    }

    @Test func otherServersAndBareNamesAreUntouched() {
        #expect(ToolPresentation.verb("mcp__github__create_issue") == "create_issue")
        #expect(ToolPresentation.verb("agent_read") == "agent_read")
        #expect(ToolPresentation.verb("ask_user") == "提问")
        #expect(ToolPresentation.verb("AskUserQuestion") == "提问")
        #expect(ToolPresentation.verb("Read") == "读取")
    }

    @Test func serverOfAFullName() {
        #expect(PersonaToolNames.server("mcp__persona__agent_read") == "persona")
        #expect(PersonaToolNames.server("mcp__plugin_x_y__tool") == "plugin_x_y")
        #expect(PersonaToolNames.server("mcp____tool") == nil)
        #expect(PersonaToolNames.server("Read") == nil)
        #expect(PersonaToolNames.servers == ["persona"])
        #expect(PersonaToolNames.isPersona("mcp__persona__kb_get"))
        for server in ["other-product", "personas", "personad"] {
            #expect(!PersonaToolNames.isPersona("mcp__\(server)__kb_get"))
        }
        #expect(!PersonaToolNames.isPersona("mcp__cua-driver__click"))
    }

    /// The full name is what everything else matches on; only the words change.
    @Test func baseNameIsUnchanged() {
        #expect(ToolPresentation.baseName("mcp__persona__agent_read") == "agent_read")
        #expect(ToolPresentation.symbol("mcp__persona__agent_read") == "puzzlepiece.extension")
    }
}
