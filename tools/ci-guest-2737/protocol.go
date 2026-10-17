// This helper prepares a disposable guest. It is not an official CI executor.
package main

import (
	sourcepacket "cc/ci-source-2737"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net"
	"time"
)

const guestPort = 27373
const publicRunnerImage = "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011"
const publicSccacheSHA = "45f1447fbe231e3037bde351ef70677dd212216c8d62ae7ca409fecc4d6acc89"

func fixedSourcePin() sourcepacket.Pin {
	return sourcepacket.Pin{HeadSHA: "b194d66524ef86330bb73ca3f22f9e5dd6ae0a07", TreeSHA: "f7eea82c96449b7749f98dd6c4b0344b64f1d651", WorkflowSHA256: "01d3351d4a6bce1faeb7e3b1eaf29555affcc304ea7b91caa979825cc94bc4b0"}
}

type sourceResult struct {
	ID         string               `json:"id"`
	Proof      sourcepacket.Receipt `json:"proof"`
	Failure    string               `json:"failure,omitempty"`
	OfficialCI bool                 `json:"official_ci_success"`
}

const offlineLabel = "cc-product-2737-1154066087-offline-bootstrap"

type offlineConfiguration struct{ Name, Token string }
type offlineResult struct {
	ID         string `json:"id"`
	Configured bool   `json:"configured"`
	Failure    string `json:"failure,omitempty"`
}

type sdkManifest struct {
	Image      string `json:"image"`
	RunnerSHA  string `json:"runner_sha256"`
	RustupSHA  string `json:"rustup_sha256"`
	SccacheSHA string `json:"sccache_sha256"`
}
type sdkResult struct {
	ID               string `json:"id"`
	RunnerVersion    string `json:"runner_version,omitempty"`
	CargoVersion     string `json:"cargo_version,omitempty"`
	DockerVersion    string `json:"docker_version,omitempty"`
	BuildxVersion    string `json:"buildx_version,omitempty"`
	SccacheVersion   string `json:"sccache_version,omitempty"`
	Failure          string `json:"failure,omitempty"`
	ProductSource    bool   `json:"product_source"`
	RunnerRegistered bool   `json:"runner_registered"`
	Log              []byte `json:"log,omitempty"`
}

func writeBlob(w io.Writer, b []byte) error {
	if e := binary.Write(w, binary.BigEndian, uint64(len(b))); e != nil {
		return e
	}
	_, e := w.Write(b)
	return e
}
func readBlob(r io.Reader, limit uint64) ([]byte, error) {
	var n uint64
	if e := binary.Read(r, binary.BigEndian, &n); e != nil {
		return nil, e
	}
	if n == 0 || n > limit {
		return nil, errors.New("SDK frame size rejected")
	}
	b := make([]byte, int(n))
	_, e := io.ReadFull(r, b)
	return b, e
}

type publicConfig struct {
	Instance          string `json:"instance"`
	ClientCertificate string `json:"client_certificate_sha256"`
}
type ready struct {
	Instance    string `json:"instance"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Certificate string `json:"server_certificate_sha256"`
}
type request struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type fixtureResult struct {
	ID        string `json:"id"`
	Parent    int    `json:"parent_pid"`
	Child     int    `json:"child_pid"`
	Group     int    `json:"process_group"`
	Observed  bool   `json:"child_observed"`
	Cancelled bool   `json:"cancelled"`
	Done      bool   `json:"done"`
	Gone      bool   `json:"group_gone"`
	Failure   string `json:"failure,omitempty"`
}
type identity struct {
	Certificate tls.Certificate
	Digest      string
}

func hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	_, e := io.Copy(h, r)
	return hex.EncodeToString(h.Sum(nil)), e
}
func newIdentity() (identity, error) {
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return identity{}, e
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if e != nil {
		return identity{}, e
	}
	cert := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "ci2737-ephemeral"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(2 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	der, e := x509.CreateCertificate(rand.Reader, &cert, &cert, pub, key)
	if e != nil {
		return identity{}, e
	}
	return identity{tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, hash(der)}, nil
}
func validHash(s string) bool { return len(s) == 64 && lenFromHex(s) == 32 }
func lenFromHex(s string) int {
	b, e := hex.DecodeString(s)
	if e != nil {
		return -1
	}
	return len(b)
}
func validInstance(s string) bool { return len(s) == 32 && lenFromHex(s) == 16 }
func verifyPeer(expected string) func(tls.ConnectionState) error {
	return func(cs tls.ConnectionState) error {
		if !validHash(expected) || len(cs.PeerCertificates) != 1 || hash(cs.PeerCertificates[0].Raw) != expected {
			return errors.New("guest certificate rejected")
		}
		return nil
	}
}
func serverTLS(i identity, client string) *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{i.Certificate}, ClientAuth: tls.RequireAnyClientCert, VerifyConnection: verifyPeer(client)}
}
func clientTLS(i identity, server string) *tls.Config {
	// The public certificate pin arrives only through this owned VM's COM pipe.
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{i.Certificate}, InsecureSkipVerify: true, VerifyConnection: verifyPeer(server)}
}
func validateReady(r ready, instance string) error {
	ip := net.ParseIP(r.IP)
	if r.Instance != instance || !validInstance(instance) || ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || r.Port != guestPort || !validHash(r.Certificate) {
		return errors.New("owned guest readiness rejected")
	}
	return nil
}
func decodeRequest(d *json.Decoder) (request, error) {
	d.DisallowUnknownFields()
	var q request
	if e := d.Decode(&q); e != nil {
		return q, e
	}
	if (q.Kind != "normal" && q.Kind != "cancel" && q.Kind != "status" && q.Kind != "continue" && q.Kind != "prepare-sdk" && q.Kind != "materialize-source" && q.Kind != "configure-offline") || !validInstance(q.ID) {
		return q, errors.New("fixture request rejected")
	}
	return q, nil
}
