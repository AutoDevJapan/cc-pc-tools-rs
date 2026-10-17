//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func hide(c *exec.Cmd) {}

// Windows FILETIME receipts cannot prove a Unix process identity.
func processReceiptAlive(uint32, uint64) bool { return false }
func replace(from, to string) error           { return os.Rename(from, to) }
func acquire(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		return nil, e
	}
	return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
func discordToken() string { return os.Getenv("GODD_DISCORD_BOT_TOKEN") }
