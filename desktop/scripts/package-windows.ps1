param(
  [string]$Version = "0.2.0"
)

$ErrorActionPreference = "Stop"
$env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User") + ";C:\Program Files\Go\bin;C:\Program Files\nodejs;C:\Program Files (x86)\NSIS;C:\Program Files\NSIS;$env:USERPROFILE\go\bin"

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$desktopDir = Join-Path $repoRoot "desktop"
$frontendDir = Join-Path $desktopDir "frontend"
$installerDir = Join-Path $desktopDir "installer"
$binDir = Join-Path $repoRoot "bin"

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

Write-Host "==> [1/4] Generating multi-resolution appicon.ico & Windows .syso manifest..."
Push-Location $installerDir
go run gen_icon.go .
$manifestXml = @"
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0" xmlns:asmv3="urn:schemas-microsoft-com:asm.v3">
  <assemblyIdentity type="win32" name="dev.shnex.desktop" version="$Version.0" processorArchitecture="amd64"/>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <asmv3:application>
    <asmv3:windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>
    </asmv3:windowsSettings>
  </asmv3:application>
</assembly>
"@
[System.IO.File]::WriteAllText((Join-Path $installerDir "app.manifest"), $manifestXml, [System.Text.UTF8Encoding]::new($false))
wails3 generate syso -arch amd64 -icon (Join-Path $installerDir "appicon.ico") -manifest (Join-Path $installerDir "app.manifest") -out (Join-Path $desktopDir "rsrc_windows_amd64.syso")
Pop-Location

Write-Host "==> [2/4] Building React + xterm.js WebGL frontend..."
Push-Location $frontendDir
npm run build
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Pop-Location

Write-Host "==> [3/4] Compiling staged binary with embedded High-DPI icon & WebView2 frontend..."
$stageDir = Join-Path $env:TEMP "shnex-stage-$PID"
New-Item -ItemType Directory -Force -Path $stageDir | Out-Null
$exeOut = Join-Path $stageDir "Shnex.exe"
Push-Location $desktopDir
go build -trimpath -ldflags="-H windowsgui -s -w" -o $exeOut .
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Remove-Item (Join-Path $desktopDir "rsrc_windows_amd64.syso") -ErrorAction SilentlyContinue
Pop-Location

Write-Host "==> [4/4] Packaging NSIS Setup Installer..."
$setupOut = Join-Path $binDir "Shnex-Setup-$Version-windows-amd64.exe"
Push-Location $installerDir
# Write UTF-8 with BOM copy for NSIS Unicode parser
$nsiContent = Get-Content -Raw -Encoding UTF8 "shnex.nsi"
$utf8Bom = New-Object System.Text.UTF8Encoding($true)
[System.IO.File]::WriteAllText((Join-Path $installerDir "shnex_build.nsi"), $nsiContent, $utf8Bom)

makensis /V2 "/DPRODUCT_VERSION=$Version" "/DEXE_SOURCE=$exeOut" "/DOUT_INSTALLER=$setupOut" "shnex_build.nsi"
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Remove-Item "shnex_build.nsi", "app.manifest" -ErrorAction SilentlyContinue
Remove-Item -Recurse -Force $stageDir -ErrorAction SilentlyContinue
Pop-Location

Write-Host "=================================================================="
Write-Host "  Windows Setup   : $setupOut"
Write-Host "=================================================================="
