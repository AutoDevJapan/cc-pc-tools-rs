//go:build windows && amd64

package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strings"
	"sync"
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

type vmProbe struct {
	Kind     string `json:"kind"`
	Instance string `json:"instance"`
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
	return tls.Server(newSerialFrameConn(&serialConn{file: file}), vmServerTLS(identity, clientPin))
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

const serialFrameLimit = 1 << 20

type serialFrameConn struct {
	raw     net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
	pending []byte
}

func newSerialFrameConn(raw net.Conn) *serialFrameConn {
	return &serialFrameConn{raw: raw, reader: bufio.NewReaderSize(raw, 64*1024)}
}

func (c *serialFrameConn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(c.pending) == 0 {
		line := make([]byte, 0, 64*1024)
		for {
			part, err := c.reader.ReadSlice('\n')
			if len(line)+len(part) > base64.StdEncoding.EncodedLen(serialFrameLimit)+1 {
				return 0, errors.New("serial TLS frame exceeds limit")
			}
			line = append(line, part...)
			if err == nil {
				break
			}
			if !errors.Is(err, bufio.ErrBufferFull) {
				return 0, err
			}
		}
		if len(line) == 0 || line[len(line)-1] != '\n' {
			return 0, errors.New("serial TLS frame delimiter missing")
		}
		decoded := make([]byte, base64.StdEncoding.DecodedLen(len(line)-1))
		n, err := base64.StdEncoding.Decode(decoded, line[:len(line)-1])
		if err != nil {
			return 0, errors.New("serial TLS frame encoding rejected")
		}
		c.pending = decoded[:n]
	}
	n := copy(p, c.pending)
	c.pending = c.pending[n:]
	return n, nil
}

func (c *serialFrameConn) Write(p []byte) (int, error) {
	if len(p) > serialFrameLimit {
		return 0, errors.New("serial TLS frame exceeds limit")
	}
	frame := make([]byte, base64.StdEncoding.EncodedLen(len(p))+1)
	base64.StdEncoding.Encode(frame[:len(frame)-1], p)
	frame[len(frame)-1] = '\n'
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for offset := 0; offset < len(frame); {
		n, err := c.raw.Write(frame[offset:])
		offset += n
		if err != nil {
			return 0, err
		}
		if n == 0 {
			return 0, io.ErrShortWrite
		}
	}
	return len(p), nil
}

func (c *serialFrameConn) Close() error                       { return c.raw.Close() }
func (c *serialFrameConn) LocalAddr() net.Addr                { return c.raw.LocalAddr() }
func (c *serialFrameConn) RemoteAddr() net.Addr               { return c.raw.RemoteAddr() }
func (c *serialFrameConn) SetDeadline(t time.Time) error      { return c.raw.SetDeadline(t) }
func (c *serialFrameConn) SetReadDeadline(t time.Time) error  { return c.raw.SetReadDeadline(t) }
func (c *serialFrameConn) SetWriteDeadline(t time.Time) error { return c.raw.SetWriteDeadline(t) }

type vmSerialAddr string

func (a vmSerialAddr) Network() string { return "hyperv-com" }
func (a vmSerialAddr) String() string  { return string(a) }
