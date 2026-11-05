package persona.core.contracts.api

import persona.core.contracts.CoreTextException
import persona.core.contracts.CoreText.Key
import persona.core.contracts.CoreText

/** The node refused the answer because the terminal prompt changed before input. */
class InteractionPromptChangedException : CoreTextException(CoreText.of(Key.ScreenChanged))
