//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

// Lance yt-dlp sans ouvrir de fenêtre de console
func hideWindow(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// Pas de console (exe en -H=windowsgui) : l'erreur est affichée dans une boîte de dialogue
func fatal(msg string) {
	user32 := syscall.NewLazyDLL("user32.dll")
	t, _ := syscall.UTF16PtrFromString("DashGallery")
	m, _ := syscall.UTF16PtrFromString(msg)
	user32.NewProc("MessageBoxW").Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x10)
	os.Exit(1)
}
