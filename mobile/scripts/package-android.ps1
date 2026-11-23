param(
  [string]$Version = "0.3.3"
)

$ErrorActionPreference = "Stop"
$env:JAVA_HOME = "D:\ace5pro\work_tools\jdk\jdk-21.0.13.1+2"
$sdkRoot = Join-Path $env:LOCALAPPDATA "Android\Sdk"
$env:ANDROID_HOME = $sdkRoot
$env:ANDROID_SDK_ROOT = $sdkRoot
$env:ANDROID_NDK_HOME = Join-Path $sdkRoot "ndk\27.2.12479018"
$gradleBin = Join-Path $env:LOCALAPPDATA "Gradle\gradle-8.11.1\bin"
$env:Path = "$env:JAVA_HOME\bin;$gradleBin;C:\Program Files\Go\bin;$env:USERPROFILE\go\bin;" + [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" + [Environment]::GetEnvironmentVariable("Path", "User")

$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$coreDir = Join-Path $repoRoot "core"
$androidDir = Join-Path $repoRoot "mobile\android"
$libsDir = Join-Path $androidDir "app\libs"
$binDir = Join-Path $repoRoot "bin"

New-Item -ItemType Directory -Force -Path $libsDir | Out-Null
New-Item -ItemType Directory -Force -Path $binDir | Out-Null
Remove-Item (Join-Path $androidDir ".gitkeep") -Force -ErrorAction SilentlyContinue

Write-Host "==> [1/3] Binding pure-Go Shnex core engine into Android JNI AAR (arm64-v8a)..."
Push-Location $coreDir
$env:GOWORK = "off"
go get golang.org/x/mobile/bind@latest
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
$aarOut = Join-Path $libsDir "shnexcore.aar"
gomobile bind -target=android/arm64 -androidapi 26 -ldflags="-s -w" -o $aarOut ./mobilebridge
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
go mod tidy
Remove-Item Env:\GOWORK -ErrorAction SilentlyContinue
Pop-Location

Write-Host "==> [2/3] Generating Gradle Wrapper & compiling Jetpack Compose Material 3 APK..."
Push-Location $androidDir
if (-not (Test-Path (Join-Path $androidDir "gradlew.bat"))) {
  & gradle wrapper --gradle-version 8.11.1
  if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
}
& .\gradlew.bat assembleRelease --no-daemon
if ($LASTEXITCODE -ne 0) { Pop-Location; exit $LASTEXITCODE }
Pop-Location

Write-Host "==> [3/3] Exporting signed Android APK to bin/..."
$builtApk = Join-Path $androidDir "app\build\outputs\apk\release\app-release.apk"
if (-not (Test-Path $builtApk)) {
  throw "Expected APK not found at $builtApk"
}
$destApk = Join-Path $binDir "Shnex-Android-$Version-arm64-v8a.apk"
Copy-Item -Force $builtApk $destApk

Write-Host "=================================================================="
Write-Host "  Android APK     : $destApk"
Write-Host "=================================================================="
