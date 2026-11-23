//go:build windows

package localpty

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"unicode/utf16"
	"unsafe"

	"github.com/shnex/shnex/core/transport"
	"golang.org/x/sys/windows"
)

var (
	modKernel32                   = windows.NewLazySystemDLL("kernel32.dll")
	procUpdateProcThreadAttribute = modKernel32.NewProc("UpdateProcThreadAttribute")
)

type winConPTY struct {
	hpc       windows.Handle
	proc      windows.Handle
	thread    windows.Handle
	inWriter  *os.File
	outReader *os.File
	closeOnce sync.Once
}

const powerlineInitScript = `try { Set-PSReadLineOption -PredictionSource None } catch {}
function global:prompt {
  $e = [char]27; $b = [char]7
  $cwd = (Get-Location).ProviderPath
  $uriPath = $cwd.Replace('\', '/')
  $leaf = Split-Path -Leaf $cwd
  if (-not $leaf) { $leaf = $cwd }
  $br = ''
  $d = $cwd
  for ($i = 0; $i -lt 6 -and $d; $i++) {
    $hf = Join-Path $d '.git\HEAD'
    if (Test-Path -LiteralPath $hf) {
      $raw = (Get-Content -LiteralPath $hf -TotalCount 1 -ErrorAction SilentlyContinue)
      if ($raw -match '^ref:\s*refs/heads/(.+)$') { $br = $Matches[1] }
      elseif ($raw) { $br = $raw.Substring(0, [Math]::Min(7, $raw.Length)) }
      break
    }
    $p = Split-Path -Parent $d
    if ($p -eq $d) { break }
    $d = $p
  }
  $osc = "$e]7;file://localhost/$uriPath$b"
  $seg1 = "$e[48;2;30;64;175;38;2;255;255;255;1m $leaf $e[0m"
  $seg2 = if ($br) { "$e[48;2;109;40;217;38;2;243;232;255m ⎇ $br $e[0m" } else { '' }
  $mark = "$e]133;B$b"
  return "$osc$seg1$seg2 $e[38;2;96;165;250;1m❯$e[0m $mark"
}`

func encodePowerShellUTF16LE(script string) string {
	u16 := utf16.Encode([]rune(script))
	raw := make([]byte, len(u16)*2)
	for i, v := range u16 {
		raw[i*2] = byte(v)
		raw[i*2+1] = byte(v >> 8)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func resolveWindowsShellCommand(shellKey string, builtinPowerline bool) (cmdLine string, label string) {
	psArgs := "-NoLogo"
	if builtinPowerline {
		psArgs = "-NoLogo -NoExit -EncodedCommand " + encodePowerShellUTF16LE(powerlineInitScript)
	}
	switch shellKey {
	case "pwsh":
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			return fmt.Sprintf(`"%s" %s`, p, psArgs), "PowerShell 7"
		}
		return "powershell.exe " + psArgs, "Windows PowerShell"
	case "cmd":
		return "cmd.exe", "命令提示符 (CMD)"
	case "wsl":
		return "wsl.exe", "WSL"
	case "gitbash":
		candidates := []string{
			`C:\Program Files\Git\bin\bash.exe`,
			`C:\Program Files (x86)\Git\bin\bash.exe`,
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return fmt.Sprintf(`"%s" --login -i`, c), "Git Bash"
			}
		}
		return "powershell.exe " + psArgs, "Windows PowerShell"
	default:
		return "powershell.exe " + psArgs, "Windows PowerShell"
	}
}

func startPlatformPTY(shellKey string, workDir string, size transport.TerminalSize, builtinPowerline bool) (platformPTY, string, error) {
	cmdStr, label := resolveWindowsShellCommand(shellKey, builtinPowerline)

	var ptyInRead, ptyInWrite windows.Handle
	if err := windows.CreatePipe(&ptyInRead, &ptyInWrite, nil, 0); err != nil {
		return nil, "", fmt.Errorf("localpty: CreatePipe stdin: %w", err)
	}

	var ptyOutRead, ptyOutWrite windows.Handle
	if err := windows.CreatePipe(&ptyOutRead, &ptyOutWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(ptyInRead)
		_ = windows.CloseHandle(ptyInWrite)
		return nil, "", fmt.Errorf("localpty: CreatePipe stdout: %w", err)
	}

	coord := windows.Coord{
		X: int16(size.Cols),
		Y: int16(size.Rows),
	}
	var hpc windows.Handle
	if err := windows.CreatePseudoConsole(coord, ptyInRead, ptyOutWrite, 0, &hpc); err != nil {
		_ = windows.CloseHandle(ptyInRead)
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		_ = windows.CloseHandle(ptyOutWrite)
		return nil, "", fmt.Errorf("localpty: CreatePseudoConsole: %w", err)
	}

	// Child side handles can be closed after CreatePseudoConsole duplicates them.
	_ = windows.CloseHandle(ptyInRead)
	_ = windows.CloseHandle(ptyOutWrite)

	attrList, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, "", fmt.Errorf("localpty: NewProcThreadAttributeList: %w", err)
	}
	defer attrList.Delete()

	r1, _, e1 := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attrList.List())),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0,
		0,
	)
	if r1 == 0 {
		windows.ClosePseudoConsole(hpc)
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, "", fmt.Errorf("localpty: UpdateProcThreadAttribute: %v", e1)
	}

	var siEx windows.StartupInfoEx
	siEx.StartupInfo.Cb = uint32(unsafe.Sizeof(siEx))
	siEx.StartupInfo.Flags = windows.STARTF_USESTDHANDLES
	siEx.ProcThreadAttributeList = attrList.List()

	cmdUTF16, err := windows.UTF16PtrFromString(cmdStr)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, "", err
	}

	var dirPtr *uint16
	if workDir != "" {
		if st, sErr := os.Stat(workDir); sErr == nil && st.IsDir() {
			dirPtr, _ = windows.UTF16PtrFromString(workDir)
		}
	}

	var pi windows.ProcessInformation
	createFlags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT)
	err = windows.CreateProcess(
		nil,
		cmdUTF16,
		nil,
		nil,
		false,
		createFlags,
		nil,
		dirPtr,
		&siEx.StartupInfo,
		&pi,
	)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		_ = windows.CloseHandle(ptyInWrite)
		_ = windows.CloseHandle(ptyOutRead)
		return nil, "", fmt.Errorf("localpty: CreateProcess(%s): %w", cmdStr, err)
	}

	c := &winConPTY{
		hpc:       hpc,
		proc:      pi.Process,
		thread:    pi.Thread,
		inWriter:  os.NewFile(uintptr(ptyInWrite), "conpty-stdin"),
		outReader: os.NewFile(uintptr(ptyOutRead), "conpty-stdout"),
	}
	return c, label, nil
}

func (c *winConPTY) Read(p []byte) (int, error) {
	return c.outReader.Read(p)
}

func (c *winConPTY) Write(p []byte) (int, error) {
	return c.inWriter.Write(p)
}

func (c *winConPTY) Resize(cols, rows uint16) error {
	return windows.ResizePseudoConsole(c.hpc, windows.Coord{
		X: int16(cols),
		Y: int16(rows),
	})
}

func (c *winConPTY) Wait() error {
	_, err := windows.WaitForSingleObject(c.proc, windows.INFINITE)
	return err
}

func (c *winConPTY) Close() error {
	c.closeOnce.Do(func() {
		windows.ClosePseudoConsole(c.hpc)
		_ = windows.TerminateProcess(c.proc, 0)
		_ = c.inWriter.Close()
		_ = c.outReader.Close()
		_ = windows.CloseHandle(c.thread)
		_ = windows.CloseHandle(c.proc)
	})
	return nil
}
