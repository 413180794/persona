package persona.core.domain.sort

import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText
import persona.core.contracts.Session

/** What closing an agent asks, the same from the list's long press, its swipe and the chat's menu (iOS CloseAgentSheet). */
data class CloseConfirmation(val title: CoreText, val message: CoreText)

/**
 * Says which agent goes and where: its title, then "machine / space › tab", then what happens (the agent quits, its tab
 * closes, work in progress stops) and that the conversation stays for later.
 */
fun Session.closeConfirmation(machineName: String?): CloseConfirmation {
    val machine = (machineName?.takeIf(String::isNotBlank) ?: machineId).takeIf(String::isNotBlank)
        ?.let(CoreText::plain) ?: CoreText.of(Key.ComputerFallback)
    val tab = slotName?.takeIf(String::isNotBlank)
    val place = listOfNotNull(spaceName?.takeIf(String::isNotBlank), tab).joinToString(" › ")
    val where = CoreText.joined(" / ", listOf(machine, CoreText.plain(place)))
    return CloseConfirmation(
        CoreText.of(Key.CloseAgentTitle),
        if (tab != null) CoreText.of(Key.CloseAgentMessageTab, displayTitle, where, machine, agentLabel, tab)
        else CoreText.of(Key.CloseAgentMessagePane, displayTitle, where, machine, agentLabel),
    )
}
