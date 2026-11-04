package persona.core.domain.sort

import persona.core.contracts.Session

/**
 * What 删除对话 asks, the same from the Agents list and the chat's menu (the web's DeleteSessionDialog, iOS
 * DeleteSessionText): it only leaves every app's list; the record on the computer stays and brings it back once used.
 */
fun Session.deleteConfirmation(machineName: String?): CloseConfirmation {
    val place = machineName?.takeIf(String::isNotBlank)?.let { "$it 上的" } ?: "电脑上的"
    // The command alone ("claude --resume"), without the directory and id.
    val back = resumeCommand(cwd = "")?.substringBeforeLast(' ')?.let { "之后在电脑上用 $it 继续，它会重新出现。" } ?: "之后再用到它，它会重新出现。"
    return CloseConfirmation("删除这个对话？", "「$displayTitle」会从各端的列表里移除。${place}对话记录不会删除，$back")
}
