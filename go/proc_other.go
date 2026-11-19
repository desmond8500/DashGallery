//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
)

func hideWindow(c *exec.Cmd) {}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "DashGallery :", msg)
	os.Exit(1)
}
