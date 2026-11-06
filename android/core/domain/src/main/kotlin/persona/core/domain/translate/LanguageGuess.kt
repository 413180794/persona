package persona.core.domain.translate

/**
 * Whether a text needs translating (Settings › Translation). iOS asks the system's language recognizer; Android has none built in,
 * so this looks at the scripts a text is written in and, for Latin script, at common words: the same rules as the
 * web's `lib/translate.ts`, so the two decide alike.
 */
object LanguageGuess {
    private val latin = listOf("en", "fr", "de", "es", "pt")

    private val stopwords: Map<String, Set<String>> = mapOf(
        "en" to "the and is are was you your to of in it that for with on this we i be have has can could would will please thanks thank hi hello not do does what when",
        "fr" to "le la les et est vous nous je de des une un pour que qui pas avec dans sur merci bonjour sont ce cette au aux",
        "de" to "der die das und ist nicht ich sie wir ein eine mit für auf zu den dem danke bitte wie sind auch es",
        "es" to "el la los las y es que de en un una por para con no gracias hola está son como pero del lo",
        "pt" to "o a os as e é que de em um uma para com não obrigado obrigada você do da dos das mas como",
    ).mapValues { (_, words) -> words.split(" ").toSet() }

    private val accents: Map<String, Regex?> = mapOf(
        "en" to null,
        "fr" to Regex("[èêëàâîïôûùœ]"),
        "de" to Regex("[äöüß]"),
        "es" to Regex("[ñ¿¡]"),
        "pt" to Regex("[ãõ]"),
    )

    /** The language part of a BCP 47 code: zh-Hans and zh-Hant are both zh. */
    fun family(code: String): String = code.substringBefore('-').lowercase()

    /**
     * The language a text is in, as a family ("zh", "ja", "ko", "ru", "en", "fr", "de", "es", "pt"), "latin" for Latin
     * script whose language is unclear, "other" for another script, or null when there is too little to tell.
     */
    fun detect(text: String): String? {
        val sample = text.take(1500)
        var han = 0; var kana = 0; var hangul = 0; var cyrillic = 0; var latinCount = 0; var other = 0
        var i = 0
        while (i < sample.length) {
            val cp = sample.codePointAt(i)
            i += Character.charCount(cp)
            when (Character.UnicodeScript.of(cp)) {
                Character.UnicodeScript.HAN -> han++
                Character.UnicodeScript.HIRAGANA, Character.UnicodeScript.KATAKANA -> kana++
                Character.UnicodeScript.HANGUL -> hangul++
                Character.UnicodeScript.CYRILLIC -> cyrillic++
                Character.UnicodeScript.LATIN -> if (Character.isLetter(cp)) latinCount++
                else -> if (Character.isLetter(cp)) other++
            }
        }
        if (han + kana + hangul + cyrillic + latinCount + other < 6) return null
        // A CJK character carries about as much as a short Latin word: weigh them so a Chinese sentence with a few
        // English terms in it is still Chinese.
        val scores = listOf("cjk" to (han + kana) * 4, "ko" to hangul * 4, "ru" to cyrillic, "latin" to latinCount, "other" to other)
        val best = scores.reduce { a, b -> if (b.second > a.second) b else a }.first
        return when (best) {
            "cjk" -> if (kana > 0 && kana * 10 >= han + kana) "ja" else "zh"
            "latin" -> latinLanguage(sample)
            else -> best
        }
    }

    private fun latinLanguage(text: String): String {
        val lower = text.lowercase()
        val words = Regex("\\p{L}+").findAll(lower).map { it.value }.toList()
        val score = latin.associateWith { lang -> words.count { it in stopwords.getValue(lang) } +
            (accents.getValue(lang)?.findAll(lower)?.count() ?: 0) }
        val ranked = latin.sortedByDescending { score.getValue(it) }
        val first = score.getValue(ranked[0]); val second = score.getValue(ranked[1])
        return if (first >= 2 && first > second) ranked[0] else "latin"
    }

    /**
     * Whether [text] is in another language than [target]. Too little text to tell is not; Latin script whose language
     * is unclear is foreign only to a target in another script (it might well be the target itself).
     */
    fun isForeign(text: String, target: String): Boolean {
        val lang = detect(text) ?: return false
        val t = family(target)
        if (lang == "latin") return t !in latin
        return lang != t
    }
}
