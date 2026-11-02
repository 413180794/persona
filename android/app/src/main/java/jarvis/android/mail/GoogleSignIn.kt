package jarvis.android.mail

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import jarvis.android.JarvisApp
import jarvis.android.data.ApiClient
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/**
 * Gmail's 「使用 Google 账号登录」 on Android (docs/mail.md「Android」).
 *
 * The hub's OAuth client is an **iOS** client (no secret, PKCE, the reversed client ID as a custom redirect scheme;
 * Google stopped custom schemes for Android clients). The hub builds the consent URL and later swaps the code, so
 * the phone only has to open the URL in the browser and catch the redirect. Android cannot catch an arbitrary scheme
 * at run time: [OAuthRedirectActivity][jarvis.android.ui.mail.OAuthRedirectActivity] declares the scheme from the build
 * (`jarvis.googleOAuthScheme` in android/local.properties or `JARVIS_GOOGLE_OAUTH_SCHEME`: the reversed client ID of
 * the hub's `google.client_id`). [canReceive] tells whether this build catches the scheme the hub uses; when it does
 * not, the page says so and offers Gmail with an app password.
 */
object GoogleSignIn {
    sealed interface Result {
        data class Code(val code: String, val state: String?) : Result
        data class Failed(val error: String, val state: String?) : Result
        data object Cancelled : Result
    }

    /** How a flow ended, for whichever page is there to show it (the one that started it may be gone). */
    sealed interface Finished {
        data class Added(val account: MailAccount) : Finished
        data class Error(val error: Exception) : Finished
    }

    // The flow outlives the page that started it (the activity can be destroyed while the browser is in front):
    // waiting and finishing run in this process scope, and the expected `state` is kept on disk so a redirect that
    // lands in a fresh process can still be finished.
    private val scope by lazy { CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate) }
    @Volatile private var pending: CompletableDeferred<Result>? = null
    private val _finished = MutableStateFlow<Finished?>(null)
    val finished: StateFlow<Finished?> = _finished.asStateFlow()

    /** Takes the last outcome exactly once (null if another collector took it). */
    fun takeFinished(): Finished? {
        while (true) {
            val v = _finished.value ?: return null
            if (_finished.compareAndSet(v, null)) return v
        }
    }

    private fun prefs() = JarvisApp.instance.getSharedPreferences("google_signin", Context.MODE_PRIVATE)
    private var expectedState: String?
        get() = prefs().getString("state", null)
        set(v) { prefs().edit().apply { if (v == null) remove("state") else putString("state", v) }.apply() }

    /** This build has an activity for `<scheme>:/oauth2redirect`. */
    fun canReceive(ctx: Context, scheme: String): Boolean {
        if (scheme.isBlank()) return false
        val probe = Intent(Intent.ACTION_VIEW, Uri.parse("$scheme:/oauth2redirect")).setPackage(ctx.packageName)
            .addCategory(Intent.CATEGORY_BROWSABLE)
        return ctx.packageManager.queryIntentActivities(probe, PackageManager.MATCH_DEFAULT_ONLY).isNotEmpty()
    }

    val isWaiting: Boolean get() = pending?.isActive == true

    /** The user came back without finishing (closed the browser). */
    fun cancel() {
        pending?.complete(Result.Cancelled)
    }

    /**
     * From the redirect activity: `…:/oauth2redirect?code=…&state=…` or `?error=…`. A redirect without the state
     * of the flow in progress is ignored (an injected redirect must not take over the real one).
     */
    fun onRedirect(uri: Uri) {
        val state = uri.getQueryParameter("state")
        val expected = expectedState
        if (state.isNullOrEmpty() || expected == null || state != expected) return
        val r = parse(uri.getQueryParameter("code"), state, uri.getQueryParameter("error"))
        val d = pending
        if (d != null && d.isActive) {
            d.complete(r)
            return
        }
        // Nobody is waiting (the process was restarted while the browser was open): finish it here.
        expectedState = null
        val api = JarvisApp.instance.hub.value?.api ?: return
        scope.launch { finish(api, r, expected) }
    }

    /** Maps the redirect's query to a result (access_denied = the user said no: cancelled, nothing to show). */
    fun parse(code: String?, state: String?, error: String?): Result = when {
        !error.isNullOrEmpty() -> if (error == "access_denied") Result.Cancelled else Result.Failed(error, state)
        !code.isNullOrEmpty() -> Result.Code(code, state)
        else -> Result.Cancelled
    }

    private suspend fun finish(api: ApiClient, r: Result, state: String) {
        val outcome = try {
            when (r) {
                is Result.Cancelled -> null
                is Result.Failed -> api.googleSignInFinish(GoogleFinish(state, null, r.error))
                is Result.Code -> api.googleSignInFinish(GoogleFinish(state, r.code))
            }?.let { Finished.Added(it) }
        } catch (e: CancellationException) {
            throw e
        } catch (e: Exception) {
            Finished.Error(e)
        }
        if (outcome != null) _finished.value = outcome
    }

    /**
     * The whole flow: start on the hub, open Google in the browser, wait, finish on the hub. The outcome is published
     * in [finished] (take it with [takeFinished]); this returns when the flow is over, or when the caller goes away (the
     * flow then continues on its own). Throws [UnsupportedOperationException] when this build cannot catch the hub's
     * redirect scheme, and the hub's error when the flow cannot start.
     */
    suspend fun signIn(ctx: Context, api: ApiClient, loginHint: String? = null) {
        val start = api.googleSignInStart(loginHint)
        if (!canReceive(ctx, start.callbackScheme)) throw UnsupportedOperationException(start.callbackScheme)
        pending?.complete(Result.Cancelled)
        val d = CompletableDeferred<Result>()
        pending = d
        expectedState = start.state
        ctx.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(start.authUrl)).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        val job = scope.launch {
            try {
                finish(api, d.await(), start.state)
            } finally {
                if (pending === d) {
                    pending = null
                    expectedState = null
                }
            }
        }
        job.join()
    }
}
