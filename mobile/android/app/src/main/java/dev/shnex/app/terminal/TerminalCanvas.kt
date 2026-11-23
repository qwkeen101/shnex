package dev.shnex.app.terminal

import android.graphics.Paint
import android.graphics.Typeface
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTransformGestures
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.sp
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.roundToInt

@Composable
fun TerminalCanvasView(
    buffer: AnsiTerminalBuffer,
    renderTick: Long,
    initialFontSp: Float = 13f,
    onTerminalResize: (cols: Int, rows: Int) -> Unit,
    modifier: Modifier = Modifier
) {
    val density = LocalDensity.current
    var fontSp by remember(initialFontSp) { mutableFloatStateOf(initialFontSp) }
    var scrollOffset by remember { mutableIntStateOf(0) }
    var scrollFracPx by remember { mutableFloatStateOf(0f) }
    var canvasSize by remember { mutableIntStateOf(0) }
    var sizePair by remember { androidx.compose.runtime.mutableStateOf(IntSize.Zero) }

    val textPaint = remember {
        Paint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG).apply {
            typeface = Typeface.MONOSPACE
        }
    }
    val bgPaint = remember { Paint() }

    val fontPx = with(density) { fontSp.sp.toPx() }
    textPaint.textSize = fontPx
    val cellW = max(6f, textPaint.measureText("M"))
    val fm = textPaint.fontMetrics
    val cellH = max(12f, (fm.descent - fm.ascent + fm.leading) * 1.12f)
    val baselineOffset = -fm.ascent + (cellH - (fm.descent - fm.ascent)) * 0.5f

    LaunchedEffect(sizePair, fontSp) {
        if (sizePair.width > 40 && sizePair.height > 40) {
            val padPx = with(density) { 12f * this.density }
            val cols = max(24, floor((sizePair.width - padPx) / cellW).toInt())
            val rows = max(8, floor((sizePair.height - padPx) / cellH).toInt())
            if (buffer.resize(cols, rows)) {
                onTerminalResize(cols, rows)
            } else {
                onTerminalResize(cols, rows)
            }
        }
    }

    // Snap back to bottom when new output arrives while near bottom
    LaunchedEffect(renderTick) {
        if (scrollOffset <= 1) {
            scrollOffset = 0
        }
    }

    val snap = remember(renderTick, scrollOffset, fontSp, sizePair, canvasSize) {
        buffer.snapshot(scrollOffset)
    }

    Canvas(
        modifier = modifier
            .fillMaxSize()
            .background(Color(0xFF121316))
            .onSizeChanged { sz ->
                sizePair = sz
                canvasSize = sz.width xor sz.height
            }
            .pointerInput(cellH) {
                detectTransformGestures { _, pan, zoom, _ ->
                    if (zoom != 1f) {
                        fontSp = (fontSp * zoom).coerceIn(9.5f, 22f)
                    }
                    if (pan.y != 0f) {
                        scrollFracPx += pan.y
                        val deltaLines = (scrollFracPx / cellH).roundToInt()
                        if (deltaLines != 0) {
                            scrollFracPx -= deltaLines * cellH
                            scrollOffset = (scrollOffset + deltaLines).coerceIn(0, snap.scrollbackSize)
                        }
                    }
                }
            }
    ) {
        val pad = 6f * density.density
        val nc = drawContext.canvas.nativeCanvas

        for (r in 0 until snap.rows) {
            val rowCells = snap.lines.getOrNull(r) ?: continue
            val topY = pad + r * cellH
            val baseY = topY + baselineOffset

            var c = 0
            while (c < snap.cols && c < rowCells.size) {
                val cell = rowCells[c]
                if (cell.width == 0) {
                    c++
                    continue
                }
                val spanW = cellW * cell.width.coerceAtLeast(1)
                val leftX = pad + c * cellW

                val effFg = if (cell.inverse) cell.bg else cell.fg
                val effBg = if (cell.inverse) cell.fg else cell.bg

                if (effBg != DEFAULT_BG) {
                    bgPaint.color = effBg
                    nc.drawRect(leftX, topY, leftX + spanW + 0.6f, topY + cellH + 0.6f, bgPaint)
                }

                if (cell.text.isNotEmpty() && cell.text != " ") {
                    textPaint.color = effFg
                    textPaint.isFakeBoldText = cell.bold
                    textPaint.isUnderlineText = cell.underline
                    nc.drawText(cell.text, leftX, baseY, textPaint)
                }
                c++
            }
        }

        if (snap.cursorVisible) {
            val cx = pad + snap.cursorX * cellW
            val cy = pad + snap.cursorY * cellH
            bgPaint.color = 0x9960A5FA.toInt()
            nc.drawRect(cx, cy, cx + cellW, cy + cellH, bgPaint)
        }
    }
}
