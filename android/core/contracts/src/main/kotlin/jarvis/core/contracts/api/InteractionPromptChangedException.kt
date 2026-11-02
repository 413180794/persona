package jarvis.core.contracts.api

/** The node refused the answer because the terminal prompt changed before input. */
class InteractionPromptChangedException : Exception("屏幕内容已变化")
