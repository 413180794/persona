package persona.feature.settings

import android.app.Activity
import android.content.Intent
import android.os.Bundle

/**
 * Catches Google's redirect after 「使用 Google 账号登录」 (`<scheme>:/oauth2redirect?code&state`),
 * hands it to [GoogleSignIn] and brings the app back to the front.
 */
class OAuthRedirectActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        intent?.data?.let { GoogleSignIn.onRedirect(it) }
        val mainIntent = packageManager.getLaunchIntentForPackage(packageName)
        if (mainIntent != null) {
            mainIntent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP)
            startActivity(mainIntent)
        }
        finish()
    }
}
