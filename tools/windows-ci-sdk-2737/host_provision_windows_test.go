//go:build windows && amd64

package sdkprep

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
)

func TestClassifyNetworkFailureDoesNotExposeRawTransportDetails(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "no such host", IsNotFound: true}, "name-resolution"},
		{"proxy", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("proxyconnect tcp: private proxy endpoint")}, "proxy-connect"},
		{"connection", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("dial tcp: connection refused")}, "connection-refused"},
		{"unknown", &url.Error{Op: "Get", URL: "https://aka.ms/", Err: errors.New("unexpected transport detail")}, "network-or-protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNetworkFailure(tc.err); got != tc.want {
				t.Fatalf("classification=%q want=%q", got, tc.want)
			}
		})
	}
}

func TestValidatePreparedISORequiresMatchingSignatureAndSizeEvidence(t *testing.T) {
	bytes := []byte("public ISO")
	sum := sha256.Sum256(bytes)
	digest := hex.EncodeToString(sum[:])
	measure := []byte(fmt.Sprintf("size=%d sha256=%s\n", len(bytes), digest))
	receipt, err := json.Marshal(evaluationISOValidationReceipt{
		Source:      "Microsoft Evaluation Center HTTPS",
		ISOSize:     int64(len(bytes)),
		ISOSHA256:   digest,
		SetupStatus: "Valid",
		SetupSigner: "Microsoft Corporation",
		Edition:     "Windows Server 2025 Standard Evaluation",
		ValidatedAt: "2026-10-18T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if size, gotDigest, err := validatePreparedISOEvidence(measure, receipt, int64(len(bytes))); err != nil || size != int64(len(bytes)) || gotDigest != digest {
		t.Fatalf("prepared evidence rejected: size=%d digest_matches=%t err=%v", size, gotDigest == digest, err)
	}
	if _, _, err := validatePreparedISOEvidence(measure, receipt, int64(len(bytes)+1)); err == nil {
		t.Fatal("file size mismatch accepted")
	}
	badReceipt, err := json.Marshal(evaluationISOValidationReceipt{
		Source: "Microsoft Evaluation Center HTTPS", ISOSize: int64(len(bytes)), ISOSHA256: digest,
		SetupStatus: "Valid", SetupSigner: "Untrusted", Edition: "Windows Server 2025 Standard Evaluation", ValidatedAt: "2026-10-18T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePreparedISOEvidence(measure, badReceipt, int64(len(bytes))); err == nil {
		t.Fatal("untrusted setup signer accepted")
	}
}

func TestAdminBrokerScriptsEmbedTheOwnedRoot(t *testing.T) {
	root := `D:\CC\owned root's`
	for name, script := range map[string][]byte{"provision": provisionVMPS, "recovery": removeVMKeepVHDPS} {
		t.Run(name, func(t *testing.T) {
			rendered, err := renderAdminPowerShellScript(script, root)
			if err != nil {
				t.Fatal(err)
			}
			text := string(rendered)
			if strings.Contains(text, ownedRootPlaceholder) || strings.Contains(text, "$PSScriptRoot") || !strings.Contains(text, `[IO.Path]::GetFullPath('D:\CC\owned root''s')`) {
				t.Fatal("admin script did not embed the fixed owned root")
			}
		})
	}
}
