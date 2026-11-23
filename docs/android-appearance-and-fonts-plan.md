# Shnex Android 终端外观与内置 Maple Mono 字体方案（精简版）

> 范围：纯 Android 端改造（`mobile/android/`），**零 Go 核心改动、零新增 Gradle 依赖**。

---

## 1. 底层矛盾

手机终端「换不了外观」只有两处写死：

1. **颜色在解析期烘焙**：[`AnsiTerminalBuffer.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt) 解析 SGR 时把 16 色与默认前/背景直接写成 ARGB 存入 `TermCell`（`DEFAULT_FG` / `DEFAULT_BG` / `ansi16Color`），换配色时已有屏幕与滚屏无法跟着变。
2. **字体与排版写死**：[`TerminalCanvas.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/TerminalCanvas.kt) 写死 `Typeface.MONOSPACE`、行距 `1.12f`、背景 `#121316`、蓝色方块光标；双指缩放字号只存在 `remember` 里，切会话或重启即丢失；系统等宽字体没有 Nerd Font 图标（显示为豆腐块）。

解开这两处即可，**不需要重做整个设置体系**。

---

## 2. 不做什么（以及为什么）

| 原想法 | 隐性代价 | 处理 |
| --- | --- | --- |
| 内置 `Maple Mono NF CN` | 单字重约十几~二十 MB（官方 `MapleMono-NF-CN-unhinted.zip` 151.6 MB / 16 个字重），APK 与 Git 历史永久膨胀 | 改内置 **`Maple Mono NF`**（2.2 MB）；中文由系统 Noto CJK 自动回退。终端本就逐格绘制、宽字符固定占 2 格，字形宽度差异不会造成列错位 |
| 主字体 + 回退字体双选项、双 `Paint` + 每格 `hasGlyph` | 安卓 `drawText` 本就自动回退系统字体；每格每帧一次 JNI 调用 | 只保留一个「字体」选项 |
| 导入 TTF/OTF + 字体管理页 | 多一整页 UI；MIME 过滤不可靠、`createFromFile` 遇非法文件静默返回默认字体，需要自行校验 | 本期不做，只提供 `Maple Mono` / `系统等宽` 两项 |
| App 深色/浅色/跟随系统、壁纸取色、跟随系统时两行配色 | `MainActivity.kt` 约 100 处硬编码颜色要全部改造 | 本期只做**终端配色**，App 外壳保持深色 |
| 设置拆成 4 个二级页 | 现有设置只有 4 项 + 关于，拆开后每页 1~2 项，多一次点击 | 设置首页保持现状，只在顶部加一行「外观」 |
| 字间距 | 等宽终端缩小字间距会叠字，且与连字网格对齐冲突 | 不做 |

---

## 3. 方案

### 3.1 内置字体（已入库）

- `mobile/android/app/src/main/assets/fonts/MapleMono-NF-Regular.ttf`
  - 来源：[subframe7536/maple-font v7.9](https://github.com/subframe7536/maple-font/releases/tag/v7.9) 的 `MapleMono-NF-unhinted.zip`（官方 sha256 校验通过），2,299,276 字节。
  - 特性：带连字、Nerd Font 3.4 完整图标（非 Mono 版，图标可略宽于一格，属正常表现）。
  - 只打包 Regular，粗体继续用 `Paint.isFakeBoldText` 合成。
- `mobile/android/app/src/main/assets/fonts/OFL.txt`：原样附带的 SIL OFL 1.1 协议（协议要求随字体分发）。
- [`app/build.gradle.kts`](../mobile/android/app/build.gradle.kts) 的 `android { }` 中加入：
  ```kotlin
  androidResources { noCompress += "ttf" }
  ```
  字体在 APK 中不压缩，`Typeface.createFromAsset` 直接映射读取，不必先解压进内存。

### 3.2 外观模型（新增 `terminal/TerminalAppearance.kt`）

```kotlin
enum class TerminalFont { MAPLE_MONO, SYSTEM_MONO }
enum class CursorStyle { BLOCK, BAR, UNDERLINE }

class TerminalColorScheme(
    val id: String, val name: String,
    val bg: Int, val fg: Int, val cursor: Int,
    val ansi16: IntArray
)

data class TerminalAppearance(
    val font: TerminalFont = TerminalFont.MAPLE_MONO,
    val schemeId: String = "shnex_dark",
    val fontSizeSp: Float = 13f,        // 9f..22f，与双指缩放共用
    val lineHeightScale: Float = 1.12f, // 1.0f..1.5f
    val cursorStyle: CursorStyle = CursorStyle.BLOCK
)
```

- **内置 7 套配色**：`shnex_dark`（默认，严格保留现有 `#121316` / `#E5E7EB` 与现有 16 色）、`oled_black`、`solarized_dark`、`solarized_light`、`tokyo_night`、`gruvbox_dark`、`one_dark`。
- **持久化**：复用 `shnex_mobile_ui_prefs`，读取时 `coerceIn` 裁剪；未知 `schemeId` 回落 `shnex_dark`。
- **字体加载**：`TerminalFonts.typeface(context, font)`，进程内缓存；`createFromAsset` 异常时降级 `Typeface.MONOSPACE`，不崩溃。

### 3.3 语义化颜色（改造 `AnsiTerminalBuffer.kt`）

`TermCell.fg/bg` 存「语义码」而非 ARGB，绘制期再按当前配色解析：

- `COLOR_DEFAULT_FG = 0x00000000`、`COLOR_DEFAULT_BG = 0x00000001`
- `colorIndexed(idx) = 0x01000000 or (idx and 0xFF)`：`0..15` 查 `scheme.ansi16`，`16..255` 走 xterm-256 色立方 / 灰阶（`38;5;n` 的 `n < 16` 也查配色）
- 24-bit 真彩照旧存 `0xFFRRGGBB`（alpha 恒为 `0xFF`，与上面两类高位天然不冲突）
- 新增 `resolveColor(code: Int, scheme: TerminalColorScheme, isBg: Boolean): Int`
- 保留现有行为：`SGR 1` 之后的 `30..37` 映射到亮色索引 `+8`；`TermCell.reset(bgColor)` 的背景色擦除（BCE）同样存语义码。

### 3.4 渲染（改造 `TerminalCanvas.kt`）

- **新增入参**：`appearance`、`scheme`、`typeface`、`onFontSizeChanged: (Float) -> Unit`。
- **抽出纯绘制函数** `drawTerminalGrid(nc, snap, metrics, scheme, cursorStyle, ...)`：终端与外观页预览卡共用同一份代码，避免两套渲染逐渐走样。
- **网格指标**：`cellW = max(6f, paint.measureText("M"))`，`cellH = max(12f, (descent - ascent + leading) * lineHeightScale)`；`LaunchedEffect(sizePair, fontSp, lineHeightScale, typeface)` 重算行列并 `buffer.resize` + `onTerminalResize`。
- **颜色**：背景 `Box` 与单元格底色、文字色全部经 `resolveColor`；先解析再按 `inverse` 互换；仅 `!inverse && bg == COLOR_DEFAULT_BG` 时跳过底色 `drawRect`（浅色配色下反显依然正确）。
- **光标**：`BLOCK` 用 `scheme.cursor` 半透明整格；`BAR` 左侧 `2dp` 竖线；`UNDERLINE` 底部 `2dp` 横线。输入法预编辑区底色/文字色同样取自配色。
- **连字**：同一行内连续、同样式、单宽、可打印 ASCII 的单元格合并为一次 `drawText`，Maple Mono 的连字保持各字符原有步进宽度，因此与网格对齐不受影响；遇到宽字符、样式变化或空格即断开。
- **字号持久化**：双指缩放范围统一为 `9f..22f`，手势结束时调用一次 `onFontSizeChanged` 写入偏好，而非每帧写。

### 3.5 ViewModel（改造 `WorkbenchViewModel.kt`）

- `var appearance by mutableStateOf(store.load())`
- `val terminalScheme: TerminalColorScheme`（由 `appearance.schemeId` 派生）
- `fun terminalTypeface(): Typeface`
- `fun updateAppearance(transform: (TerminalAppearance) -> TerminalAppearance)`：更新状态并持久化

所有已打开会话共享同一份 `appearance`，修改后下一帧即生效，无需重连。

### 3.6 设置 UI

- **设置首页**（[`MainActivity.kt`](../mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt) `MobileSettingsScreen`）：顶部新增一行「外观」，副标题 `Maple Mono · Shnex 暗`，点击进入外观页；其余项不动。
- **外观页**（新增 `ui/AppearanceScreen.kt`，`BackHandler` 返回设置首页）：
  1. **预览卡**：用 `drawTerminalGrid` 渲染一个小 `AnsiTerminalBuffer`，内容为三行带颜色的示例（提示符 + `ls` 输出含 Nerd 图标与中文文件名 + `>= != && ->` 连字与 `0O 1lI` 易混字符）及 16 色块；任何参数变化实时联动。
  2. **字体**：`Maple Mono` / `系统等宽` 二选一。
  3. **配色方案**：7 套，每项带背景色 + 4 个代表色的小色条。
  4. **字号**（`9–22`）、**行距**（`1.0–1.5`）：滑块 + 数值读数；字号与双指缩放是同一个值。
  5. **光标**：`方块` / `竖线` / `下划线`。

---

## 4. 实施顺序与验收

### 4.1 顺序

1. 字体资源与 `noCompress`（字体已入库）。
2. `AnsiTerminalBuffer.kt` 语义颜色 → `TerminalAppearance.kt` → `TerminalCanvas.kt`（`drawTerminalGrid`、光标、连字、字号持久化）。
3. `WorkbenchViewModel.kt` 接入 → `AppearanceScreen.kt` → `MainActivity.kt` 增加入口与 `TerminalCanvasView` 接线。

### 4.2 验收

1. 本地执行 `powershell -NoProfile -ExecutionPolicy Bypass -File mobile\scripts\package-android.ps1 -Version <next> -SkipCoreBind` 退出码为 `0` 并产出 APK；APK 体积增量约 2.2 MB。
2. 首次安装即为 Maple Mono：Nerd 图标正常显示、`>=` / `!=` / `&&` / `->` 呈现连字，中文按 2 格对齐、列不错位。
3. 外观页切换配色（如 `Solarized 暗` ↔ `Solarized 浅`）、字体、行距、光标时，预览卡与已连接会话的当前屏幕和历史滚屏同步变化，无需重连；`vim` / `htop` 等全屏程序行列数随字号、行距正确重算。
4. 双指缩放后重启 App，字号保持；外观页字号滑块显示同一数值。
