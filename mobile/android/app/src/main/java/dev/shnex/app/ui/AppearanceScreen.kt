package dev.shnex.app.ui

import android.graphics.Paint
import android.graphics.Typeface
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Slider
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.shnex.app.terminal.AnsiTerminalBuffer
import dev.shnex.app.terminal.CursorStyle
import dev.shnex.app.terminal.TerminalAppearance
import dev.shnex.app.terminal.TerminalColorScheme
import dev.shnex.app.terminal.TerminalColorSchemes
import dev.shnex.app.terminal.TerminalFont
import dev.shnex.app.terminal.drawTerminalGrid
import dev.shnex.app.terminal.measureTerminalGrid
import kotlin.math.floor
import kotlin.math.roundToInt

private const val PREVIEW_ROWS = 6
private const val ESC = "\u001b"

// Prompt, ls output with Nerd Font icons + CJK file name, ligatures and look-alike glyphs, 16 ANSI colors.
private val PREVIEW_SAMPLE: String = buildString {
    append("$ESC[32mshnex$ESC[0m@$ESC[34mdev$ESC[0m:$ESC[36m~/app$ESC[0m$ ls\r\n")
    append("$ESC[34m\uF07B src$ESC[0m  \uF48A README.md  \uF15C 笔记.md\r\n")
    append("$ESC[35mif$ESC[0m x >= 0 && x != n -> ok  0O 1lI\r\n")
    for (i in 0..7) append("$ESC[${40 + i}m  ")
    append("$ESC[0m\r\n")
    for (i in 0..7) append("$ESC[${100 + i}m  ")
    append("$ESC[0m\r\n")
    append("$ESC[32mshnex$ESC[0m:$ESC[36m~/app$ESC[0m$ ")
}

private val CardColor = Color(0xFF1B1C22)
private val CardBorder = Color(0xFF2C2F3A)
private val MutedText = Color(0xFF8B909A)

@Composable
fun AppearanceScreen(vm: WorkbenchViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    val a = vm.appearance
    val scheme = vm.terminalScheme
    val typeface = vm.terminalTypeface()

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) {
                Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回", tint = Color.White)
            }
            Text("终端外观", color = Color.White, fontSize = 19.sp, fontWeight = FontWeight.Bold)
        }

        TerminalPreviewCard(a, scheme, typeface)

        SettingsGroup("字体") {
            OptionChips(TerminalFont.entries, a.font, { it.label }) { f ->
                vm.updateAppearance { it.copy(font = f) }
            }
        }

        SettingsGroup("配色方案") {
            val all = TerminalColorSchemes.all
            all.forEachIndexed { i, s ->
                SchemeRow(s, selected = s.id == scheme.id) {
                    vm.updateAppearance { it.copy(schemeId = s.id) }
                }
                if (i < all.lastIndex) HorizontalDivider(color = Color(0xFF262934))
            }
        }

        SettingsGroup("排版") {
            SliderRow(
                label = "字号",
                value = a.fontSizeSp,
                range = TerminalAppearance.FONT_SIZE_MIN..TerminalAppearance.FONT_SIZE_MAX,
                stepSize = 0.5f,
                display = { "%.1f".format(it) }
            ) { v -> vm.updateAppearance { it.copy(fontSizeSp = v) } }
            SliderRow(
                label = "行距",
                value = a.lineHeightScale,
                range = TerminalAppearance.LINE_HEIGHT_MIN..TerminalAppearance.LINE_HEIGHT_MAX,
                stepSize = 0.05f,
                display = { "%.2f".format(it) }
            ) { v -> vm.updateAppearance { it.copy(lineHeightScale = v) } }
            Text("终端内双指缩放同样调整字号", color = MutedText, fontSize = 11.sp)
        }

        SettingsGroup("光标") {
            OptionChips(CursorStyle.entries, a.cursorStyle, { it.label }) { c ->
                vm.updateAppearance { it.copy(cursorStyle = c) }
            }
        }
    }
}

@Composable
private fun TerminalPreviewCard(a: TerminalAppearance, scheme: TerminalColorScheme, typeface: Typeface) {
    val density = LocalDensity.current
    val textPaint = remember { Paint(Paint.ANTI_ALIAS_FLAG or Paint.SUBPIXEL_TEXT_FLAG) }
    val bgPaint = remember { Paint() }
    val pad = 10f * density.density
    val fontPx = with(density) { a.fontSizeSp.sp.toPx() }
    val metrics = measureTerminalGrid(textPaint, typeface, fontPx, a.lineHeightScale, pad)

    var widthPx by remember { mutableIntStateOf(0) }
    val cols = if (widthPx > 0) floor((widthPx - 2f * pad) / metrics.cellW).toInt().coerceAtLeast(20) else 40
    val snap = remember(cols) {
        AnsiTerminalBuffer(cols, PREVIEW_ROWS).apply { feedBytes(PREVIEW_SAMPLE.toByteArray()) }.snapshot(0)
    }
    val heightDp = with(density) { (PREVIEW_ROWS * metrics.cellH + 2f * pad).toDp() }
    val shape = RoundedCornerShape(14.dp)

    Canvas(
        modifier = Modifier
            .fillMaxWidth()
            .height(heightDp)
            .clip(shape)
            .background(Color(scheme.bg))
            .border(1.dp, CardBorder, shape)
            .onSizeChanged { widthPx = it.width }
    ) {
        drawTerminalGrid(
            nc = drawContext.canvas.nativeCanvas,
            snap = snap,
            m = metrics,
            textPaint = textPaint,
            bgPaint = bgPaint,
            scheme = scheme,
            cursorStyle = a.cursorStyle,
            showCursor = true,
            cursorThickness = 2f * density.density
        )
    }
}

@Composable
private fun SettingsGroup(title: String, content: @Composable ColumnScope.() -> Unit) {
    Surface(
        shape = RoundedCornerShape(18.dp),
        color = CardColor,
        modifier = Modifier
            .fillMaxWidth()
            .border(1.dp, CardBorder, RoundedCornerShape(18.dp))
    ) {
        Column(modifier = Modifier.padding(15.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, color = MutedText, fontSize = 12.sp, fontWeight = FontWeight.Medium)
            content()
        }
    }
}

@Composable
private fun <T> OptionChips(options: List<T>, selected: T, label: (T) -> String, onSelect: (T) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        options.forEach { opt ->
            val isSel = opt == selected
            val shape = RoundedCornerShape(999.dp)
            Box(
                modifier = Modifier
                    .clip(shape)
                    .background(if (isSel) Color(0xFF2563EB) else Color(0xFF242731))
                    .border(1.dp, if (isSel) Color(0xFF3B82F6) else Color(0xFF323644), shape)
                    .clickable { onSelect(opt) }
                    .padding(horizontal = 14.dp, vertical = 7.dp)
            ) {
                Text(label(opt), color = if (isSel) Color.White else Color(0xFFD1D5DB), fontSize = 13.sp)
            }
        }
    }
}

@Composable
private fun SchemeRow(scheme: TerminalColorScheme, selected: Boolean, onClick: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        Row(
            modifier = Modifier
                .clip(RoundedCornerShape(6.dp))
                .background(Color(scheme.bg))
                .border(1.dp, CardBorder, RoundedCornerShape(6.dp))
                .padding(horizontal = 7.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(4.dp)
        ) {
            listOf(scheme.fg, scheme.ansi16[1], scheme.ansi16[2], scheme.ansi16[4], scheme.ansi16[5]).forEach { c ->
                Box(Modifier.size(7.dp).background(Color(c), CircleShape))
            }
        }
        Spacer(Modifier.width(12.dp))
        Text(scheme.name, color = Color.White, fontSize = 14.sp, modifier = Modifier.weight(1f))
        if (selected) {
            Icon(Icons.Default.Check, contentDescription = "已选", tint = Color(0xFF60A5FA), modifier = Modifier.size(18.dp))
        }
    }
}

@Composable
private fun SliderRow(
    label: String,
    value: Float,
    range: ClosedFloatingPointRange<Float>,
    stepSize: Float,
    display: (Float) -> String,
    onChange: (Float) -> Unit
) {
    val steps = ((range.endInclusive - range.start) / stepSize).roundToInt() - 1
    Column {
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(label, color = Color.White, fontSize = 14.sp, modifier = Modifier.weight(1f))
            Text(display(value), color = Color(0xFFD1D5DB), fontSize = 13.sp, fontFamily = FontFamily.Monospace)
        }
        Slider(
            value = value,
            onValueChange = { v -> onChange(((v - range.start) / stepSize).roundToInt() * stepSize + range.start) },
            valueRange = range,
            steps = steps
        )
    }
}
