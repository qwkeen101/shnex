package dev.shnex.app.ui

import android.app.Application
import android.content.Context
import android.graphics.Typeface
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
import dev.shnex.app.data.RemoteTmuxSession
import dev.shnex.app.data.SessionInfo
import dev.shnex.app.data.SessionSocketHandle
import dev.shnex.app.data.TailnetPeer
import dev.shnex.app.data.TailnetStatusInfo
import dev.shnex.app.security.KeystoreSecretVault
import dev.shnex.app.service.ShnexRelayService
import dev.shnex.app.terminal.AnsiTerminalBuffer
import dev.shnex.app.terminal.TerminalAppearance
import dev.shnex.app.terminal.TerminalAppearanceStore
import dev.shnex.app.terminal.TerminalColorScheme
import dev.shnex.app.terminal.TerminalColorSchemes
import dev.shnex.app.terminal.FontCatalog
import dev.shnex.app.terminal.FontEntry
import dev.shnex.app.terminal.TerminalFontPair
import dev.shnex.app.terminal.TerminalFontStore
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
    title: String,
    val subtitle: String,
    val buffer: AnsiTerminalBuffer = AnsiTerminalBuffer(80, 28),
    val markdownDoc: CompiledMarkdownDoc? = null,
    /** Remote tmux session (conversation) this tab is attached to; only for tmux hosts. */
    val tmuxId: String? = null
) {
    var title by mutableStateOf(title)
    var socket: SessionSocketHandle? = null
    var renderTick by mutableLongStateOf(0L)
    var rttMs by mutableStateOf<Int?>(null)
    var approvalSnippet by mutableStateOf<String?>(null)
    var connected by mutableStateOf(true)
    /** tmux history shown while scrolled back (tmux keeps it remotely; the local screen has none). */
    var history by mutableStateOf<AnsiTerminalBuffer?>(null)
    var historyLoading = false
    private val tickSeq = AtomicLong(0L)

    fun bumpRender() {
        renderTick = tickSeq.incrementAndGet()
    }
}

enum class BottomNavTab { HOSTS, SESSIONS, SETTINGS }

/** One chip of the session tab strip. A tmux host's chips are its remote conversations. */
sealed class TopTab {
    data class Live(val tab: LiveTerminalTab, val remote: RemoteTmuxSession?) : TopTab()
    data class Ghost(val hostId: String, val remote: RemoteTmuxSession) : TopTab()
}

data class PendingEndConversation(
    val hostId: String,
    val tmuxId: String,
    val title: String,
    val command: String
)

private val TMUX_ID = Regex("^[A-Za-z0-9_-]{1,48}$")

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

    // Terminal appearance, shared by every open session and the settings preview
    private val appearanceStore = TerminalAppearanceStore(prefs)
    var appearance by mutableStateOf(appearanceStore.load())
        private set

    val terminalScheme: TerminalColorScheme
        get() = TerminalColorSchemes.byId(appearance.schemeId)

    // Terminal fonts: bundled / system fonts plus a small downloadable catalog
    private val fontStore = TerminalFontStore(app)
    var installedFontIds by mutableStateOf(scanInstalledFonts())
        private set
    /** Fonts currently downloading, id -> progress 0f..1f. */
    val fontDownloads = mutableStateMapOf<String, Float>()
    private var fontPair: TerminalFontPair? = null

    private fun scanInstalledFonts(): Set<String> =
        FontCatalog.all.filter { fontStore.isInstalled(it) }.map { it.id }.toSet()

    /** Typefaces for the current appearance; falls back to built-ins if a chosen font is missing. */
    fun terminalFonts(): TerminalFontPair {
        val primary = fontStore.typeface(appearance.primaryFontId)
            ?: fontStore.typeface(FontCatalog.DEFAULT_PRIMARY)
            ?: Typeface.MONOSPACE
        val fallback = fontStore.typeface(appearance.fallbackFontId) ?: Typeface.DEFAULT
        val cur = fontPair
        if (cur != null && cur.primary === primary && cur.fallback === fallback) return cur
        return TerminalFontPair(primary, fallback).also { fontPair = it }
    }

    fun selectPrimaryFont(id: String) = updateAppearance { it.copy(primaryFontId = id) }

    fun selectFallbackFont(id: String) = updateAppearance { it.copy(fallbackFontId = id) }

    fun downloadFont(entry: FontEntry) {
        if (!entry.downloadable || fontDownloads.containsKey(entry.id)) return
        fontDownloads[entry.id] = 0f
        viewModelScope.launch {
            val result = withContext(Dispatchers.IO) {
                runCatching {
                    fontStore.install(entry) { p -> fontDownloads[entry.id] = p }
                }
            }
            fontDownloads.remove(entry.id)
            installedFontIds = scanInstalledFonts()
            result
                .onSuccess { showToast("${entry.name} 已下载") }
                .onFailure { showToast("下载失败：${it.message ?: it.javaClass.simpleName}") }
        }
    }

    fun deleteFont(entry: FontEntry) {
        if (!entry.downloadable) return
        fontStore.delete(entry)
        installedFontIds = scanInstalledFonts()
        updateAppearance {
            it.copy(
                primaryFontId = if (it.primaryFontId == entry.id) FontCatalog.DEFAULT_PRIMARY else it.primaryFontId,
                fallbackFontId = if (it.fallbackFontId == entry.id) FontCatalog.DEFAULT_FALLBACK else it.fallbackFontId
            )
        }
    }

    fun updateAppearance(transform: (TerminalAppearance) -> TerminalAppearance) {
        val next = transform(appearance).normalized()
        if (next == appearance) return
        appearance = next
        appearanceStore.save(next)
    }

    val hosts = mutableStateListOf<HostItem>()
    val latencies = mutableStateMapOf<String, Long>()
    val tabs = mutableStateListOf<LiveTerminalTab>()
    var activeTabId by mutableStateOf<String?>(null)

    var connectingHostId by mutableStateOf<String?>(null)
        private set
    var toastMessage by mutableStateOf<String?>(null)

    // Password fallback prompt when auth_mode == "auto" needs interactive password
    var passwordPromptHost by mutableStateOf<HostItem?>(null)
    // What the password prompt retries instead of connectHost (e.g. attaching a conversation)
    private var passwordRetry: ((String) -> Unit)? = null
    // Passwords typed during this run, memory only, so further conversations of the host do not prompt again
    private val hostPasswords = mutableMapOf<String, String>()

    // Remote tmux conversations of tmux hosts, keyed by hostId
    val remoteTmux = mutableStateMapOf<String, List<RemoteTmuxSession>>()
    var pendingEnd by mutableStateOf<PendingEndConversation?>(null)
    private val attachingTmux = mutableSetOf<String>()

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
        viewModelScope.launch(Dispatchers.IO) {
            // Warm the font cache off the main thread before the first terminal frame.
            fontStore.typeface(appearance.primaryFontId)
            fontStore.typeface(appearance.fallbackFontId)
        }
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

    fun submitPassword(host: HostItem, password: String) {
        val retry = passwordRetry
        if (retry != null) {
            passwordRetry = null
            passwordPromptHost = null
            retry(password)
        } else {
            connectHost(host, passwordOverride = password)
        }
    }

    fun dismissPasswordPrompt() {
        passwordPromptHost = null
        passwordRetry = null
    }

    fun connectHost(host: HostItem, passwordOverride: String? = null, openSftpAfter: Boolean = false) {
        // A tmux host is represented by its conversation tabs (connected or not): focus instead of duplicating.
        val existing = tabs.firstOrNull {
            !it.isMarkdown && it.hostId == host.id && (it.connected || it.tmuxId != null)
        }
        if (existing != null && passwordOverride == null) {
            selectTab(existing)
            currentNav = BottomNavTab.SESSIONS
            if (openSftpAfter && existing.connected) openSftpForActiveSession()
            refreshRemoteTmux(host.id)
            return
        }

        val client = apiClient ?: return
        connectingHostId = host.id
        viewModelScope.launch {
            try {
                val sess = client.createSession(
                    hostId = host.id,
                    password = passwordOverride ?: hostPasswords[host.id],
                    cols = 80,
                    rows = 28
                )
                if (passwordOverride != null) hostPasswords[host.id] = passwordOverride
                passwordPromptHost = null
                val tmux = detectTmux(client, sess)
                if (tmux != null) {
                    remoteTmux[host.id] = tmux.second
                    viewModelScope.launch {
                        // the relay may create the default session just after connecting
                        delay(2000)
                        refreshRemoteTmux(host.id)
                    }
                }
                attachNewSessionTab(
                    client = client,
                    sessionId = sess.id,
                    hostId = host.id,
                    title = tmux?.let { (id, list) -> list.firstOrNull { it.id == id }?.title ?: id } ?: host.name,
                    subtitle = "${host.user}@${host.host}",
                    tmuxId = tmux?.first,
                    replace = tmux?.let { (id, _) -> tabs.firstOrNull { it.hostId == host.id && it.tmuxId == id } }
                )
                if (openSftpAfter) {
                    openSftpForActiveSession()
                }
            } catch (e: ApiException) {
                if (e.code == "AUTH_PASSWORD_NEEDED") {
                    hostPasswords.remove(host.id)
                    passwordRetry = null
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
        subtitle: String,
        tmuxId: String? = null,
        replace: LiveTerminalTab? = null
    ) {
        val tab = LiveTerminalTab(
            tabId = "tab-${UUID.randomUUID()}",
            isMarkdown = false,
            sessionId = sessionId,
            hostId = hostId,
            title = title,
            subtitle = subtitle,
            tmuxId = tmuxId
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
        val idx = replace?.let { tabs.indexOf(it) } ?: -1
        if (idx >= 0) {
            replace?.socket?.close()
            tabs[idx] = tab
        } else {
            tabs.add(tab)
        }
        activeTabId = tab.tabId
        currentNav = BottomNavTab.SESSIONS
    }

    // ---- Remote tmux conversations (shared with desktop) ----

    private suspend fun detectTmux(
        client: CoreApiClient,
        sess: SessionInfo
    ): Pair<String, List<RemoteTmuxSession>>? {
        val ws = sess.workspaceName
        if ((sess.relayMode != "tmux" && sess.relayMode != "auto") || !TMUX_ID.matches(ws)) return null
        val list = runCatching { client.listTmux(sess.id) }.getOrNull() ?: return null
        return if (list.available) ws to list.sessions else null
    }

    /** Any connected tab of the tmux host can run the tmux API; prefer one other than [avoid]. */
    private fun tmuxControlSession(hostId: String, avoid: LiveTerminalTab? = null): String? {
        val hostTabs = tabs.filter { it.hostId == hostId && it.tmuxId != null && it.connected }
        return (hostTabs.firstOrNull { it !== avoid } ?: hostTabs.firstOrNull())?.sessionId
    }

    fun refreshRemoteTmux(hostId: String) {
        val client = apiClient ?: return
        val sid = tmuxControlSession(hostId) ?: return
        viewModelScope.launch {
            val list = runCatching { client.listTmux(sid) }.getOrNull() ?: return@launch
            if (list.available) remoteTmux[hostId] = list.sessions
        }
    }

    /** Called when the sessions page shows and when the app returns to the foreground. */
    fun refreshAllRemoteTmux() {
        tabs.mapNotNull { t -> t.hostId.takeIf { t.tmuxId != null } }.distinct().forEach { refreshRemoteTmux(it) }
    }

    /** Display order of a tmux host's conversations: remote list (by creation), then attached tabs not listed yet. */
    private fun tmuxGroupIds(hostId: String): List<String> {
        val ids = (remoteTmux[hostId] ?: emptyList()).map { it.id }.toMutableList()
        for (t in tabs) {
            val id = t.tmuxId
            if (t.hostId == hostId && id != null && id !in ids) ids.add(id)
        }
        return ids
    }

    fun topTabs(): List<TopTab> {
        val out = mutableListOf<TopTab>()
        val doneHosts = mutableSetOf<String>()
        for (tab in tabs) {
            val hostId = tab.hostId
            if (tab.tmuxId == null || hostId == null) {
                out.add(TopTab.Live(tab, null))
                continue
            }
            if (!doneHosts.add(hostId)) continue
            val hostTabs = tabs.filter { it.hostId == hostId && it.tmuxId != null }
            val remote = remoteTmux[hostId] ?: emptyList()
            for (r in remote) {
                val attached = hostTabs.firstOrNull { it.tmuxId == r.id }
                out.add(if (attached != null) TopTab.Live(attached, r) else TopTab.Ghost(hostId, r))
            }
            for (t in hostTabs) {
                if (remote.none { it.id == t.tmuxId }) out.add(TopTab.Live(t, null))
            }
        }
        return out
    }

    /** First scroll-back inside tmux: fetch the remote history once and scroll it locally. */
    fun loadTmuxHistory(tab: LiveTerminalTab) {
        val client = apiClient ?: return
        val tmuxId = tab.tmuxId ?: return
        if (tab.historyLoading || tab.history != null || !tab.connected) return
        tab.historyLoading = true
        viewModelScope.launch {
            try {
                val text = client.tmuxHistory(tab.sessionId, tmuxId)
                val hb = withContext(Dispatchers.Default) {
                    AnsiTerminalBuffer(tab.buffer.cols, tab.buffer.rows, maxScrollback = 12000).also { b ->
                        val body = text.removeSuffix("\n").replace("\n", "\r\n")
                        b.feedBytes(body.toByteArray(Charsets.UTF_8))
                    }
                }
                tab.history = hb
            } catch (_: Throwable) {
                // scrolling simply stays at the live screen
            } finally {
                tab.historyLoading = false
            }
        }
    }

    fun closeTmuxHistory(tab: LiveTerminalTab) {
        tab.history = null
    }

    /** Tapping a chip: a disconnected conversation re-attaches (only if it still exists). */
    fun selectTab(tab: LiveTerminalTab) {
        activeTabId = tab.tabId
        val hostId = tab.hostId
        val tmuxId = tab.tmuxId
        if (!tab.connected && hostId != null && tmuxId != null) {
            attachTmux(hostId, tmuxId, tab.title, tab, null)
        }
    }

    fun switchToRemoteSession(hostId: String, remote: RemoteTmuxSession) {
        val existing = tabs.firstOrNull { it.hostId == hostId && it.tmuxId == remote.id }
        if (existing != null) {
            selectTab(existing)
            return
        }
        attachTmux(hostId, remote.id, remote.title, null, null)
    }

    private fun attachTmux(
        hostId: String,
        tmuxId: String,
        title: String,
        replace: LiveTerminalTab?,
        password: String?
    ) {
        val client = apiClient ?: return
        val host = hosts.firstOrNull { it.id == hostId } ?: return
        val key = "$hostId/$tmuxId"
        if (!attachingTmux.add(key)) return
        viewModelScope.launch {
            try {
                replace?.let { old -> client.deleteSession(old.sessionId) }
                val sess = client.createSession(
                    hostId = hostId,
                    password = password ?: hostPasswords[hostId],
                    cols = 80,
                    rows = 28,
                    tmuxSession = tmuxId
                )
                if (password != null) hostPasswords[hostId] = password
                attachNewSessionTab(
                    client = client,
                    sessionId = sess.id,
                    hostId = hostId,
                    title = title,
                    subtitle = "${host.user}@${host.host}",
                    tmuxId = tmuxId,
                    replace = replace
                )
            } catch (e: ApiException) {
                when (e.code) {
                    "TMUX_SESSION_GONE" -> {
                        // ended on another device: drop it instead of resurrecting an empty conversation
                        replace?.let { removeTabLocally(it) }
                        remoteTmux[hostId] = (remoteTmux[hostId] ?: emptyList()).filter { it.id != tmuxId }
                        showToast("「$title」已在其他设备结束")
                        refreshRemoteTmux(hostId)
                    }
                    "AUTH_PASSWORD_NEEDED" -> {
                        hostPasswords.remove(hostId)
                        passwordRetry = { pw -> attachTmux(hostId, tmuxId, title, replace, pw) }
                        passwordPromptHost = host
                    }
                    else -> showToast("打开对话失败: ${e.message}")
                }
            } catch (t: Throwable) {
                showToast("打开对话异常: ${t.message}")
            } finally {
                attachingTmux.remove(key)
            }
        }
    }

    /** "+" on a tmux host: create a conversation on the remote host and attach to it. */
    fun createAndAttachRemoteSession(hostId: String) {
        val client = apiClient ?: return
        val sid = tmuxControlSession(hostId) ?: run {
            showToast("请先重新连接该主机")
            return
        }
        viewModelScope.launch {
            runCatching { client.createTmux(sid) }
                .onSuccess { created ->
                    remoteTmux[hostId] = (remoteTmux[hostId] ?: emptyList()).filter { it.id != created.id } + created
                    attachTmux(hostId, created.id, created.title, null, null)
                }
                .onFailure { e -> showToast("新建对话失败: ${e.message}") }
        }
    }

    fun renameRemoteSession(hostId: String, tmuxId: String, title: String) {
        val client = apiClient ?: return
        val clean = title.trim()
        if (clean.isEmpty()) return
        val sid = tmuxControlSession(hostId) ?: return
        viewModelScope.launch {
            runCatching { client.renameTmux(sid, tmuxId, clean) }
                .onSuccess { newTitle ->
                    remoteTmux[hostId] = (remoteTmux[hostId] ?: emptyList()).map {
                        if (it.id == tmuxId) it.copy(title = newTitle) else it
                    }
                    tabs.filter { it.hostId == hostId && it.tmuxId == tmuxId }.forEach { it.title = newTitle }
                }
                .onFailure { e -> showToast("改名失败: ${e.message}") }
            refreshRemoteTmux(hostId)
        }
    }

    /** "×" on a conversation: ends it on the remote host, confirming first when something is running. */
    fun requestEndRemoteSession(hostId: String, tmuxId: String) {
        val client = apiClient ?: return
        val tab = tabs.firstOrNull { it.hostId == hostId && it.tmuxId == tmuxId }
        val sid = tmuxControlSession(hostId, tab) ?: run {
            // no live connection to ask through: just drop the local view
            tab?.let { removeTabLocally(it) }
            return
        }
        viewModelScope.launch {
            val fresh = runCatching { client.listTmux(sid) }.getOrNull()
            val info = fresh?.sessions?.firstOrNull { it.id == tmuxId }
                ?: remoteTmux[hostId]?.firstOrNull { it.id == tmuxId }
            if (info?.busy == true) {
                pendingEnd = PendingEndConversation(hostId, tmuxId, info.title, info.command)
            } else {
                endRemoteSession(hostId, tmuxId)
            }
        }
    }

    fun confirmEndRemoteSession() {
        val p = pendingEnd ?: return
        pendingEnd = null
        endRemoteSession(p.hostId, p.tmuxId)
    }

    private fun endRemoteSession(hostId: String, tmuxId: String) {
        val client = apiClient ?: return
        val tab = tabs.firstOrNull { it.hostId == hostId && it.tmuxId == tmuxId }
        val sid = tmuxControlSession(hostId, tab) ?: return
        val order = tmuxGroupIds(hostId)
        val idx = order.indexOf(tmuxId)
        val rest = order.filter { it != tmuxId }
        val nextId = if (rest.isEmpty()) null else rest[idx.coerceIn(0, rest.size - 1)]
        val nextRemote = remoteTmux[hostId]?.firstOrNull { it.id == nextId }
        val wasActive = tab != null && activeTab?.tabId == tab.tabId
        val hostHasOtherTab = tabs.any { it.hostId == hostId && it.tmuxId != null && it.tmuxId != tmuxId }
        viewModelScope.launch {
            // killing the session this connection is attached to may drop the response; the refreshed list decides
            runCatching { client.killTmux(sid, tmuxId) }
            tab?.let { removeTabLocally(it) }
            remoteTmux[hostId] = (remoteTmux[hostId] ?: emptyList()).filter { it.id != tmuxId }
            if (nextId != null && (wasActive || !hostHasOtherTab)) {
                val nextTab = tabs.firstOrNull { it.hostId == hostId && it.tmuxId == nextId }
                if (nextTab != null) {
                    selectTab(nextTab)
                } else if (nextRemote != null) {
                    attachTmux(hostId, nextRemote.id, nextRemote.title, null, null)
                }
            }
            delay(800)
            refreshRemoteTmux(hostId)
        }
    }

    fun jumpToSessionById(sessionId: String) {
        val hit = tabs.firstOrNull { it.sessionId == sessionId } ?: return
        activeTabId = hit.tabId
        currentNav = BottomNavTab.SESSIONS
    }

    fun closeTab(tab: LiveTerminalTab) {
        val hostId = tab.hostId
        val tmuxId = tab.tmuxId
        if (hostId != null && tmuxId != null) {
            requestEndRemoteSession(hostId, tmuxId)
            return
        }
        removeTabLocally(tab)
    }

    private fun removeTabLocally(tab: LiveTerminalTab) {
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
