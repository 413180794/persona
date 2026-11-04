package persona.core.designsystem

import androidx.compose.runtime.compositionLocalOf
import persona.core.contracts.LarkFeatures

/**
 * What the selected hub offers of Feishu through the lark-connector plugin ([LarkFeatures.of] on its hello features). The
 * App's shell provides it; everything is shown until a hello says otherwise, and in the demo.
 */
val LocalLarkFeatures = compositionLocalOf { LarkFeatures.All }
