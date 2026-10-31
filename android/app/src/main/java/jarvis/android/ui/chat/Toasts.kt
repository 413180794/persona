package jarvis.android.ui.chat

import android.os.Handler
import android.os.Looper
import android.widget.Toast
import jarvis.android.JarvisApp

/** How a toast reads; Android toasts look the same, the style only picks how long it stays. */
enum class ToastStyle { INFO, SUCCESS, ERROR }

/** Short confirmations and errors (「已拷贝」): system toasts, from any thread. */
object Toasts {
    fun show(text: String, style: ToastStyle = ToastStyle.INFO) {
        val app = runCatching { JarvisApp.instance }.getOrNull() ?: return
        Handler(Looper.getMainLooper()).post {
            Toast.makeText(app, text, if (style == ToastStyle.ERROR) Toast.LENGTH_LONG else Toast.LENGTH_SHORT).show()
        }
    }
}
