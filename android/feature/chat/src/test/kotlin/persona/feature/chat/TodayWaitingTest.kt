package persona.feature.chat

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test
import persona.core.contracts.Interaction
import persona.core.contracts.Question
import persona.core.contracts.Session
import persona.core.contracts.Todo
import persona.core.contracts.TodoAgent
import persona.core.domain.answer.askLine
import persona.core.domain.answer.firstBySession

/** The "Today" card's "Needs you" row says what each waiting task asks (#4752; the web's #4478). */
class TodayWaitingTest {
    private fun session(id: String) = Session(machineId = "mac", sessionId = id, state = "needs_you")
    private fun todo(id: String, agent: String) = Todo(id = id, agents = listOf(TodoAgent("mac", agent)))
    private val sessions = listOf(session("s1"), session("s2"), session("s3"))
    private val lead: (Todo) -> Session? = { t -> t.activeAgents.firstNotNullOfOrNull { a -> sessions.firstOrNull { it.sessionId == a.sessionId } } }
    private val permission = Interaction("c1", machineId = "mac", sessionId = "s1", kind = "permission", toolName = "Bash", summary = "swift test")
    private val question = Interaction("c2", machineId = "mac", sessionId = "s2", kind = "question", questions = listOf(Question("Which framework?")))
    private val cards = listOf(permission, question).firstBySession()
    private val todos = listOf(todo("t1", "s1"), todo("t2", "s2"), todo("t3", "s3"), todo("t4", "s1"))

    @Test fun oneWaitingTaskSaysOnlyWhatItsCardAsks() {
        val one = todayWaiting(listOf("t1" to "Tests"), todos, lead, cards)
        assertNotNull(permission.askLine())
        assertEquals(TodayWaiting(listOf(TodayWaitingLine("Tests", permission.askLine())), 0), one)
    }

    @Test fun oneWaitingTaskWithoutACardAddsNoLine() {
        assertEquals(TodayWaiting(emptyList(), 0), todayWaiting(listOf("t3" to "Deploy"), todos, lead, cards))
        assertEquals(TodayWaiting(emptyList(), 0), todayWaiting(listOf("gone" to "Deploy"), todos, lead, cards))
        // a card answered here is no longer among the pending ones, so it is not one the task waits on
        assertEquals(TodayWaiting(emptyList(), 0), todayWaiting(listOf("t1" to "Tests"), todos, lead, emptyMap()))
    }

    @Test fun severalListEachWithItsAskTheTitleAloneWithoutACardAndCountTheRest() {
        val waiting = listOf("t1" to "Tests", "t2" to "Framework", "t3" to "Deploy", "t4" to "Release")
        val lines = todayWaiting(waiting, todos, lead, cards)
        assertEquals(listOf(TodayWaitingLine("Tests", permission.askLine()), TodayWaitingLine("Framework", question.askLine()),
            TodayWaitingLine("Deploy", null)), lines.lines)
        assertEquals(1, lines.more)
        assertEquals(0, todayWaiting(waiting.take(3), todos, lead, cards).more)
        assertEquals(2, todayWaiting(waiting.take(2), todos, lead, cards).lines.size)
    }

    @Test fun nothingWaitingIsNothing() {
        assertEquals(TodayWaiting(emptyList(), 0), todayWaiting(emptyList(), todos, lead, cards))
    }
}
