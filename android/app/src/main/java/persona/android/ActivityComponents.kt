package persona.android

import android.content.Context
import android.content.Intent

// 与升级前的 launcher、PendingIntent 共用同一个组件，避免直接打开新类时另起一份任务栈。
internal fun mainActivityIntent(context: Context): Intent =
    Intent().setClassName(context, "jarvis.android.MainActivity")
