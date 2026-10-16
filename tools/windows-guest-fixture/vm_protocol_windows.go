//go:build windows && amd64

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

const guestServiceName = "CCGuest2746"

type vmSeed struct {
	Instance     string `json:"instance"`
	ClientSHA256 string `json:"client_certificate_sha256"`
}

type vmReady struct {
	Instance     string `json:"instance"`
	ServerSHA256 string `json:"server_certificate_sha256"`
}

type vmRequest struct {
	Kind     string `json:"kind"`
	Instance string `json:"instance"`
}

type vmStage struct {
	Kind  string `json:"kind"`
	PIDs  []int  `json:"child_pids"`
	Alive bool   `json:"children_observed_alive"`
}

type vmReply struct {
	Kind   string         `json:"kind"`
	Result *fixtureResult `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type vmIdentity struct {
	Certificate tls.Certificate
	SHA256      string
}

func newVMIdentity() (vmIdentity, error) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return vmIdentity{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		clear(private)
		return vmIdentity{}, err
	}
	now := time.Now()
	cert := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "cc-todo-2746-ephemeral"},
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &cert, &cert, public, private)
	if err != nil {
		clear(private)
		return vmIdentity{}, err
	}
	hash := sha256.Sum256(der)
	return vmIdentity{Certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: private}, SHA256: hex.EncodeToString(hash[:])}, nil
}

func pinnedPeer(expected string) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		if !validHash(expected) || len(state.PeerCertificates) != 1 {
			return errors.New("peer certificate rejected")
		}
		hash := sha256.Sum256(state.PeerCertificates[0].Raw)
		if !strings.EqualFold(hex.EncodeToString(hash[:]), expected) {
			return errors.New("peer certificate pin mismatch")
		}
		return nil
	}
}

func vmServerTLS(identity vmIdentity, clientPin string) *tls.Config {
	return &tls.Config{
		MinVersion:       tls.VersionTLS13,
		Certificates:     []tls.Certificate{identity.Certificate},
		ClientAuth:       tls.RequireAnyClientCert,
		VerifyConnection: pinnedPeer(clientPin),
	}
}

func vmClientTLS(identity vmIdentity, serverPin string) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{identity.Certificate},
		InsecureSkipVerify: true, // The exact ephemeral server certificate is pinned below.
		VerifyConnection:   pinnedPeer(serverPin),
	}
}

func tlsServerConn(file *os.File, identity vmIdentity, clientPin string) *tls.Conn {
	return tls.Server(&serialConn{file: file}, vmServerTLS(identity, clientPin))
}

func validHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == sha256.Size
}

func validInstance(value string) bool {
	if len(value) != 32 {
		return false
	}
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 16
}

func decodeVMRequest(decoder *json.Decoder, instance string) (vmRequest, error) {
	decoder.DisallowUnknownFields()
	var request vmRequest
	if err := decoder.Decode(&request); err != nil {
		return request, err
	}
	if request.Instance != instance || !validInstance(instance) {
		return request, errors.New("VM instance binding rejected")
	}
	if request.Kind != "normal" && request.Kind != "cancel" && request.Kind != "shutdown" && request.Kind != "cancel-confirm" {
		return request, errors.New("VM fixture request kind rejected")
	}
	return request, nil
}

func writeJSONLine(writer io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = writer.Write(data)
	return err
}

func readJSONLine(reader io.Reader, limit int) ([]byte, error) {
	var line []byte
	var one [1]byte
	for len(line) < limit {
		n, err := reader.Read(one[:])
		if err != nil {
			return nil, err
		}
		if n != 1 {
			continue
		}
		if one[0] == '\n' {
			return line, nil
		}
		if one[0] != '\r' {
			line = append(line, one[0])
		}
	}
	return nil, fmt.Errorf("protocol line exceeds %d bytes", limit)
}

type serialConn struct{ file io.ReadWriteCloser }

func (c *serialConn) Read(p []byte) (int, error)       { return c.file.Read(p) }
func (c *serialConn) Write(p []byte) (int, error)      { return c.file.Write(p) }
func (c *serialConn) Close() error                     { return c.file.Close() }
func (c *serialConn) LocalAddr() net.Addr              { return vmSerialAddr("host") }
func (c *serialConn) RemoteAddr() net.Addr             { return vmSerialAddr("guest") }
func (c *serialConn) SetDeadline(time.Time) error      { return nil }
func (c *serialConn) SetReadDeadline(time.Time) error  { return nil }
func (c *serialConn) SetWriteDeadline(time.Time) error { return nil }

type vmSerialAddr string

func (a vmSerialAddr) Network() string { return "hyperv-com" }
func (a vmSerialAddr) String() string  { return string(a) }
