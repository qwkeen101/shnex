package dev.shnex.app.terminal

import android.content.SharedPreferences

enum class CursorStyle(val label: String) {
    BLOCK("方块"),
    BAR("竖线"),
    UNDERLINE("下划线")
}

class TerminalColorScheme(
    val id: String,
    val name: String,
    val bg: Int,
    val fg: Int,
    val cursor: Int,
    val ansi16: IntArray
)

data class TerminalAppearance(
    /** Font for single-width cells; glyphs it lacks (and all CJK) use [fallbackFontId]. */
    val primaryFontId: String = FontCatalog.DEFAULT_PRIMARY,
    /** The 中文 / 回退字体 setting. */
    val fallbackFontId: String = FontCatalog.DEFAULT_FALLBACK,
    val schemeId: String = TerminalColorSchemes.DEFAULT_ID,
    val fontSizeSp: Float = 13f,
    val lineHeightScale: Float = 1.12f,
    val cursorStyle: CursorStyle = CursorStyle.BLOCK
) {
    fun normalized(): TerminalAppearance = copy(
        primaryFontId = FontCatalog.byId(primaryFontId)?.id ?: FontCatalog.DEFAULT_PRIMARY,
        fallbackFontId = FontCatalog.byId(fallbackFontId)?.id ?: FontCatalog.DEFAULT_FALLBACK,
        schemeId = TerminalColorSchemes.byId(schemeId).id,
        fontSizeSp = fontSizeSp.coerceIn(FONT_SIZE_MIN, FONT_SIZE_MAX),
        lineHeightScale = lineHeightScale.coerceIn(LINE_HEIGHT_MIN, LINE_HEIGHT_MAX)
    )

    companion object {
        const val FONT_SIZE_MIN = 9f
        const val FONT_SIZE_MAX = 22f
        const val LINE_HEIGHT_MIN = 1.0f
        const val LINE_HEIGHT_MAX = 1.5f
    }
}

private fun argb(rgb: Long): Int = (0xFF000000L or rgb).toInt()

private fun palette(vararg rgb: Long): IntArray = IntArray(16) { argb(rgb[it]) }

object TerminalColorSchemes {
    const val DEFAULT_ID = "shnex_dark"

    private val shnexAnsi = palette(
        0x18191D, 0xF87171, 0x4ADE80, 0xFACC15, 0x60A5FA, 0xC084FC, 0x22D3EE, 0xE5E7EB,
        0x6B7280, 0xFCA5A5, 0x86EFAC, 0xFDE047, 0x93C5FD, 0xD8B4FE, 0x67E8F9, 0xFFFFFF
    )
    private val solarizedAnsi = palette(
        0x073642, 0xDC322F, 0x859900, 0xB58900, 0x268BD2, 0xD33682, 0x2AA198, 0xEEE8D5,
        0x002B36, 0xCB4B16, 0x586E75, 0x657B83, 0x839496, 0x6C71C4, 0x93A1A1, 0xFDF6E3
    )

    val all: List<TerminalColorScheme> = listOf(
        TerminalColorScheme("shnex_dark", "Shnex 暗", argb(0x121316), argb(0xE5E7EB), argb(0x60A5FA), shnexAnsi),
        TerminalColorScheme("oled_black", "OLED 纯黑", argb(0x000000), argb(0xE5E7EB), argb(0x60A5FA), shnexAnsi),
        TerminalColorScheme("solarized_dark", "Solarized 暗", argb(0x002B36), argb(0x839496), argb(0x93A1A1), solarizedAnsi),
        TerminalColorScheme("solarized_light", "Solarized 浅", argb(0xFDF6E3), argb(0x657B83), argb(0x586E75), solarizedAnsi),
        TerminalColorScheme(
            "tokyo_night", "Tokyo Night", argb(0x1A1B26), argb(0xC0CAF5), argb(0xC0CAF5),
            palette(
                0x15161E, 0xF7768E, 0x9ECE6A, 0xE0AF68, 0x7AA2F7, 0xBB9AF7, 0x7DCFFF, 0xA9B1D6,
                0x414868, 0xF7768E, 0x9ECE6A, 0xE0AF68, 0x7AA2F7, 0xBB9AF7, 0x7DCFFF, 0xC0CAF5
            )
        ),
        TerminalColorScheme(
            "gruvbox_dark", "Gruvbox 暗", argb(0x282828), argb(0xEBDBB2), argb(0xEBDBB2),
            palette(
                0x282828, 0xCC241D, 0x98971A, 0xD79921, 0x458588, 0xB16286, 0x689D6A, 0xA89984,
                0x928374, 0xFB4934, 0xB8BB26, 0xFABD2F, 0x83A598, 0xD3869B, 0x8EC07C, 0xEBDBB2
            )
        ),
        TerminalColorScheme(
            "one_dark", "One Dark", argb(0x282C34), argb(0xABB2BF), argb(0x528BFF),
            palette(
                0x282C34, 0xE06C75, 0x98C379, 0xE5C07B, 0x61AFEF, 0xC678DD, 0x56B6C2, 0xABB2BF,
                0x5C6370, 0xE06C75, 0x98C379, 0xE5C07B, 0x61AFEF, 0xC678DD, 0x56B6C2, 0xFFFFFF
            )
        )
    )

    fun byId(id: String): TerminalColorScheme = all.firstOrNull { it.id == id } ?: all[0]
}

/**
 * Turns a semantic cell color (see AnsiTerminalBuffer: COLOR_DEFAULT_FG / COLOR_DEFAULT_BG /
 * colorIndexed / 0xFFRRGGBB true color) into a real ARGB value for the active scheme.
 */
fun resolveColor(code: Int, scheme: TerminalColorScheme): Int = when {
    code == COLOR_DEFAULT_FG -> scheme.fg
    code == COLOR_DEFAULT_BG -> scheme.bg
    (code ushr 24) == COLOR_INDEXED_TAG -> scheme.ansi16[code and 15]
    else -> code
}

class TerminalAppearanceStore(private val prefs: SharedPreferences) {
    fun load(): TerminalAppearance {
        val d = TerminalAppearance()
        return TerminalAppearance(
            primaryFontId = prefs.getString(KEY_PRIMARY_FONT, null) ?: d.primaryFontId,
            fallbackFontId = prefs.getString(KEY_FALLBACK_FONT, null) ?: d.fallbackFontId,
            schemeId = prefs.getString(KEY_SCHEME, null) ?: d.schemeId,
            fontSizeSp = prefs.getFloat(KEY_FONT_SIZE, d.fontSizeSp),
            lineHeightScale = prefs.getFloat(KEY_LINE_HEIGHT, d.lineHeightScale),
            cursorStyle = enumOr(prefs.getString(KEY_CURSOR, null), d.cursorStyle)
        ).normalized()
    }

    fun save(a: TerminalAppearance) {
        prefs.edit()
            .putString(KEY_PRIMARY_FONT, a.primaryFontId)
            .putString(KEY_FALLBACK_FONT, a.fallbackFontId)
            .putString(KEY_SCHEME, a.schemeId)
            .putFloat(KEY_FONT_SIZE, a.fontSizeSp)
            .putFloat(KEY_LINE_HEIGHT, a.lineHeightScale)
            .putString(KEY_CURSOR, a.cursorStyle.name)
            .apply()
    }

    private inline fun <reified E : Enum<E>> enumOr(name: String?, fallback: E): E =
        enumValues<E>().firstOrNull { it.name == name } ?: fallback

    private companion object {
        const val KEY_PRIMARY_FONT = "term_primary_font"
        const val KEY_FALLBACK_FONT = "term_fallback_font"
        const val KEY_SCHEME = "term_scheme"
        const val KEY_FONT_SIZE = "term_font_size_sp"
        const val KEY_LINE_HEIGHT = "term_line_height"
        const val KEY_CURSOR = "term_cursor_style"
    }
}
