//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	serviceWin32OwnProcess = 0x00000010
	serviceStartPending    = 0x00000002
	serviceRunning         = 0x00000004
	serviceStopPending     = 0x00000003
	serviceStopped         = 0x00000001
	serviceAcceptStop      = 0x00000001
	serviceControlStop     = 0x00000001
	serviceControlShutdown = 0x00000005
)

var (
	advapi32                      = syscall.NewLazyDLL("advapi32.dll")
	startServiceCtrlDispatcherW   = advapi32.NewProc("StartServiceCtrlDispatcherW")
	registerServiceCtrlHandlerExW = advapi32.NewProc("RegisterServiceCtrlHandlerExW")
	setServiceStatus              = advapi32.NewProc("SetServiceStatus")
	createSerialHandle            = syscall.NewLazyDLL("kernel32.dll").NewProc("CreateFileW")
	queryDosDeviceW               = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryDosDeviceW")
	serviceStop                   = make(chan struct{})
	serviceStopOnce               = make(chan struct{}, 1)
	serviceStatusHandle           uintptr
	serviceControlMu              sync.Mutex
	serviceActiveConn             io.Closer
)

type serviceTableEntry struct {
	Name *uint16
	Main uintptr
}

type serviceStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

func runGuestService() error {
	name, err := syscall.UTF16PtrFromString(guestServiceName)
	if err != nil {
		return err
	}
	mainCallback := syscall.NewCallback(serviceMain)
	table := [2]serviceTableEntry{{Name: name, Main: mainCallback}, {}}
	ok, _, callErr := startServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&table[0])))
	if ok == 0 {
		return fmt.Errorf("StartServiceCtrlDispatcherW: %w", callErr)
	}
	return nil
}

func serviceMain(_ uintptr, _ uintptr) uintptr {
	name, _ := syscall.UTF16PtrFromString(guestServiceName)
	handler := syscall.NewCallback(serviceControlHandler)
	statusHandle, _, callErr := registerServiceCtrlHandlerExW.Call(uintptr(unsafe.Pointer(name)), handler, 0)
	if statusHandle == 0 {
		return uintptr(callErr.(syscall.Errno))
	}
	serviceStatusHandle = statusHandle
	status := serviceStatus{ServiceType: serviceWin32OwnProcess, CurrentState: serviceStartPending, WaitHint: 30000}
	_, _, _ = setServiceStatus.Call(statusHandle, uintptr(unsafe.Pointer(&status)))
	status.CurrentState = serviceRunning
	status.ControlsAccepted = serviceAcceptStop
	status.WaitHint = 0
	_, _, _ = setServiceStatus.Call(statusHandle, uintptr(unsafe.Pointer(&status)))
	if err := serveSerial(); err != nil {
		status.CurrentState = serviceStopped
		status.Win32ExitCode = 1
		_, _, _ = setServiceStatus.Call(statusHandle, uintptr(unsafe.Pointer(&status)))
		return 1
	}
	status.CurrentState = serviceStopped
	_, _, _ = setServiceStatus.Call(statusHandle, uintptr(unsafe.Pointer(&status)))
	return 0
}

func serviceControlHandler(control uint32, _ uint32, _ uintptr, _ uintptr) uintptr {
	switch control {
	case serviceControlStop, serviceControlShutdown:
		status := serviceStatus{ServiceType: serviceWin32OwnProcess, CurrentState: serviceStopPending, WaitHint: 10000}
		_, _, _ = setServiceStatus.Call(serviceStatusHandle, uintptr(unsafe.Pointer(&status)))
		serviceControlMu.Lock()
		if serviceActiveConn != nil {
			_ = serviceActiveConn.Close()
		}
		serviceControlMu.Unlock()
		select {
		case serviceStopOnce <- struct{}{}:
			close(serviceStop)
		default:
		}
		return 0
	default:
		return 0
	}
}

func serveSerial() error {
	recordGuestServiceStage(0)
	seedBytes, err := os.ReadFile(filepath.Join(os.Getenv("SystemRoot"), `System32\cc2746-seed.json`))
	if err != nil {
		return errors.New("guest seed unavailable")
	}
	var seed vmSeed
	dec := json.NewDecoder(bytes.NewReader(seedBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&seed); err != nil || !validInstance(seed.Instance) || !validHash(seed.ClientSHA256) {
		return errors.New("guest seed rejected")
	}
	recordGuestServiceStage(1)
	recordGuestServiceStage(2)
	recordGuestCOMPortInventory()
	serial, err := openSerialPort()
	if err != nil {
		recordGuestServiceStage(7)
		recordGuestServiceFailure(fmt.Errorf("COM1 open failed: %w; guest serial ports: %v", err, guestCOMPortNames()))
		return errors.New("guest COM1 unavailable")
	}
	defer serial.Close()
	recordGuestServiceStage(3)
	serviceControlMu.Lock()
	serviceActiveConn = serial
	serviceControlMu.Unlock()
	defer func() { serviceControlMu.Lock(); serviceActiveConn = nil; serviceControlMu.Unlock() }()
	identity, err := newVMIdentity()
	if err != nil {
		return err
	}
	defer clearIdentity(&identity)
	if err := writeJSONLine(serial, vmReady{Instance: seed.Instance, ServerSHA256: identity.SHA256}); err != nil {
		recordGuestServiceStage(8)
		recordGuestServiceFailure(err)
		return err
	}
	recordGuestServiceStage(4)
	recordGuestServiceStage(9)
	probeLine, err := readJSONLine(serial, 4096)
	if err != nil {
		recordGuestServiceStage(11)
		recordGuestServiceFailure(err)
		return errors.New("guest COM probe was not received")
	}
	var probe vmProbe
	if err := json.Unmarshal(probeLine, &probe); err != nil || probe.Kind != "probe" || probe.Instance != seed.Instance {
		recordGuestServiceStage(11)
		recordGuestServiceFailure(errors.New("guest COM probe rejected"))
		return errors.New("guest COM probe rejected")
	}
	if err := writeJSONLine(serial, vmProbe{Kind: "probe-ack", Instance: seed.Instance}); err != nil {
		recordGuestServiceStage(11)
		recordGuestServiceFailure(err)
		return errors.New("guest COM probe acknowledgement failed")
	}
	recordGuestServiceStage(10)
	tlsConn := tlsServerConn(serial, identity, seed.ClientSHA256)
	defer tlsConn.Close()
	serviceControlMu.Lock()
	serviceActiveConn = tlsConn
	serviceControlMu.Unlock()
	if err := tlsConn.Handshake(); err != nil {
		recordGuestServiceStage(6)
		recordGuestServiceFailure(err)
		return errors.New("guest TLS client authentication failed")
	}
	recordGuestServiceStage(5)
	decoder := json.NewDecoder(tlsConn)
	encoder := json.NewEncoder(tlsConn)
	for {
		select {
		case <-serviceStop:
			return nil
		default:
		}
		request, err := decodeVMRequest(decoder, seed.Instance)
		if err != nil {
			return errors.New("guest request stream ended")
		}
		switch request.Kind {
		case "normal":
			result, err := runGuestNormal()
			if err != nil {
				_ = encoder.Encode(vmReply{Kind: "normal", Error: "normal fixture cleanup failed"})
				continue
			}
			if err := encoder.Encode(vmReply{Kind: "normal", Result: &result}); err != nil {
				return errors.New("normal fixture receipt delivery failed")
			}
		case "cancel":
			result, err := runGuestCancel(tlsConn, decoder, seed.Instance)
			if err != nil {
				_ = encoder.Encode(vmReply{Kind: "cancel", Error: "cancel fixture cleanup failed"})
				continue
			}
			if err := encoder.Encode(vmReply{Kind: "cancel", Result: &result}); err != nil {
				return errors.New("cancel fixture receipt delivery failed")
			}
		case "shutdown":
			if err := encoder.Encode(vmReply{Kind: "shutdown"}); err != nil {
				return errors.New("shutdown receipt delivery failed")
			}
			return nil
		}
	}
}

func recordGuestServiceStage(stage byte) {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return
	}
	path := filepath.Join(systemRoot, `System32\cc2746-service-stage`)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return
	}
	_, _ = f.Write([]byte{stage})
	_ = f.Sync()
	_ = f.Close()
}

func recordGuestServiceFailure(err error) {
	if err == nil {
		return
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return
	}
	path := filepath.Join(systemRoot, `System32\cc2746-service-failure.txt`)
	_ = os.WriteFile(path, []byte(err.Error()), 0600)
}

func guestCOMPortNames() []string {
	var names []string
	for number := 1; number <= 16; number++ {
		name, err := syscall.UTF16PtrFromString(fmt.Sprintf("COM%d", number))
		if err != nil {
			continue
		}
		buffer := make([]uint16, 1024)
		count, _, _ := queryDosDeviceW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if count != 0 {
			names = append(names, fmt.Sprintf("COM%d", number))
		}
	}
	return names
}

func recordGuestCOMPortInventory() {
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		return
	}
	path := filepath.Join(systemRoot, `System32\cc2746-com-ports.txt`)
	_ = os.WriteFile(path, []byte(fmt.Sprint(guestCOMPortNames())), 0600)
}

func openSerialPort() (*os.File, error) {
	path, err := syscall.UTF16PtrFromString(`\\.\COM1`)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createSerialHandle.Call(
		uintptr(unsafe.Pointer(path)),
		0x80000000|0x40000000, // GENERIC_READ | GENERIC_WRITE
		0,                     // Communications resources require exclusive access.
		0,
		3, // OPEN_EXISTING
		0x80,
		0,
	)
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return nil, callErr
	}
	file := os.NewFile(handle, `\\.\COM1`)
	if file == nil {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, errors.New("COM1 handle could not be wrapped")
	}
	return file, nil
}

func runGuestNormal() (fixtureResult, error) {
	started := time.Now()
	result, job, cmds, name, err := startGuestProcesses("normal", []string{"--leaf-normal", "--leaf-live"})
	if err != nil {
		return result, err
	}
	if err := cmds[0].Wait(); err != nil {
		_ = syscall.CloseHandle(job)
		waitAll(cmds)
		return result, err
	}
	if !processAlive(uint32(cmds[1].Process.Pid)) {
		_ = syscall.CloseHandle(job)
		waitAll(cmds)
		return result, errors.New("normal fixture live child disappeared early")
	}
	if err := syscall.CloseHandle(job); err != nil {
		waitAll(cmds)
		return result, err
	}
	waitAll(cmds)
	result = finishGuestFixture(result, name)
	result.ElapsedMilliseconds = time.Since(started).Milliseconds()
	return result, nil
}

func runGuestCancel(conn io.Writer, decoder *json.Decoder, instance string) (fixtureResult, error) {
	started := time.Now()
	result, job, cmds, name, err := startGuestProcesses("cancel", []string{"--leaf-live", "--leaf-live"})
	if err != nil {
		return result, err
	}
	if !writeStage(conn, cmds) {
		_ = syscall.CloseHandle(job)
		waitAll(cmds)
		return result, errors.New("cancel fixture stage delivery failed")
	}
	request, err := decodeVMRequest(decoder, instance)
	if err != nil || request.Kind != "cancel-confirm" {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = ctx.Err()
		_ = syscall.CloseHandle(job)
		waitAll(cmds)
		return result, errors.New("cancel fixture request ended before cancellation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result.CancellationFired = errors.Is(ctx.Err(), context.Canceled)
	_ = cancel
	if err := syscall.CloseHandle(job); err != nil {
		waitAll(cmds)
		return result, err
	}
	waitAll(cmds)
	result = finishGuestFixture(result, name)
	result.ElapsedMilliseconds = time.Since(started).Milliseconds()
	return result, nil
}

func startGuestProcesses(kind string, modes []string) (fixtureResult, syscall.Handle, []*exec.Cmd, string, error) {
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fixtureResult{}, 0, nil, "", err
	}
	hash := sha256.Sum256(nonce[:])
	name := `Local\cc2746-vm-` + hex.EncodeToString(hash[:16])
	clear(nonce[:])
	job, err := createJob(name)
	if err != nil {
		return fixtureResult{}, 0, nil, name, err
	}
	exe, err := os.Executable()
	if err != nil {
		_ = syscall.CloseHandle(job)
		return fixtureResult{}, 0, nil, name, err
	}
	cmds := make([]*exec.Cmd, 0, len(modes))
	for _, mode := range modes {
		cmd := exec.Command(exe, mode)
		cmd.Dir = filepath.Dir(exe)
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + os.Getenv("SystemRoot") + `\System32`}
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createSuspended | createNoWindow}
		if err := cmd.Start(); err != nil {
			_ = syscall.CloseHandle(job)
			killAll(cmds)
			waitAll(cmds)
			return fixtureResult{}, 0, nil, name, err
		}
		cmds = append(cmds, cmd)
		if err := assignAndResume(job, uint32(cmd.Process.Pid)); err != nil {
			_ = syscall.CloseHandle(job)
			killAll(cmds)
			waitAll(cmds)
			return fixtureResult{}, 0, nil, name, err
		}
	}
	result := fixtureResult{Case: kind, NamespaceCreated: true, CredentialFree: true}
	for _, cmd := range cmds {
		result.ChildPIDs = append(result.ChildPIDs, cmd.Process.Pid)
	}
	result.BothObservedAlive = len(cmds) == 2 && processAlive(uint32(cmds[0].Process.Pid)) && processAlive(uint32(cmds[1].Process.Pid))
	if !result.BothObservedAlive {
		_ = syscall.CloseHandle(job)
		waitAll(cmds)
		return fixtureResult{}, 0, nil, name, errors.New("guest children were not both observed alive")
	}
	return result, job, cmds, name, nil
}

func writeStage(conn io.Writer, cmds []*exec.Cmd) bool {
	pids := make([]int, 0, len(cmds))
	for _, cmd := range cmds {
		pids = append(pids, cmd.Process.Pid)
	}
	return json.NewEncoder(conn).Encode(vmStage{Kind: "cancel-started", PIDs: pids, Alive: true}) == nil
}

func waitAll(cmds []*exec.Cmd) {
	for _, cmd := range cmds {
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}
}

func killAll(cmds []*exec.Cmd) {
	for _, cmd := range cmds {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	}
}

func finishGuestFixture(result fixtureResult, jobName string) fixtureResult {
	result.ChildrenExited = true
	for _, pid := range result.ChildPIDs {
		result.ChildrenExited = result.ChildrenExited && waitProcessExit(uint32(pid), 5*time.Second)
	}
	result.NamespaceRemoved = namespaceAbsent(jobName)
	return result
}

func clearIdentity(identity *vmIdentity) {
	if identity == nil {
		return
	}
	if private, ok := identity.Certificate.PrivateKey.(ed25519.PrivateKey); ok {
		clear(private)
	}
	identity.Certificate.PrivateKey = nil
}
