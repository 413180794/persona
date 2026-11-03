package persona.feature.chat

import java.util.regex.Pattern

internal data class ReaderRange(val start: Int, val end: Int)

/** Offsets use UTF-16, matching Compose text layout and Kotlin String indices. */
internal class ReaderSnapshot(val text: String, pageSize: Int = 8192) {
    val pages: List<ReaderRange>

    init {
        require(pageSize > 0)
        val matcher = Pattern.compile("\\X").matcher(text)
        val ranges = mutableListOf<ReaderRange>()
        var start = 0
        var end = 0
        while (matcher.find()) {
            if (matcher.end() - start > pageSize && end > start) {
                ranges += ReaderRange(start, end)
                start = end
            }
            // A single oversized grapheme stays intact, even when it exceeds the page budget.
            end = matcher.end()
        }
        if (end > start || ranges.isEmpty()) ranges += ReaderRange(start, end)
        pages = ranges.toList()
    }

    fun pageAt(offset: Int): Int {
        val found = pages.binarySearch { range ->
            when {
                offset < range.start -> 1
                offset >= range.end -> -1
                else -> 0
            }
        }
        return if (found >= 0) found else (-found - 1).coerceIn(pages.indices)
    }

    fun search(query: String, page: Int? = null): List<ReaderRange> {
        if (query.isEmpty()) return emptyList()
        val bounds = page?.let { pages[it] } ?: ReaderRange(0, text.length)
        return buildList {
            var from = bounds.start
            while (from <= bounds.end - query.length) {
                val offset = text.indexOf(query, from, ignoreCase = true)
                if (offset < 0 || offset + query.length > bounds.end) break
                add(ReaderRange(offset, offset + query.length))
                from = offset + query.length
            }
        }
    }
}

/** Adjacent removals and additions form one navigable change. */
internal fun diffChangeStarts(diff: ToolDiff): List<Int> = diff.lines.indices.filter { index ->
    diff.lines[index].sign in "+-" && (index == 0 || diff.lines[index - 1].sign !in "+-")
}
