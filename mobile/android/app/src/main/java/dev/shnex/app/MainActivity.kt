package dev.shnex.app

import android.Manifest
import android.content.ClipboardManager
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.OpenableColumns
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.activity.viewModels
import androidx.compose.foundation.ExperimentalFoundationApi
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.combinedClickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.ime
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.InsertDriveFile
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.Key
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material.icons.filled.Upload
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.Checkbox
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FloatingActionButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.darkColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import kotlinx.coroutines.launch
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.shnex.app.data.HostItem
import dev.shnex.app.service.ShnexRelayService
import dev.shnex.app.terminal.FontCatalog
import dev.shnex.app.terminal.TerminalCanvasView
import dev.shnex.app.terminal.rememberTerminalImeController
import dev.shnex.app.ui.AppearanceScreen
import dev.shnex.app.ui.BottomNavTab
import dev.shnex.app.ui.NativeMarkdownDocumentView
import dev.shnex.app.ui.TopTab
import dev.shnex.app.ui.WorkbenchViewModel

private val ShnexDarkColors = darkColorScheme(
    primary = Color(0xFF2563EB),
    onPrimary = Color.White,
    secondary = Color(0xFF2B2F3C),
    onSecondary = Color(0xFFF3F4F6),
    background = Color(0xFF121316),
    onBackground = Color(0xFFE5E7EB),
    surface = Color(0xFF1B1C22),
    onSurface = Color(0xFFE5E7EB),
    outline = Color(0xFF2C2F3A)
)

class MainActivity : ComponentActivity() {

    private val vm: WorkbenchViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        ShnexRelayService.ensureStarted(this)
        handleJumpIntent(intent)

        setContent {
            MaterialTheme(colorScheme = ShnexDarkColors) {
                ShnexMobileWorkbench(vm)
            }
        }
    }

    override fun onResume() {
        super.onResume()
        // conversations may have been added, renamed or ended on another device
        vm.refreshAllRemoteTmux()
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleJumpIntent(intent)
    }

    private fun handleJumpIntent(intent: Intent?) {
        val jumpSession = intent?.getStringExtra(ShnexRelayService.EXTRA_JUMP_SESSION_ID)
        if (!jumpSession.isNullOrBlank()) {
            vm.jumpToSessionById(jumpSession)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ShnexMobileWorkbench(vm: WorkbenchViewModel) {
    val context = LocalContext.current

    // Request Android 13+ notification permission for Claude CLI approval & idle alerts
    val notifLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { }
    LaunchedEffect(Unit) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            notifLauncher.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }

    // Image picker for uploading phone screenshots directly into remote Claude CLI ([Image #1])
    val claudeImagePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        if (uri != null) {
            vm.uploadImageUriToActiveClaudeSession(context, uri)
        }
    }

    // Generic file picker for SFTP upload
    val sftpFilePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        if (uri != null) {
            val name = queryUriDisplayName(context, uri) ?: "upload_${System.currentTimeMillis()}"
            vm.uploadUriToSftp(context, uri, name)
        }
    }

    var editingHost by remember { mutableStateOf<HostItem?>(null) }
    var showHostDialog by remember { mutableStateOf(false) }
    var showQuickConnectDialog by remember { mutableStateOf(false) }
    var showFabMenu by remember { mutableStateOf(false) }

    val density = LocalDensity.current
    val imeVisible = WindowInsets.ime.getBottom(density) > with(density) { 80.dp.toPx() }
    val hideBottomNav = vm.currentNav == BottomNavTab.SESSIONS && imeVisible

    Scaffold(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFF121316))
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding(),
        containerColor = Color(0xFF121316),
        bottomBar = {
            if (!hideBottomNav) {
                NavigationBar(
                    containerColor = Color(0xFF1B1C22),
                    tonalElevation = 0.dp,
                    modifier = Modifier.height(64.dp)
                ) {
                    NavigationBarItem(
                        selected = vm.currentNav == BottomNavTab.HOSTS,
                        onClick = { vm.currentNav = BottomNavTab.HOSTS },
                        icon = { Icon(Icons.Default.Dns, contentDescription = "主机", modifier = Modifier.size(19.dp)) },
                        label = { Text("主机", fontSize = 11.sp) },
                        colors = NavigationBarItemDefaults.colors(
                            indicatorColor = Color(0xFF2B2F3C),
                            selectedIconColor = Color.White,
                            selectedTextColor = Color.White,
                            unselectedIconColor = Color(0xFF8B909A),
                            unselectedTextColor = Color(0xFF8B909A)
                        )
                    )
                    NavigationBarItem(
                        selected = vm.currentNav == BottomNavTab.SESSIONS,
                        onClick = { vm.currentNav = BottomNavTab.SESSIONS },
                        icon = {
                            Box {
                                Icon(Icons.Default.Terminal, contentDescription = "会话", modifier = Modifier.size(19.dp))
                                if (vm.hasAnyPendingApproval) {
                                    Box(
                                        modifier = Modifier
                                            .align(Alignment.TopEnd)
                                            .size(7.dp)
                                            .background(Color(0xFFF59E0B), CircleShape)
                                    )
                                }
                            }
                        },
                        label = {
                            Text(
                                text = if (vm.tabs.isNotEmpty()) "会话 (${vm.tabs.size})" else "会话",
                                fontSize = 11.sp
                            )
                        },
                        colors = NavigationBarItemDefaults.colors(
                            indicatorColor = Color(0xFF2B2F3C),
                            selectedIconColor = Color.White,
                            selectedTextColor = Color.White,
                            unselectedIconColor = Color(0xFF8B909A),
                            unselectedTextColor = Color(0xFF8B909A)
                        )
                    )
                    NavigationBarItem(
                        selected = vm.currentNav == BottomNavTab.SETTINGS,
                        onClick = { vm.currentNav = BottomNavTab.SETTINGS },
                        icon = { Icon(Icons.Default.Settings, contentDescription = "设置", modifier = Modifier.size(19.dp)) },
                        label = { Text("设置", fontSize = 11.sp) },
                        colors = NavigationBarItemDefaults.colors(
                            indicatorColor = Color(0xFF2B2F3C),
                            selectedIconColor = Color.White,
                            selectedTextColor = Color.White,
                            unselectedIconColor = Color(0xFF8B909A),
                            unselectedTextColor = Color(0xFF8B909A)
                        )
                    )
                }
            }
        }
    ) { innerPad ->
        Box(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPad)
        ) {
            if (vm.bootError != null) {
                Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
                    Text("核心引擎启动异常: ${vm.bootError}", color = Color(0xFFF87171))
                }
            } else if (vm.apiClient == null) {
                Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                    Column(horizontalAlignment = Alignment.CenterHorizontally) {
                        CircularProgressIndicator(color = Color(0xFF2563EB), modifier = Modifier.size(28.dp))
                        Spacer(Modifier.height(10.dp))
                        Text("正在启动 Shnex 127.0.0.1 安全核心...", color = Color(0xFF9CA3AF), fontSize = 12.sp)
                    }
                }
            } else {
                when (vm.currentNav) {
                    BottomNavTab.HOSTS -> HostsScreen(
                        vm = vm,
                        onAddHost = {
                            editingHost = null
                            showHostDialog = true
                        },
                        onEditHost = { hp ->
                            editingHost = hp
                            showHostDialog = true
                        },
                        onQuickConnect = { showQuickConnectDialog = true },
                        onOpenMathDemo = { vm.openMarkdownSampleTab() },
                        showFabMenu = showFabMenu,
                        onToggleFabMenu = { showFabMenu = it }
                    )

                    BottomNavTab.SESSIONS -> SessionsScreen(
                        vm = vm,
                        imeVisible = imeVisible,
                        onPickImageForClaude = { claudeImagePicker.launch("image/*") },
                        onOpenMathDemo = { vm.openMarkdownSampleTab() }
                    )

                    BottomNavTab.SETTINGS -> MobileSettingsScreen(vm = vm)
                }
            }

            // Floating Toast Pill
            vm.toastMessage?.let { msg ->
                Surface(
                    color = Color(0xFF222530),
                    shape = RoundedCornerShape(999.dp),
                    modifier = Modifier
                        .align(Alignment.TopCenter)
                        .padding(top = 10.dp)
                        .border(1.dp, Color(0xFF3B82F6), RoundedCornerShape(999.dp))
                ) {
                    Text(
                        text = msg,
                        color = Color.White,
                        fontSize = 12.sp,
                        modifier = Modifier.padding(horizontal = 14.dp, vertical = 7.dp)
                    )
                }
            }
        }
    }

    // Add / Edit Host Dialog
    if (showHostDialog) {
        HostEditDialog(
            initial = editingHost,
            onDismiss = { showHostDialog = false },
            onSave = { name, host, port, user, pw, pk, tmux, tsnet, script, onDone ->
                vm.saveHost(
                    id = editingHost?.id,
                    name = name,
                    host = host,
                    port = port,
                    user = user,
                    password = pw,
                    privateKeyPem = pk,
                    tmuxSession = tmux,
                    useTsnet = tsnet,
                    postLoginScript = script,
                    onResult = { res ->
                        onDone(res.exceptionOrNull()?.message)
                        if (res.isSuccess) {
                            showHostDialog = false
                        }
                    }
                )
            }
        )
    }

    // Quick Connect Dialog
    if (showQuickConnectDialog) {
        QuickConnectDialog(
            onDismiss = { showQuickConnectDialog = false },
            onConnect = { expr, pw ->
                showQuickConnectDialog = false
                vm.quickConnect(expr, pw)
            }
        )
    }

    // Auto-Auth Password Fallback Prompt
    vm.passwordPromptHost?.let { targetHost ->
        var pw by remember { mutableStateOf("") }
        AlertDialog(
            onDismissRequest = { vm.dismissPasswordPrompt() },
            title = { Text("输入 SSH 密码 (${targetHost.name})", fontSize = 16.sp) },
            text = {
                OutlinedTextField(
                    value = pw,
                    onValueChange = { pw = it },
                    label = { Text("${targetHost.user}@${targetHost.host}") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation()
                )
            },
            confirmButton = {
                Button(onClick = { vm.submitPassword(targetHost, pw) }) {
                    Text("连接")
                }
            },
            dismissButton = {
                TextButton(onClick = { vm.dismissPasswordPrompt() }) {
                    Text("取消")
                }
            }
        )
    }

    // SFTP Bottom Sheet
    if (vm.showSftpSheet) {
        ModalBottomSheet(
            onDismissRequest = { vm.showSftpSheet = false },
            containerColor = Color(0xFF1B1C22)
        ) {
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .height(440.dp)
                    .padding(horizontal = 14.dp)
            ) {
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        IconButton(onClick = {
                            val cur = vm.sftpCurPath
                            if (cur != "/" && cur.isNotBlank()) {
                                val parent = cur.substringBeforeLast('/').ifBlank { "/" }
                                vm.openSftpForActiveSession(parent)
                            }
                        }) {
                            Icon(Icons.Default.ArrowUpward, contentDescription = "上级目录", tint = Color.White)
                        }
                        Text(
                            text = vm.sftpCurPath,
                            color = Color(0xFFD1D5DB),
                            fontFamily = FontFamily.Monospace,
                            fontSize = 12.sp,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.width(200.dp)
                        )
                    }
                    Button(
                        onClick = { sftpFilePicker.launch("*/*") },
                        contentPadding = PaddingValues(horizontal = 12.dp, vertical = 4.dp)
                    ) {
                        Icon(Icons.Default.Upload, contentDescription = null, modifier = Modifier.size(15.dp))
                        Spacer(Modifier.width(4.dp))
                        Text("上传", fontSize = 12.sp)
                    }
                }
                HorizontalDivider(color = Color(0xFF2B2E38), modifier = Modifier.padding(vertical = 6.dp))
                if (vm.sftpLoading) {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator(modifier = Modifier.size(24.dp))
                    }
                } else {
                    LazyColumn(modifier = Modifier.fillMaxSize()) {
                        items(vm.sftpEntries) { entry ->
                            val isMd = entry.name.endsWith(".md", true) || entry.name.endsWith(".markdown", true)
                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable {
                                        if (entry.isDir) {
                                            vm.openSftpForActiveSession(entry.path)
                                        } else if (isMd) {
                                            vm.openRemoteSftpMarkdown(entry)
                                        }
                                    }
                                    .padding(vertical = 10.dp, horizontal = 4.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.SpaceBetween
                            ) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    modifier = Modifier.weight(1f)
                                ) {
                                    if (entry.isDir) {
                                        Icon(Icons.Default.Folder, null, tint = Color(0xFF60A5FA), modifier = Modifier.size(18.dp))
                                    } else if (isMd) {
                                        Box(
                                            modifier = Modifier
                                                .background(Color(0xFF2563EB), RoundedCornerShape(4.dp))
                                                .padding(horizontal = 4.dp, vertical = 1.dp)
                                        ) {
                                            Text("M↓", color = Color.White, fontSize = 9.sp, fontWeight = FontWeight.Bold)
                                        }
                                    } else {
                                        Icon(Icons.AutoMirrored.Filled.InsertDriveFile, null, tint = Color(0xFF9CA3AF), modifier = Modifier.size(18.dp))
                                    }
                                    Spacer(Modifier.width(10.dp))
                                    Column {
                                        Text(entry.name, color = Color.White, fontSize = 13.sp, maxLines = 1)
                                        Text(
                                            text = if (entry.isDir) entry.mode else "${entry.size} B  ${entry.mode}",
                                            color = Color(0xFF7C828E),
                                            fontSize = 10.sp,
                                            fontFamily = FontFamily.Monospace
                                        )
                                    }
                                }
                            }
                            HorizontalDivider(color = Color(0xFF23252E))
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun HostsScreen(
    vm: WorkbenchViewModel,
    onAddHost: () -> Unit,
    onEditHost: (HostItem) -> Unit,
    onQuickConnect: () -> Unit,
    onOpenMathDemo: () -> Unit,
    showFabMenu: Boolean,
    onToggleFabMenu: (Boolean) -> Unit
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(14.dp)
    ) {
        Column(modifier = Modifier.fillMaxSize()) {
            if (vm.hosts.isEmpty()) {
                Surface(
                    shape = RoundedCornerShape(18.dp),
                    color = Color(0xFF1B1C22),
                    modifier = Modifier
                        .fillMaxWidth()
                        .border(1.dp, Color(0xFF2C2F3A), RoundedCornerShape(18.dp))
                ) {
                    Column(
                        modifier = Modifier.padding(24.dp),
                        horizontalAlignment = Alignment.CenterHorizontally
                    ) {
                        Text("还没有添加主机", color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold)
                        Spacer(Modifier.height(6.dp))
                        Text(
                            "点击右下角 + 添加 SSH 服务器，自动附着远端 tmux 并随时接力 Claude CLI。",
                            color = Color(0xFF8B909A),
                            fontSize = 12.sp
                        )
                        Spacer(Modifier.height(14.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                            Button(onClick = onAddHost) { Text("新建主机") }
                            OutlinedButton(onClick = onQuickConnect) { Text("快速连接") }
                        }
                    }
                }
            } else {
                Surface(
                    shape = RoundedCornerShape(18.dp),
                    color = Color(0xFF1B1C22),
                    modifier = Modifier
                        .fillMaxWidth()
                        .border(1.dp, Color(0xFF2C2F3A), RoundedCornerShape(18.dp))
                ) {
                    LazyColumn {
                        items(vm.hosts, key = { it.id }) { hp ->
                            var rowMenu by remember { mutableStateOf(false) }
                            val hostTabs = vm.tabs.filter { !it.isMarkdown && it.hostId == hp.id && it.connected }
                            val pendingApproval = hostTabs.any { it.approvalSnippet != null }
                            val lat = vm.latencies[hp.id]

                            Row(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .clickable { vm.connectHost(hp) }
                                    .padding(horizontal = 14.dp, vertical = 13.dp),
                                verticalAlignment = Alignment.CenterVertically,
                                horizontalArrangement = Arrangement.SpaceBetween
                            ) {
                                Row(
                                    verticalAlignment = Alignment.CenterVertically,
                                    modifier = Modifier.weight(1f)
                                ) {
                                    Icon(
                                        Icons.Default.Dns,
                                        contentDescription = null,
                                        tint = if (hostTabs.isNotEmpty()) Color(0xFF4ADE80) else Color(0xFF9CA3AF),
                                        modifier = Modifier.size(21.dp)
                                    )
                                    Spacer(Modifier.width(12.dp))
                                    Column {
                                        Row(verticalAlignment = Alignment.CenterVertically) {
                                            Text(hp.name, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                                            if (pendingApproval) {
                                                Spacer(Modifier.width(8.dp))
                                                Box(
                                                    modifier = Modifier
                                                        .border(1.dp, Color(0xFFF59E0B), RoundedCornerShape(999.dp))
                                                        .padding(horizontal = 6.dp, vertical = 1.dp)
                                                ) {
                                                    Text("待审批", color = Color(0xFFFBBF24), fontSize = 10.sp)
                                                }
                                            } else if (hostTabs.isNotEmpty()) {
                                                Spacer(Modifier.width(7.dp))
                                                Box(
                                                    modifier = Modifier
                                                        .size(7.dp)
                                                        .background(Color(0xFF4ADE80), CircleShape)
                                                )
                                            }
                                            if (vm.connectingHostId == hp.id) {
                                                Spacer(Modifier.width(8.dp))
                                                CircularProgressIndicator(
                                                    color = Color(0xFF60A5FA),
                                                    strokeWidth = 2.dp,
                                                    modifier = Modifier.size(13.dp)
                                                )
                                            }
                                        }
                                        val tags = buildList {
                                            add(vm.maskAddress("${hp.user}@${hp.host}"))
                                            if (hp.tmuxSession.isNotBlank()) add("tmux:${hp.tmuxSession}")
                                            if (hp.useTsnet) add("Tailnet")
                                            if (lat != null) add(if (lat >= 0) "${lat}ms" else "超时")
                                        }.joinToString("  ")
                                        Text(
                                            text = tags,
                                            color = Color(0xFF8B909A),
                                            fontSize = 11.sp,
                                            fontFamily = FontFamily.Monospace,
                                            maxLines = 1,
                                            overflow = TextOverflow.Ellipsis
                                        )
                                    }
                                }

                                Box {
                                    IconButton(onClick = { rowMenu = true }) {
                                        Icon(Icons.Default.MoreVert, contentDescription = "更多", tint = Color(0xFF8B909A))
                                    }
                                    DropdownMenu(expanded = rowMenu, onDismissRequest = { rowMenu = false }) {
                                        DropdownMenuItem(
                                            text = { Text("打开远程文件 (SFTP)") },
                                            onClick = {
                                                rowMenu = false
                                                vm.connectHost(hp, openSftpAfter = true)
                                            }
                                        )
                                        DropdownMenuItem(
                                            text = { Text("编辑主机") },
                                            onClick = {
                                                rowMenu = false
                                                onEditHost(hp)
                                            }
                                        )
                                        DropdownMenuItem(
                                            text = { Text("删除主机", color = Color(0xFFF87171)) },
                                            onClick = {
                                                rowMenu = false
                                                vm.deleteHost(hp.id)
                                            }
                                        )
                                    }
                                }
                            }
                            HorizontalDivider(color = Color(0xFF262934))
                        }
                    }
                }
            }
        }

        // Material 3 Rounded-Square FAB (+)
        Box(modifier = Modifier.align(Alignment.BottomEnd)) {
            FloatingActionButton(
                onClick = { onToggleFabMenu(true) },
                shape = RoundedCornerShape(16.dp),
                containerColor = Color(0xFFF3F4F6),
                contentColor = Color(0xFF121316)
            ) {
                Icon(Icons.Default.Add, contentDescription = "新建")
            }
            DropdownMenu(
                expanded = showFabMenu,
                onDismissRequest = { onToggleFabMenu(false) }
            ) {
                DropdownMenuItem(
                    text = { Text("新建 SSH 主机") },
                    onClick = {
                        onToggleFabMenu(false)
                        onAddHost()
                    }
                )
                DropdownMenuItem(
                    text = { Text("快速连接 SSH...") },
                    onClick = {
                        onToggleFabMenu(false)
                        onQuickConnect()
                    }
                )
                DropdownMenuItem(
                    text = { Text("M↓ 数学公式排版示例") },
                    onClick = {
                        onToggleFabMenu(false)
                        onOpenMathDemo()
                    }
                )
            }
        }
    }
}

@Composable
private fun SessionsScreen(
    vm: WorkbenchViewModel,
    imeVisible: Boolean,
    onPickImageForClaude: () -> Unit,
    onOpenMathDemo: () -> Unit
) {
    val context = LocalContext.current
    val active = vm.activeTab
    val imeController = rememberTerminalImeController()
    var ctrlArmed by remember { mutableStateOf(false) }

    if (active == null) {
        Box(Modifier.fillMaxSize().padding(24.dp), contentAlignment = Alignment.Center) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                Text("当前没有打开的接力会话", color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.Medium)
                Spacer(Modifier.height(6.dp))
                Text("从「主机」页点击任意服务器即可进入终端", color = Color(0xFF8B909A), fontSize = 12.sp)
                Spacer(Modifier.height(14.dp))
                OutlinedButton(onClick = onOpenMathDemo) {
                    Text("打开 M↓ 数学公式文档示例")
                }
            }
        }
        return
    }

    LaunchedEffect(active.tabId) {
        imeController.resetState()
        if (!active.isMarkdown && imeVisible) {
            imeController.showKeyboard()
        }
    }

    // Refresh the remote conversation list whenever the sessions page is shown.
    LaunchedEffect(Unit) { vm.refreshAllRemoteTmux() }

    // Rename a conversation (long press on its chip): hostId, tmuxId, current title
    var renameTarget by remember { mutableStateOf<Triple<String, String, String>?>(null) }
    renameTarget?.let { (hostId, tmuxId, oldTitle) ->
        var text by remember(hostId, tmuxId) { mutableStateOf(oldTitle) }
        AlertDialog(
            onDismissRequest = { renameTarget = null },
            title = { Text("重命名对话", fontSize = 16.sp) },
            text = {
                OutlinedTextField(
                    value = text,
                    onValueChange = { text = it.take(40) },
                    singleLine = true
                )
            },
            confirmButton = {
                Button(onClick = {
                    renameTarget = null
                    if (text.isNotBlank() && text.trim() != oldTitle) {
                        vm.renameRemoteSession(hostId, tmuxId, text)
                    }
                }) { Text("确定") }
            },
            dismissButton = {
                TextButton(onClick = { renameTarget = null }) { Text("取消") }
            }
        )
    }

    // Ending a conversation that is running something needs one confirmation.
    vm.pendingEnd?.let { p ->
        AlertDialog(
            onDismissRequest = { vm.pendingEnd = null },
            text = { Text("「${p.title}」正在运行 ${p.command}，确定结束？", fontSize = 14.sp) },
            confirmButton = {
                TextButton(onClick = { vm.confirmEndRemoteSession() }) {
                    Text("结束对话", color = Color(0xFFF87171))
                }
            },
            dismissButton = {
                TextButton(onClick = { vm.pendingEnd = null }) { Text("取消") }
            }
        )
    }

    Column(modifier = Modifier.fillMaxSize()) {
        // 1. Top Compact Session Switcher Chips + RTT + Upload Image + SFTP
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .background(Color(0xFF1B1C22))
                .padding(horizontal = 8.dp, vertical = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween
        ) {
            Row(
                modifier = Modifier
                    .weight(1f)
                    .horizontalScroll(rememberScrollState()),
                horizontalArrangement = Arrangement.spacedBy(6.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                for (item in vm.topTabs()) {
                    when (item) {
                        is TopTab.Ghost -> SessionChip(
                            title = item.remote.title,
                            selected = false,
                            dim = true,
                            markdown = false,
                            onClick = { vm.switchToRemoteSession(item.hostId, item.remote) },
                            onLongClick = {
                                renameTarget = Triple(item.hostId, item.remote.id, item.remote.title)
                            },
                            onClose = { vm.requestEndRemoteSession(item.hostId, item.remote.id) }
                        )
                        is TopTab.Live -> {
                            val t = item.tab
                            val hostId = t.hostId
                            val tmuxId = t.tmuxId
                            val title = item.remote?.title ?: t.title
                            SessionChip(
                                title = title,
                                selected = t.tabId == active.tabId,
                                dim = tmuxId != null && !t.connected,
                                markdown = t.isMarkdown,
                                onClick = { vm.selectTab(t) },
                                onLongClick = if (hostId != null && tmuxId != null) {
                                    { renameTarget = Triple(hostId, tmuxId, title) }
                                } else {
                                    null
                                },
                                onClose = { vm.closeTab(t) }
                            )
                        }
                    }
                }
                val activeHostId = active.hostId
                if (active.tmuxId != null && activeHostId != null) {
                    Icon(
                        Icons.Default.Add,
                        contentDescription = "新建对话",
                        tint = Color(0xFF9CA3AF),
                        modifier = Modifier
                            .size(22.dp)
                            .clip(CircleShape)
                            .clickable { vm.createAndAttachRemoteSession(activeHostId) }
                            .padding(3.dp)
                    )
                }
            }

            if (!active.isMarkdown) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    active.rttMs?.let { ms ->
                        Text(
                            text = "${ms}ms",
                            color = Color(0xFF4ADE80),
                            fontSize = 11.sp,
                            fontFamily = FontFamily.Monospace,
                            modifier = Modifier.padding(horizontal = 4.dp)
                        )
                    }
                    IconButton(
                        onClick = onPickImageForClaude,
                        modifier = Modifier.size(30.dp)
                    ) {
                        Icon(
                            Icons.Default.Image,
                            contentDescription = "上传截图给 Claude CLI",
                            tint = Color(0xFF93C5FD),
                            modifier = Modifier.size(17.dp)
                        )
                    }
                    IconButton(
                        onClick = { vm.openSftpForActiveSession() },
                        modifier = Modifier.size(30.dp)
                    ) {
                        Icon(
                            Icons.Default.Folder,
                            contentDescription = "SFTP 文件",
                            tint = Color(0xFF9CA3AF),
                            modifier = Modifier.size(17.dp)
                        )
                    }
                }
            }
        }

        // 2. Center Stage: Native Markdown Math Sheet OR Direct-IME TrueColor Terminal Canvas
        if (active.isMarkdown && active.markdownDoc != null) {
            NativeMarkdownDocumentView(
                doc = active.markdownDoc,
                modifier = Modifier.weight(1f)
            )
        } else {
            TerminalCanvasView(
                buffer = active.buffer,
                renderTick = active.renderTick,
                appearance = vm.appearance,
                scheme = vm.terminalScheme,
                fonts = vm.terminalFonts(),
                onFontSizeChanged = { sp -> vm.updateAppearance { it.copy(fontSizeSp = sp) } },
                imeController = imeController,
                onInterceptCtrlLetter = { ch ->
                    val low = ch.lowercaseChar()
                    if (ctrlArmed && low in 'a'..'z') {
                        val code = (low.code - 'a'.code + 1).toChar()
                        vm.sendTerminalText(code.toString())
                        ctrlArmed = false
                        true
                    } else {
                        false
                    }
                },
                onTerminalInput = { text, useBracketedPaste ->
                    vm.sendTerminalText(text, useBracketedIfEnabled = useBracketedPaste)
                },
                onTerminalResize = { cols, rows ->
                    active.socket?.sendResize(cols, rows)
                },
                modifier = Modifier.weight(1f),
                historyBuffer = active.history,
                onNeedHistory = { vm.loadTmuxHistory(active) },
                onHistoryClosed = { vm.closeTmuxHistory(active) }
            )

            // 3. Contextual Slim Claude Approval Strip
            active.approvalSnippet?.let { snippet ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 8.dp, vertical = 4.dp)
                        .background(Color(0xFF1F2129), RoundedCornerShape(12.dp))
                        .border(1.dp, Color(0xFFF59E0B), RoundedCornerShape(12.dp))
                        .padding(horizontal = 10.dp, vertical = 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.SpaceBetween
                ) {
                    Text(
                        text = "待确认：$snippet",
                        color = Color(0xFFFBBF24),
                        fontSize = 12.sp,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                        modifier = Modifier.weight(1f)
                    )
                    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                        Button(
                            onClick = {
                                vm.sendTerminalText("1\r")
                                active.approvalSnippet = null
                            },
                            contentPadding = PaddingValues(horizontal = 10.dp, vertical = 2.dp),
                            modifier = Modifier.height(28.dp)
                        ) {
                            Text("允许", fontSize = 11.sp)
                        }
                        OutlinedButton(
                            onClick = {
                                vm.sendTerminalText("2\r")
                                active.approvalSnippet = null
                            },
                            contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
                            modifier = Modifier.height(28.dp)
                        ) {
                            Text("始终", fontSize = 11.sp)
                        }
                        OutlinedButton(
                            onClick = {
                                vm.sendTerminalText("\u001b")
                                active.approvalSnippet = null
                            },
                            contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp),
                            modifier = Modifier.height(28.dp)
                        ) {
                            Text("拒绝", fontSize = 11.sp, color = Color(0xFFF87171))
                        }
                    }
                }
            }

            // 4. Single Slim Extra Key Strip (Tap anywhere on terminal canvas to type directly!)
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(Color(0xFF1B1C22))
                    .horizontalScroll(rememberScrollState())
                    .padding(horizontal = 8.dp, vertical = 6.dp),
                horizontalArrangement = Arrangement.spacedBy(5.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                val appCur = active.buffer.appCursorKeys
                val sendSpecial: (String, Boolean) -> Unit = { seq, commitFirst ->
                    imeController.sendSpecialKey(
                        seq = seq,
                        commitComposingFirst = commitFirst,
                        fallbackSend = { vm.sendTerminalText(it) }
                    )
                }
                ExtraKeyChip("Esc") { sendSpecial("\u001b", false) }
                ExtraKeyChip("Tab") { sendSpecial("\t", true) }
                ExtraKeyChip("S-Tab") { sendSpecial("\u001b[Z", true) }
                ExtraKeyChip("Ctrl", highlighted = ctrlArmed) { ctrlArmed = !ctrlArmed }
                ExtraKeyChip("^C") { sendSpecial("\u0003", false) }
                ExtraKeyChip("↑") { sendSpecial(if (appCur) "\u001bOA" else "\u001b[A", true) }
                ExtraKeyChip("↓") { sendSpecial(if (appCur) "\u001bOB" else "\u001b[B", true) }
                ExtraKeyChip("←") { sendSpecial(if (appCur) "\u001bOD" else "\u001b[D", true) }
                ExtraKeyChip("→") { sendSpecial(if (appCur) "\u001bOC" else "\u001b[C", true) }
                ExtraKeyChip("粘贴") {
                    val cm = context.getSystemService(ClipboardManager::class.java)
                    val clip = cm?.primaryClip
                        ?.takeIf { it.itemCount > 0 }
                        ?.getItemAt(0)
                        ?.coerceToText(context)
                        ?.toString()
                        .orEmpty()
                    if (clip.isNotEmpty()) {
                        imeController.resetState()
                        val normalized = clip.replace("\r\n", "\r").replace("\n", "\r")
                        vm.sendTerminalText(normalized, useBracketedIfEnabled = true)
                    }
                }
                ExtraKeyChip("↵") { sendSpecial("\r", true) }
                ExtraKeyChip("键盘", highlighted = imeVisible) {
                    imeController.toggleKeyboard(imeVisible)
                }
            }
        }
    }
}

/** A chip of the session tab strip. Dim = a conversation not attached on this device. */
@OptIn(ExperimentalFoundationApi::class)
@Composable
private fun SessionChip(
    title: String,
    selected: Boolean,
    dim: Boolean,
    markdown: Boolean,
    onClick: () -> Unit,
    onLongClick: (() -> Unit)?,
    onClose: () -> Unit
) {
    Row(
        modifier = Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) Color(0xFF2B2F3C) else Color.Transparent)
            .border(
                1.dp,
                if (selected) Color(0xFF3B82F6) else Color(0xFF2C2F3A),
                RoundedCornerShape(999.dp)
            )
            .combinedClickable(onClick = onClick, onLongClick = onLongClick)
            .padding(horizontal = 10.dp, vertical = 5.dp),
        verticalAlignment = Alignment.CenterVertically
    ) {
        if (markdown) {
            Text(
                "M↓ ",
                color = Color(0xFF93C5FD),
                fontSize = 10.sp,
                fontFamily = FontFamily.Monospace,
                fontWeight = FontWeight.Bold
            )
        }
        Text(
            text = title,
            color = when {
                selected -> Color.White
                dim -> Color(0xFF5D626D)
                else -> Color(0xFF9CA3AF)
            },
            fontSize = 12.sp,
            maxLines = 1
        )
        Spacer(Modifier.width(6.dp))
        Icon(
            Icons.Default.Close,
            contentDescription = "关闭",
            tint = if (dim) Color(0xFF5D626D) else Color(0xFF8B909A),
            modifier = Modifier
                .size(13.dp)
                .clickable { onClose() }
        )
    }
}

@Composable
private fun ExtraKeyChip(
    label: String,
    highlighted: Boolean = false,
    onClick: () -> Unit
) {
    Box(
        modifier = Modifier
            .clip(RoundedCornerShape(7.dp))
            .background(if (highlighted) Color(0xFF2563EB) else Color(0xFF242731))
            .border(1.dp, Color(0xFF323644), RoundedCornerShape(7.dp))
            .clickable(onClick = onClick)
            .padding(horizontal = 9.dp, vertical = 4.dp)
    ) {
        Text(
            text = label,
            color = Color.White,
            fontSize = 11.sp,
            fontFamily = FontFamily.Monospace
        )
    }
}

@Composable
private fun MobileSettingsScreen(vm: WorkbenchViewModel) {
    val context = LocalContext.current
    val versionName = remember {
        runCatching { context.packageManager.getPackageInfo(context.packageName, 0).versionName }
            .getOrNull()
            .orEmpty()
    }
    var controlUrl by remember { mutableStateOf("") }
    var authKey by remember { mutableStateOf("") }
    var showTsDialog by remember { mutableStateOf(false) }
    var showAppearance by remember { mutableStateOf(false) }

    if (showAppearance) {
        AppearanceScreen(vm = vm, onBack = { showAppearance = false })
        return
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .verticalScroll(rememberScrollState())
            .padding(14.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        Text("设置", color = Color.White, fontSize = 19.sp, fontWeight = FontWeight.Bold)

        Surface(
            shape = RoundedCornerShape(18.dp),
            color = Color(0xFF1B1C22),
            modifier = Modifier
                .fillMaxWidth()
                .border(1.dp, Color(0xFF2C2F3A), RoundedCornerShape(18.dp))
        ) {
            Column {
                // 0. Terminal appearance (font, color scheme, size, cursor)
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { showAppearance = true }
                        .padding(15.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("终端外观", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                        Text(
                            text = "${FontCatalog.byId(vm.appearance.primaryFontId)?.name.orEmpty()} · ${vm.terminalScheme.name}",
                            color = Color(0xFF8B909A),
                            fontSize = 12.sp
                        )
                    }
                    Icon(
                        Icons.AutoMirrored.Filled.KeyboardArrowRight,
                        contentDescription = null,
                        tint = Color(0xFF8B909A),
                        modifier = Modifier.size(20.dp)
                    )
                }
                HorizontalDivider(color = Color(0xFF262934))

                // 1. Tailscale / Headscale tsnet
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { showTsDialog = true }
                        .padding(15.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("Tailscale / Headscale", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                        Text(
                            text = if (vm.tailnetStatus.running)
                                "已连接 (${vm.tailnetStatus.tailscaleIp.ifBlank { vm.tailnetStatus.backendState }})"
                            else "用户态 tsnet 免 root 直连内网机器",
                            color = Color(0xFF8B909A),
                            fontSize = 12.sp
                        )
                    }
                    Text(
                        text = if (vm.tailnetStatus.running) "已连接" else "配置",
                        color = if (vm.tailnetStatus.running) Color(0xFF4ADE80) else Color(0xFF60A5FA),
                        fontSize = 12.sp,
                        fontWeight = FontWeight.Medium
                    )
                }
                HorizontalDivider(color = Color(0xFF262934))

                // 2. Android Keystore
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(15.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("SSH 密钥与凭据安全", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                        Text("Android Keystore (AES-256-GCM) 硬件级加密存储", color = Color(0xFF8B909A), fontSize = 12.sp)
                    }
                    Text("${vm.secretCount} 项机密", color = Color(0xFF9CA3AF), fontSize = 12.sp)
                }
                HorizontalDivider(color = Color(0xFF262934))

                // 3. Claude Approval & Idle Push Notification
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .clickable { vm.triggerTestNotification() }
                        .padding(15.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("Claude 待审批与完工通知", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                        Text("前台服务保活推送，点击通知直达对应会话（点此发送测试通知）", color = Color(0xFF8B909A), fontSize = 12.sp)
                    }
                    Text("已开启", color = Color(0xFF4ADE80), fontSize = 12.sp, fontWeight = FontWeight.Medium)
                }
                HorizontalDivider(color = Color(0xFF262934))

                // 4. Privacy Mode
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(horizontal = 15.dp, vertical = 10.dp),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column(modifier = Modifier.weight(1f)) {
                        Text("隐私模式 (隐藏 IP 地址)", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
                        Text("在主机列表中自动脱敏服务器公网 IP", color = Color(0xFF8B909A), fontSize = 12.sp)
                    }
                    Switch(
                        checked = vm.privacyMaskIp,
                        onCheckedChange = { vm.togglePrivacyMask(it) }
                    )
                }
            }
        }

        Surface(
            shape = RoundedCornerShape(18.dp),
            color = Color(0xFF1B1C22),
            modifier = Modifier
                .fillMaxWidth()
                .border(1.dp, Color(0xFF2C2F3A), RoundedCornerShape(18.dp))
        ) {
            Column(modifier = Modifier.padding(15.dp)) {
                Text("Shnex Android v$versionName", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
                Spacer(Modifier.height(4.dp))
                Text(
                    "架构：Go 单核引擎 (127.0.0.1 进程内桥接) + Jetpack Compose M3 + 纯原生真彩终端与矢量公式渲染（零 WebView、零 GPL 依赖）",
                    color = Color(0xFF8B909A),
                    fontSize = 11.sp,
                    lineHeight = 17.sp
                )
            }
        }
    }

    if (showTsDialog) {
        AlertDialog(
            onDismissRequest = { showTsDialog = false },
            title = { Text("Tailscale / Headscale 组网", fontSize = 16.sp) },
            text = {
                Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = controlUrl,
                        onValueChange = { controlUrl = it },
                        label = { Text("Headscale 地址 (留空默认官方)") },
                        singleLine = true
                    )
                    OutlinedTextField(
                        value = authKey,
                        onValueChange = { authKey = it },
                        label = { Text("AuthKey (tskey-auth-...)") },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation()
                    )
                }
            },
            confirmButton = {
                if (vm.tailnetStatus.running) {
                    Button(
                        colors = ButtonDefaults.buttonColors(containerColor = Color(0xFFDC2626)),
                        onClick = {
                            showTsDialog = false
                            vm.toggleTailnet(false, "", "")
                        }
                    ) {
                        Text("停止组网")
                    }
                } else {
                    Button(
                        enabled = !vm.tailnetBusy,
                        onClick = {
                            showTsDialog = false
                            vm.toggleTailnet(true, controlUrl, authKey)
                        }
                    ) {
                        Text("启动连接")
                    }
                }
            },
            dismissButton = {
                TextButton(onClick = { showTsDialog = false }) { Text("关闭") }
            }
        )
    }
}

@Composable
private fun HostEditDialog(
    initial: HostItem?,
    onDismiss: () -> Unit,
    onSave: (
        name: String,
        host: String,
        port: Int,
        user: String,
        password: String,
        privateKeyPem: String,
        tmuxSession: String,
        useTsnet: Boolean,
        postLoginScript: String,
        onDone: (errorMessage: String?) -> Unit
    ) -> Unit
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val scrollState = rememberScrollState()

    var host by remember { mutableStateOf(initial?.host ?: "") }
    var portStr by remember { mutableStateOf((initial?.port ?: 22).toString()) }
    var name by remember { mutableStateOf(initial?.name ?: "") }
    var user by remember { mutableStateOf(initial?.user ?: "ubuntu") }
    var password by remember { mutableStateOf("") }
    var privateKey by remember { mutableStateOf("") }
    var tmuxSession by remember { mutableStateOf(initial?.tmuxSession ?: "main") }
    var useTsnet by remember { mutableStateOf(initial?.useTsnet ?: false) }
    var postScript by remember { mutableStateOf(initial?.postLoginScript ?: "") }

    var hostMissingError by remember { mutableStateOf(false) }
    var formError by remember { mutableStateOf<String?>(null) }
    var saving by remember { mutableStateOf(false) }

    val pemFilePicker = rememberLauncherForActivityResult(
        ActivityResultContracts.GetContent()
    ) { uri: Uri? ->
        if (uri != null) {
            runCatching {
                context.contentResolver.openInputStream(uri)?.use { stream ->
                    stream.bufferedReader(Charsets.UTF_8).readText()
                }
            }.onSuccess { content ->
                if (!content.isNullOrBlank()) {
                    privateKey = content.trim()
                    formError = null
                }
            }.onFailure { e ->
                formError = "读取私钥文件失败: ${e.message}"
            }
        }
    }

    AlertDialog(
        onDismissRequest = { if (!saving) onDismiss() },
        title = { Text(if (initial == null) "新建主机" else "编辑主机", fontSize = 16.sp) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                Column(
                    modifier = Modifier
                        .weight(1f, fill = false)
                        .verticalScroll(scrollState),
                    verticalArrangement = Arrangement.spacedBy(8.dp)
                ) {
                    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                        OutlinedTextField(
                            value = host,
                            onValueChange = {
                                host = it
                                if (it.isNotBlank()) hostMissingError = false
                                formError = null
                            },
                            label = { Text("主机地址 (必填 IP/域名) *") },
                            placeholder = { Text("如 108.137.19.108") },
                            isError = hostMissingError,
                            singleLine = true,
                            modifier = Modifier.weight(1f)
                        )
                        OutlinedTextField(
                            value = portStr,
                            onValueChange = { portStr = it },
                            label = { Text("端口") },
                            singleLine = true,
                            modifier = Modifier.width(82.dp)
                        )
                    }
                    OutlinedTextField(
                        value = name,
                        onValueChange = {
                            name = it
                            formError = null
                        },
                        label = { Text("备注名称 (可选，留空自动用地址)") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        value = user,
                        onValueChange = { user = it },
                        label = { Text("用户名") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        value = password,
                        onValueChange = { password = it },
                        label = {
                            Text(
                                if (initial?.hasSecret == true) "密码 / 私钥口令 (已保存，留空不变)"
                                else "密码 / 私钥口令 (存入 Keystore)"
                            )
                        },
                        singleLine = true,
                        visualTransformation = PasswordVisualTransformation(),
                        modifier = Modifier.fillMaxWidth()
                    )
                    Row(
                        modifier = Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.SpaceBetween,
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Text(
                            text = if (privateKey.contains("BEGIN ")) "已载入 PEM 私钥" else "SSH 私钥 (可选)",
                            color = if (privateKey.contains("BEGIN ")) Color(0xFF4ADE80) else Color(0xFF9CA3AF),
                            fontSize = 12.sp
                        )
                        TextButton(
                            onClick = { pemFilePicker.launch("*/*") },
                            contentPadding = PaddingValues(horizontal = 8.dp, vertical = 2.dp)
                        ) {
                            Icon(Icons.Default.Key, contentDescription = null, modifier = Modifier.size(14.dp))
                            Spacer(Modifier.width(4.dp))
                            Text("导入私钥文件 (.pem)", fontSize = 12.sp)
                        }
                    }
                    OutlinedTextField(
                        value = privateKey,
                        onValueChange = {
                            privateKey = it
                            formError = null
                        },
                        label = { Text("私钥 PEM (粘贴整段或点击上方导入文件)") },
                        maxLines = 3,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        value = tmuxSession,
                        onValueChange = { tmuxSession = it },
                        label = { Text("tmux 会话 (桌面/手机无缝接力，留空关闭)") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        value = postScript,
                        onValueChange = { postScript = it },
                        label = { Text("登录后执行命令 (可选)") },
                        maxLines = 2,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Checkbox(checked = useTsnet, onCheckedChange = { useTsnet = it })
                        Text("通过内置 Tailscale / Headscale 直连", fontSize = 12.sp)
                    }
                }

                formError?.let { errText ->
                    Surface(
                        color = Color(0xFF3B181E),
                        shape = RoundedCornerShape(10.dp),
                        modifier = Modifier
                            .fillMaxWidth()
                            .border(1.dp, Color(0xFFEF4444), RoundedCornerShape(10.dp))
                    ) {
                        Text(
                            text = errText,
                            color = Color(0xFFFCA5A5),
                            fontSize = 12.sp,
                            modifier = Modifier.padding(horizontal = 10.dp, vertical = 7.dp)
                        )
                    }
                }
            }
        },
        confirmButton = {
            Button(
                enabled = !saving,
                onClick = {
                    var rawHost = host.trim().removePrefix("ssh ").trim()
                    var resolvedUser = user.trim()
                    var resolvedPort = portStr.trim().toIntOrNull() ?: 22

                    // Fallback if user accidentally typed IP/domain/user@host into the Name field
                    if (rawHost.isEmpty() && (name.contains(".") || name.contains("@") || name.contains(":"))) {
                        rawHost = name.trim().removePrefix("ssh ").trim()
                    }

                    // Support pasting `ubuntu@108.137.19.108:22` directly
                    if (rawHost.contains("@")) {
                        val uPart = rawHost.substringBefore("@").trim()
                        if (uPart.isNotEmpty()) resolvedUser = uPart
                        rawHost = rawHost.substringAfter("@").trim()
                    }
                    if (rawHost.count { it == ':' } == 1) {
                        val pPart = rawHost.substringAfterLast(":").toIntOrNull()
                        if (pPart != null && pPart in 1..65535) {
                            resolvedPort = pPart
                            rawHost = rawHost.substringBeforeLast(":").trim()
                        }
                    }

                    if (rawHost.isEmpty()) {
                        hostMissingError = true
                        formError = "请填写服务器主机地址（如 IP 或域名）"
                        scope.launch { scrollState.animateScrollTo(0) }
                        return@Button
                    }

                    val trimmedKey = privateKey.trim()
                    if (trimmedKey.isNotEmpty() && !trimmedKey.contains("-----BEGIN ")) {
                        formError = "私钥内容不完整：缺少 -----BEGIN ... PRIVATE KEY----- 头部，请粘贴整段私钥或点击「导入私钥文件」"
                        return@Button
                    }

                    val cleanName = name.trim().ifEmpty { rawHost }
                    saving = true
                    formError = null
                    onSave(
                        cleanName,
                        rawHost,
                        resolvedPort,
                        resolvedUser.ifEmpty { "root" },
                        password,
                        trimmedKey,
                        tmuxSession.trim(),
                        useTsnet,
                        postScript
                    ) { errMsg ->
                        saving = false
                        if (errMsg != null) {
                            formError = "保存失败：$errMsg"
                        }
                    }
                }
            ) {
                if (saving) {
                    CircularProgressIndicator(
                        color = Color.White,
                        strokeWidth = 2.dp,
                        modifier = Modifier.size(15.dp)
                    )
                    Spacer(Modifier.width(6.dp))
                    Text("保存中")
                } else {
                    Text("保存")
                }
            }
        },
        dismissButton = {
            TextButton(enabled = !saving, onClick = onDismiss) { Text("取消") }
        }
    )
}

@Composable
private fun QuickConnectDialog(
    onDismiss: () -> Unit,
    onConnect: (expr: String, password: String) -> Unit
) {
    var expr by remember { mutableStateOf("") }
    var pw by remember { mutableStateOf("") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("快速连接 SSH", fontSize = 16.sp) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedTextField(
                    value = expr,
                    onValueChange = { expr = it },
                    label = { Text("user@example.com:22") },
                    singleLine = true
                )
                OutlinedTextField(
                    value = pw,
                    onValueChange = { pw = it },
                    label = { Text("密码") },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation()
                )
            }
        },
        confirmButton = {
            Button(onClick = { if (expr.isNotBlank()) onConnect(expr, pw) }) {
                Text("立即连接")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        }
    )
}

private fun queryUriDisplayName(context: android.content.Context, uri: Uri): String? {
    return runCatching {
        context.contentResolver.query(uri, null, null, null, null)?.use { cursor ->
            val idx = cursor.getColumnIndex(OpenableColumns.DISPLAY_NAME)
            if (idx >= 0 && cursor.moveToFirst()) cursor.getString(idx) else null
        }
    }.getOrNull()
}
