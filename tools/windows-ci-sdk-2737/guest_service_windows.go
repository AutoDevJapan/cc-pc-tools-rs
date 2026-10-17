//go:build windows && amd64

package sdkprep

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const guestServiceName = "WindowsCISDK2737"

var (
	serviceAdvapi          = syscall.NewLazyDLL("advapi32.dll")
	serviceKernel          = syscall.NewLazyDLL("kernel32.dll")
	startServiceDispatcher = serviceAdvapi.NewProc("StartServiceCtrlDispatcherW")
	registerServiceHandler = serviceAdvapi.NewProc("RegisterServiceCtrlHandlerExW")
	setServiceStatusProc   = serviceAdvapi.NewProc("SetServiceStatus")
	createFileProc         = serviceKernel.NewProc("CreateFileW")
	getCommStateProc       = serviceKernel.NewProc("GetCommState")
	setCommStateProc       = serviceKernel.NewProc("SetCommState")
	setCommTimeoutsProc    = serviceKernel.NewProc("SetCommTimeouts")
	serviceStop            = make(chan struct{})
	serviceStopOnce        sync.Once
	serviceHandle          uintptr
)

type serviceTableEntry struct {
	Name *uint16
	Main uintptr
}
type winServiceStatus struct {
	ServiceType, CurrentState, ControlsAccepted, Win32ExitCode,
	ServiceSpecificExitCode, CheckPoint, WaitHint uint32
}
type serialDCB struct {
	Length, BaudRate, Flags                                                    uint32
	Reserved, XonLimit, XoffLimit                                              uint16
	ByteSize, Parity, StopBits, XonChar, XoffChar, ErrorChar, EofChar, EvtChar byte
	Reserved1                                                                  uint16
}
type serialTimeouts struct{ ReadInterval, ReadTotalMultiplier, ReadTotalConstant, WriteTotalMultiplier, WriteTotalConstant uint32 }

type publicSeed struct {
	Instance     string `json:"instance"`
	ClientSHA256 string `json:"clientCertificateSHA256"`
}

func RunGuestService() error {
	name, err := syscall.UTF16PtrFromString(guestServiceName)
	if err != nil {
		return errors.New("service name rejected")
	}
	mainFn := syscall.NewCallback(serviceMain)
	table := [2]serviceTableEntry{{Name: name, Main: mainFn}, {}}
	ok, _, _ := startServiceDispatcher.Call(uintptr(unsafe.Pointer(&table[0])))
	if ok == 0 {
		return errors.New("Windows service dispatcher failed")
	}
	return nil
}

func serviceMain(_ uintptr, _ uintptr) uintptr {
	name, _ := syscall.UTF16PtrFromString(guestServiceName)
	handler := syscall.NewCallback(serviceControl)
	h, _, callErr := registerServiceHandler.Call(uintptr(unsafe.Pointer(name)), handler, 0)
	if h == 0 {
		return uintptr(callErr.(syscall.Errno))
	}
	serviceHandle = h
	if !reportServiceState(2, 0, 30000) {
		return 1
	} // START_PENDING
	err := serveGuestAgent()
	code := uint32(0)
	if err != nil {
		code = 1
	}
	_ = reportServiceState(1, code, 0) // STOPPED
	return uintptr(code)
}

func serviceControl(control uint32, _ uint32, _ uintptr, _ uintptr) uintptr {
	if control == 1 || control == 5 { // STOP or SHUTDOWN
		_ = reportServiceState(3, 0, 30000) // STOP_PENDING
		serviceStopOnce.Do(func() { close(serviceStop) })
	}
	return 0
}

func reportServiceState(state, exitCode, waitHint uint32) bool {
	status := winServiceStatus{ServiceType: 0x10, CurrentState: state, Win32ExitCode: exitCode, WaitHint: waitHint}
	if state == 4 {
		status.ControlsAccepted = 1
	}
	ok, _, _ := setServiceStatusProc.Call(serviceHandle, uintptr(unsafe.Pointer(&status)))
	return ok != 0
}

func serveGuestAgent() error {
	if err := initializeGuestRoot(guestSDKRoot); err != nil {
		return errors.New("guest SDK root preparation failed")
	}
	seed, err := readPublicSeed()
	if err != nil {
		return err
	}
	if err := applyGuestFirewallRule(); err != nil {
		return err
	}
	defer removeGuestFirewallRule()
	ip, err := firstPrivateIPv4()
	if err != nil {
		return err
	}
	serverIdentity, err := NewIdentity(seed.Instance, ip, x509.ExtKeyUsageServerAuth)
	if err != nil {
		return err
	}
	defer clearIdentity(&serverIdentity)
	tlsConfig, err := ServerTLSConfig(serverIdentity, seed.ClientSHA256)
	if err != nil {
		return err
	}
	ops, err := NewWindowsOperations()
	if err != nil {
		return err
	}
	handler, err := NewHandler(seed.Instance, ops)
	if err != nil {
		return err
	}
	server, listener, err := ListenPrivateIPv4(ip, handler, tlsConfig)
	if err != nil {
		return err
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	if !reportServiceState(4, 0, 0) {
		_ = server.Close()
		<-serveDone
		return errors.New("service running state update failed")
	}
	if err := waitForHostCOM(seed.Instance, ip, CertificateHash(serverIdentity.Leaf)); err != nil {
		_ = server.Close()
		<-serveDone
		return err
	}
	<-serviceStop
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		_ = server.Close()
	}
	if err := <-serveDone; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.New("guest SDK listener stopped unexpectedly")
	}
	return nil
}

func readPublicSeed() (publicSeed, error) {
	exe, err := os.Executable()
	if err != nil {
		return publicSeed{}, errors.New("guest helper path unavailable")
	}
	if !within(guestSDKRoot, filepath.Clean(exe)) {
		return publicSeed{}, errors.New("guest helper is outside the fixed SDK root")
	}
	file, err := os.Open(filepath.Join(filepath.Dir(exe), "seed.json"))
	if err != nil {
		return publicSeed{}, errors.New("public seed unavailable")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return publicSeed{}, errors.New("public seed size rejected")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var seed publicSeed
	if dec.Decode(&seed) != nil || dec.Decode(new(json.RawMessage)) != io.EOF || !validInstance(seed.Instance) || !validDigest(seed.ClientSHA256) {
		return publicSeed{}, errors.New("public seed rejected")
	}
	return seed, nil
}

func applyGuestFirewallRule() error {
	exe, err := os.Executable()
	if err != nil || !within(guestSDKRoot, filepath.Clean(exe)) {
		return errors.New("guest helper path rejected for firewall rule")
	}
	netsh := filepath.Join(os.Getenv("SystemRoot"), "System32", "netsh.exe")
	removeGuestFirewallRule()
	args := []string{"advfirewall", "firewall", "add", "rule", "name=WindowsCISDK2737", "dir=in", "action=allow", "protocol=TCP", "localport=38473", "remoteip=LocalSubnet", "program=" + exe, "profile=any", "enable=yes"}
	env := []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32")}
	if err := runFixed(netsh, args, env); err != nil {
		return errors.New("guest-only firewall rule setup failed")
	}
	return nil
}

func removeGuestFirewallRule() {
	netsh := filepath.Join(os.Getenv("SystemRoot"), "System32", "netsh.exe")
	args := []string{"advfirewall", "firewall", "delete", "rule", "name=WindowsCISDK2737", "protocol=TCP", "localport=38473", "remoteip=LocalSubnet"}
	env := []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + filepath.Join(os.Getenv("SystemRoot"), "System32")}
	_ = runFixed(netsh, args, env)
}

func firstPrivateIPv4() (net.IP, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, errors.New("guest network enumeration failed")
	}
	sort.Slice(interfaces, func(i, j int) bool { return interfaces[i].Index < interfaces[j].Index })
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if v4 := ip.To4(); v4 != nil && v4.IsPrivate() && !v4.IsLoopback() && !v4.IsLinkLocalUnicast() {
				return v4, nil
			}
		}
	}
	return nil, errors.New("guest private IPv4 address unavailable")
}

func waitForHostCOM(instance string, ip net.IP, serverHash string) error {
	port, err := openGuestCOM1()
	if err != nil {
		return errors.New("guest COM1 unavailable")
	}
	defer port.Close()
	line, err := readCOMLine(port, 10*time.Minute)
	if err != nil {
		return errors.New("COM bootstrap timed out")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var request comRequest
	if dec.Decode(&request) != nil || dec.Decode(new(json.RawMessage)) != io.EOF || request.Kind != "sdk-hello" || request.Instance != instance || !validDigest(request.Nonce) {
		return errors.New("COM bootstrap request rejected")
	}
	reply, err := json.Marshal(comReply{Kind: "sdk-ready", Instance: instance, Nonce: request.Nonce, IPv4: ip.To4().String(), ServerSHA256: serverHash})
	if err != nil {
		return errors.New("COM bootstrap response encoding failed")
	}
	reply = append(reply, '\n')
	if _, err := port.Write(reply); err != nil {
		return errors.New("COM bootstrap response failed")
	}
	return nil
}

func readCOMLine(file *os.File, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	line := make([]byte, 0, 256)
	for time.Now().Before(deadline) && len(line) < 4096 {
		select {
		case <-serviceStop:
			return nil, errors.New("COM bootstrap interrupted by service stop")
		default:
		}
		var one [1]byte
		n, err := file.Read(one[:])
		if err != nil {
			return nil, err
		}
		if n == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if one[0] == '\n' {
			return bytes.TrimSpace(line), nil
		}
		if one[0] != '\r' {
			line = append(line, one[0])
		}
	}
	return nil, errors.New("COM line timeout or size exceeded")
}

func openGuestCOM1() (*os.File, error) {
	name, err := syscall.UTF16PtrFromString(`\\.\COM1`)
	if err != nil {
		return nil, err
	}
	handle, _, callErr := createFileProc.Call(uintptr(unsafe.Pointer(name)), 0x80000000|0x40000000, 0, 0, 3, 0x80, 0)
	if syscall.Handle(handle) == syscall.InvalidHandle {
		return nil, callErr
	}
	file := os.NewFile(handle, `\\.\COM1`)
	if file == nil {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, errors.New("COM1 handle wrapping failed")
	}
	if err := configureGuestCOM1(syscall.Handle(handle)); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func configureGuestCOM1(handle syscall.Handle) error {
	dcb := serialDCB{Length: uint32(unsafe.Sizeof(serialDCB{}))}
	if ok, _, _ := getCommStateProc.Call(uintptr(handle), uintptr(unsafe.Pointer(&dcb))); ok == 0 {
		return errors.New("COM1 state read failed")
	}
	dcb.BaudRate, dcb.Flags, dcb.ByteSize, dcb.Parity, dcb.StopBits = 115200, 1, 8, 0, 0
	if ok, _, _ := setCommStateProc.Call(uintptr(handle), uintptr(unsafe.Pointer(&dcb))); ok == 0 {
		return errors.New("COM1 configuration failed")
	}
	timeouts := serialTimeouts{ReadInterval: ^uint32(0), ReadTotalMultiplier: ^uint32(0), ReadTotalConstant: 1000}
	if ok, _, _ := setCommTimeoutsProc.Call(uintptr(handle), uintptr(unsafe.Pointer(&timeouts))); ok == 0 {
		return errors.New("COM1 timeout configuration failed")
	}
	return nil
}
