package dev.shnex.app.terminal

import android.content.Context
import android.graphics.Paint
import android.graphics.Typeface
import okhttp3.OkHttpClient
import okhttp3.Request
import java.io.File
import java.io.FilterInputStream
import java.io.IOException
import java.io.InputStream
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.zip.Inflater
import java.util.zip.InflaterInputStream
import kotlin.math.max
import kotlin.math.min

sealed interface FontSource {
    object SystemMono : FontSource
    object SystemSans : FontSource
    class Bundled(val asset: String) : FontSource

    /** A TTF served as a single file. */
    class Direct(val url: String, val sha256: String) : FontSource

    /**
     * One TTF inside an official release zip. Only that entry is fetched (HTTP range
     * requests against the zip's central directory), not the whole archive.
     */
    class ZipEntry(val zipUrl: String, val entryName: String, val sha256: String) : FontSource
}

class FontEntry(
    val id: String,
    val name: String,
    val subtitle: String,
    val description: String,
    val tags: List<String>,
    val source: FontSource,
    /** Bytes actually transferred when downloading (compressed size for zip entries). */
    val downloadBytes: Long = 0L
) {
    val downloadable: Boolean
        get() = source is FontSource.Direct || source is FontSource.ZipEntry
}

/** Versions and checksums are pinned; a changed upstream file fails the sha256 check instead of loading. */
object FontCatalog {
    const val DEFAULT_PRIMARY = "maple_nf"
    const val DEFAULT_FALLBACK = "system_sans"

    val all: List<FontEntry> = listOf(
        FontEntry(
            id = "maple_nf",
            name = "Maple Mono",
            subtitle = "Maple Mono NF · OFL",
            description = "圆角等宽 · Nerd 图标 · 连字",
            tags = listOf("内置", "连字"),
            source = FontSource.Bundled("fonts/MapleMono-NF-Regular.ttf")
        ),
        FontEntry(
            id = "maple_nf_cn",
            name = "Maple Mono · 中英等宽",
            subtitle = "Maple Mono NF CN · OFL",
            description = "含中文（2:1）+ Nerd 图标 · 高分屏 unhinted",
            tags = listOf("含中文", "连字"),
            source = FontSource.ZipEntry(
                zipUrl = "https://github.com/subframe7536/maple-font/releases/download/v7.9/MapleMono-NF-CN-unhinted.zip",
                entryName = "MapleMono-NF-CN-Regular.ttf",
                sha256 = "8b4f149beaead3eac78ffb84adf31513ebf06fe6dbc6e181ccd9debad3c70f1a"
            ),
            downloadBytes = 9_649_779L
        ),
        FontEntry(
            id = "jetbrains_mono",
            name = "JetBrains Mono",
            subtitle = "JetBrains Mono 2.304 · OFL",
            description = "连字 · 英文",
            tags = listOf("连字"),
            source = FontSource.Direct(
                url = "https://raw.githubusercontent.com/JetBrains/JetBrainsMono/v2.304/fonts/ttf/JetBrainsMono-Regular.ttf",
                sha256 = "a0bf60ef0f83c5ed4d7a75d45838548b1f6873372dfac88f71804491898d138f"
            ),
            downloadBytes = 273_900L
        ),
        FontEntry(
            id = "fira_code",
            name = "Fira Code",
            subtitle = "Fira Code 6.2 · OFL",
            description = "连字 · 英文",
            tags = listOf("连字"),
            source = FontSource.ZipEntry(
                zipUrl = "https://github.com/tonsky/FiraCode/releases/download/6.2/Fira_Code_v6.2.zip",
                entryName = "ttf/FiraCode-Regular.ttf",
                sha256 = "5992ab9640e2df491b2f609467b1de60e8bc39b2c28db184342a0592d98f6117"
            ),
            downloadBytes = 141_642L
        ),
        FontEntry(
            id = "hack",
            name = "Hack",
            subtitle = "Hack 3.003 · MIT",
            description = "编程等宽 · 英文，无连字",
            tags = emptyList(),
            source = FontSource.Direct(
                url = "https://raw.githubusercontent.com/source-foundry/Hack/v3.003/build/ttf/Hack-Regular.ttf",
                sha256 = "15f55cc0c85a2988d2b4b3a8cdb5d77fdfbaf319e1bb5309d725db9818fb7125"
            ),
            downloadBytes = 309_408L
        ),
        FontEntry(
            id = "system_mono",
            name = "系统等宽",
            subtitle = "Android 系统字体",
            description = "Droid Sans Mono",
            tags = listOf("内置"),
            source = FontSource.SystemMono
        ),
        FontEntry(
            id = "system_sans",
            name = "系统黑体",
            subtitle = "Android 系统字体",
            description = "Noto Sans CJK · 未下载中文字体时的默认回退",
            tags = listOf("内置", "含中文"),
            source = FontSource.SystemSans
        )
    )

    fun byId(id: String?): FontEntry? = all.firstOrNull { it.id == id }
}

/**
 * The two typefaces a terminal draws with, plus the system font chain as a last resort.
 *
 * Fonts loaded from a file or asset do not fall back to system fonts on their own, so a
 * glyph neither chosen font has (e.g. U+23F5 "⏵" in Claude Code's status line, emoji) would
 * render as a missing-glyph box. Lookup order:
 *  - single-width cells: [primary] → [fallback] → system
 *  - double-width (CJK) cells: [fallback] → [primary] → system
 * Results are cached per cell text; drawing happens on the main thread only.
 */
class TerminalFontPair(val primary: Typeface, val fallback: Typeface) {
    private val primaryProbe = Paint().apply { typeface = primary }
    private val fallbackProbe = Paint().apply { typeface = fallback }
    private val narrowCache = HashMap<String, Typeface>()
    private val wideCache = HashMap<String, Typeface>()

    fun typefaceFor(text: String): Typeface =
        lookup(narrowCache, text, primary, primaryProbe, fallback, fallbackProbe)

    fun wideTypefaceFor(text: String): Typeface =
        lookup(wideCache, text, fallback, fallbackProbe, primary, primaryProbe)

    private fun lookup(
        cache: HashMap<String, Typeface>,
        text: String,
        first: Typeface,
        firstProbe: Paint,
        second: Typeface,
        secondProbe: Paint
    ): Typeface {
        cache[text]?.let { return it }
        if (cache.size > 4096) cache.clear()
        val tf = when {
            firstProbe.hasGlyph(text) -> first
            second !== first && secondProbe.hasGlyph(text) -> second
            else -> Typeface.DEFAULT
        }
        cache[text] = tf
        return tf
    }
}

class TerminalFontStore(context: Context) {
    private val appContext = context.applicationContext
    private val dir = File(appContext.filesDir, "fonts")
    private val cache = ConcurrentHashMap<String, Typeface>()
    private val http: OkHttpClient by lazy {
        OkHttpClient.Builder()
            .connectTimeout(20, TimeUnit.SECONDS)
            .readTimeout(60, TimeUnit.SECONDS)
            .followRedirects(true)
            .followSslRedirects(true)
            .build()
    }

    private fun fileFor(entry: FontEntry) = File(dir, entry.id + ".ttf")

    fun isInstalled(entry: FontEntry): Boolean = !entry.downloadable || fileFor(entry).isFile

    /** Null when the font is not installed or cannot be loaded. Safe to call from any thread. */
    fun typeface(id: String): Typeface? {
        cache[id]?.let { return it }
        val entry = FontCatalog.byId(id) ?: return null
        val tf: Typeface = when (val s = entry.source) {
            FontSource.SystemMono -> Typeface.MONOSPACE
            FontSource.SystemSans -> Typeface.DEFAULT
            is FontSource.Bundled -> runCatching { Typeface.createFromAsset(appContext.assets, s.asset) }.getOrNull()
            is FontSource.Direct, is FontSource.ZipEntry -> fileFor(entry).takeIf { it.isFile }
                ?.let { f -> runCatching { Typeface.createFromFile(f) }.getOrNull() }
        } ?: return null
        cache[id] = tf
        return tf
    }

    fun delete(entry: FontEntry) {
        if (!entry.downloadable) return
        cache.remove(entry.id)
        fileFor(entry).delete()
    }

    /** Blocking network + disk work: call from Dispatchers.IO. [onProgress] receives 0f..1f. */
    fun install(entry: FontEntry, onProgress: (Float) -> Unit) {
        dir.mkdirs()
        val tmp = File(dir, entry.id + ".part")
        try {
            val digest = MessageDigest.getInstance("SHA-256")
            val expected = when (val s = entry.source) {
                is FontSource.Direct -> {
                    downloadDirect(s.url, tmp, digest, onProgress)
                    s.sha256
                }
                is FontSource.ZipEntry -> {
                    downloadZipEntry(s, tmp, digest, onProgress)
                    s.sha256
                }
                else -> return
            }
            val actual = digest.digest().joinToString("") { "%02x".format(it) }
            if (!actual.equals(expected, ignoreCase = true)) {
                throw IOException("字体校验失败（sha256 不匹配）")
            }
            val target = fileFor(entry)
            target.delete()
            if (!tmp.renameTo(target)) throw IOException("无法保存字体文件")
            cache.remove(entry.id)
            if (typeface(entry.id) == null) {
                target.delete()
                throw IOException("字体文件无法加载")
            }
            onProgress(1f)
        } finally {
            tmp.delete()
        }
    }

    private fun downloadDirect(url: String, out: File, digest: MessageDigest, onProgress: (Float) -> Unit) {
        http.newCall(Request.Builder().url(url).build()).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("下载失败 HTTP ${resp.code}")
            val body = resp.body ?: throw IOException("下载失败：空响应")
            val total = body.contentLength()
            val input = ProgressInputStream(body.byteStream(), limit = -1L) { read ->
                if (total > 0) onProgress(read.toFloat() / total)
            }
            copyTo(input, out, digest)
        }
    }

    private fun downloadZipEntry(s: FontSource.ZipEntry, out: File, digest: MessageDigest, onProgress: (Float) -> Unit) {
        val head = Request.Builder().url(s.zipUrl).head().header("Accept-Encoding", "identity").build()
        val zipLen = http.newCall(head).execute().use { resp ->
            if (!resp.isSuccessful) throw IOException("下载失败 HTTP ${resp.code}")
            resp.header("Content-Length")?.toLongOrNull() ?: throw IOException("无法获取字体包大小")
        }

        // 1. End-of-central-directory record sits within the last 64 KB + 22 bytes of the zip.
        val tailStart = max(0L, zipLen - 65_557L)
        val tail = fetchRange(s.zipUrl, tailStart, zipLen - 1)
        var eocd = tail.size - 22
        while (eocd >= 0 && u32(tail, eocd) != 0x06054b50L) eocd--
        if (eocd < 0) throw IOException("字体包格式异常（缺少目录）")
        val cdSize = u32(tail, eocd + 12)
        val cdOffset = u32(tail, eocd + 16)
        val cd = if (cdOffset >= tailStart && cdOffset + cdSize <= zipLen) {
            tail.copyOfRange((cdOffset - tailStart).toInt(), (cdOffset - tailStart + cdSize).toInt())
        } else {
            fetchRange(s.zipUrl, cdOffset, cdOffset + cdSize - 1)
        }

        // 2. Find the wanted entry in the central directory.
        var p = 0
        var method = -1
        var compressedSize = 0L
        var size = 0L
        var localOffset = 0L
        while (p + 46 <= cd.size && u32(cd, p) == 0x02014b50L) {
            val nameLen = u16(cd, p + 28)
            val extraLen = u16(cd, p + 30)
            val commentLen = u16(cd, p + 32)
            if (String(cd, p + 46, nameLen, Charsets.UTF_8) == s.entryName) {
                method = u16(cd, p + 10)
                compressedSize = u32(cd, p + 20)
                size = u32(cd, p + 24)
                localOffset = u32(cd, p + 42)
                break
            }
            p += 46 + nameLen + extraLen + commentLen
        }
        if (method < 0) throw IOException("字体包中未找到 ${s.entryName}")
        if (method != 0 && method != 8) throw IOException("不支持的压缩方式 $method")

        // 3. Fetch local header + data. A little slack past the data lets the inflater see the stream end.
        val end = min(zipLen - 1, localOffset + 30 + 0xFFFF * 2 + compressedSize)
        http.newCall(rangeRequest(s.zipUrl, localOffset, end)).execute().use { resp ->
            if (resp.code != 206) throw IOException("服务器不支持分段下载 HTTP ${resp.code}")
            val raw = resp.body?.byteStream() ?: throw IOException("下载失败：空响应")
            val header = readExactly(raw, 30)
            if (u32(header, 0) != 0x04034b50L) throw IOException("字体包格式异常（文件头）")
            skipExactly(raw, (u16(header, 26) + u16(header, 28)).toLong())
            val counted = ProgressInputStream(raw, limit = if (method == 0) compressedSize else -1L) { read ->
                onProgress(min(1f, read.toFloat() / compressedSize))
            }
            val data: InputStream = if (method == 8) InflaterInputStream(counted, Inflater(true), 64 * 1024) else counted
            val written = copyTo(data, out, digest)
            if (written != size) throw IOException("字体解压大小不符")
        }
    }

    private fun rangeRequest(url: String, start: Long, endInclusive: Long): Request =
        Request.Builder().url(url).header("Range", "bytes=$start-$endInclusive").build()

    private fun fetchRange(url: String, start: Long, endInclusive: Long): ByteArray =
        http.newCall(rangeRequest(url, start, endInclusive)).execute().use { resp ->
            if (resp.code != 206) throw IOException("服务器不支持分段下载 HTTP ${resp.code}")
            val bytes = resp.body?.bytes() ?: throw IOException("下载失败：空响应")
            if (bytes.size.toLong() != endInclusive - start + 1) throw IOException("分段下载长度不符")
            bytes
        }

    private fun copyTo(input: InputStream, out: File, digest: MessageDigest): Long {
        var total = 0L
        out.outputStream().use { os ->
            val buf = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buf)
                if (n < 0) break
                digest.update(buf, 0, n)
                os.write(buf, 0, n)
                total += n
            }
        }
        return total
    }

    private fun readExactly(input: InputStream, n: Int): ByteArray {
        val buf = ByteArray(n)
        var off = 0
        while (off < n) {
            val r = input.read(buf, off, n - off)
            if (r < 0) throw IOException("下载中断")
            off += r
        }
        return buf
    }

    private fun skipExactly(input: InputStream, n: Long) {
        var left = n
        val buf = ByteArray(4096)
        while (left > 0) {
            val r = input.read(buf, 0, min(left, buf.size.toLong()).toInt())
            if (r < 0) throw IOException("下载中断")
            left -= r
        }
    }

    private fun u16(b: ByteArray, i: Int): Int =
        (b[i].toInt() and 0xFF) or ((b[i + 1].toInt() and 0xFF) shl 8)

    private fun u32(b: ByteArray, i: Int): Long =
        (u16(b, i).toLong()) or (u16(b, i + 2).toLong() shl 16)
}

/** Counts bytes read (reporting progress) and optionally stops after [limit] bytes. */
private class ProgressInputStream(
    input: InputStream,
    private val limit: Long,
    private val onRead: (Long) -> Unit
) : FilterInputStream(input) {
    private var count = 0L
    private var lastReported = 0L

    override fun read(): Int {
        val one = ByteArray(1)
        return if (read(one, 0, 1) < 0) -1 else one[0].toInt() and 0xFF
    }

    override fun read(b: ByteArray, off: Int, len: Int): Int {
        val want = if (limit >= 0) min(len.toLong(), limit - count).toInt() else len
        if (want <= 0) return -1
        val n = super.read(b, off, want)
        if (n > 0) {
            count += n
            if (count - lastReported >= 64 * 1024) {
                lastReported = count
                onRead(count)
            }
        }
        return n
    }
}
