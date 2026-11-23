; Shnex Windows Installer Script (NSIS Modern UI 2)
Unicode True
SetCompressor /SOLID lzma

!ifndef PRODUCT_NAME
  !define PRODUCT_NAME "Shnex"
!endif
!ifndef PRODUCT_VERSION
  !define PRODUCT_VERSION "0.2.0"
!endif
!ifndef PRODUCT_PUBLISHER
  !define PRODUCT_PUBLISHER "Shnex Open Source"
!endif
!ifndef EXE_SOURCE
  !define EXE_SOURCE "..\..\bin\Shnex.exe"
!endif
!ifndef OUT_INSTALLER
  !define OUT_INSTALLER "..\..\bin\Shnex-Setup-${PRODUCT_VERSION}-windows-amd64.exe"
!endif
!ifndef ICON_FILE
  !define ICON_FILE "appicon.ico"
!endif

!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"

Name "${PRODUCT_NAME} ${PRODUCT_VERSION}"
OutFile "${OUT_INSTALLER}"
InstallDir "$LOCALAPPDATA\Programs\${PRODUCT_NAME}"
InstallDirRegKey HKCU "Software\${PRODUCT_NAME}" "InstallDir"
RequestExecutionLevel user
BrandingText "${PRODUCT_NAME} v${PRODUCT_VERSION} — SSH & Claude CLI 接力工作台"

!include "MUI2.nsh"
!include "FileFunc.nsh"

!define MUI_ICON "${ICON_FILE}"
!define MUI_UNICON "${ICON_FILE}"
!define MUI_ABORTWARNING

!define MUI_FINISHPAGE_RUN "$INSTDIR\Shnex.exe"
!define MUI_FINISHPAGE_RUN_TEXT "立即启动 ${PRODUCT_NAME}"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "SimpChinese"
!insertmacro MUI_LANGUAGE "English"

VIProductVersion "${PRODUCT_VERSION}.0"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "ProductName" "${PRODUCT_NAME}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "CompanyName" "${PRODUCT_PUBLISHER}"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "FileDescription" "${PRODUCT_NAME} Setup"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "FileVersion" "${PRODUCT_VERSION}.0"
VIAddVersionKey /LANG=${LANG_SIMPCHINESE} "LegalCopyright" "MIT License"

Section "MainApplication" SEC01
  ; Gracefully close any running Shnex.exe instance before overwriting binary
  nsExec::ExecToLog 'taskkill /F /IM Shnex.exe'

  SetOutPath "$INSTDIR"
  SetOverwrite on

  File "${EXE_SOURCE}"
  File "${ICON_FILE}"

  WriteRegStr HKCU "Software\${PRODUCT_NAME}" "InstallDir" "$INSTDIR"

  ; Start Menu Shortcuts
  CreateDirectory "$SMPROGRAMS\${PRODUCT_NAME}"
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk" "$INSTDIR\Shnex.exe" "" "$INSTDIR\appicon.ico" 0
  CreateShortCut "$SMPROGRAMS\${PRODUCT_NAME}\卸载 ${PRODUCT_NAME}.lnk" "$INSTDIR\Uninstall.exe" "" "$INSTDIR\appicon.ico" 0

  ; Desktop Shortcut
  CreateShortCut "$DESKTOP\${PRODUCT_NAME}.lnk" "$INSTDIR\Shnex.exe" "" "$INSTDIR\appicon.ico" 0

  ; Write Uninstaller & Windows Installed Apps Registry Entry
  WriteUninstaller "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${PRODUCT_NAME}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\Shnex.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\Uninstall.exe$\""
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1

  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "Uninstall"
  Delete "$DESKTOP\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\${PRODUCT_NAME}.lnk"
  Delete "$SMPROGRAMS\${PRODUCT_NAME}\卸载 ${PRODUCT_NAME}.lnk"
  RMDir "$SMPROGRAMS\${PRODUCT_NAME}"

  Delete "$INSTDIR\Shnex.exe"
  Delete "$INSTDIR\appicon.ico"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"

  DeleteRegKey HKCU "${UNINST_KEY}"
  DeleteRegKey HKCU "Software\${PRODUCT_NAME}"
SectionEnd
