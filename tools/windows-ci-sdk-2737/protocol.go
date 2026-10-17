package sdkprep

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	Instance  string    `json:"instance"`
	Operation Operation `json:"operation"`
	Package   PackageID `json:"package,omitempty"`
	Stage     string    `json:"stage"`
	OK        bool      `json:"ok"`
	Code      string    `json:"code,omitempty"`
}

type Artifact struct {
	ID      PackageID `json:"id"`
	Version string    `json:"version"`
	URL     string    `json:"url"`
	SHA256  string    `json:"sha256"`
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
		"gitforwindows.org", "aka.ms", "download.visualstudio.microsoft.com",
		"static.rust-lang.org", "rustup.rs", "learn.microsoft.com":
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
