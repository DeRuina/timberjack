//go:build linux
// +build linux

// This example demonstrates log rotation on receiving SIGHUP.
// It only runs on Linux because SIGHUP and syscall behavior are OS-specific.

package timberjack

import (
	"log"
	"os"
	"os/signal"
	"syscall"
)

// Example of how to rotate in response to SIGHUP.
func ExampleLogger_Rotate() {
	l := &Logger{}
	log.SetOutput(l)
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGHUP)

	go func() {
		for {
			<-c
			l.Rotate()
		}
	}()
}
