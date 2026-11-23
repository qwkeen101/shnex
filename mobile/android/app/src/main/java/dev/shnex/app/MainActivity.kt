package dev.shnex.app

import android.Manifest
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
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
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
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Add
import androidx.compose.material.icons.filled.ArrowUpward
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.filled.Folder
import androidx.compose.material.icons.filled.Image
import androidx.compose.material.icons.filled.InsertDriveFile
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Speed
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
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.shnex.app.data.HostItem
import dev.shnex.app.service.ShnexRelayService
import dev.shnex.app.terminal.TerminalCanvasView
import dev.shnex.app.ui.BottomNavTab
import dev.shnex.app.ui.NativeMarkdownDocumentView
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

    Scaffold(
        modifier = Modifier
            .fillMaxSize()
            .background(Color(0xFF121316))
            .statusBarsPadding()
            .navigationBarsPadding()
            .imePadding(),
        containerColor = Color(0xFF121316),
        bottomBar = {
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
            onSave = { name, host, port, user, pw, pk, tmux, tsnet, script ->
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
                    postLoginScript = script
                )
                showHostDialog = false
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
            onDismissRequest = { vm.passwordPromptHost = null },
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
                Button(onClick = { vm.connectHost(targetHost, passwordOverride = pw) }) {
                    Text("连接")
                }
            },
            dismissButton = {
                TextButton(onClick = { vm.passwordPromptHost = null }) {
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
                                        Icon(Icons.Default.InsertDriveFile, null, tint = Color(0xFF9CA3AF), modifier = Modifier.size(18.dp))
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
            Row(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(bottom = 12.dp),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically
            ) {
                Column {
                    Text("Shnex 接力主机", color = Color.White, fontSize = 19.sp, fontWeight = FontWeight.Bold)
                    Text(
                        text = "${vm.hosts.size} 台已存主机  |  ${vm.tabs.count { !it.isMarkdown }} 个活跃会话",
                        color = Color(0xFF8B909A),
                        fontSize = 12.sp
                    )
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    TextButton(onClick = onOpenMathDemo) {
                        Text("M↓ 公式演示", color = Color(0xFF93C5FD), fontSize = 12.sp)
                    }
                    IconButton(onClick = { vm.pingHosts() }) {
                        Icon(Icons.Default.Speed, contentDescription = "测速", tint = Color(0xFF9CA3AF))
                    }
                }
            }

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
                                                Text("连接中...", color = Color(0xFF60A5FA), fontSize = 11.sp)
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
    onPickImageForClaude: () -> Unit,
    onOpenMathDemo: () -> Unit
) {
    val active = vm.activeTab
    var inputText by remember { mutableStateOf("") }
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
                for (t in vm.tabs) {
                    val selected = t.tabId == active.tabId
                    Row(
                        modifier = Modifier
                            .clip(RoundedCornerShape(999.dp))
                            .background(if (selected) Color(0xFF2B2F3C) else Color.Transparent)
                            .border(
                                1.dp,
                                if (selected) Color(0xFF3B82F6) else Color(0xFF2C2F3A),
                                RoundedCornerShape(999.dp)
                            )
                            .clickable { vm.activeTabId = t.tabId }
                            .padding(horizontal = 10.dp, vertical = 5.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        if (t.isMarkdown) {
                            Text(
                                "M↓ ",
                                color = Color(0xFF93C5FD),
                                fontSize = 10.sp,
                                fontFamily = FontFamily.Monospace,
                                fontWeight = FontWeight.Bold
                            )
                        }
                        Text(
                            text = t.title,
                            color = if (selected) Color.White else Color(0xFF9CA3AF),
                            fontSize = 12.sp,
                            maxLines = 1
                        )
                        Spacer(Modifier.width(6.dp))
                        Icon(
                            Icons.Default.Close,
                            contentDescription = "关闭",
                            tint = Color(0xFF8B909A),
                            modifier = Modifier
                                .size(13.dp)
                                .clickable { vm.closeTab(t) }
                        )
                    }
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

        // 2. Center Stage: Native Markdown Math Sheet OR TrueColor Terminal Canvas
        if (active.isMarkdown && active.markdownDoc != null) {
            NativeMarkdownDocumentView(
                doc = active.markdownDoc,
                modifier = Modifier.weight(1f)
            )
        } else {
            TerminalCanvasView(
                buffer = active.buffer,
                renderTick = active.renderTick,
                onTerminalResize = { cols, rows ->
                    active.socket?.sendResize(cols, rows)
                },
                modifier = Modifier.weight(1f)
            )

            // 3. Contextual Slim Claude Approval Strip (Matches mobile_m3_preview.html!)
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

            // 4. Native Completion Strip (Powered by core/completion!)
            if (vm.completionItems.isNotEmpty()) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .background(Color(0xFF171920))
                        .horizontalScroll(rememberScrollState())
                        .padding(horizontal = 8.dp, vertical = 4.dp),
                    horizontalArrangement = Arrangement.spacedBy(6.dp)
                ) {
                    for (cand in vm.completionItems) {
                        Box(
                            modifier = Modifier
                                .clip(RoundedCornerShape(8.dp))
                                .background(Color(0xFF252834))
                                .clickable {
                                    val keepLen = (inputText.length - cand.replaceLen).coerceAtLeast(0)
                                    inputText = inputText.take(keepLen) + cand.insertText
                                    vm.queryInputCompletions(inputText)
                                }
                                .padding(horizontal = 9.dp, vertical = 4.dp)
                        ) {
                            Text(
                                text = cand.label,
                                color = Color(0xFFE5E7EB),
                                fontSize = 12.sp,
                                fontFamily = FontFamily.Monospace
                            )
                        }
                    }
                }
            }

            // 5. Slim Extra Key Row + Chinese/Voice-Friendly Input Bar (Matches mobile_m3_preview.html!)
            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .background(Color(0xFF1B1C22))
                    .padding(horizontal = 8.dp, vertical = 6.dp),
                verticalArrangement = Arrangement.spacedBy(6.dp)
            ) {
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .horizontalScroll(rememberScrollState()),
                    horizontalArrangement = Arrangement.spacedBy(5.dp)
                ) {
                    val appCur = active.buffer.appCursorKeys
                    ExtraKeyChip("Esc") { vm.sendTerminalText("\u001b") }
                    ExtraKeyChip("Tab") { vm.sendTerminalText("\t") }
                    ExtraKeyChip("S-Tab") { vm.sendTerminalText("\u001b[Z") }
                    ExtraKeyChip("Ctrl", highlighted = ctrlArmed) { ctrlArmed = !ctrlArmed }
                    ExtraKeyChip("^C") { vm.sendTerminalText("\u0003") }
                    ExtraKeyChip("↑") { vm.sendTerminalText(if (appCur) "\u001bOA" else "\u001b[A") }
                    ExtraKeyChip("↓") { vm.sendTerminalText(if (appCur) "\u001bOB" else "\u001b[B") }
                    ExtraKeyChip("←") { vm.sendTerminalText(if (appCur) "\u001bOD" else "\u001b[D") }
                    ExtraKeyChip("→") { vm.sendTerminalText(if (appCur) "\u001bOC" else "\u001b[C") }
                }

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp)
                ) {
                    BasicTextField(
                        value = inputText,
                        onValueChange = { next ->
                            if (ctrlArmed && next.length == inputText.length + 1) {
                                val ch = next.last().lowercaseChar()
                                if (ch in 'a'..'z') {
                                    val code = (ch.code - 'a'.code + 1).toChar()
                                    vm.sendTerminalText(code.toString())
                                    ctrlArmed = false
                                    return@BasicTextField
                                }
                            }
                            inputText = next
                            vm.queryInputCompletions(next)
                        },
                        singleLine = true,
                        textStyle = TextStyle(
                            color = Color.White,
                            fontSize = 13.sp,
                            fontFamily = FontFamily.Monospace
                        ),
                        cursorBrush = SolidColor(Color(0xFF60A5FA)),
                        keyboardOptions = KeyboardOptions(imeAction = ImeAction.Send),
                        keyboardActions = KeyboardActions(
                            onSend = {
                                if (inputText.isNotEmpty()) {
                                    vm.sendTerminalText("$inputText\r")
                                    inputText = ""
                                    vm.completionItems.clear()
                                } else {
                                    vm.sendTerminalText("\r")
                                }
                            }
                        ),
                        decorationBox = { innerField ->
                            Box(
                                modifier = Modifier
                                    .fillMaxWidth()
                                    .background(Color(0xFF121316), RoundedCornerShape(10.dp))
                                    .border(1.dp, Color(0xFF2E313D), RoundedCornerShape(10.dp))
                                    .padding(horizontal = 10.dp, vertical = 8.dp)
                            ) {
                                if (inputText.isEmpty()) {
                                    Text(
                                        "直接敲命令，或中文/语音输入...",
                                        color = Color(0xFF6B7280),
                                        fontSize = 12.sp
                                    )
                                }
                                innerField()
                            }
                        },
                        modifier = Modifier.weight(1f)
                    )

                    Surface(
                        color = Color(0xFFF3F4F6),
                        shape = RoundedCornerShape(10.dp),
                        modifier = Modifier
                            .clickable {
                                if (inputText.isNotEmpty()) {
                                    vm.sendTerminalText("$inputText\r")
                                    inputText = ""
                                    vm.completionItems.clear()
                                } else {
                                    vm.sendTerminalText("\r")
                                }
                            }
                    ) {
                        Text(
                            text = "↵",
                            color = Color(0xFF121316),
                            fontWeight = FontWeight.Bold,
                            fontSize = 15.sp,
                            modifier = Modifier.padding(horizontal = 13.dp, vertical = 6.dp)
                        )
                    }
                }
            }
        }
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
    var controlUrl by remember { mutableStateOf("") }
    var authKey by remember { mutableStateOf("") }
    var showTsDialog by remember { mutableStateOf(false) }

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
                Text("Shnex Android v0.3.3", color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.SemiBold)
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
        postLoginScript: String
    ) -> Unit
) {
    var name by remember { mutableStateOf(initial?.name ?: "") }
    var host by remember { mutableStateOf(initial?.host ?: "") }
    var portStr by remember { mutableStateOf((initial?.port ?: 22).toString()) }
    var user by remember { mutableStateOf(initial?.user ?: "root") }
    var password by remember { mutableStateOf("") }
    var privateKey by remember { mutableStateOf("") }
    var tmuxSession by remember { mutableStateOf(initial?.tmuxSession ?: "main") }
    var useTsnet by remember { mutableStateOf(initial?.useTsnet ?: false) }
    var postScript by remember { mutableStateOf(initial?.postLoginScript ?: "") }

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(if (initial == null) "新建主机" else "编辑主机", fontSize = 16.sp) },
        text = {
            Column(
                modifier = Modifier.verticalScroll(rememberScrollState()),
                verticalArrangement = Arrangement.spacedBy(8.dp)
            ) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("名称 (如 aws / 新加坡)") },
                    singleLine = true
                )
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedTextField(
                        value = host,
                        onValueChange = { host = it },
                        label = { Text("主机地址") },
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
                    value = user,
                    onValueChange = { user = it },
                    label = { Text("用户名") },
                    singleLine = true
                )
                OutlinedTextField(
                    value = password,
                    onValueChange = { password = it },
                    label = {
                        Text(
                            if (initial?.hasSecret == true) "密码 (已加密保存，留空不变)"
                            else "密码 (存入 Keystore)"
                        )
                    },
                    singleLine = true,
                    visualTransformation = PasswordVisualTransformation()
                )
                OutlinedTextField(
                    value = privateKey,
                    onValueChange = { privateKey = it },
                    label = { Text("私钥 PEM (可选，粘贴整段私钥)") },
                    maxLines = 3
                )
                OutlinedTextField(
                    value = tmuxSession,
                    onValueChange = { tmuxSession = it },
                    label = { Text("tmux 会话 (桌面/手机无缝接力，留空关闭)") },
                    singleLine = true
                )
                OutlinedTextField(
                    value = postScript,
                    onValueChange = { postScript = it },
                    label = { Text("登录后执行命令 (可选)") },
                    maxLines = 2
                )
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Checkbox(checked = useTsnet, onCheckedChange = { useTsnet = it })
                    Text("通过内置 Tailscale / Headscale 直连", fontSize = 12.sp)
                }
            }
        },
        confirmButton = {
            Button(
                onClick = {
                    val cleanHost = host.trim()
                    if (cleanHost.isEmpty()) return@Button
                    val cleanName = name.trim().ifEmpty { cleanHost }
                    onSave(
                        cleanName,
                        cleanHost,
                        portStr.toIntOrNull() ?: 22,
                        user.trim().ifEmpty { "root" },
                        password,
                        privateKey,
                        tmuxSession.trim(),
                        useTsnet,
                        postScript
                    )
                }
            ) {
                Text("保存")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
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
