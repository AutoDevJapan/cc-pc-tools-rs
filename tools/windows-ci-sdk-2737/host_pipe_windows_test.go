//go:build windows && amd64

package sdkprep

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestOwnedPipeReadDeadlineWithRealWindowsPipe(t *testing.T) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	instance := "pipe-test-" + hex.EncodeToString(nonce[:])
	name, _ := COMPipeName(instance)
	wide, _ := syscall.UTF16PtrFromString(name)
	kernel := syscall.NewLazyDLL("kernel32.dll")
	server, _, err := kernel.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(wide)), 3, 0, 1, 4096, 4096, 1000, 0)
	if syscall.Handle(server) == syscall.InvalidHandle {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(syscall.Handle(server))
	connected := make(chan struct{})
	go func() { kernel.NewProc("ConnectNamedPipe").Call(server, 0); close(connected) }()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := OpenOwnedGuestCOMPipe(ctx, instance)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	select {
	case <-connected:
	case <-ctx.Done():
		t.Fatal("connection did not settle")
	}
	if err := client.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	_, err = client.Read(one[:])
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("idle read must time out, got %v", err)
	}
}
