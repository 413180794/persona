package persona.android

import persona.android.data.ApiClient
import org.junit.Assert.assertEquals
import org.junit.Test

class ApiClientTest {
    @Test fun apiUrlsKeepEncodedItemIds() {
        val api = ApiClient("http://hub.example.com:18660/", "t")
        val u = api.url("/api/v1/sessions/server/u1/items").newBuilder().addPathSegment("e1#2").addPathSegment("image").build()
        assertEquals("/api/v1/sessions/server/u1/items/e1%232/image", u.encodedPath)
    }
}
