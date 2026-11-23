# Shnex Android 设置重构、外观定制与内置 Maple Mono NF CN 字体方案

> 范围：纯 Android 端改造（`mobile/android/`），**零 Go 核心改动、零新增 Gradle 依赖**（`material3` 与 `material-icons-extended` 已内置于 [build.gradle.kts](../mobile/android/app/build.gradle.kts)）。

---

## 1. 底层矛盾与设计原则

### 1.1 当前架构瓶颈
1. **终端渲染写死，无法响应外观变更**：[`TerminalCanvas.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/TerminalCanvas.kt#L611-L624) 写死了系统 `Typeface.MONOSPACE`、固定行距 `1.12f`、无字间距系数、写死的 `#121316` 背景与蓝色方块光标；双指缩放字号仅存在局部 `remember` 状态中，切换会话或重启即丢失；安卓系统默认等宽字体在中英混排与 Nerd Font 图标下无法严格 2:1 对齐。
2. **颜色在解析期烘焙，无法实时换肤**：[`AnsiTerminalBuffer.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt#L551-L571) 在解析 ANSI SGR 时直接将 16 色与默认前/背景写成固定 ARGB 整数存入 `TermCell`，导致切换配色方案时已渲染屏幕与历史滚屏无法跟着变色。
3. **设置扁平堆叠**：[`MainActivity.kt`](../mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt#L999-L1120) 中所有配置都塞在一页，后续外观、排版、字体与终端参数无法自然扩展。

### 1.2 「少即是多」取舍（对照参考图，不搬冗余设计）
- **不做空壳入口**：不放无后端对应的占位项（如「同步 未设置」「语言」「代理 0 个」），设置首页只保留真实可用的 4 组入口。
- **不做在线字体下载商店**：避免外部字体源不可达、弱网失败与校验维护成本。改为 **APK 直接内置 `Maple Mono NF CN`（Regular，OFL-1.1，unhinted）** 作为默认主字体与默认回退字体，开箱即享中英严格 2:1 等宽、Nerd 图标与编程连字；同时保留 `系统等宽` 与本地 `.ttf` / `.otf` 离线导入。
- **不常驻三行重复配色开关**：仅在界面主题为「跟随系统」时同时展开「配色方案 · 深色模式」与「配色方案 · 浅色模式」；主题固定为「深色」或「浅色」时只展示一行「配色方案」。
- **网格安全排版**：字间距通过缩放等宽单元格宽度 `cellW`（而不是修改 `Paint.letterSpacing`）实现，并与字号、行距共同触发真实的 `buffer.resize(cols, rows)` 与 `onTerminalResize(cols, rows)`，杜绝终端列错位。

---

## 2. 架构与文件规划

### 2.1 内置字体资源
- **新增资源**：`mobile/android/app/src/main/assets/fonts/MapleMono-NF-CN-Regular.ttf`
  - **来源**：官方 `subframe7536/maple-font` 发布的 `MapleMono-NF-CN-unhinted.zip`（OFL-1.1 开源协议，unhinted 在高分屏渲染更饱满）。
  - **体积控制**：仅打包 Regular 字重，粗体通过终端绘制 `Paint.isFakeBoldText = cell.bold` 合成，控制 APK 增量在单字体文件范围内。
  - **仓库检查**：根目录 [`.gitignore`](../.gitignore) 未屏蔽 `*.ttf` / `*.otf`，可直接纳入源码或由构建流程放入 `assets/fonts/`。

### 2.2 终端外观配置与配色体系（新增）
- **新增文件**：`mobile/android/app/src/main/java/dev/shnex/app/terminal/TerminalAppearance.kt`
- **核心模型**：
  - `enum class AppThemeMode { DARK, LIGHT, SYSTEM }`
  - `enum class CursorStyle { BLOCK, BAR, UNDERLINE }`
  - `data class TerminalColorScheme(val id: String, val name: String, val isDark: Boolean, val bg: Int, val fg: Int, val cursor: Int, val ansi16: IntArray)`
  - `data class TerminalAppearanceConfig(...)`：
    - `themeMode: AppThemeMode = AppThemeMode.DARK`
    - `dynamicColor: Boolean = false`（壁纸取色，仅 Android 12+ 生效）
    - `primaryFontId: String = FontManager.ID_MAPLE_MONO_NF_CN`
    - `fallbackFontId: String = FontManager.ID_MAPLE_MONO_NF_CN`
    - `fontSizeSp: Float = 14f`（范围 `9f..24f`，步进 `0.5f`）
    - `lineHeightScale: Float = 1.12f`（范围 `1.0f..1.5f`，步进 `0.05f`）
    - `letterSpacingScale: Float = 1.0f`（范围 `0.9f..1.2f`，步进 `0.02f`）
    - `darkSchemeId: String = "shnex_dark"`
    - `lightSchemeId: String = "solarized_light"`
    - `cursorStyle: CursorStyle = CursorStyle.BLOCK`
- **内置 7 套经典配色**：
  1. `shnex_dark`（**Shnex 暗**，默认，严格保留现有 `#121316` 背景、`#E5E7EB` 前景与现有 16 色）
  2. `oled_black`（**OLED 纯黑**，`#000000` 纯黑省电背景）
  3. `solarized_dark`（**Solarized 暗**，`#002B36` 背景）
  4. `solarized_light`（**Solarized 浅**，`#FDF6E3` 背景）
  5. `tokyo_night`（**Tokyo Night**，`#1A1B26` 背景）
  6. `gruvbox_dark`（**Gruvbox 暗**，`#282828` 背景）
  7. `one_dark`（**One Dark**，`#282C34` 背景）
- **持久化 `TerminalAppearanceStore`**：复用 `shnex_mobile_ui_prefs` `SharedPreferences`，读写全字段并做边界裁剪（`coerceIn`）。

### 2.3 字体管理器（新增）
- **新增文件**：`mobile/android/app/src/main/java/dev/shnex/app/terminal/FontManager.kt`
- **内置条目**：
  - `maple_mono_nf_cn`：`Maple Mono · 中英等宽`（`Maple Mono NF CN · OFL`，标签 `内置` / `含中文` / `连字`，通过 `Typeface.createFromAsset(context.assets, "fonts/MapleMono-NF-CN-Regular.ttf")` 加载；若资源缺失则安全降级 `Typeface.MONOSPACE` 不崩溃）。
  - `system_mono`：`系统等宽`（`Typeface.MONOSPACE`，标签 `内置`）。
- **自定义字体导入与管理**：
  - 字体文件目录：`context.filesDir/fonts/`，元数据存于 `SharedPreferences`（`id`、`displayName`、`fileName`、`sizeBytes`）。
  - `importFont(uri: Uri): Result<FontEntry>`：校验扩展名（`.ttf` / `.otf`），通过 `ContentResolver` 拷入私有目录并用 `Typeface.createFromFile` 试加载校验；非合法字体文件立即删除临时文件并返回明确错误信息。
  - `deleteFont(id: String)`：删除私有目录文件并清理元数据；若删除的字体当前正被主字体或回退字体引用，自动回退为 `maple_mono_nf_cn`。
  - 内部维护 `ConcurrentHashMap<String, Typeface>` 缓存，绘制阶段零重复 IO。

### 2.4 语义化颜色缓冲（改造）
- **修改文件**：[`mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/AnsiTerminalBuffer.kt)
- **语义编码（零对象分配的 32-bit `Int` 标签）**：
  - `COLOR_DEFAULT_FG = 0x00000000`
  - `COLOR_DEFAULT_BG = 0x00000001`
  - `colorAnsi(idx: Int) = 0x01000000 or (idx and 0xFF)`（`0..15` 在绘制期查当前 `TerminalColorScheme.ansi16`；`16..255` 走 xterm-256 6×6×6 色立方与灰阶）
  - `colorTrueRgb(r, g, b) = (0xFF shl 24) or (r shl 16) or (g shl 8) or b`（24-bit TrueColor 直接透传，alpha=`0xFF` 与上面 `0x00`/`0x01` 高位天然正交）
- **实现注意点**：
  - `TermCell` 默认 `fg = COLOR_DEFAULT_FG`、`bg = COLOR_DEFAULT_BG`，`SGR` 解析时写入语义码而非写死 ARGB。
  - 提供 `resolveFgColor(encoded: Int, scheme: TerminalColorScheme): Int` 与 `resolveBgColor(encoded: Int, scheme: TerminalColorScheme): Int`。
  - **反显（Inverse）与浅色背景边界**：绘制时先将 `cell.fg` / `cell.bg` 解析为实际 ARGB 再按 `cell.inverse` 互换；仅在 `!cell.inverse && cell.bg == COLOR_DEFAULT_BG` 时跳过单元格底色 `drawRect`，保证在浅色主题（如 Solarized 浅）下反显光标与选中块颜色完全准确。

### 2.5 双字体网格、排版与多光标渲染（改造）
- **修改文件**：[`mobile/android/app/src/main/java/dev/shnex/app/terminal/TerminalCanvas.kt`](../mobile/android/app/src/main/java/dev/shnex/app/terminal/TerminalCanvas.kt)
- **新增入参**：
  - `appearance: TerminalAppearanceConfig`
  - `scheme: TerminalColorScheme`
  - `primaryTypeface: Typeface`
  - `fallbackTypeface: Typeface`
  - `onFontSizeChanged: (Float) -> Unit`（双指缩放手势结束或缩放变化后同步持久化字号，重启不丢失）
- **双 `Paint` 字形回退**：
  - 同时维护 `primaryPaint`（应用 `primaryTypeface`）与 `fallbackPaint`（应用 `fallbackTypeface`）。
  - 单元格绘制时，当 `primaryTypeface != fallbackTypeface` 且 `!primaryPaint.hasGlyph(cell.text)` 时自动切到 `fallbackPaint`（API 23+ 原生支持，本项目 `minSdk = 26`）；宽字符（`cell.width == 2`）严格占据 `2 * cellW` 网格槽位。
- **网格指标与 Resize**：
  - `cellW = max(6f, primaryPaint.measureText("M") * appearance.letterSpacingScale)`
  - `cellH = max(12f, (fm.descent - fm.ascent + fm.leading) * appearance.lineHeightScale)`
  - `LaunchedEffect(sizePair, fontSp, appearance.lineHeightScale, appearance.letterSpacingScale)` 自动重算 `cols` / `rows` 并同步远端 PTY 尺寸。
- **3 种光标样式 + 连字绘制优化**：
  - `BLOCK`：使用 `scheme.cursor` 叠加合适透明度绘制整格方块。
  - `BAR`：左侧绘制 `2.5f * density.density` 宽度的竖线。
  - `UNDERLINE`：底部绘制 `2.5f * density.density` 高度的下划线。
  - **连字支持（可选增强）**：对同一行内相邻、同样式、同用 `primaryPaint` 的 ASCII 可见字符段合并一次 `nc.drawText(run, startX, baseY, primaryPaint)`，当 `letterSpacingScale == 1.0f` 时既保持严格等宽格对齐，又能原生触发 Maple Mono 的 `>=`、`!=`、`&&`、`->` 编程连字。

### 2.6 ViewModel 状态接入（改造）
- **修改文件**：[`mobile/android/app/src/main/java/dev/shnex/app/ui/WorkbenchViewModel.kt`](../mobile/android/app/src/main/java/dev/shnex/app/ui/WorkbenchViewModel.kt)
- 新增 `FontManager` 与 `TerminalAppearanceStore` 持有，对外暴露：
  - `var appearance by mutableStateOf(appearanceStore.load())`
  - `val fonts = mutableStateListOf<FontEntry>()`
  - `activeColorScheme(isSystemDark: Boolean): TerminalColorScheme`
  - `primaryTypeface(): Typeface`、`fallbackTypeface(): Typeface`
  - `updateAppearance(transform: (TerminalAppearanceConfig) -> TerminalAppearanceConfig)`
  - `importFont(uri: Uri)` / `deleteFont(id: String)`

### 2.7 设置层级与外观 / 字体管理 UI（新增与改造）
- **新增文件**：`mobile/android/app/src/main/java/dev/shnex/app/ui/SettingsScreens.kt`
- **修改文件**：[`mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt`](../mobile/android/app/src/main/java/dev/shnex/app/MainActivity.kt)
- **设置内部导航（`SettingsRoute`）**：`HOME`、`APPEARANCE`、`FONTS`、`TERMINAL_INPUT`、`NETWORK_SECURITY`、`ABOUT`，使用 `BackHandler(enabled = route != HOME)` 响应系统返回键。
- **设置首页（参考图 4）**：
  1. **外观**：副标题实时展示 `<主字体短名> · <当前配色名>`（如 `Maple Mono · Shnex 暗`）
  2. **终端与输入**：隐私模式隐藏 IP、Claude 待审批测试通知（以及后续输入辅助开关）
  3. **网络与安全**：Tailscale / Headscale 组网配置、Android Keystore 凭据计数
  4. **关于**：版本与单核架构说明
- **外观二级页（参考图 1、图 3）**：
  - **顶部固定实时预览卡（`TerminalPreviewCard`）**：使用与终端一致的 `TerminalColorScheme`、`primaryTypeface`、`fallbackTypeface`、字号/行距/字间距渲染 3 行示例文本（`user@shnex:~/app$ ls`、`src  README.md  deploy.sh  笔记.md`、`if x >= 0 && x != n { }  你好  001lI`）及下方 2×8 16 色 ANSI 色块；调节任意参数实时无延迟联动刷新。
  - **界面**：主题（`深色` / `浅色` / `跟随系统`）；壁纸取色开关（仅 `Build.VERSION.SDK_INT >= Build.VERSION_CODES.S` 时显示，低版本自动隐藏）。
  - **字体**：主字体选择、中文/回退字体选择、`字体管理` 入口。
  - **排版**：字号（`9–24`）、行距（`1.0–1.5`）、字间距（`0.9–1.2`），统一复用 `SliderRow` 组件（`-` 步进、滑块、`+` 步进、等宽数值读数）。
  - **配色与光标**：仅「跟随系统」展开「配色方案 · 深色模式」与「配色方案 · 浅色模式」，否则显示单行「配色方案」；光标样式（`方块` / `竖线` / `下划线`）。
- **字体管理二级页（参考图 2）**：
  - 顶部主按钮 `导入字体 (TTF / OTF)`（通过 `ActivityResultContracts.OpenDocument` 打开，过滤 `font/ttf`、`font/otf`、`application/octet-stream`）。
  - 字体卡片列表：展示名称、字体副标题/来源协议、标签徽章（`内置` / `含中文` / `连字` / `已导入`）、简要说明，以及高亮切换状态的 `主字体`、`回退` 按钮与导入字体的 `删除` 操作。
- **App 主题与系统栏联动**（`MainActivity.kt`）：
  - 根据 `vm.appearance.themeMode`、`isSystemInDarkTheme()` 与 `dynamicColor`（API 31+ `dynamicDarkColorScheme` / `dynamicLightColorScheme`）切换 Material 3 `colorScheme`；使用 `MaterialTheme.colorScheme` 替换 `MainActivity.kt` 硬编码深色，保证浅色模式下设置/主机页可读；通过 `WindowCompat.getInsetsController` 同步状态栏与导航栏图标明暗。

---

## 3. 分阶段实施顺序与验收标准

### 3.1 实施顺序
1. **Phase 1（底座与字体）**：放入 `MapleMono-NF-CN-Regular.ttf` → 改造 `AnsiTerminalBuffer.kt` 语义颜色 → 新增 `TerminalAppearance.kt` 与 `FontManager.kt` → 升级 `TerminalCanvas.kt`（双 Paint、网格排版、3 种光标、缩放持久化）。
2. **Phase 2（ViewModel 与设置 UI）**：扩展 `WorkbenchViewModel.kt` → 新建 `SettingsScreens.kt`（设置首页、外观页+预览卡、字体管理页、子页）→ 更新 `MainActivity.kt` 主题联动与 `TerminalCanvasView` 接线。

### 3.2 验收标准
1. **编译打包通过**：在 Windows 本地执行 `powershell -NoProfile -ExecutionPolicy Bypass -File mobile\scripts\package-android.ps1 -Version <next> -SkipCoreBind`，退出码为 `0` 并产出目标 APK。
2. **开箱体验**：首次安装打开即可见 Maple Mono NF CN 渲染效果，中英 2:1 严格对齐、Nerd 图标正常显示、`>=` / `!=` / `&&` 连字清晰。
3. **实时所见即所得**：外观页滑动字号、行距、字间距或切换配色（如 `Solarized 暗` ↔ `Solarized 浅`）与光标样式时，顶部预览卡与已连接的「会话」标签页内历史/当前终端输出同步刷新，无需重连。
4. **手势持久化与自定义字体**：终端双指缩放字号在重启 App 后保持生效；导入本地 `.ttf` / `.otf` 可切换为主字体或回退字体，删除当前正在使用的导入字体后安全回退到 `Maple Mono · 中英等宽` 无崩溃。
