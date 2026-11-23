package dev.shnex.app.terminal

import android.content.Context
import android.graphics.Paint
import android.graphics.Typeface
import android.os.Looper
import android.text.Editable
import android.text.InputType
import android.text.Selection
import android.text.SpannableStringBuilder
import android.view.KeyEvent
import android.view.View
import android.view.inputmethod.BaseInputConnection
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputConnection
import android.view.inputmethod.InputMethodManager
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.awaitEachGesture
import androidx.compose.foundation.gestures.awaitFirstDown
import androidx.compose.foundation.gestures.calculatePan
import androidx.compose.foundation.gestures.calculateZoom
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import kotlin.math.floor
import kotlin.math.hypot
import kotlin.math.max
import kotlin.math.min
import kotlin.math.roundToInt

private const val IME_SENTINEL = "\u200B\u200B\u200B "
private const val IME_SENTINEL_LEN = IME_SENTINEL.length

class TerminalImeController internal constructor() {
    internal var bridgeView: TerminalImeBridgeView? = null

    fun showKeyboard() {
        bridgeView?.focusAndShowKeyboard()
    }

    fun toggleKeyboard(currentlyVisible: Boolean? = null) {
        bridgeView?.toggleKeyboard(currentlyVisible)
    }

    fun sendSpecialKey(
        seq: String,
        commitComposingFirst: Boolean = true,
        fallbackSend: (String) -> Unit = {}
    ) {
        val v = bridgeView
        if (v != null) {
            v.sendExternalKeySequence(seq, commitComposingFirst)
        } else {
            fallbackSend(seq)
        }
    }

    fun resetState() {
        bridgeView?.resetBufferState(restartImm = true)
    }
}

@Composable
fun rememberTerminalImeController(): TerminalImeController {
    return remember { TerminalImeController() }
}

/**
 * Invisible 1dp native Android View backed by a real Editable buffer and
 * committed-prefix diff engine. Handles Sogou/Baidu/ColorOS/Gboard/WeChat IME
 * `finishComposingText() + setComposingRegion() + setComposingText()` cycles,
 * Chinese Pinyin pre-edit, English word suggestions, Voice Input revisions,
 * and reliable Backspace without ever duplicating intermediate characters.
 */
internal class TerminalImeBridgeView(context: Context) : View(context) {
    var onCommitText: (text: String, multilinePaste: Boolean) -> Unit = { _, _ -> }
    var onRawKey: (seq: String) -> Unit = {}
    var onComposingChanged: (text: String) -> Unit = {}
    var isAppCursorMode: () -> Boolean = { false }
    var tryConsumeCtrlLetter: (ch: Char) -> Boolean = { false }

    private val imeBuffer: Editable = SpannableStringBuilder(IME_SENTINEL)
    private var syncedText: String = ""
    private var batchDepth: Int = 0
    private var batchSawCommitOrDelete: Boolean = false
    private var internalMutating: Boolean = false
    private var imeVisibleFlag: Boolean = false

    private var lastSelStart: Int = IME_SENTINEL_LEN
    private var lastSelEnd: Int = IME_SENTINEL_LEN
    private var lastCompStart: Int = -1
    private var lastCompEnd: Int = -1

    private val deferredFinishFlush = Runnable {
        if (batchDepth == 0) {
            syncBufferWithPty(immediateCommit = true)
        }
    }

    init {
        isFocusable = true
        isFocusableInTouchMode = true
        Selection.setSelection(imeBuffer, IME_SENTINEL_LEN)
    }

    private fun imm(): InputMethodManager? {
        return context.getSystemService(Context.INPUT_METHOD_SERVICE) as? InputMethodManager
    }

    fun focusAndShowKeyboard() {
        requestFocus()
        post {
            val manager = imm() ?: return@post
            val shown = manager.showSoftInput(this, 0)
            if (!shown) {
                manager.restartInput(this)
                manager.showSoftInput(this, 0)
            }
            imeVisibleFlag = true
        }
    }

    fun toggleKeyboard(currentlyVisible: Boolean? = null) {
        val manager = imm() ?: return
        val visible = currentlyVisible ?: imeVisibleFlag
        if (visible) {
            manager.hideSoftInputFromWindow(windowToken, 0)
            imeVisibleFlag = false
        } else {
            focusAndShowKeyboard()
        }
    }

    fun sendExternalKeySequence(seq: String, commitComposingFirst: Boolean) {
        removeCallbacks(deferredFinishFlush)
        if (commitComposingFirst) {
            BaseInputConnection.removeComposingSpans(imeBuffer)
            syncBufferWithPty(immediateCommit = true)
        }
        resetBufferState(restartImm = true)
        onRawKey(seq)
    }

    fun resetBufferState(restartImm: Boolean) {
        removeCallbacks(deferredFinishFlush)
        internalMutating = true
        BaseInputConnection.removeComposingSpans(imeBuffer)
        imeBuffer.replace(0, imeBuffer.length, IME_SENTINEL)
        Selection.setSelection(imeBuffer, IME_SENTINEL_LEN)
        syncedText = ""
        batchDepth = 0
        batchSawCommitOrDelete = false
        lastSelStart = IME_SENTINEL_LEN
        lastSelEnd = IME_SENTINEL_LEN
        lastCompStart = -1
        lastCompEnd = -1
        internalMutating = false
        onComposingChanged("")
        if (restartImm) {
            imm()?.restartInput(this)
        } else {
            notifySelectionToImm()
        }
    }

    private fun notifySelectionToImm() {
        val selS = Selection.getSelectionStart(imeBuffer).coerceAtLeast(0)
        val selE = Selection.getSelectionEnd(imeBuffer).coerceAtLeast(0)
        val rawCompS = BaseInputConnection.getComposingSpanStart(imeBuffer)
        val rawCompE = BaseInputConnection.getComposingSpanEnd(imeBuffer)
        val compS = if (rawCompS >= 0 && rawCompE > rawCompS) rawCompS else -1
        val compE = if (compS >= 0) rawCompE else -1

        if (selS != lastSelStart || selE != lastSelEnd || compS != lastCompStart || compE != lastCompEnd) {
            lastSelStart = selS
            lastSelEnd = selE
            lastCompStart = compS
            lastCompEnd = compE
            imm()?.updateSelection(this, selS, selE, compS, compE)
        }
    }

    private fun commonPrefixLength(a: String, b: String): Int {
        val limit = min(a.length, b.length)
        var i = 0
        while (i < limit && a[i] == b[i]) {
            i++
        }
        if (i > 0 && i < limit && Character.isHighSurrogate(a[i - 1])) {
            i--
        }
        return i
    }

    private fun applyDiffToPty(targetCommitted: String) {
        if (targetCommitted == syncedText) return
        val common = commonPrefixLength(syncedText, targetCommitted)
        if (common < syncedText.length) {
            val removed = syncedText.substring(common)
            val delCount = removed.codePointCount(0, removed.length)
            if (delCount > 0) {
                onRawKey("\u007f".repeat(delCount))
            }
        }
        if (common < targetCommitted.length) {
            val added = targetCommitted.substring(common)
            if (added.isNotEmpty()) {
                onCommitText(added, false)
            }
        }
        syncedText = targetCommitted
    }

    private fun syncBufferWithPty(immediateCommit: Boolean) {
        if (internalMutating) return

        val raw = imeBuffer.toString()

        // 1. Sentinel check: if user backspaced into the leading sentinel, emit exact DELs and restore sentinel
        if (!raw.startsWith(IME_SENTINEL)) {
            removeCallbacks(deferredFinishFlush)
            var intact = 0
            while (intact < IME_SENTINEL_LEN && intact < raw.length && raw[intact] == IME_SENTINEL[intact]) {
                intact++
            }
            val deletedSentinelChars = (IME_SENTINEL_LEN - intact).coerceAtLeast(1)
            val survivingUserText = raw.substring(intact)

            val prevCodePoints = syncedText.codePointCount(0, syncedText.length)
            val totalBackspaces = prevCodePoints + deletedSentinelChars
            if (totalBackspaces > 0) {
                onRawKey("\u007f".repeat(totalBackspaces))
            }
            syncedText = ""
            onComposingChanged("")

            internalMutating = true
            BaseInputConnection.removeComposingSpans(imeBuffer)
            imeBuffer.replace(0, imeBuffer.length, IME_SENTINEL + survivingUserText)
            Selection.setSelection(imeBuffer, imeBuffer.length)
            internalMutating = false

            if (survivingUserText.isNotEmpty()) {
                applyDiffToPty(survivingUserText)
            }
            notifySelectionToImm()
            return
        }

        val compS = BaseInputConnection.getComposingSpanStart(imeBuffer)
        val compE = BaseInputConnection.getComposingSpanEnd(imeBuffer)
        val hasComposing = compS >= IME_SENTINEL_LEN && compE > compS && compE <= raw.length

        // 2. Active composing span: keep pre-edit text local on canvas without prematurely leaking to PTY
        if (hasComposing) {
            removeCallbacks(deferredFinishFlush)
            val committedBefore = raw.substring(IME_SENTINEL_LEN, compS)
            val composingMid = raw.substring(compS, compE)
            val committedAfter = raw.substring(compE)
            val fullUserText = raw.substring(IME_SENTINEL_LEN)

            if (committedBefore.length >= syncedText.length || !fullUserText.startsWith(syncedText)) {
                applyDiffToPty(committedBefore)
                onComposingChanged(composingMid + committedAfter)
            } else {
                // IME pulled already-synced prefix back into ComposingSpan via setComposingRegion:
                // only display the unsynced suffix so screen never shows duplicate characters!
                onComposingChanged(fullUserText.substring(syncedText.length))
            }
            notifySelectionToImm()
            return
        }

        // 3. No active composing span
        val userText = raw.substring(IME_SENTINEL_LEN)
        if (!immediateCommit && userText != syncedText) {
            // Coalesce standalone finishComposingText() in case setComposingRegion() + setComposingText()
            // arrives immediately in the next Binder call on Sogou/Baidu/ColorOS IMEs.
            removeCallbacks(deferredFinishFlush)
            postDelayed(deferredFinishFlush, 24L)
            notifySelectionToImm()
            return
        }

        removeCallbacks(deferredFinishFlush)
        onComposingChanged("")

        // Multi-line paste or embedded newline -> commit and reset line mirror
        if (userText.contains('\n') || userText.contains('\r')) {
            val common = commonPrefixLength(syncedText, userText)
            if (common < syncedText.length) {
                val removed = syncedText.substring(common)
                val delCount = removed.codePointCount(0, removed.length)
                if (delCount > 0) {
                    onRawKey("\u007f".repeat(delCount))
                }
            }
            val added = userText.substring(common)
            if (added.isNotEmpty()) {
                val normalized = added.replace("\r\n", "\r").replace("\n", "\r")
                val isPaste = added.length > 1 && (added.contains('\n') || added.contains('\r'))
                onCommitText(normalized, isPaste)
            }
            resetBufferState(restartImm = true)
            return
        }

        applyDiffToPty(userText)

        // Bound buffer size for very long lines
        if (syncedText.length > 240 && Selection.getSelectionStart(imeBuffer) == imeBuffer.length) {
            val tail = syncedText.takeLast(64)
            internalMutating = true
            imeBuffer.replace(0, imeBuffer.length, IME_SENTINEL + tail)
            Selection.setSelection(imeBuffer, imeBuffer.length)
            syncedText = tail
            internalMutating = false
        }

        notifySelectionToImm()
    }

    override fun onCheckIsTextEditor(): Boolean = true

    override fun onCreateInputConnection(outAttrs: EditorInfo): InputConnection {
        val hostView = this@TerminalImeBridgeView
        outAttrs.inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
        outAttrs.imeOptions = EditorInfo.IME_ACTION_SEND or
            EditorInfo.IME_FLAG_NO_FULLSCREEN or
            EditorInfo.IME_FLAG_NO_EXTRACT_UI
        val selS = Selection.getSelectionStart(imeBuffer).coerceIn(0, imeBuffer.length)
        val selE = Selection.getSelectionEnd(imeBuffer).coerceIn(0, imeBuffer.length)
        outAttrs.initialSelStart = selS
        outAttrs.initialSelEnd = selE
        lastSelStart = selS
        lastSelEnd = selE
        lastCompStart = BaseInputConnection.getComposingSpanStart(imeBuffer)
        lastCompEnd = BaseInputConnection.getComposingSpanEnd(imeBuffer)

        return object : BaseInputConnection(hostView, true) {
            // IMPORTANT: Do NOT name the backing field `editable`, because BaseInputConnection
            // exposes `getEditable()` as synthetic Kotlin property `editable`, which would shadow
            // an outer `editable` field and cause infinite recursion!
            override fun getEditable(): Editable = hostView.imeBuffer

            override fun beginBatchEdit(): Boolean {
                hostView.batchDepth++
                return super.beginBatchEdit()
            }

            override fun endBatchEdit(): Boolean {
                val ok = super.endBatchEdit()
                hostView.batchDepth = (hostView.batchDepth - 1).coerceAtLeast(0)
                if (hostView.batchDepth == 0) {
                    val immediate = hostView.batchSawCommitOrDelete
                    hostView.batchSawCommitOrDelete = false
                    hostView.syncBufferWithPty(immediateCommit = immediate)
                }
                return ok
            }

            override fun setComposingText(text: CharSequence?, newCursorPosition: Int): Boolean {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                val str = text?.toString().orEmpty()
                if (str.length == 1 && hostView.tryConsumeCtrlLetter(str[0])) {
                    hostView.resetBufferState(restartImm = true)
                    return true
                }
                val ok = super.setComposingText(text, newCursorPosition)
                if (hostView.batchDepth == 0) {
                    hostView.syncBufferWithPty(immediateCommit = false)
                }
                return ok
            }

            override fun setComposingRegion(start: Int, end: Int): Boolean {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                val s = start.coerceIn(0, hostView.imeBuffer.length)
                val e = end.coerceIn(0, hostView.imeBuffer.length)
                val ok = super.setComposingRegion(s, e)
                if (hostView.batchDepth == 0) {
                    hostView.syncBufferWithPty(immediateCommit = false)
                }
                return ok
            }

            override fun finishComposingText(): Boolean {
                val hadComp = getComposingSpanStart(hostView.imeBuffer) >= 0
                val ok = super.finishComposingText()
                if (hostView.batchDepth == 0 && hadComp) {
                    hostView.syncBufferWithPty(immediateCommit = false)
                }
                return ok
            }

            override fun commitText(text: CharSequence?, newCursorPosition: Int): Boolean {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                val str = text?.toString().orEmpty()
                if (str.length == 1 && hostView.tryConsumeCtrlLetter(str[0])) {
                    hostView.resetBufferState(restartImm = true)
                    return true
                }
                if (hostView.batchDepth > 0) {
                    hostView.batchSawCommitOrDelete = true
                }
                val ok = super.commitText(text, newCursorPosition)
                if (hostView.batchDepth == 0) {
                    hostView.syncBufferWithPty(immediateCommit = true)
                }
                return ok
            }

            override fun deleteSurroundingText(beforeLength: Int, afterLength: Int): Boolean {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                if (hostView.batchDepth > 0) {
                    hostView.batchSawCommitOrDelete = true
                }
                val ok = super.deleteSurroundingText(beforeLength, afterLength)
                if (hostView.batchDepth == 0) {
                    hostView.syncBufferWithPty(immediateCommit = true)
                }
                return ok
            }

            override fun deleteSurroundingTextInCodePoints(beforeLength: Int, afterLength: Int): Boolean {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                if (hostView.batchDepth > 0) {
                    hostView.batchSawCommitOrDelete = true
                }
                val ok = super.deleteSurroundingTextInCodePoints(beforeLength, afterLength)
                if (hostView.batchDepth == 0) {
                    hostView.syncBufferWithPty(immediateCommit = true)
                }
                return ok
            }

            override fun setSelection(start: Int, end: Int): Boolean {
                val s = start.coerceIn(0, hostView.imeBuffer.length)
                val e = end.coerceIn(0, hostView.imeBuffer.length)
                val ok = super.setSelection(s, e)
                if (hostView.batchDepth == 0) {
                    hostView.notifySelectionToImm()
                }
                return ok
            }

            override fun performEditorAction(actionCode: Int): Boolean {
                hostView.sendExternalKeySequence("\r", commitComposingFirst = true)
                return true
            }

            override fun sendKeyEvent(event: KeyEvent): Boolean {
                hostView.handleKeyEventInternal(event)
                return true
            }

            override fun closeConnection() {
                hostView.removeCallbacks(hostView.deferredFinishFlush)
                hostView.internalMutating = true
                removeComposingSpans(hostView.imeBuffer)
                hostView.internalMutating = false
                if (Looper.myLooper() == Looper.getMainLooper()) {
                    hostView.onComposingChanged("")
                } else {
                    hostView.post { hostView.onComposingChanged("") }
                }
            }
        }
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean {
        if (handleKeyEventInternal(event)) return true
        return super.onKeyDown(keyCode, event)
    }

    private fun handleKeyEventInternal(event: KeyEvent): Boolean {
        if (event.action != KeyEvent.ACTION_DOWN) return false

        val appCur = isAppCursorMode()
        when (event.keyCode) {
            KeyEvent.KEYCODE_DEL -> {
                removeCallbacks(deferredFinishFlush)
                val selS = Selection.getSelectionStart(imeBuffer).coerceIn(0, imeBuffer.length)
                val selE = Selection.getSelectionEnd(imeBuffer).coerceIn(0, imeBuffer.length)
                if (selS != selE) {
                    imeBuffer.delete(min(selS, selE), max(selS, selE))
                    syncBufferWithPty(immediateCommit = true)
                } else if (selS > IME_SENTINEL_LEN) {
                    val prevCharIdx = Character.offsetByCodePoints(imeBuffer, selS, -1)
                    imeBuffer.delete(prevCharIdx, selS)
                    syncBufferWithPty(immediateCommit = true)
                } else {
                    onRawKey("\u007f")
                }
                return true
            }
            KeyEvent.KEYCODE_FORWARD_DEL -> {
                sendExternalKeySequence("\u001b[3~", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> {
                sendExternalKeySequence("\r", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_TAB -> {
                sendExternalKeySequence(if (event.isShiftPressed) "\u001b[Z" else "\t", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_ESCAPE -> {
                sendExternalKeySequence("\u001b", commitComposingFirst = false)
                return true
            }
            KeyEvent.KEYCODE_DPAD_UP -> {
                sendExternalKeySequence(if (appCur) "\u001bOA" else "\u001b[A", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_DPAD_DOWN -> {
                sendExternalKeySequence(if (appCur) "\u001bOB" else "\u001b[B", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_DPAD_RIGHT -> {
                sendExternalKeySequence(if (appCur) "\u001bOC" else "\u001b[C", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_DPAD_LEFT -> {
                sendExternalKeySequence(if (appCur) "\u001bOD" else "\u001b[D", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_MOVE_HOME -> {
                sendExternalKeySequence("\u001b[H", commitComposingFirst = true)
                return true
            }
            KeyEvent.KEYCODE_MOVE_END -> {
                sendExternalKeySequence("\u001b[F", commitComposingFirst = true)
                return true
            }
        }

        if (event.isCtrlPressed && event.keyCode in KeyEvent.KEYCODE_A..KeyEvent.KEYCODE_Z) {
            val code = (event.keyCode - KeyEvent.KEYCODE_A + 1).toChar()
            sendExternalKeySequence(code.toString(), commitComposingFirst = false)
            return true
        }

        val uni = event.unicodeChar
        if (uni > 0) {
            val chStr = String(Character.toChars(uni))
            if (chStr.length == 1 && tryConsumeCtrlLetter(chStr[0])) {
                resetBufferState(restartImm = true)
                return true
            }
            removeCallbacks(deferredFinishFlush)
            BaseInputConnection.removeComposingSpans(imeBuffer)
            val selS = Selection.getSelectionStart(imeBuffer).coerceIn(IME_SENTINEL_LEN, imeBuffer.length)
            val selE = Selection.getSelectionEnd(imeBuffer).coerceIn(IME_SENTINEL_LEN, imeBuffer.length)
            imeBuffer.replace(min(selS, selE), max(selS, selE), chStr)
            Selection.setSelection(imeBuffer, min(selS, selE) + chStr.length)
            syncBufferWithPty(immediateCommit = true)
            return true
        }
        return false
    }
}

@Composable
fun TerminalCanvasView(
    buffer: AnsiTerminalBuffer,
    renderTick: Long,
    initialFontSp: Float = 13f,
    imeController: TerminalImeController? = null,
    onInterceptCtrlLetter: (ch: Char) -> Boolean = { false },
    onTerminalInput: (text: String, useBracketedPaste: Boolean) -> Unit = { _, _ -> },
    onTerminalResize: (cols: Int, rows: Int) -> Unit,
    modifier: Modifier = Modifier
) {
    val density = LocalDensity.current
    var fontSp by remember(initialFontSp) { mutableFloatStateOf(initialFontSp) }
    var scrollOffset by remember { mutableIntStateOf(0) }
    var scrollFracPx by remember { mutableFloatStateOf(0f) }
    var canvasSize by remember { mutableIntStateOf(0) }
    var sizePair by remember { mutableStateOf(IntSize.Zero) }
    var composingText by remember { mutableStateOf("") }

    val latestInputCb by rememberUpdatedState(onTerminalInput)
    val latestCtrlCb by rememberUpdatedState(onInterceptCtrlLetter)
    val latestBuffer by rememberUpdatedState(buffer)
    val internalController = imeController ?: rememberTerminalImeController()

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
            buffer.resize(cols, rows)
            onTerminalResize(cols, rows)
        }
    }

    LaunchedEffect(renderTick) {
        if (scrollOffset <= 1) {
            scrollOffset = 0
        }
    }

    val snap = remember(renderTick, scrollOffset, fontSp, sizePair, canvasSize, composingText) {
        buffer.snapshot(scrollOffset)
    }

    Box(modifier = modifier.fillMaxSize().background(Color(0xFF121316))) {
        // Hidden 1dp native Android IME receiver view
        AndroidView(
            factory = { ctx ->
                TerminalImeBridgeView(ctx).also { v ->
                    internalController.bridgeView = v
                }
            },
            update = { v ->
                internalController.bridgeView = v
                v.isAppCursorMode = { latestBuffer.appCursorKeys }
                v.tryConsumeCtrlLetter = { ch -> latestCtrlCb(ch) }
                v.onComposingChanged = { txt -> composingText = txt }
                v.onCommitText = { txt, isPaste ->
                    scrollOffset = 0
                    latestInputCb(txt, isPaste)
                }
                v.onRawKey = { seq ->
                    scrollOffset = 0
                    latestInputCb(seq, false)
                }
            },
            modifier = Modifier.size(1.dp).alpha(0f)
        )

        Canvas(
            modifier = Modifier
                .fillMaxSize()
                .onSizeChanged { sz ->
                    sizePair = sz
                    canvasSize = sz.width xor sz.height
                }
                .pointerInput(cellH) {
                    val slop = viewConfiguration.touchSlop
                    awaitEachGesture {
                        val down = awaitFirstDown(requireUnconsumed = false)
                        val startPos: Offset = down.position
                        var movedBeyondSlop = false
                        var maxPointers = 1

                        do {
                            val event = awaitPointerEvent()
                            val activeCount = event.changes.count { it.pressed }
                            if (activeCount >= 2) {
                                maxPointers = max(maxPointers, activeCount)
                                val zoom = event.calculateZoom()
                                if (zoom != 1f) {
                                    fontSp = (fontSp * zoom).coerceIn(9.5f, 22f)
                                    event.changes.forEach { it.consume() }
                                }
                            } else if (activeCount == 1 && maxPointers == 1) {
                                val curPos = event.changes.firstOrNull { it.pressed }?.position ?: startPos
                                if (!movedBeyondSlop) {
                                    val dist = hypot(curPos.x - startPos.x, curPos.y - startPos.y)
                                    if (dist > slop) {
                                        movedBeyondSlop = true
                                    }
                                }
                                if (movedBeyondSlop) {
                                    val pan = event.calculatePan()
                                    if (pan.y != 0f) {
                                        scrollFracPx += pan.y
                                        val deltaLines = (scrollFracPx / cellH).roundToInt()
                                        if (deltaLines != 0) {
                                            scrollFracPx -= deltaLines * cellH
                                            val curScrollMax = latestBuffer.snapshot(0).scrollbackSize
                                            scrollOffset = (scrollOffset + deltaLines).coerceIn(0, curScrollMax)
                                        }
                                        event.changes.forEach { it.consume() }
                                    }
                                }
                            }
                        } while (event.changes.any { it.pressed })

                        // Single finger clean tap anywhere on terminal -> snap to bottom & open soft keyboard!
                        if (!movedBeyondSlop && maxPointers == 1) {
                            scrollOffset = 0
                            internalController.showKeyboard()
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

            val cx = pad + snap.cursorX * cellW
            val cy = pad + snap.cursorY * cellH

            if (composingText.isNotEmpty() && scrollOffset == 0) {
                // Draw inline IME pre-edit text right at the live cursor cell!
                textPaint.isFakeBoldText = false
                textPaint.isUnderlineText = false
                val compW = max(cellW, textPaint.measureText(composingText) + 6f)
                bgPaint.color = 0xEE1E2433.toInt()
                nc.drawRect(cx, cy, cx + compW, cy + cellH, bgPaint)
                bgPaint.color = 0xFF60A5FA.toInt()
                nc.drawRect(cx, cy + cellH - 2.5f * density.density, cx + compW, cy + cellH, bgPaint)
                textPaint.color = 0xFFF9FAFB.toInt()
                nc.drawText(composingText, cx + 3f, cy + baselineOffset, textPaint)
            } else if (snap.cursorVisible && scrollOffset == 0) {
                bgPaint.color = 0x9960A5FA.toInt()
                nc.drawRect(cx, cy, cx + cellW, cy + cellH, bgPaint)
            }
        }
    }
}
