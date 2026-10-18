package sdkprep

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
)

// PackageID is deliberately closed: the guest accepts SDK preparation IDs,
// never a command line, executable path, or repository operation from the host.
type PackageID string

const (
	GitForWindows  PackageID = "git-for-windows"
	PowerShell7    PackageID = "powershell-7"
	RustToolchain  PackageID = "rust-1.97.1"
	MSVCBuildTools PackageID = "msvc-build-tools"
	WindowsSDK     PackageID = "windows-sdk"
	Sccache        PackageID = "sccache"
)

type Operation string

const (
	OpStatus   Operation = "status"
	OpPrepare  Operation = "prepare-sdk"
	OpVersions Operation = "verify-versions"
)

// Request intentionally has no fields for commands, paths, URLs, or shell text.
type Request struct {
	Instance  string    `json:"instance"`
	Operation Operation `json:"operation"`
	Package   PackageID `json:"package,omitempty"`
}

type Result struct {
	Instance           string    `json:"instance"`
	Operation          Operation `json:"operation"`
	Package            PackageID `json:"package,omitempty"`
	Stage              string    `json:"stage"`
	OK                 bool      `json:"ok"`
	Code               string    `json:"code,omitempty"`
	Version            string    `json:"version,omitempty"`
	VersionProofSHA256 string    `json:"version_proof_sha256,omitempty"`
}

type Artifact struct {
	ID      PackageID `json:"id"`
	Version string    `json:"version"`
	URL     string    `json:"url"`
	SHA256  string    `json:"sha256"`
}

type comRequest struct {
	Kind     string `json:"kind"`
	Instance string `json:"instance"`
	Nonce    string `json:"nonce"`
}
type comReply struct {
	Kind         string `json:"kind"`
	Instance     string `json:"instance"`
	IPv4         string `json:"ipv4"`
	ServerSHA256 string `json:"serverCertificateSHA256"`
}
type BootstrapResult struct {
	IPv4         net.IP
	ServerSHA256 string
}

type PublicSeed struct {
	Instance     string `json:"instance"`
	ClientSHA256 string `json:"clientCertificateSHA256"`
}
type PreparationReceipt struct {
	Instance     string   `json:"instance"`
	GuestIPv4    string   `json:"guestIPv4"`
	ServerSHA256 string   `json:"serverCertificateSHA256"`
	Results      []Result `json:"results"`
}

func COMPipeName(instance string) (string, error) {
	if !validInstance(instance) {
		return "", errors.New("invalid guest pipe instance")
	}
	return `\\.\pipe\windows-ci-sdk-2737-` + instance, nil
}

func mustCOMPipe(instance string) string {
	pipe, _ := COMPipeName(instance)
	return pipe
}

func BuildPublicSeed(instance string, client Identity) ([]byte, error) {
	if !validInstance(instance) || client.Leaf == nil || client.Leaf.Subject.CommonName != "windows-ci-sdk-2737/"+instance || !hasUsage(client.Leaf, x509.ExtKeyUsageClientAuth) {
		return nil, errors.New("host identity does not match the SDK instance")
	}
	return json.Marshal(PublicSeed{Instance: instance, ClientSHA256: CertificateHash(client.Leaf)})
}

func PrepareGuestSDK(com io.ReadWriter, instance string, client Identity) (PreparationReceipt, error) {
	if _, err := BuildPublicSeed(instance, client); err != nil {
		return PreparationReceipt{}, err
	}
	bootstrap, err := SendCOMBootstrap(com, instance)
	if err != nil {
		return PreparationReceipt{}, err
	}
	results, err := PrepareSDKSet(bootstrap.IPv4, bootstrap.ServerSHA256, instance, client)
	receipt := PreparationReceipt{Instance: instance, GuestIPv4: bootstrap.IPv4.String(), ServerSHA256: bootstrap.ServerSHA256, Results: results}
	if err != nil {
		return receipt, err
	}
	return receipt, nil
}

// SendCOMBootstrap receives only public identity metadata, then challenges the
// bidirectional COM channel. The caller supplies the exact new VM's COM stream.
func SendCOMBootstrap(stream io.ReadWriter, instance string) (BootstrapResult, error) {
	if stream == nil || !validInstance(instance) {
		return BootstrapResult{}, errors.New("invalid COM bootstrap context")
	}
	line, err := readBoundedLine(bufio.NewReader(stream), 4096)
	if err != nil {
		return BootstrapResult{}, errors.New("COM bootstrap response unavailable")
	}
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	var reply comReply
	if dec.Decode(&reply) != nil || dec.Decode(new(json.RawMessage)) != io.EOF || reply.Kind != "sdk-ready" || reply.Instance != instance || !validDigest(reply.ServerSHA256) {
		return BootstrapResult{}, errors.New("COM bootstrap response rejected")
	}
	ip := net.ParseIP(reply.IPv4)
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return BootstrapResult{}, errors.New("COM bootstrap private IPv4 rejected")
	}
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return BootstrapResult{}, errors.New("COM probe nonce generation failed")
	}
	nonceText := hex.EncodeToString(nonce[:])
	probe, err := json.Marshal(comRequest{Kind: "sdk-probe", Instance: instance, Nonce: nonceText})
	if err != nil {
		return BootstrapResult{}, errors.New("COM probe encoding failed")
	}
	if _, err := stream.Write(append(probe, '\n')); err != nil {
		return BootstrapResult{}, errors.New("COM probe write failed")
	}
	ackLine, err := readBoundedLine(bufio.NewReader(stream), 4096)
	if err != nil {
		return BootstrapResult{}, errors.New("COM probe acknowledgement unavailable")
	}
	ackDecoder := json.NewDecoder(bytes.NewReader(ackLine))
	ackDecoder.DisallowUnknownFields()
	var ack comRequest
	if ackDecoder.Decode(&ack) != nil || ackDecoder.Decode(new(json.RawMessage)) != io.EOF || ack.Kind != "sdk-probe-ack" || ack.Instance != instance || ack.Nonce != nonceText {
		return BootstrapResult{}, errors.New("COM probe acknowledgement rejected")
	}
	return BootstrapResult{IPv4: ip.To4(), ServerSHA256: reply.ServerSHA256}, nil
}

func readBoundedLine(reader *bufio.Reader, max int) ([]byte, error) {
	line := make([]byte, 0, 256)
	for len(line) < max {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == '\n' {
			return bytes.TrimSpace(line), nil
		}
		if b != '\r' {
			line = append(line, b)
		}
	}
	return nil, errors.New("line exceeds limit")
}

func validInstance(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// ValidateArtifact enforces a pinned, public HTTPS artifact and a lowercase
// SHA-256 digest before any bytes can be accepted by the guest.
func ValidateArtifact(a Artifact) error {
	if !allowedPackage(a.ID) {
		return errors.New("unknown SDK package")
	}
	if version, ok := pinnedVersion(a.ID); !ok || a.Version != version {
		return errors.New("SDK version is not pinned for this package")
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || !allowedHost(u.Hostname()) {
		return errors.New("SDK URL is outside the public download allowlist")
	}
	if len(a.SHA256) != sha256.Size*2 || strings.ToLower(a.SHA256) != a.SHA256 {
		return errors.New("invalid SHA-256 digest")
	}
	if _, err := hex.DecodeString(a.SHA256); err != nil {
		return errors.New("invalid SHA-256 digest")
	}
	return nil
}

func VerifyArtifact(r io.Reader, a Artifact) (int64, error) {
	if err := ValidateArtifact(a); err != nil {
		return 0, err
	}
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return n, fmt.Errorf("read SDK artifact: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return n, errors.New("SDK artifact SHA-256 mismatch")
	}
	return n, nil
}

func DecodeRequest(r io.Reader) (Request, error) {
	dec := json.NewDecoder(io.LimitReader(r, 4097))
	dec.DisallowUnknownFields()
	var req Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, errors.New("invalid request")
	}
	var extra json.RawMessage
	if err := dec.Decode(&extra); err != io.EOF {
		return Request{}, errors.New("request must contain exactly one JSON value")
	}
	if err := ValidateRequest(req); err != nil {
		return Request{}, err
	}
	return req, nil
}

func ValidateRequest(req Request) error {
	if req.Instance == "" || len(req.Instance) > 128 || strings.ContainsAny(req.Instance, "\r\n\x00") {
		return errors.New("invalid guest instance")
	}
	switch req.Operation {
	case OpStatus:
		if req.Package != "" {
			return errors.New("status request cannot name a package")
		}
	case OpPrepare, OpVersions:
		if !allowedPackage(req.Package) {
			return errors.New("unknown SDK package")
		}
	default:
		return errors.New("operation is not allowed")
	}
	return nil
}

func allowedPackage(id PackageID) bool {
	switch id {
	case GitForWindows, PowerShell7, RustToolchain, MSVCBuildTools, WindowsSDK, Sccache:
		return true
	default:
		return false
	}
}

func allowedHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com", "objects.githubusercontent.com", "github-releases.githubusercontent.com", "release-assets.githubusercontent.com",
		"gitforwindows.org", "aka.ms", "go.microsoft.com", "download.microsoft.com", "software-download.microsoft.com", "software-static.download.prss.microsoft.com",
		"download.visualstudio.microsoft.com", "static.rust-lang.org", "rustup.rs", "learn.microsoft.com":
		return true
	default:
		return false
	}
}

// IsSingleJSON is used by receipt readers that must reject appended data.
func IsSingleJSON(data []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(data))
	var value json.RawMessage
	if dec.Decode(&value) != nil {
		return false
	}
	return dec.Decode(&value) == io.EOF
}
