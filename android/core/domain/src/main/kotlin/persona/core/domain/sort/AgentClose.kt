package persona.core.domain.sort

import persona.core.contracts.Session

/** What closing an agent asks, the same from the list's long press, its swipe and the chat's menu (iOS CloseAgentSheet). */
data class CloseConfirmation(val title: String, val message: String)

/**
 * Says which agent goes and where: its title, then 「机器 / 空间 › 标签」, then what happens (the agent quits, its tab
 * closes, work in progress stops) and that the conversation stays for later.
 */
fun Session.closeConfirmation(machineName: String?): CloseConfirmation {
    val machine = machineName?.takeIf(String::isNotBlank) ?: machineId.ifBlank { "电脑" }
    val tab = slotName?.takeIf(String::isNotBlank)
    val place = listOfNotNull(spaceName?.takeIf(String::isNotBlank), tab).joinToString(" › ")
    val pane = tab?.let { "，并关掉它在终端里的标签页「$it」（分屏的标签页只关它这一格）" } ?: "，并关掉它所在的终端窗格"
    val where = listOf(machine, place).filter(String::isNotEmpty).joinToString(" / ")
    return CloseConfirmation(
        "关闭这个 Agent？",
        "「$displayTitle」\n$where\n\n会退出 $machine 上的 $agentLabel$pane。正在做的事会中断；对话记录会保留，之后可以在 Agents 的「历史」里继续。",
    )
}
