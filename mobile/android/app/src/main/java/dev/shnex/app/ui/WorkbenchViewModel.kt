package dev.shnex.app.ui

import android.app.Application
import android.content.Context
import android.net.Uri
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateListOf
import androidx.compose.runtime.mutableStateMapOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.setValue
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import dev.shnex.app.data.ApiException
import dev.shnex.app.data.CompiledMarkdownDoc
import dev.shnex.app.data.CompletionItem
import dev.shnex.app.data.CoreApiClient
import dev.shnex.app.data.HostItem
import dev.shnex.app.data.RemoteFileEntry
import dev.shnex.app.data.SessionSocketHandle
import dev.shnex.app.data.TailnetPeer
import dev.shnex.app.data.TailnetStatusInfo
import dev.shnex.app.security.KeystoreSecretVault
import dev.shnex.app.service.ShnexRelayService
import dev.shnex.app.terminal.AnsiTerminalBuffer
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.util.UUID
import java.util.concurrent.atomic.AtomicLong

class LiveTerminalTab(
    val tabId: String,
    val isMarkdown: Boolean = false,
    val sessionId: String = "",
    val hostId: String? = null,
    val title: String,
    val subtitle: String,
    val buffer: AnsiTerminalBuffer = AnsiTerminalBuffer(80, 28),
    val markdownDoc: CompiledMarkdownDoc? = null
) {
    var socket: SessionSocketHandle? = null
    var renderTick by mutableLongStateOf(0L)
    var rttMs by mutableStateOf<Int?>(null)
    var approvalSnippet by mutableStateOf<String?>(null)
    var connected by mutableStateOf(true)
    private val tickSeq = AtomicLong(0L)

    fun bumpRender() {
        renderTick = tickSeq.incrementAndGet()
    }
}

enum class BottomNavTab { HOSTS, SESSIONS, SETTINGS }

class WorkbenchViewModel(app: Application) : AndroidViewModel(app) {

    private val prefs = app.getSharedPreferences("shnex_mobile_ui_prefs", Context.MODE_PRIVATE)
    private val vault = KeystoreSecretVault(app)

    var apiClient: CoreApiClient? by mutableStateOf(null)
        private set
    var bootError: String? by mutableStateOf(null)
        private set

    var currentNav by mutableStateOf(BottomNavTab.HOSTS)
    var privacyMaskIp by mutableStateOf(prefs.getBoolean("privacy_mask_ip", true))
        private set

    val hosts = mutableStateListOf<HostItem>()
    val latencies = mutableStateMapOf<String, Long>()
    val tabs = mutableStateListOf<LiveTerminalTab>()
    var activeTabId by mutableStateOf<String?>(null)

    var connectingHostId by mutableStateOf<String?>(null)
        private set
    var toastMessage by mutableStateOf<String?>(null)

    // Password fallback prompt when auth_mode == "auto" needs interactive password
    var passwordPromptHost by mutableStateOf<HostItem?>(null)

    // SFTP Sheet State
    var showSftpSheet by mutableStateOf(false)
    var sftpCurPath by mutableStateOf("/")
    val sftpEntries = mutableStateListOf<RemoteFileEntry>()
    var sftpLoading by mutableStateOf(false)

    // Mobile Input Bar Autocomplete State
    val completionItems = mutableStateListOf<CompletionItem>()
    private var completionJob: Job? = null

    // Tailscale / Headscale State
    var tailnetStatus by mutableStateOf(
        TailnetStatusInfo(false, "Stopped", "", "", "")
    )
    val tailnetPeers = mutableStateListOf<TailnetPeer>()
    var tailnetBusy by mutableStateOf(false)

    var secretCount by mutableStateOf(0)
        private set

    val activeTab: LiveTerminalTab?
        get() = tabs.firstOrNull { it.tabId == activeTabId } ?: tabs.firstOrNull()

    val hasAnyPendingApproval: Boolean
        get() = tabs.any { it.approvalSnippet != null }

    init {
        refreshSecretCount()
        viewModelScope.launch {
            ShnexRelayService.bootErrorFlow.collect { err ->
                bootError = err
            }
        }
        viewModelScope.launch {
            ShnexRelayService.endpointFlow.collect { ep ->
                if (ep != null) {
                    val client = CoreApiClient(ep)
                    apiClient = client
                    reloadHosts()
                    refreshTailnet()
                }
            }
        }
    }

    fun refreshSecretCount() {
        secretCount = vault.countStoredSecrets()
    }

    fun showToast(msg: String) {
        toastMessage = msg
        viewModelScope.launch {
            delay(3200)
            if (toastMessage == msg) {
                toastMessage = null
            }
        }
    }

    fun togglePrivacyMask(enabled: Boolean) {
        privacyMaskIp = enabled
        prefs.edit().putBoolean("privacy_mask_ip", enabled).apply()
    }

    fun maskAddress(userAtHost: String): String {
        if (!privacyMaskIp || userAtHost.isBlank()) return userAtHost
        val at = userAtHost.indexOf('@')
        val prefix = if (at >= 0) userAtHost.substring(0, at + 1) else ""
        val host = if (at >= 0) userAtHost.substring(at + 1) else userAtHost
        val parts = host.split('.')
        if (parts.size == 4 && parts.all { it.all(Char::isDigit) }) {
            return "$prefix${parts[0]}.${parts[1]}.xx.xx"
        }
        if (host.length > 8) {
            return "$prefix${host.take(4)}...${host.takeLast(3)}"
        }
        return userAtHost
    }

    fun reloadHosts() {
        val client = apiClient ?: return
        viewModelScope.launch {
            runCatching { client.listHosts() }
                .onSuccess { list ->
                    hosts.clear()
                    hosts.addAll(list)
                    refreshSecretCount()
                }
        }
    }

    fun pingHosts() {
        val client = apiClient ?: return
        viewModelScope.launch {
            runCatching { client.pingAllHosts() }
                .onSuccess { map ->
                    latencies.clear()
                    latencies.putAll(map)
                    showToast("已完成主机延迟测速")
                }
        }
    }

    fun saveHost(
        id: String?,
        name: String,
        host: String,
        port: Int,
        user: String,
        password: String,
        privateKeyPem: String,
        tmuxSession: String,
        useTsnet: Boolean,
        postLoginScript: String,
        onResult: (Result<HostItem>) -> Unit = {}
    ) {
        viewModelScope.launch {
            var client = apiClient
            if (client == null) {
                ShnexRelayService.ensureStarted(getApplication())
                repeat(20) {
                    delay(120)
                    client = apiClient
                    if (client != null) return@repeat
                }
            }
            val readyClient = client
            if (readyClient == null) {
                val err = IllegalStateException("本地安全核心尚未就绪，请稍候再试")
                showToast(err.message ?: "核心未就绪")
                onResult(Result.failure(err))
                return@launch
            }
            val result = runCatching {
                readyClient.upsertHost(
                    id = id,
                    name = name,
                    host = host,
                    port = port,
                    user = user,
                    password = password,
                    privateKeyPem = privateKeyPem,
                    tmuxSession = tmuxSession,
                    useTsnet = useTsnet,
                    postLoginScript = postLoginScript
                )
            }
            result.onSuccess {
                reloadHosts()
                showToast("主机「$name」已加密保存")
            }.onFailure { e ->
                showToast("保存失败: ${e.message}")
            }
            onResult(result)
        }
    }

    fun deleteHost(hostId: String) {
        val client = apiClient ?: return
        viewModelScope.launch {
            runCatching { client.deleteHost(hostId) }
                .onSuccess { reloadHosts() }
        }
    }

    fun connectHost(host: HostItem, passwordOverride: String? = null, openSftpAfter: Boolean = false) {
        val existing = tabs.firstOrNull { !it.isMarkdown && it.hostId == host.id && it.connected }
        if (existing != null && passwordOverride == null) {
            activeTabId = existing.tabId
            currentNav = BottomNavTab.SESSIONS
            if (openSftpAfter) openSftpForActiveSession()
            return
        }

        val client = apiClient ?: return
        connectingHostId = host.id
        viewModelScope.launch {
            try {
                val sess = client.createSession(
                    hostId = host.id,
                    password = passwordOverride,
                    cols = 80,
                    rows = 28
                )
                passwordPromptHost = null
                attachNewSessionTab(
                    client = client,
                    sessionId = sess.id,
                    hostId = host.id,
                    title = host.name,
                    subtitle = "${host.user}@${host.host}"
                )
                if (openSftpAfter) {
                    openSftpForActiveSession()
                }
            } catch (e: ApiException) {
                if (e.code == "AUTH_PASSWORD_NEEDED") {
                    passwordPromptHost = host
                } else {
                    showToast("连接失败: ${e.message}")
                }
            } catch (t: Throwable) {
                showToast("连接异常: ${t.message}")
            } finally {
                connectingHostId = null
            }
        }
    }

    fun quickConnect(expr: String, password: String) {
        val client = apiClient ?: return
        var clean = expr.trim().removePrefix("ssh ").trim()
        var user = "root"
        if (clean.contains("@")) {
            user = clean.substringBefore("@").ifBlank { "root" }
            clean = clean.substringAfter("@")
        }
        val target = if (clean.contains(":")) clean else "$clean:22"
        viewModelScope.launch {
            runCatching {
                client.createSession(
                    target = target,
                    user = user,
                    password = password.ifBlank { null },
                    cols = 80,
                    rows = 28
                )
            }.onSuccess { sess ->
                attachNewSessionTab(
                    client = client,
                    sessionId = sess.id,
                    hostId = null,
                    title = clean.substringBefore(":"),
                    subtitle = "$user@$clean"
                )
            }.onFailure { e ->
                showToast("快速连接失败: ${e.message}")
            }
        }
    }

    private fun attachNewSessionTab(
        client: CoreApiClient,
        sessionId: String,
        hostId: String?,
        title: String,
        subtitle: String
    ) {
        val tab = LiveTerminalTab(
            tabId = "tab-${UUID.randomUUID()}",
            isMarkdown = false,
            sessionId = sessionId,
            hostId = hostId,
            title = title,
            subtitle = subtitle
        )
        val sock = client.openSessionWebSocket(
            sessionId = sessionId,
            onPtyOutput = { chunk ->
                tab.buffer.feedBytes(chunk)
                tab.bumpRender()
            },
            onRttMs = { ms -> tab.rttMs = ms },
            onApprovalRequired = { snippet -> tab.approvalSnippet = snippet },
            onIdleSilence = { /* Notification handled natively by ShnexRelayService */ },
            onDisconnected = { tab.connected = false }
        )
        tab.socket = sock
        tabs.add(tab)
        activeTabId = tab.tabId
        currentNav = BottomNavTab.SESSIONS
    }

    fun jumpToSessionById(sessionId: String) {
        val hit = tabs.firstOrNull { it.sessionId == sessionId } ?: return
        activeTabId = hit.tabId
        currentNav = BottomNavTab.SESSIONS
    }

    fun closeTab(tab: LiveTerminalTab) {
        tab.socket?.close()
        if (!tab.isMarkdown && tab.sessionId.isNotBlank()) {
            val client = apiClient
            viewModelScope.launch { client?.deleteSession(tab.sessionId) }
        }
        tabs.remove(tab)
        if (activeTabId == tab.tabId) {
            activeTabId = tabs.lastOrNull()?.tabId
        }
    }

    fun sendTerminalText(text: String, useBracketedIfEnabled: Boolean = false) {
        val tab = activeTab ?: return
        if (tab.isMarkdown) return
        val payload = if (useBracketedIfEnabled && tab.buffer.bracketedPasteMode) {
            "\u001b[200~$text\u001b[201~"
        } else {
            text
        }
        tab.socket?.sendText(payload)
        if (text.contains('\r') || text.contains('\n')) {
            tab.approvalSnippet = null
        }
    }

    fun queryInputCompletions(inputDraft: String) {
        val client = apiClient ?: return
        val tab = activeTab ?: return
        if (tab.isMarkdown || tab.sessionId.isBlank()) return
        completionJob?.cancel()
        if (inputDraft.isBlank()) {
            completionItems.clear()
            return
        }
        completionJob = viewModelScope.launch {
            delay(60)
            val list = client.suggestCompletion(tab.sessionId, inputDraft)
            completionItems.clear()
            completionItems.addAll(list)
        }
    }

    fun uploadImageUriToActiveClaudeSession(context: Context, uri: Uri) {
        val client = apiClient ?: return
        val tab = activeTab ?: return
        if (tab.isMarkdown || tab.sessionId.isBlank()) return

        viewModelScope.launch {
            showToast("正在上传截图到远端会话...")
            runCatching {
                val bytes = withContext(Dispatchers.IO) {
                    context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
                        ?: throw IllegalStateException("无法读取所选图片")
                }
                client.uploadImageToSession(tab.sessionId, bytes, "mobile_shot.png")
            }.onSuccess { (remotePath, shellArg) ->
                if (tab.buffer.bracketedPasteMode) {
                    // Triggers Claude Code CLI native [Image #1] vision attachment!
                    tab.socket?.sendText("\u001b[200~$remotePath\u001b[201~")
                } else {
                    tab.socket?.sendText("$shellArg ")
                }
                showToast("已传入图片: $remotePath")
            }.onFailure { e ->
                showToast("上传图片失败: ${e.message}")
            }
        }
    }

    fun openSftpForActiveSession(path: String = "") {
        val client = apiClient ?: return
        val tab = activeTab ?: return
        if (tab.isMarkdown || tab.sessionId.isBlank()) return
        showSftpSheet = true
        sftpLoading = true
        viewModelScope.launch {
            runCatching { client.sftpList(tab.sessionId, path) }
                .onSuccess { (resolved, list) ->
                    sftpCurPath = resolved
                    sftpEntries.clear()
                    sftpEntries.addAll(list)
                }
                .onFailure { e ->
                    showToast("SFTP 读取失败: ${e.message}")
                }
            sftpLoading = false
        }
    }

    fun uploadUriToSftp(context: Context, uri: Uri, fileName: String) {
        val client = apiClient ?: return
        val tab = activeTab ?: return
        if (tab.isMarkdown || tab.sessionId.isBlank()) return
        viewModelScope.launch {
            sftpLoading = true
            runCatching {
                val bytes = withContext(Dispatchers.IO) {
                    context.contentResolver.openInputStream(uri)?.use { it.readBytes() }
                        ?: ByteArray(0)
                }
                val dest = if (sftpCurPath.endsWith("/")) "$sftpCurPath$fileName" else "$sftpCurPath/$fileName"
                client.sftpUploadBytes(tab.sessionId, dest, bytes)
            }.onSuccess {
                showToast("已上传 $fileName")
                openSftpForActiveSession(sftpCurPath)
            }.onFailure { e ->
                sftpLoading = false
                showToast("SFTP 上传失败: ${e.message}")
            }
        }
    }

    fun openMarkdownSampleTab() {
        val client = apiClient ?: return
        val existing = tabs.firstOrNull { it.isMarkdown && it.title == "math-rendering.md" }
        if (existing != null) {
            activeTabId = existing.tabId
            currentNav = BottomNavTab.SESSIONS
            return
        }
        viewModelScope.launch {
            runCatching { client.getMarkdownSample() }
                .onSuccess { doc ->
                    val mdTab = LiveTerminalTab(
                        tabId = "md-${UUID.randomUUID()}",
                        isMarkdown = true,
                        title = doc.title,
                        subtitle = "原生矢量公式",
                        markdownDoc = doc
                    )
                    tabs.add(mdTab)
                    activeTabId = mdTab.tabId
                    currentNav = BottomNavTab.SESSIONS
                }
                .onFailure { e ->
                    showToast("加载公式示例失败: ${e.message}")
                }
        }
    }

    fun openRemoteSftpMarkdown(entry: RemoteFileEntry) {
        val client = apiClient ?: return
        val tab = activeTab ?: return
        if (tab.sessionId.isBlank()) return
        viewModelScope.launch {
            runCatching {
                val raw = client.sftpReadBytes(tab.sessionId, entry.path)
                val content = String(raw, Charsets.UTF_8)
                client.renderMarkdown(entry.name, content)
            }.onSuccess { doc ->
                showSftpSheet = false
                val mdTab = LiveTerminalTab(
                    tabId = "md-${UUID.randomUUID()}",
                    isMarkdown = true,
                    title = entry.name,
                    subtitle = entry.path,
                    markdownDoc = doc
                )
                tabs.add(mdTab)
                activeTabId = mdTab.tabId
                currentNav = BottomNavTab.SESSIONS
            }.onFailure { e ->
                showToast("打开远端 Markdown 失败: ${e.message}")
            }
        }
    }

    fun refreshTailnet() {
        val client = apiClient ?: return
        viewModelScope.launch {
            runCatching { client.getTailnetStatus() }
                .onSuccess { st ->
                    tailnetStatus = st
                    if (st.running) {
                        runCatching { client.listTailnetPeers() }
                            .onSuccess { peers ->
                                tailnetPeers.clear()
                                tailnetPeers.addAll(peers)
                            }
                    }
                }
        }
    }

    fun toggleTailnet(start: Boolean, controlUrl: String, authKey: String) {
        val client = apiClient ?: return
        tailnetBusy = true
        viewModelScope.launch {
            runCatching {
                if (start) client.startTailnet(controlUrl, authKey, "shnex-android")
                else client.stopTailnet()
            }.onSuccess { st ->
                tailnetStatus = st
                refreshTailnet()
                showToast(if (st.running) "Tailnet 用户态组网已启动" else "Tailnet 已停止")
            }.onFailure { e ->
                showToast("Tailnet 操作失败: ${e.message}")
            }
            tailnetBusy = false
        }
    }

    fun triggerTestNotification() {
        val client = apiClient ?: return
        viewModelScope.launch {
            runCatching { client.sendTestNotification() }
                .onSuccess { showToast("已发送系统测试通知") }
        }
    }
}
