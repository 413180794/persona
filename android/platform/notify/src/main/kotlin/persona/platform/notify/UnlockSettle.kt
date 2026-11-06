package persona.platform.notify

import kotlinx.coroutines.delay
import kotlinx.coroutines.withTimeoutOrNull

/**
 * Whether the phone is still locked once an unlock in progress has had [settleMillis] to land. SystemUI sends a
 * notification button that needs authentication as soon as the user's PIN is accepted, a moment before KeyguardManager
 * stops reporting the device as locked: read at once, every approval made from the lock screen came back "Unlock first"
 * (docs/lessons/android.md). A phone still locked after that (a system that did not ask for the unlock) is locked.
 */
internal suspend fun lockedAfterUnlockSettles(
    locked: () -> Boolean,
    settleMillis: Long = UNLOCK_SETTLE_MILLIS,
    pollMillis: Long = UNLOCK_POLL_MILLIS,
): Boolean {
    if (!locked()) return false
    return withTimeoutOrNull(settleMillis) {
        while (locked()) delay(pollMillis)
        false
    } ?: true
}

/** How long an unlock that let a button through may take to show (config value, not a measurement). */
internal const val UNLOCK_SETTLE_MILLIS = 3_000L
private const val UNLOCK_POLL_MILLIS = 100L
