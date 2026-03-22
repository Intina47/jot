//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func registerResizeSignal(sigCh chan<- os.Signal) {
	signal.Notify(sigCh, syscall.SIGWINCH)
}
