package dev.shnex.app.data

import dev.shnex.app.service.CoreEndpoint
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import okio.ByteString
import okio.ByteString.Companion.toByteString
import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.nio.ByteBuffer
import java.util.concurrent.TimeUnit

data class HostItem(
    val id: String,
    val name: String,
    val group: String,
    val host: String,
    val port: Int,
    val user: String,
    val authMode: String,
    val tmuxSession: String,
    val useTsnet: Boolean,
    val postLoginScript: String,
    val hasSecret: Boolean
)

data class SessionInfo(
    val id: String,
    val protocol: String,
    val target: String,
    val user: String,
    val relayMode: String = "",
    val workspaceName: String = ""
)

/** A conversation on the remote host (a tmux session), shared with the desktop app. */
data class RemoteTmuxSession(
    val id: String,
    val title: String,
    val command: String,
    val busy: Boolean,
    val path: String,
    val attachedClients: Int,
    val createdUnix: Long
)

data class RemoteTmuxList(
    val available: Boolean,
    val sessions: List<RemoteTmuxSession>
)

data class RemoteFileEntry(
    val name: String,
    val path: String,
    val size: Long,
    val mode: String,
    val isDir: Boolean
)

data class CompletionItem(
    val label: String,
    val insertText: String,
    val replaceLen: Int,
    val source: String
)

data class TailnetStatusInfo(
    val running: Boolean,
    val backendState: String,
    val selfDnsName: String,
    val tailscaleIp: String,
    val authUrl: String
)

data class TailnetPeer(
    val id: String,
    val hostname: String,
    val dnsName: String,
    val ip: String,
    val os: String,
    val online: Boolean
)

data class MarkdownMathBox(
    val tex: String,
    val display: Boolean,
    val svg: String,
    val width: Float,
    val height: Float
)

data class MarkdownBlockNode(
    val kind: String,
    val level: Int = 0,
    val plainText: String = "",
    val code: String = "",
    val lang: String = "",
    val alertType: String = "",
    val math: MarkdownMathBox? = null,
    val listItems: List<String> = emptyList(),
    val inlineMaths: List<MarkdownMathBox> = emptyList()
)

data class CompiledMarkdownDoc(
    val title: String,
    val rawMarkdown: String,
    val blocks: List<MarkdownBlockNode>
)

class ApiException(message: String, val code: String? = null) : IOException(message)

class CoreApiClient(private val endpoint: CoreEndpoint) {

    private val http = OkHttpClient.Builder()
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(35, TimeUnit.SECONDS)
        .writeTimeout(35, TimeUnit.SECONDS)
        .pingInterval(20, TimeUnit.SECONDS)
        .build()

    private val jsonMedia = "application/json; charset=utf-8".toMediaType()
    private val binMedia = "application/octet-stream".toMediaType()

    private suspend fun requestJson(
        path: String,
        method: String = "GET",
        bodyObj: JSONObject? = null
    ): JSONObject = withContext(Dispatchers.IO) {
        val reqBody = bodyObj?.toString()?.toRequestBody(jsonMedia)
        val builder = Request.Builder()
            .url("${endpoint.apiBase}$path")
            .header("Authorization", "Bearer ${endpoint.token}")
        when (method) {
            "POST" -> builder.post(reqBody ?: "{}".toRequestBody(jsonMedia))
            "PUT" -> builder.put(reqBody ?: "{}".toRequestBody(jsonMedia))
            "PATCH" -> builder.patch(reqBody ?: "{}".toRequestBody(jsonMedia))
            "DELETE" -> if (reqBody != null) builder.delete(reqBody) else builder.delete()
            else -> builder.get()
        }
        http.newCall(builder.build()).execute().use { resp ->
            val raw = resp.body?.string().orEmpty()
            val parsed = runCatching { JSONObject(raw) }.getOrElse { JSONObject() }
            if (!resp.isSuccessful) {
                val err = parsed.optString("error").ifBlank { "HTTP ${resp.code}" }
                val code = parsed.optString("code").takeIf { it.isNotBlank() }
                throw ApiException(err, code)
            }
            parsed
        }
    }

    suspend fun listHosts(): List<HostItem> {
        val obj = requestJson("/api/v1/hosts")
        val arr = obj.optJSONArray("hosts") ?: JSONArray()
        return buildList {
            for (i in 0 until arr.length()) {
                val h = arr.getJSONObject(i)
                val route = h.optJSONObject("route")
                val relay = h.optJSONObject("relay")
                val tmux = h.optString("tmux_session").ifBlank {
                    if (relay?.optString("mode") == "tmux") relay.optString("session_name") else ""
                }
                add(
                    HostItem(
                        id = h.optString("id"),
                        name = h.optString("name"),
                        group = h.optString("group"),
                        host = h.optString("host"),
                        port = h.optInt("port", 22),
                        user = h.optString("user", "root"),
                        authMode = h.optString("auth_mode", "auto"),
                        tmuxSession = tmux,
                        useTsnet = route?.optBoolean("use_tsnet", false) ?: false,
                        postLoginScript = h.optString("post_login_script"),
                        hasSecret = h.optBoolean("has_secret", false)
                    )
                )
            }
        }
    }

    suspend fun upsertHost(
        id: String? = null,
        name: String,
        host: String,
        port: Int,
        user: String,
        password: String,
        privateKeyPem: String,
        tmuxSession: String,
        useTsnet: Boolean,
        postLoginScript: String
    ): HostItem {
        val authMode = when {
            privateKeyPem.isNotBlank() -> "private_key"
            password.isNotBlank() -> "password"
            else -> "auto"
        }
        val payload = JSONObject().apply {
            if (!id.isNullOrBlank()) put("id", id)
            put("name", name)
            put("host", host)
            put("port", port)
            put("user", user)
            put("auth_mode", authMode)
            if (password.isNotBlank()) put("password", password)
            if (privateKeyPem.isNotBlank()) put("private_key_pem", privateKeyPem)
            put("tmux_session", tmuxSession)
            put("post_login_script", postLoginScript)
            put("host_key", JSONObject().put("insecure_ignore", true))
            put("route", JSONObject().put("use_tsnet", useTsnet))
            if (tmuxSession.isNotBlank()) {
                put(
                    "relay",
                    JSONObject()
                        .put("mode", "tmux")
                        .put("session_name", tmuxSession)
                )
            } else {
                put("relay", JSONObject().put("mode", "none"))
            }
        }
        val h = requestJson("/api/v1/hosts", "POST", payload)
        return HostItem(
            id = h.optString("id"),
            name = h.optString("name"),
            group = h.optString("group"),
            host = h.optString("host"),
            port = h.optInt("port", 22),
            user = h.optString("user", "root"),
            authMode = h.optString("auth_mode"),
            tmuxSession = h.optString("tmux_session"),
            useTsnet = useTsnet,
            postLoginScript = h.optString("post_login_script"),
            hasSecret = h.optBoolean("has_secret", false)
        )
    }

    suspend fun deleteHost(id: String) {
        requestJson("/api/v1/hosts/$id", "DELETE")
    }

    suspend fun pingAllHosts(): Map<String, Long> {
        val res = requestJson("/api/v1/hosts/ping", "POST", JSONObject())
        val mapObj = res.optJSONObject("latencies_ms") ?: return emptyMap()
        val out = mutableMapOf<String, Long>()
        val keys = mapObj.keys()
        while (keys.hasNext()) {
            val k = keys.next()
            out[k] = mapObj.optLong(k, -1L)
        }
        return out
    }

    suspend fun createSession(
        hostId: String? = null,
        target: String? = null,
        user: String? = null,
        password: String? = null,
        cols: Int = 80,
        rows: Int = 28,
        tmuxSession: String? = null
    ): SessionInfo {
        val body = JSONObject().apply {
            if (!hostId.isNullOrBlank()) put("host_id", hostId)
            if (!tmuxSession.isNullOrBlank()) {
                // attach to an existing conversation only; 410 TMUX_SESSION_GONE if it was ended
                put(
                    "relay",
                    JSONObject()
                        .put("mode", "tmux")
                        .put("session_name", tmuxSession)
                        .put("must_exist", true)
                )
            }
            if (!target.isNullOrBlank()) put("target", target)
            if (!user.isNullOrBlank()) put("user", user)
            put("cols", cols)
            put("rows", rows)
            put("notify_on_bell", true)
            put("notify_on_approval", true)
            put("notify_on_idle", true)
            if (!password.isNullOrBlank()) {
                put(
                    "auth",
                    JSONObject()
                        .put("mode", "password")
                        .put("password", password)
                        .put("host_key", JSONObject().put("insecure_ignore", true))
                )
            }
        }
        val res = requestJson("/api/v1/sessions", "POST", body)
        return SessionInfo(
            id = res.optString("id"),
            protocol = res.optString("protocol", "ssh"),
            target = res.optString("target"),
            user = res.optString("user"),
            relayMode = res.optString("relay_mode"),
            workspaceName = res.optString("workspace_name")
        )
    }

    private fun parseTmuxSession(o: JSONObject): RemoteTmuxSession {
        val id = o.optString("id")
        return RemoteTmuxSession(
            id = id,
            title = o.optString("title").ifBlank { id },
            command = o.optString("command"),
            busy = o.optBoolean("busy", false),
            path = o.optString("path"),
            attachedClients = o.optInt("attached_clients", 0),
            createdUnix = o.optLong("created_unix", 0L)
        )
    }

    suspend fun listTmux(sessionId: String): RemoteTmuxList {
        val res = requestJson("/api/v1/sessions/$sessionId/tmux")
        val arr = res.optJSONArray("sessions")
        val sessions = buildList {
            if (arr != null) {
                for (i in 0 until arr.length()) add(parseTmuxSession(arr.getJSONObject(i)))
            }
        }
        return RemoteTmuxList(res.optBoolean("available", false), sessions)
    }

    suspend fun createTmux(sessionId: String, title: String? = null): RemoteTmuxSession {
        val body = JSONObject().apply { if (!title.isNullOrBlank()) put("title", title) }
        return parseTmuxSession(requestJson("/api/v1/sessions/$sessionId/tmux", "POST", body))
    }

    suspend fun renameTmux(sessionId: String, name: String, title: String): String {
        val res = requestJson("/api/v1/sessions/$sessionId/tmux/$name", "PATCH", JSONObject().put("title", title))
        return res.optString("title", title)
    }

    /** Recent history (with colors) of a conversation's active pane, oldest first. */
    suspend fun tmuxHistory(sessionId: String, name: String, lines: Int = 3000): String {
        return requestJson("/api/v1/sessions/$sessionId/tmux/$name/history?lines=$lines").optString("text")
    }

    suspend fun killTmux(sessionId: String, name: String) {
        requestJson("/api/v1/sessions/$sessionId/tmux/$name", "DELETE")
    }

    suspend fun deleteSession(sessionId: String) {
        runCatching { requestJson("/api/v1/sessions/$sessionId", "DELETE") }
    }

    suspend fun suggestCompletion(sessionId: String, line: String): List<CompletionItem> {
        if (line.isBlank()) return emptyList()
        val res = runCatching {
            requestJson(
                "/api/v1/sessions/$sessionId/complete",
                "POST",
                JSONObject().put("line", line)
            )
        }.getOrNull() ?: return emptyList()
        val arr = res.optJSONArray("candidates") ?: return emptyList()
        return buildList {
            for (i in 0 until minOf(arr.length(), 10)) {
                val c = arr.getJSONObject(i)
                add(
                    CompletionItem(
                        label = c.optString("label"),
                        insertText = c.optString("insert_text"),
                        replaceLen = c.optInt("replace_len", 0),
                        source = c.optString("source")
                    )
                )
            }
        }
    }

    suspend fun uploadImageToSession(
        sessionId: String,
        bytes: ByteArray,
        fileName: String
    ): Pair<String, String> = withContext(Dispatchers.IO) {
        val req = Request.Builder()
            .url("${endpoint.apiBase}/api/v1/sessions/$sessionId/upload-image?name=$fileName")
            .header("Authorization", "Bearer ${endpoint.token}")
            .post(bytes.toRequestBody(binMedia))
            .build()
        http.newCall(req).execute().use { resp ->
            val raw = resp.body?.string().orEmpty()
            val obj = runCatching { JSONObject(raw) }.getOrElse { JSONObject() }
            if (!resp.isSuccessful) {
                throw ApiException(obj.optString("error").ifBlank { "上传失败 (${resp.code})" })
            }
            Pair(obj.optString("path"), obj.optString("shell_arg"))
        }
    }

    suspend fun sftpList(sessionId: String, path: String = ""): Pair<String, List<RemoteFileEntry>> {
        val q = if (path.isNotBlank()) "?path=${java.net.URLEncoder.encode(path, "UTF-8")}" else ""
        val obj = requestJson("/api/v1/sessions/$sessionId/sftp/list$q")
        val resolved = obj.optString("path", "/")
        val arr = obj.optJSONArray("entries") ?: JSONArray()
        val entries = buildList {
            for (i in 0 until arr.length()) {
                val e = arr.getJSONObject(i)
                add(
                    RemoteFileEntry(
                        name = e.optString("name"),
                        path = e.optString("path"),
                        size = e.optLong("size"),
                        mode = e.optString("mode"),
                        isDir = e.optBoolean("is_dir")
                    )
                )
            }
        }
        return Pair(resolved, entries)
    }

    suspend fun sftpReadBytes(sessionId: String, remotePath: String): ByteArray {
        val enc = java.net.URLEncoder.encode(remotePath, "UTF-8")
        val obj = requestJson("/api/v1/sessions/$sessionId/sftp/read?path=$enc")
        val b64 = obj.optString("content_base64")
        return android.util.Base64.decode(b64, android.util.Base64.DEFAULT)
    }

    suspend fun sftpUploadBytes(
        sessionId: String,
        remotePath: String,
        bytes: ByteArray
    ) = withContext(Dispatchers.IO) {
        val enc = java.net.URLEncoder.encode(remotePath, "UTF-8")
        val req = Request.Builder()
            .url("${endpoint.apiBase}/api/v1/sessions/$sessionId/sftp/upload?path=$enc")
            .header("Authorization", "Bearer ${endpoint.token}")
            .post(bytes.toRequestBody(binMedia))
            .build()
        http.newCall(req).execute().use { resp ->
            if (!resp.isSuccessful) {
                val raw = resp.body?.string().orEmpty()
                val obj = runCatching { JSONObject(raw) }.getOrElse { JSONObject() }
                throw ApiException(obj.optString("error").ifBlank { "上传失败 (${resp.code})" })
            }
        }
    }

    suspend fun getMarkdownSample(): CompiledMarkdownDoc {
        val obj = requestJson("/api/v1/markdown/sample")
        val title = obj.optString("title", "math-rendering.md")
        val content = obj.optString("content")
        val docObj = obj.optJSONObject("document") ?: JSONObject()
        return parseMarkdownDocument(title, content, docObj)
    }

    suspend fun renderMarkdown(title: String, content: String): CompiledMarkdownDoc {
        val docObj = requestJson(
            "/api/v1/markdown/render",
            "POST",
            JSONObject().put("content", content)
        )
        return parseMarkdownDocument(title, content, docObj)
    }

    private fun parseMarkdownDocument(
        title: String,
        rawMarkdown: String,
        docObj: JSONObject
    ): CompiledMarkdownDoc {
        val arr = docObj.optJSONArray("blocks") ?: JSONArray()
        val blocks = buildList {
            for (i in 0 until arr.length()) {
                val b = arr.getJSONObject(i)
                val kind = b.optString("kind")
                val inlinesArr = b.optJSONArray("inlines")
                val inlineMaths = mutableListOf<MarkdownMathBox>()
                val text = flattenInlines(inlinesArr, inlineMaths)

                val mathObj = b.optJSONObject("math")
                val mathBox = mathObj?.let {
                    MarkdownMathBox(
                        tex = it.optString("tex"),
                        display = it.optBoolean("display", true),
                        svg = it.optString("svg"),
                        width = it.optDouble("width", 120.0).toFloat(),
                        height = it.optDouble("height", 40.0).toFloat()
                    )
                }

                val listItems = mutableListOf<String>()
                val itemsArr = b.optJSONArray("items")
                if (itemsArr != null) {
                    for (j in 0 until itemsArr.length()) {
                        val it = itemsArr.getJSONObject(j)
                        listItems.add(flattenInlines(it.optJSONArray("inlines"), inlineMaths))
                    }
                }

                add(
                    MarkdownBlockNode(
                        kind = kind,
                        level = b.optInt("level", 2),
                        plainText = text,
                        code = b.optString("code"),
                        lang = b.optString("lang"),
                        alertType = b.optString("alert_type"),
                        math = mathBox,
                        listItems = listItems,
                        inlineMaths = inlineMaths
                    )
                )
            }
        }
        return CompiledMarkdownDoc(title = title, rawMarkdown = rawMarkdown, blocks = blocks)
    }

    private fun flattenInlines(arr: JSONArray?, mathCollector: MutableList<MarkdownMathBox>): String {
        if (arr == null) return ""
        val sb = StringBuilder()
        for (i in 0 until arr.length()) {
            val inl = arr.getJSONObject(i)
            when (inl.optString("kind")) {
                "text", "code" -> sb.append(inl.optString("text"))
                "math_inline" -> {
                    val m = inl.optJSONObject("math")
                    if (m != null) {
                        val tex = m.optString("tex")
                        sb.append(" ").append(tex).append(" ")
                        mathCollector.add(
                            MarkdownMathBox(
                                tex = tex,
                                display = false,
                                svg = m.optString("svg"),
                                width = m.optDouble("width", 60.0).toFloat(),
                                height = m.optDouble("height", 24.0).toFloat()
                            )
                        )
                    }
                }
                else -> sb.append(flattenInlines(inl.optJSONArray("children"), mathCollector))
            }
        }
        return sb.toString()
    }

    suspend fun getTailnetStatus(): TailnetStatusInfo {
        val obj = requestJson("/api/v1/tailnet/status")
        val ips = obj.optJSONArray("tailscale_ips")
        val firstIp = if (ips != null && ips.length() > 0) ips.optString(0) else ""
        return TailnetStatusInfo(
            running = obj.optBoolean("running", false),
            backendState = obj.optString("backend_state", "Stopped"),
            selfDnsName = obj.optString("self_dns_name"),
            tailscaleIp = firstIp,
            authUrl = obj.optString("auth_url")
        )
    }

    suspend fun startTailnet(controlUrl: String, authKey: String, hostname: String): TailnetStatusInfo {
        val body = JSONObject().apply {
            if (controlUrl.isNotBlank()) put("control_url", controlUrl)
            if (authKey.isNotBlank()) put("auth_key", authKey)
            put("hostname", hostname.ifBlank { "shnex-android" })
        }
        requestJson("/api/v1/tailnet/up", "POST", body)
        return getTailnetStatus()
    }

    suspend fun stopTailnet(): TailnetStatusInfo {
        requestJson("/api/v1/tailnet/down", "POST", JSONObject())
        return getTailnetStatus()
    }

    suspend fun listTailnetPeers(): List<TailnetPeer> {
        val obj = requestJson("/api/v1/tailnet/nodes?online=false")
        val arr = obj.optJSONArray("nodes") ?: JSONArray()
        return buildList {
            for (i in 0 until arr.length()) {
                val n = arr.getJSONObject(i)
                val ips = n.optJSONArray("tailscale_ips")
                add(
                    TailnetPeer(
                        id = n.optString("id"),
                        hostname = n.optString("hostname"),
                        dnsName = n.optString("magic_dns_name"),
                        ip = if (ips != null && ips.length() > 0) ips.optString(0) else "",
                        os = n.optString("os"),
                        online = n.optBoolean("online")
                    )
                )
            }
        }
    }

    suspend fun sendTestNotification() {
        requestJson("/api/v1/notifications/test", "POST", JSONObject())
    }

    fun openSessionWebSocket(
        sessionId: String,
        onPtyOutput: (ByteArray) -> Unit,
        onRttMs: (Int) -> Unit,
        onApprovalRequired: (String) -> Unit,
        onIdleSilence: () -> Unit,
        onDisconnected: () -> Unit
    ): SessionSocketHandle {
        val url = "${endpoint.wsBase}/api/v1/sessions/$sessionId/ws?token=${endpoint.token}"
        val req = Request.Builder().url(url).build()
        val ws = http.newWebSocket(req, object : WebSocketListener() {
            override fun onMessage(webSocket: WebSocket, bytes: ByteString) {
                val arr = bytes.toByteArray()
                if (arr.isEmpty()) return
                when (arr[0].toInt() and 0xFF) {
                    0x00 -> {
                        if (arr.size > 1) {
                            onPtyOutput(arr.copyOfRange(1, arr.size))
                        }
                    }
                    0x03 -> {
                        if (arr.size >= 17) {
                            val micros = ByteBuffer.wrap(arr, 9, 8).long
                            onRttMs((micros / 1000L).toInt())
                        }
                    }
                    0x04 -> {
                        if (arr.size > 1) {
                            val json = String(arr, 1, arr.size - 1, Charsets.UTF_8)
                            runCatching {
                                val ev = JSONObject(json)
                                when (ev.optString("type")) {
                                    "keepalive_rtt" -> {
                                        val ns = ev.optLong("rtt_ns", 0L)
                                        if (ns > 0) onRttMs((ns / 1_000_000L).toInt())
                                    }
                                    "ai_approval_required" -> {
                                        onApprovalRequired(
                                            ev.optString("prompt_snippet")
                                                .ifBlank { "Claude CLI 等待确认" }
                                        )
                                    }
                                    "output_silence" -> onIdleSilence()
                                    "disconnected" -> onDisconnected()
                                }
                            }
                        }
                    }
                }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                onDisconnected()
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                onDisconnected()
            }
        })
        return SessionSocketHandle(ws)
    }
}

class SessionSocketHandle(private val ws: WebSocket) {
    fun sendInput(bytes: ByteArray) {
        val frame = ByteArray(1 + bytes.size)
        frame[0] = 0x00
        System.arraycopy(bytes, 0, frame, 1, bytes.size)
        ws.send(frame.toByteString())
    }

    fun sendText(text: String) {
        sendInput(text.toByteArray(Charsets.UTF_8))
    }

    fun sendResize(cols: Int, rows: Int) {
        val c = cols.coerceIn(20, 400)
        val r = rows.coerceIn(8, 200)
        val buf = ByteBuffer.allocate(5)
        buf.put(0x01.toByte())
        buf.putShort(c.toShort())
        buf.putShort(r.toShort())
        ws.send(buf.array().toByteString())
    }

    fun close() {
        runCatching { ws.close(1000, "closed") }
    }
}
