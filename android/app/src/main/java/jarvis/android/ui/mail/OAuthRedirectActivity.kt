package jarvis.android.ui.mail

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import jarvis.android.MainActivity
import jarvis.android.mail.GoogleSignIn

/**
 * Catches Google's redirect after 「使用 Google 账号登录」 (`<reversed client id>:/oauth2redirect?code&state`), hands it
 * to [GoogleSignIn] and brings the app back to the front (the browser stays behind it in its own task). It only
 * forwards the query; the hub checks the state.
 */
class OAuthRedirectActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        intent?.data?.let { GoogleSignIn.onRedirect(it) }
        startActivity(
            Intent(this, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP),
        )
        finish()
    }
}
