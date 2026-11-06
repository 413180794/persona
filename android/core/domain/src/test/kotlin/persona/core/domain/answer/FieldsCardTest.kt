package persona.core.domain.answer

import persona.core.contracts.Interaction
import persona.core.contracts.JsonCodec
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

/** A permission card with fields to fill in (#4987): the hub refuses allow without their values (fill_in). */
class FieldsCardTest {
    private fun card(fields: String) = JsonCodec.decodeFromString<Interaction>(
        """{"id":"ix","machine_id":"m","session_id":"s","kind":"permission","tool_name":"add_mail_account","fields":$fields}""",
    )
    private val password = card("""[{"key":"password","label":"Password","secret":true}]""")

    @Test fun requiredFieldsOfferNoAllowAnywhere() {
        for (entry in AnswerEntry.entries) {
            val context = AnswerContext(entry = entry)
            assertFalse(entry.wire, AnswerBuilder.build(password, AnswerAction.Allow, context = context).allowed)
            assertFalse(entry.wire, AnswerBuilder.build(password, AnswerAction.Always, context = context).allowed)
        }
        assertTrue(AnswerBuilder.build(password, AnswerAction.Deny).allowed)
    }

    @Test fun optionalFieldsStillAllow() {
        val optional = card("""[{"key":"note","label":"Note","optional":true}]""")
        assertTrue(AnswerBuilder.build(optional, AnswerAction.Allow).allowed)
        assertTrue(AnswerBuilder.build(card("[]"), AnswerAction.Allow).allowed)
    }
}
