//go:build windows

package main

import "os"

func registerResizeSignal(sigCh chan<- os.Signal) {
	_ = sigCh
}
