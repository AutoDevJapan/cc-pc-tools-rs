package sdkprep

import (
	"bytes"
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
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const ServicePort = 38473

type Identity struct {
	Certificate tls.Certificate
	Leaf        *x509.Certificate
}

func NewIdentity(instance string, serverIP net.IP, usage x509.ExtKeyUsage) (Identity, error) {
	if !validInstance(instance) {
		return Identity{}, errors.New("invalid identity instance")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, errors.New("ephemeral identity generation failed")
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		clear(privateKey)
		return Identity{}, errors.New("ephemeral certificate serial generation failed")
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "windows-ci-sdk-2737/" + instance},
		NotBefore:    now.Add(-time.Minute), NotAfter: now.Add(20 * time.Minute),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{usage},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if usage == x509.ExtKeyUsageServerAuth {
		if ip := serverIP.To4(); ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			clear(privateKey)
			return Identity{}, errors.New("guest service must bind to a private IPv4 address")
		}
		tmpl.IPAddresses = []net.IP{serverIP.To4()}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, publicKey, privateKey)
	if err != nil {
		clear(privateKey)
		return Identity{}, errors.New("ephemeral certificate creation failed")
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		clear(privateKey)
		return Identity{}, errors.New("ephemeral certificate parse failed")
	}
	return Identity{Certificate: tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey, Leaf: leaf}, Leaf: leaf}, nil
}

func CertificateHash(cert *x509.Certificate) string {
	if cert == nil {
		return ""
	}
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}

func clearIdentity(identity *Identity) {
	if identity == nil {
		return
	}
	if key, ok := identity.Certificate.PrivateKey.(ed25519.PrivateKey); ok {
		clear(key)
	}
	identity.Certificate.PrivateKey = nil
}

func ServerTLSConfig(server Identity, clientHash string) (*tls.Config, error) {
	if server.Leaf == nil || len(server.Certificate.Certificate) == 0 || len(clientHash) != sha256.Size*2 {
		return nil, errors.New("ephemeral mTLS identity is incomplete")
	}
	if _, err := hex.DecodeString(clientHash); err != nil || !hasUsage(server.Leaf, x509.ExtKeyUsageServerAuth) {
		return nil, errors.New("mTLS client certificate pin or server identity is invalid")
	}
	wantClient := strings.ToLower(clientHash)
	instance := server.Leaf.Subject.CommonName
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{server.Certificate},
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) != 1 {
				return errors.New("unexpected mTLS certificate chain")
			}
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil || CertificateHash(cert) != wantClient || cert.Subject.CommonName != instance || !hasUsage(cert, x509.ExtKeyUsageClientAuth) || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) {
				return errors.New("mTLS client certificate pin mismatch")
			}
			if err := cert.CheckSignatureFrom(cert); err != nil {
				return errors.New("mTLS client certificate is not self-signed")
			}
			return nil
		},
	}, nil
}

func ClientTLSConfig(client Identity, serverIP net.IP, serverHash string) (*tls.Config, error) {
	if client.Leaf == nil || len(client.Certificate.Certificate) == 0 || len(serverHash) != sha256.Size*2 || serverIP.To4() == nil || !serverIP.IsPrivate() || serverIP.IsLoopback() || serverIP.IsLinkLocalUnicast() || !hasUsage(client.Leaf, x509.ExtKeyUsageClientAuth) {
		return nil, errors.New("ephemeral client identity or server pin is invalid")
	}
	if _, err := hex.DecodeString(serverHash); err != nil {
		return nil, errors.New("server certificate pin is invalid")
	}
	return &tls.Config{
		MinVersion:         tls.VersionTLS13,
		Certificates:       []tls.Certificate{client.Certificate},
		InsecureSkipVerify: true, // Verification is the exact certificate pin below.
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) != 1 {
				return errors.New("unexpected mTLS server certificate chain")
			}
			cert, err := x509.ParseCertificate(rawCerts[0])
			if err != nil || CertificateHash(cert) != strings.ToLower(serverHash) || cert.Subject.CommonName != client.Leaf.Subject.CommonName || !hasUsage(cert, x509.ExtKeyUsageServerAuth) || time.Now().Before(cert.NotBefore) || time.Now().After(cert.NotAfter) || cert.VerifyHostname(serverIP.To4().String()) != nil {
				return errors.New("mTLS server certificate pin mismatch")
			}
			if err := cert.CheckSignatureFrom(cert); err != nil {
				return errors.New("mTLS server certificate is not self-signed")
			}
			return nil
		},
	}, nil
}

func hasUsage(cert *x509.Certificate, want x509.ExtKeyUsage) bool {
	if cert == nil {
		return false
	}
	for _, usage := range cert.ExtKeyUsage {
		if usage == want {
			return true
		}
	}
	return false
}

// SendRequest contacts only the instance-bound private IPv4 service endpoint.
// The response is bounded and must echo the exact request identity and action.
func SendRequest(ip net.IP, serverHash string, client Identity, req Request) (Result, error) {
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return Result{}, errors.New("guest endpoint is not a private IPv4 address")
	}
	if err := ValidateRequest(req); err != nil {
		return Result{}, err
	}
	if client.Leaf == nil || client.Leaf.Subject.CommonName != "windows-ci-sdk-2737/"+req.Instance || !hasUsage(client.Leaf, x509.ExtKeyUsageClientAuth) {
		return Result{}, errors.New("guest request identity does not match its instance")
	}
	tlsConfig, err := ClientTLSConfig(client, ip, serverHash)
	if err != nil {
		return Result{}, err
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig, ForceAttemptHTTP2: false, DisableKeepAlives: true, DisableCompression: true, Proxy: nil}
	defer transport.CloseIdleConnections()
	clientHTTP := &http.Client{Transport: transport, Timeout: 60 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	payload, err := json.Marshal(req)
	if err != nil {
		return Result{}, errors.New("request encoding failed")
	}
	endpoint := url.URL{Scheme: "https", Host: net.JoinHostPort(ip.To4().String(), fmt.Sprint(ServicePort)), Path: "/v1/request"}
	httpReq, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return Result{}, errors.New("request construction failed")
	}
	httpReq.Header.Set("Content-Type", "application/json")
	response, err := clientHTTP.Do(httpReq)
	if err != nil {
		return Result{}, errors.New("guest SDK request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Result{}, errors.New("guest SDK request was rejected")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(body) > 4096 {
		return Result{}, errors.New("guest SDK response is invalid")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var result Result
	if err := dec.Decode(&result); err != nil {
		return Result{}, errors.New("guest SDK response is invalid")
	}
	if dec.Decode(new(json.RawMessage)) != io.EOF || result.Instance != req.Instance || result.Operation != req.Operation || result.Package != req.Package || result.Stage == "" || strings.ContainsAny(result.Stage+result.Code, "\r\n\x00") {
		return Result{}, errors.New("guest SDK response does not match the request")
	}
	return result, nil
}

func NewHostIdentity(instance string) (Identity, error) {
	return NewIdentity(instance, nil, x509.ExtKeyUsageClientAuth)
}

func PrepareSDKSet(ip net.IP, serverHash, instance string, client Identity) ([]Result, error) {
	installOrder := []PackageID{GitForWindows, PowerShell7, RustToolchain, MSVCBuildTools, Sccache}
	verifyOrder := []PackageID{GitForWindows, PowerShell7, RustToolchain, MSVCBuildTools, WindowsSDK, Sccache}
	results := make([]Result, 0, len(installOrder)+len(verifyOrder))
	for _, id := range installOrder {
		prepared, err := SendRequest(ip, serverHash, client, Request{Instance: instance, Operation: OpPrepare, Package: id})
		if err != nil || !prepared.OK || prepared.Stage != "prepared" {
			return results, fmt.Errorf("SDK preparation failed for %s", id)
		}
		results = append(results, prepared)
	}
	for _, id := range verifyOrder {
		verified, err := SendRequest(ip, serverHash, client, Request{Instance: instance, Operation: OpVersions, Package: id})
		if err != nil || !verified.OK || verified.Stage != "version-confirmed" {
			return results, fmt.Errorf("SDK version check failed for %s", id)
		}
		results = append(results, verified)
	}
	return results, nil
}

type Operations interface {
	Status(instance string) Result
	Prepare(instance string, id PackageID) Result
	Verify(instance string, id PackageID) Result
}

func NewHandler(instance string, operations Operations) (http.Handler, error) {
	if instance == "" || strings.ContainsAny(instance, "\r\n\x00") || operations == nil {
		return nil, errors.New("invalid SDK service configuration")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/request" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if r.ContentLength > 4096 || mediaErr != nil || mediaType != "application/json" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		req, err := DecodeRequest(http.MaxBytesReader(w, r.Body, 4096))
		if err != nil || req.Instance != instance {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var result Result
		switch req.Operation {
		case OpStatus:
			result = operations.Status(instance)
		case OpPrepare:
			result = operations.Prepare(instance, req.Package)
		case OpVersions:
			result = operations.Verify(instance, req.Package)
		default:
			http.Error(w, "operation is not allowed", http.StatusBadRequest)
			return
		}
		result.Instance, result.Operation, result.Package = instance, req.Operation, req.Package
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(result)
	}), nil
}

func ListenPrivateIPv4(ip net.IP, handler http.Handler, tlsConfig *tls.Config) (*http.Server, net.Listener, error) {
	if ip == nil || ip.To4() == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || handler == nil || tlsConfig == nil || tlsConfig.MinVersion < tls.VersionTLS13 || tlsConfig.ClientAuth != tls.RequireAnyClientCert || tlsConfig.VerifyPeerCertificate == nil || len(tlsConfig.Certificates) != 1 || tlsConfig.Certificates[0].Leaf == nil || tlsConfig.Certificates[0].Leaf.VerifyHostname(ip.To4().String()) != nil {
		return nil, nil, errors.New("invalid private guest service listener")
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(ip.To4().String(), fmt.Sprint(ServicePort)))
	if err != nil {
		return nil, nil, errors.New("private guest service bind failed")
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Minute, IdleTimeout: 20 * time.Second, MaxHeaderBytes: 4096, TLSConfig: tlsConfig}
	return server, tls.NewListener(listener, tlsConfig), nil
}
