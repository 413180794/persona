package persona.core.hub

import okhttp3.Protocol
import okhttp3.Request
import okhttp3.Response
import okhttp3.ResponseBody.Companion.toResponseBody
import org.junit.Assert.*
import org.junit.Test

/** Which refusals end the pairing (#4692): only the credential's own, never a per-request 403. */
class HubFailureResponseTest {
    private fun response(code: Int, body: String) = Response.Builder()
        .request(Request.Builder().url("http://hub.example.com/api/v1/ws").build())
        .protocol(Protocol.HTTP_1_1).code(code).message("refused").body(body.toResponseBody()).build()

    @Test fun plaintextFromAPublicSourceIsTlsRequired() {
        assertEquals(HubError.TlsRequired, HubFailure.from(response(403, """{"error":"tls","code":"tls_required"}""")).kind)
    }

    @Test fun aCodedRequestRefusalKeepsThePairing() {
        val failure = HubFailure.from(response(403, """{"error":"gone","code":"this_machine_only"}"""))
        assertNotEquals(HubError.PairingExpired, failure.kind)
        assertFalse(failure.retryable)
    }

    @Test fun anUnauthorizedCredentialStillEndsThePairing() {
        assertEquals(HubError.PairingExpired, HubFailure.from(response(401, """{"error":"bad","code":"invalid_token"}""")).kind)
    }

    /** #5514 moved a node's own words to `detail` when the hub localized `error` (#5702). */
    @Test fun aLocalizedFailureKeepsTheNodesOwnWords() {
        val failure = HubFailure.from(response(502, """{"error":"示例摘要","code":"internal","detail":"unknown method node.doctor"}"""))
        assertEquals("示例摘要", failure.message)
        assertEquals("unknown method node.doctor", failure.original)
        val structured = HubFailure.from(response(502, """{"error":"示例摘要","code":"internal","detail":{"kind":"x"}}"""))
        assertEquals("a structured detail is not the node's words", "示例摘要", structured.message)
        assertNull(structured.original)
    }
}
