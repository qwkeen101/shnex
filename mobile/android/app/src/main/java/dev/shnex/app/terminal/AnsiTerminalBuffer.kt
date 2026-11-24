package dev.shnex.app.terminal

import java.nio.ByteBuffer
import java.nio.CharBuffer
import java.nio.charset.CharsetDecoder
import java.nio.charset.CodingErrorAction
import kotlin.math.max
import kotlin.math.min

data class TermCell(
    var text: String = " ",
    var fg: Int = COLOR_DEFAULT_FG,
    var bg: Int = COLOR_DEFAULT_BG,
    var bold: Boolean = false,
    var inverse: Boolean = false,
    var underline: Boolean = false,
    var width: Int = 1 // 1 = normal, 2 = CJK wide lead, 0 = wide trailing spacer
) {
    fun reset(bgColor: Int = COLOR_DEFAULT_BG) {
        text = " "
        fg = COLOR_DEFAULT_FG
        bg = bgColor
        bold = false
        inverse = false
        underline = false
        width = 1
    }
}

data class TerminalSnapshot(
    val cols: Int,
    val rows: Int,
    val cursorX: Int,
    val cursorY: Int,
    val cursorVisible: Boolean,
    val bracketedPaste: Boolean,
    val appCursorKeys: Boolean,
    val lines: List<Array<TermCell>>,
    val scrollbackSize: Int
)

/**
 * Cell colors are stored as semantic codes and resolved against the active
 * TerminalColorScheme at draw time (see resolveColor), so switching schemes
 * recolors the live screen and the whole scrollback:
 *  - COLOR_DEFAULT_FG / COLOR_DEFAULT_BG: the scheme's default foreground / background
 *  - colorIndexed(0..15): the scheme's 16 ANSI colors
 *  - 0xFFRRGGBB (alpha always 0xFF): xterm-256 cube / grayscale and 24-bit true color
 */
const val COLOR_DEFAULT_FG: Int = 0x00000000
const val COLOR_DEFAULT_BG: Int = 0x00000001
const val COLOR_INDEXED_TAG: Int = 0x01

fun colorIndexed(idx: Int): Int = (COLOR_INDEXED_TAG shl 24) or (idx and 15)

/**
 * Pure-Kotlin VT100 / xterm-256color / 24-bit TrueColor Terminal Emulator.
 * Supports UTF-8 streaming, CJK wide characters, Alternate Screen (?1049h for Claude CLI / tmux / vim),
 * Bracketed Paste Mode (?2004h), Application Cursor Keys (?1h), DECSTBM scroll regions, and scrollback.
 * 100% permissive first-party code (Zero GPL, Zero WebView).
 */
class AnsiTerminalBuffer(
    initialCols: Int = 80,
    initialRows: Int = 28,
    private val maxScrollback: Int = 2500
) {
    var cols: Int = initialCols.coerceIn(20, 320)
        private set
    var rows: Int = initialRows.coerceIn(6, 160)
        private set

    private var mainScreen: Array<Array<TermCell>> = allocGrid(cols, rows)
    private var altScreen: Array<Array<TermCell>> = allocGrid(cols, rows)
    private val scrollback = ArrayDeque<Array<TermCell>>()

    private var usingAltScreen = false
    var bracketedPasteMode: Boolean = false
        private set
    var appCursorKeys: Boolean = false
        private set
    var cursorVisible: Boolean = true
        private set

    private var curX = 0
    private var curY = 0
    private var savedMainX = 0
    private var savedMainY = 0
    private var savedAltX = 0
    private var savedAltY = 0

    private var scrollTop = 0
    private var scrollBottom = rows - 1

    // Current SGR Pen attributes
    private var penFg = COLOR_DEFAULT_FG
    private var penBg = COLOR_DEFAULT_BG
    private var penBold = false
    private var penInverse = false
    private var penUnderline = false

    // Streaming UTF-8 decoder across arbitrary network chunks
    private val utf8Decoder: CharsetDecoder = Charsets.UTF_8.newDecoder()
        .onMalformedInput(CodingErrorAction.REPLACE)
        .onUnmappableCharacter(CodingErrorAction.REPLACE)
    private var pendingUtf8 = ByteArray(0)

    // Escape parser state machine
    private enum class ParseState { GROUND, ESC, CSI, OSC, ESC_CHARSET }
    private var state = ParseState.GROUND
    private val csiBuf = StringBuilder()
    private val oscBuf = StringBuilder()

    /** True while a full-screen app (tmux, vim, ...) owns the screen; local scrollback is then empty. */
    val isAltScreen: Boolean
        @Synchronized get() = usingAltScreen

    /** Lines reachable by scrolling back on the current screen. */
    val scrollbackLines: Int
        @Synchronized get() = if (usingAltScreen) 0 else scrollback.size

    @Synchronized
    fun resize(newCols: Int, newRows: Int): Boolean {
        val c = newCols.coerceIn(20, 320)
        val r = newRows.coerceIn(6, 160)
        if (c == cols && r == rows) return false

        mainScreen = copyResizeGrid(mainScreen, c, r)
        altScreen = copyResizeGrid(altScreen, c, r)
        cols = c
        rows = r
        curX = curX.coerceIn(0, cols - 1)
        curY = curY.coerceIn(0, rows - 1)
        scrollTop = 0
        scrollBottom = rows - 1
        return true
    }

    @Synchronized
    fun feedBytes(chunk: ByteArray) {
        if (chunk.isEmpty()) return
        val input = if (pendingUtf8.isEmpty()) {
            ByteBuffer.wrap(chunk)
        } else {
            val merged = ByteArray(pendingUtf8.size + chunk.size)
            System.arraycopy(pendingUtf8, 0, merged, 0, pendingUtf8.size)
            System.arraycopy(chunk, 0, merged, pendingUtf8.size, chunk.size)
            ByteBuffer.wrap(merged)
        }
        val outChars = CharBuffer.allocate(input.remaining() * 2 + 8)
        utf8Decoder.decode(input, outChars, false)
        pendingUtf8 = if (input.hasRemaining()) {
            ByteArray(input.remaining()).also { input.get(it) }
        } else {
            ByteArray(0)
        }
        outChars.flip()
        val str = outChars.toString()
        var i = 0
        while (i < str.length) {
            val cp = str.codePointAt(i)
            i += Character.charCount(cp)
            processCodePoint(cp)
        }
    }

    @Synchronized
    fun snapshot(scrollOffsetLines: Int = 0): TerminalSnapshot {
        val grid = activeGrid()
        val linesOut = ArrayList<Array<TermCell>>(rows)
        if (usingAltScreen || scrollOffsetLines <= 0 || scrollback.isEmpty()) {
            for (r in 0 until rows) {
                linesOut.add(cloneRow(grid[r]))
            }
        } else {
            val maxOff = scrollback.size
            val off = scrollOffsetLines.coerceIn(0, maxOff)
            val startIdx = maxOff - off
            var r = 0
            while (r < rows) {
                val sbIdx = startIdx + r
                if (sbIdx < scrollback.size) {
                    linesOut.add(cloneRow(scrollback[sbIdx]))
                } else {
                    val gridRow = sbIdx - scrollback.size
                    if (gridRow in 0 until rows) {
                        linesOut.add(cloneRow(grid[gridRow]))
                    } else {
                        linesOut.add(Array(cols) { TermCell() })
                    }
                }
                r++
            }
        }
        return TerminalSnapshot(
            cols = cols,
            rows = rows,
            cursorX = curX.coerceIn(0, cols - 1),
            cursorY = curY.coerceIn(0, rows - 1),
            cursorVisible = cursorVisible && (usingAltScreen || scrollOffsetLines == 0),
            bracketedPaste = bracketedPasteMode,
            appCursorKeys = appCursorKeys,
            lines = linesOut,
            scrollbackSize = if (usingAltScreen) 0 else scrollback.size
        )
    }

    /**
     * Extracts the text on the active cursor row right after the shell prompt for mobile completion.
     */
    @Synchronized
    fun currentInputLineBeforeCursor(): String {
        if (usingAltScreen) return ""
        val row = activeGrid()[curY.coerceIn(0, rows - 1)]
        val sb = StringBuilder()
        for (c in 0 until min(curX, cols)) {
            if (row[c].width != 0) {
                sb.append(row[c].text)
            }
        }
        val raw = sb.toString()
        val promptRegexes = listOf(
            Regex("""^.*?(?:❯|➜)\s"""),
            Regex("""^[^\r\n]*?(?:[$#%])\s""")
        )
        for (re in promptRegexes) {
            val m = re.find(raw)
            if (m != null) {
                return raw.substring(m.range.last + 1)
            }
        }
        return ""
    }

    private fun activeGrid(): Array<Array<TermCell>> = if (usingAltScreen) altScreen else mainScreen

    private fun processCodePoint(cp: Int) {
        when (state) {
            ParseState.GROUND -> {
                when (cp) {
                    0x1B -> state = ParseState.ESC
                    0x07 -> { /* BEL */ }
                    0x08 -> if (curX > 0) curX--
                    0x09 -> curX = min(cols - 1, ((curX / 8) + 1) * 8)
                    0x0A, 0x0B, 0x0C -> lineFeed()
                    0x0D -> curX = 0
                    0x0E, 0x0F -> { /* SO/SI charset shift */ }
                    else -> if (cp >= 0x20) putGraphicCodePoint(cp)
                }
            }

            ParseState.ESC -> {
                when (cp.toChar()) {
                    '[' -> {
                        csiBuf.setLength(0)
                        state = ParseState.CSI
                    }
                    ']' -> {
                        oscBuf.setLength(0)
                        state = ParseState.OSC
                    }
                    '(', ')', '*', '+' -> state = ParseState.ESC_CHARSET
                    '7' -> {
                        saveCursor()
                        state = ParseState.GROUND
                    }
                    '8' -> {
                        restoreCursor()
                        state = ParseState.GROUND
                    }
                    'D' -> {
                        lineFeed()
                        state = ParseState.GROUND
                    }
                    'E' -> {
                        curX = 0
                        lineFeed()
                        state = ParseState.GROUND
                    }
                    'M' -> {
                        reverseIndex()
                        state = ParseState.GROUND
                    }
                    'c' -> {
                        fullReset()
                        state = ParseState.GROUND
                    }
                    else -> state = ParseState.GROUND
                }
            }

            ParseState.ESC_CHARSET -> {
                state = ParseState.GROUND
            }

            ParseState.OSC -> {
                if (cp == 0x07) {
                    state = ParseState.GROUND
                } else if (cp == 0x1B) {
                    // Possible ESC \ string terminator
                    state = ParseState.ESC
                } else if (oscBuf.length < 2048) {
                    oscBuf.appendCodePoint(cp)
                }
            }

            ParseState.CSI -> {
                if (cp in 0x40..0x7E) {
                    dispatchCSI(csiBuf.toString(), cp.toChar())
                    state = ParseState.GROUND
                } else if (csiBuf.length < 256) {
                    csiBuf.appendCodePoint(cp)
                } else {
                    state = ParseState.GROUND
                }
            }
        }
    }

    private fun putGraphicCodePoint(cp: Int) {
        val w = charCellWidth(cp)
        if (w <= 0) return
        val grid = activeGrid()

        if (curX + w > cols) {
            curX = 0
            lineFeed()
        }
        val y = curY.coerceIn(0, rows - 1)
        val x = curX.coerceIn(0, cols - 1)

        // Clean broken wide-char halves if overwriting middle of an existing wide cell
        if (grid[y][x].width == 0 && x > 0) {
            grid[y][x - 1].reset(penBg)
        }
        if (grid[y][x].width == 2 && x + 1 < cols) {
            grid[y][x + 1].reset(penBg)
        }

        val chStr = String(Character.toChars(cp))
        grid[y][x].apply {
            text = chStr
            fg = penFg
            bg = penBg
            bold = penBold
            inverse = penInverse
            underline = penUnderline
            width = w
        }
        if (w == 2 && x + 1 < cols) {
            grid[y][x + 1].apply {
                text = ""
                fg = penFg
                bg = penBg
                bold = penBold
                inverse = penInverse
                underline = penUnderline
                width = 0
            }
        }
        curX = min(cols, x + w)
    }

    private fun lineFeed() {
        if (curY == scrollBottom) {
            scrollUpRegion(scrollTop, scrollBottom, 1)
        } else if (curY < rows - 1) {
            curY++
        }
    }

    private fun reverseIndex() {
        if (curY == scrollTop) {
            scrollDownRegion(scrollTop, scrollBottom, 1)
        } else if (curY > 0) {
            curY--
        }
    }

    private fun scrollUpRegion(top: Int, bottom: Int, count: Int) {
        val grid = activeGrid()
        val t = top.coerceIn(0, rows - 1)
        val b = bottom.coerceIn(t, rows - 1)
        repeat(count.coerceAtLeast(1)) {
            if (!usingAltScreen && t == 0) {
                scrollback.addLast(cloneRow(grid[0]))
                while (scrollback.size > maxScrollback) {
                    scrollback.removeFirst()
                }
            }
            for (r in t until b) {
                grid[r] = grid[r + 1]
            }
            grid[b] = Array(cols) { TermCell(bg = penBg) }
        }
    }

    private fun scrollDownRegion(top: Int, bottom: Int, count: Int) {
        val grid = activeGrid()
        val t = top.coerceIn(0, rows - 1)
        val b = bottom.coerceIn(t, rows - 1)
        repeat(count.coerceAtLeast(1)) {
            for (r in b downTo t + 1) {
                grid[r] = grid[r - 1]
            }
            grid[t] = Array(cols) { TermCell(bg = penBg) }
        }
    }

    private fun dispatchCSI(rawParams: String, finalChar: Char) {
        val isPrivate = rawParams.startsWith("?")
        val clean = rawParams.removePrefix("?").removePrefix(">")
        val nums = if (clean.isEmpty()) {
            emptyList()
        } else {
            clean.split(';', ':').map { it.toIntOrNull() ?: 0 }
        }
        fun p(idx: Int, def: Int): Int = nums.getOrNull(idx)?.let { if (it == 0) def else it } ?: def

        if (isPrivate) {
            val enable = finalChar == 'h'
            if (finalChar == 'h' || finalChar == 'l') {
                for (mode in nums) {
                    when (mode) {
                        1 -> appCursorKeys = enable
                        25 -> cursorVisible = enable
                        47, 1047, 1049 -> {
                            if (enable && !usingAltScreen) {
                                if (mode == 1049) saveCursor()
                                altScreen = allocGrid(cols, rows)
                                usingAltScreen = true
                                curX = 0
                                curY = 0
                                scrollTop = 0
                                scrollBottom = rows - 1
                            } else if (!enable && usingAltScreen) {
                                usingAltScreen = false
                                scrollTop = 0
                                scrollBottom = rows - 1
                                if (mode == 1049) restoreCursor()
                            }
                        }
                        2004 -> bracketedPasteMode = enable
                    }
                }
            }
            return
        }

        val grid = activeGrid()
        when (finalChar) {
            'A' -> curY = max(scrollTop, curY - p(0, 1))
            'B' -> curY = min(scrollBottom, curY + p(0, 1))
            'C' -> curX = min(cols - 1, curX + p(0, 1))
            'D' -> curX = max(0, curX - p(0, 1))
            'E' -> {
                curY = min(scrollBottom, curY + p(0, 1))
                curX = 0
            }
            'F' -> {
                curY = max(scrollTop, curY - p(0, 1))
                curX = 0
            }
            'G', '`' -> curX = (p(0, 1) - 1).coerceIn(0, cols - 1)
            'd' -> curY = (p(0, 1) - 1).coerceIn(0, rows - 1)
            'H', 'f' -> {
                curY = (p(0, 1) - 1).coerceIn(0, rows - 1)
                curX = (p(1, 1) - 1).coerceIn(0, cols - 1)
            }
            'J' -> {
                val mode = nums.getOrElse(0) { 0 }
                val y = curY.coerceIn(0, rows - 1)
                val x = curX.coerceIn(0, cols - 1)
                when (mode) {
                    0 -> {
                        for (c in x until cols) grid[y][c].reset(penBg)
                        for (r in y + 1 until rows) {
                            for (c in 0 until cols) grid[r][c].reset(penBg)
                        }
                    }
                    1 -> {
                        for (r in 0 until y) {
                            for (c in 0 until cols) grid[r][c].reset(penBg)
                        }
                        for (c in 0..x) grid[y][c].reset(penBg)
                    }
                    2, 3 -> {
                        for (r in 0 until rows) {
                            for (c in 0 until cols) grid[r][c].reset(penBg)
                        }
                        if (mode == 3 && !usingAltScreen) scrollback.clear()
                    }
                }
            }
            'K' -> {
                val mode = nums.getOrElse(0) { 0 }
                val y = curY.coerceIn(0, rows - 1)
                val x = curX.coerceIn(0, cols - 1)
                when (mode) {
                    0 -> for (c in x until cols) grid[y][c].reset(penBg)
                    1 -> for (c in 0..x) grid[y][c].reset(penBg)
                    2 -> for (c in 0 until cols) grid[y][c].reset(penBg)
                }
            }
            'X' -> {
                val n = p(0, 1)
                val y = curY.coerceIn(0, rows - 1)
                for (c in curX until min(cols, curX + n)) {
                    grid[y][c].reset(penBg)
                }
            }
            'P' -> {
                val n = p(0, 1)
                val y = curY.coerceIn(0, rows - 1)
                val x = curX.coerceIn(0, cols - 1)
                for (c in x until cols) {
                    if (c + n < cols) {
                        grid[y][c] = grid[y][c + n].copy()
                    } else {
                        grid[y][c].reset(penBg)
                    }
                }
            }
            '@' -> {
                val n = p(0, 1)
                val y = curY.coerceIn(0, rows - 1)
                val x = curX.coerceIn(0, cols - 1)
                for (c in cols - 1 downTo x) {
                    if (c - n >= x) {
                        grid[y][c] = grid[y][c - n].copy()
                    } else {
                        grid[y][c].reset(penBg)
                    }
                }
            }
            'L' -> scrollDownRegion(curY.coerceIn(scrollTop, scrollBottom), scrollBottom, p(0, 1))
            'M' -> scrollUpRegion(curY.coerceIn(scrollTop, scrollBottom), scrollBottom, p(0, 1))
            'S' -> scrollUpRegion(scrollTop, scrollBottom, p(0, 1))
            'T' -> scrollDownRegion(scrollTop, scrollBottom, p(0, 1))
            'r' -> {
                val top = (p(0, 1) - 1).coerceIn(0, rows - 1)
                val bot = (p(1, rows) - 1).coerceIn(top, rows - 1)
                scrollTop = top
                scrollBottom = bot
                curX = 0
                curY = 0
            }
            's' -> saveCursor()
            'u' -> restoreCursor()
            'm' -> applySGR(if (nums.isEmpty()) listOf(0) else nums)
        }
    }

    private fun applySGR(codes: List<Int>) {
        var i = 0
        while (i < codes.size) {
            when (val code = codes[i]) {
                0 -> {
                    penFg = COLOR_DEFAULT_FG
                    penBg = COLOR_DEFAULT_BG
                    penBold = false
                    penInverse = false
                    penUnderline = false
                }
                1 -> penBold = true
                4 -> penUnderline = true
                7 -> penInverse = true
                22 -> penBold = false
                24 -> penUnderline = false
                27 -> penInverse = false
                in 30..37 -> penFg = ansi16Color(code - 30 + if (penBold) 8 else 0)
                39 -> penFg = COLOR_DEFAULT_FG
                in 40..47 -> penBg = ansi16Color(code - 40)
                49 -> penBg = COLOR_DEFAULT_BG
                in 90..97 -> penFg = ansi16Color(code - 90 + 8)
                in 100..107 -> penBg = ansi16Color(code - 100 + 8)
                38, 48 -> {
                    val isFg = code == 38
                    val mode = codes.getOrNull(i + 1) ?: 0
                    if (mode == 5 && i + 2 < codes.size) {
                        val color = xterm256Color(codes[i + 2])
                        if (isFg) penFg = color else penBg = color
                        i += 2
                    } else if (mode == 2 && i + 4 < codes.size) {
                        val r = codes[i + 2].coerceIn(0, 255)
                        val g = codes[i + 3].coerceIn(0, 255)
                        val b = codes[i + 4].coerceIn(0, 255)
                        val color = (0xFF shl 24) or (r shl 16) or (g shl 8) or b
                        if (isFg) penFg = color else penBg = color
                        i += 4
                    }
                }
            }
            i++
        }
    }

    private fun saveCursor() {
        if (usingAltScreen) {
            savedAltX = curX
            savedAltY = curY
        } else {
            savedMainX = curX
            savedMainY = curY
        }
    }

    private fun restoreCursor() {
        if (usingAltScreen) {
            curX = savedAltX.coerceIn(0, cols - 1)
            curY = savedAltY.coerceIn(0, rows - 1)
        } else {
            curX = savedMainX.coerceIn(0, cols - 1)
            curY = savedMainY.coerceIn(0, rows - 1)
        }
    }

    private fun fullReset() {
        usingAltScreen = false
        bracketedPasteMode = false
        appCursorKeys = false
        cursorVisible = true
        penFg = COLOR_DEFAULT_FG
        penBg = COLOR_DEFAULT_BG
        penBold = false
        penInverse = false
        penUnderline = false
        curX = 0
        curY = 0
        scrollTop = 0
        scrollBottom = rows - 1
        mainScreen = allocGrid(cols, rows)
        altScreen = allocGrid(cols, rows)
    }

    private fun allocGrid(c: Int, r: Int): Array<Array<TermCell>> =
        Array(r) { Array(c) { TermCell() } }

    private fun cloneRow(row: Array<TermCell>): Array<TermCell> =
        Array(row.size) { idx -> row[idx].copy() }

    private fun copyResizeGrid(old: Array<Array<TermCell>>, newC: Int, newR: Int): Array<Array<TermCell>> {
        val next = allocGrid(newC, newR)
        val copyR = min(old.size, newR)
        for (r in 0 until copyR) {
            val copyC = min(old[r].size, newC)
            for (c in 0 until copyC) {
                next[r][c] = old[r][c].copy()
            }
        }
        return next
    }

    private fun charCellWidth(cp: Int): Int {
        if (cp < 0x20 || cp in 0x7F..0x9F) return 0
        // Combining marks
        if (cp in 0x0300..0x036F || cp in 0x20D0..0x20FF || cp in 0xFE00..0xFE0F) return 0
        // CJK Ideographs, Hiragana, Katakana, Hangul, Fullwidth forms
        if (cp in 0x1100..0x115F ||
            cp in 0x2E80..0x303E ||
            cp in 0x3040..0xA4CF ||
            cp in 0xAC00..0xD7A3 ||
            cp in 0xF900..0xFAFF ||
            cp in 0xFE10..0xFE6F ||
            cp in 0xFF01..0xFF60 ||
            cp in 0xFFE0..0xFFE6 ||
            cp in 0x1F300..0x1FAFF ||
            cp in 0x20000..0x2FA1F
        ) {
            return 2
        }
        return 1
    }

    private fun ansi16Color(idx: Int): Int = colorIndexed(idx)

    private fun xterm256Color(idx: Int): Int {
        val n = idx.coerceIn(0, 255)
        if (n < 16) return ansi16Color(n)
        if (n in 16..231) {
            val v = n - 16
            val levels = intArrayOf(0, 95, 135, 175, 215, 255)
            val r = levels[(v / 36) % 6]
            val g = levels[(v / 6) % 6]
            val b = levels[v % 6]
            return (0xFF shl 24) or (r shl 16) or (g shl 8) or b
        }
        val gray = (8 + (n - 232) * 10).coerceIn(0, 255)
        return (0xFF shl 24) or (gray shl 16) or (gray shl 8) or gray
    }
}
