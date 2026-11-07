package persona.feature.settings

import android.content.Intent
import android.os.Bundle
import android.view.View
import com.journeyapps.barcodescanner.CaptureActivity
import com.journeyapps.barcodescanner.DecoratedBarcodeView

/** Keeps ZXing capture behavior while providing pairing-specific instructions and actions. */
class PairingScanActivity : CaptureActivity() {
    // The app's language, as in every other activity (docs/android/android.md "Internationalization").
    override fun attachBaseContext(base: android.content.Context) = super.attachBaseContext(persona.core.designsystem.AppLanguages.wrap(base))

    override fun initializeContent(): DecoratedBarcodeView {
        setContentView(R.layout.pairing_scan)
        return findViewById(R.id.zxing_barcode_scanner)
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val root = findViewById<View>(R.id.pairing_scan_root)
        root.setOnApplyWindowInsetsListener { view, insets ->
            val bars = insets.getInsets(android.view.WindowInsets.Type.systemBars())
            view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            insets
        }
        root.requestApplyInsets()
        findViewById<View>(R.id.pairing_scan_close).setOnClickListener { finish() }
        findViewById<View>(R.id.pairing_scan_manual).setOnClickListener {
            setResult(RESULT_CANCELED, Intent().putExtra(ManualInput, true))
            finish()
        }
    }

    companion object {
        const val ManualInput = "persona.feature.settings.manual_input"
    }
}
