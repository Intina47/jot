package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole    = kernel32.NewProc("AttachConsole")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procGetStdHandle     = kernel32.NewProc("GetStdHandle")
	procSetStdHandle     = kernel32.NewProc("SetStdHandle")
	procCreateFile       = kernel32.NewProc("CreateFileW")
)

// attachParentProcess is the special value for AttachConsole meaning
// "attach to the console of the process that launched me".
const (
	attachParentProcess = ^uintptr(0)     // (DWORD)-1
	stdInputHandle      = uintptr(10 - 1) // (DWORD)-10 = STD_INPUT_HANDLE
	stdOutputHandle     = uintptr(11 - 1) // (DWORD)-11 = STD_OUTPUT_HANDLE
	stdErrorHandle      = uintptr(12 - 1) // (DWORD)-12 = STD_ERROR_HANDLE
	genericRead         = 0x80000000
	genericWrite        = 0x40000000
	openExisting        = 3
	fileShareReadWrite  = 0x00000001 | 0x00000002
)

func openConsoleHandle(name string, access uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	r, _, e := procCreateFile.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(access),
		uintptr(fileShareReadWrite),
		0,
		uintptr(openExisting),
		0,
		0,
	)
	h := syscall.Handle(r)
	if h == syscall.InvalidHandle {
		return syscall.InvalidHandle, e
	}
	return h, nil
}

func init() {
	if isGoTestBinary(os.Args) {
		return
	}

	// If we already have a console window, nothing to do.
	// This shouldn't happen when built with -H windowsgui, but be defensive.
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd != 0 {
		return
	}

	// Try to attach to the parent process's console.
	// Succeeds when launched from cmd.exe, PowerShell, Windows Terminal, etc.
	// Fails (returns 0) when launched from Explorer, the registry, or any GUI context.
	ret, _, _ := procAttachConsole.Call(attachParentProcess)
	if ret == 0 {
		// No parent console — we were launched from Explorer or a GUI shortcut.
		// Redirect all standard handles to nul so nothing panics on write,
		// and so the OS doesn't allocate a new console window for us.
		devNull, err := os.OpenFile("nul", os.O_RDWR, 0)
		if err == nil {
			os.Stdin = devNull
			os.Stdout = devNull
			os.Stderr = devNull
		}
		return
	}

	if h, err := openConsoleHandle("CONOUT$", genericRead|genericWrite); err == nil {
		procSetStdHandle.Call(stdOutputHandle, uintptr(h))
		procSetStdHandle.Call(stdErrorHandle, uintptr(h))
		os.Stdout = os.NewFile(uintptr(h), "CONOUT$")
		os.Stderr = os.NewFile(uintptr(h), "CONOUT$")
	}

	if h, err := openConsoleHandle("CONIN$", genericRead|genericWrite); err == nil {
		procSetStdHandle.Call(stdInputHandle, uintptr(h))
		os.Stdin = os.NewFile(uintptr(h), "CONIN$")
	}

	// We attached successfully — re-open the real console handles.
	// Go captured stdin/stdout/stderr at process start before we attached,
	// so those file descriptors point at nothing useful. Replace them now.
	// if conout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
	// 	os.Stdout = conout
	// 	os.Stderr = conout
	// }
	// if conin, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
	// 	os.Stdin = conin
	// }
}

func isGoTestBinary(args []string) bool {
	if len(args) == 0 {
		return false
	}

	name := strings.ToLower(filepath.Base(args[0]))
	return strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe")
}
