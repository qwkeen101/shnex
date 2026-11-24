package dev.shnex.app.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.shnex.app.terminal.FontCatalog
import dev.shnex.app.terminal.FontEntry

/**
 * 字体 page: every font is a card; installed fonts can be set as 主字体 or 回退, downloadable
 * ones show their size and download in place.
 */
@Composable
fun FontStoreScreen(vm: WorkbenchViewModel, onBack: () -> Unit) {
    BackHandler(onBack = onBack)
    val a = vm.appearance

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
            Text("字体", color = Color.White, fontSize = 19.sp, fontWeight = FontWeight.Bold)
        }
        Text(
            "中文以及主字体缺少的字形（如 Nerd 图标）由回退字体显示",
            color = Color(0xFF8B909A),
            fontSize = 12.sp,
            modifier = Modifier.padding(horizontal = 4.dp)
        )

        FontCatalog.all.forEach { entry ->
            FontCard(
                entry = entry,
                installed = entry.id in vm.installedFontIds,
                progress = vm.fontDownloads[entry.id],
                isPrimary = a.primaryFontId == entry.id,
                isFallback = a.fallbackFontId == entry.id,
                onPrimary = { vm.selectPrimaryFont(entry.id) },
                onFallback = { vm.selectFallbackFont(entry.id) },
                onDownload = { vm.downloadFont(entry) },
                onDelete = { vm.deleteFont(entry) }
            )
        }
    }
}

@Composable
private fun FontCard(
    entry: FontEntry,
    installed: Boolean,
    progress: Float?,
    isPrimary: Boolean,
    isFallback: Boolean,
    onPrimary: () -> Unit,
    onFallback: () -> Unit,
    onDownload: () -> Unit,
    onDelete: () -> Unit
) {
    Surface(
        shape = RoundedCornerShape(18.dp),
        color = Color(0xFF1B1C22),
        modifier = Modifier
            .fillMaxWidth()
            .border(1.dp, Color(0xFF2C2F3A), RoundedCornerShape(18.dp))
    ) {
        Column(modifier = Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
            Row(verticalAlignment = Alignment.Top) {
                Column(modifier = Modifier.weight(1f)) {
                    Text(entry.name, color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.Bold)
                    Text(entry.subtitle, color = Color(0xFF8B909A), fontSize = 12.sp, fontFamily = FontFamily.Monospace)
                }
                Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                    entry.tags.forEach { tag ->
                        Box(
                            modifier = Modifier
                                .clip(RoundedCornerShape(8.dp))
                                .background(Color(0xFF2B2F3C))
                                .padding(horizontal = 8.dp, vertical = 4.dp)
                        ) {
                            Text(tag, color = Color(0xFFB4B8C0), fontSize = 11.sp)
                        }
                    }
                }
            }
            if (entry.description.isNotEmpty()) {
                Text(entry.description, color = Color(0xFFB4B8C0), fontSize = 12.sp)
            }
            Spacer(Modifier.padding(top = 2.dp))
            Row(
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically
            ) {
                when {
                    installed -> {
                        CardButton("主字体", selected = isPrimary, onClick = onPrimary)
                        CardButton("回退", selected = isFallback, onClick = onFallback)
                        if (entry.downloadable) {
                            Spacer(Modifier.weight(1f))
                            Text(
                                "删除",
                                color = Color(0xFFF87171),
                                fontSize = 13.sp,
                                modifier = Modifier
                                    .clip(RoundedCornerShape(8.dp))
                                    .clickable(onClick = onDelete)
                                    .padding(horizontal = 10.dp, vertical = 8.dp)
                            )
                        }
                    }
                    progress != null -> CardButton("下载中 ${(progress * 100).toInt()}%", selected = false, onClick = null)
                    else -> CardButton("下载 · ${formatBytes(entry.downloadBytes)}", selected = false, onClick = onDownload)
                }
            }
        }
    }
}

@Composable
private fun CardButton(label: String, selected: Boolean, onClick: (() -> Unit)?) {
    val shape = RoundedCornerShape(12.dp)
    var m = Modifier
        .clip(shape)
        .background(if (selected) Color(0xFF2563EB) else Color.Transparent)
        .border(1.dp, if (selected) Color(0xFF3B82F6) else Color(0xFF3A3F4B), shape)
    if (onClick != null) m = m.clickable(onClick = onClick)
    Box(modifier = m.padding(horizontal = 16.dp, vertical = 8.dp)) {
        Text(
            label,
            color = if (selected) Color.White else if (onClick == null) Color(0xFF8B909A) else Color(0xFFD1D5DB),
            fontSize = 13.sp,
            fontWeight = FontWeight.Medium
        )
    }
}

private fun formatBytes(bytes: Long): String =
    if (bytes >= 1_000_000L) "%.1f MB".format(bytes / 1_000_000.0) else "${(bytes + 999) / 1000} KB"
