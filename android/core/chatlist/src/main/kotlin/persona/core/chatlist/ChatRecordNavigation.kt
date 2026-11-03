package persona.core.chatlist

/** Stop following before expanding a record's group; the owner freezes its item window. */
fun ChatListState.beginRecordNavigation() {
    epoch++
    restored = true
    dragged = false
    position = position.copy(followsEnd = false)
}

/** Use a stable row key so pending messages and older pages cannot shift the destination. */
suspend fun ChatListState.scrollToRecord(rowId: String, offsetPx: Int = 0): Boolean {
    val index = rowIds.indexOf(rowId)
    if (index < 0) return false
    beginRecordNavigation()
    programmaticScroll = true
    try {
        list.scrollToItem(index, offsetPx)
        position = position.copy(rowId = rowId, offsetPx = offsetPx)
    } finally { programmaticScroll = false }
    return true
}
