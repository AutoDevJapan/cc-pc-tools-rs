package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// Opt-in check of the public build attestation for one fixed GoDD-Matrix
// artifact: the zip produced by run 44655701843 (attempt 1, workflow_dispatch
// on main at ece931cb71c191ab1b977629e3fdac1846bb07d1).
//
// The attestation bundles are fetched by this test from the public GitHub
// attestation listing and are untrusted input. The Sigstore public-good trust
// root obtained through TUF, the identity policy and the subject digest in this
// file are the authority. A passing run says nothing about any installed
// program and grants no permission to read, replace or start one.

const (
	matrixRepoID       = int64(1293650669)
	matrixRepoIDText   = "1293650669"
	matrixRepoURI      = "https://github.com/AutoDevJapan/GoDD-Matrix"
	matrixOwnerIDText  = "246834983"
	matrixOwnerURI     = "https://github.com/AutoDevJapan"
	matrixSource       = "ece931cb71c191ab1b977629e3fdac1846bb07d1"
	matrixWorkflowURI  = "https://github.com/AutoDevJapan/GoDD-Matrix/.github/workflows/mcp-artifact.yml@refs/heads/main"
	matrixIssuer       = "https://token.actions.githubusercontent.com"
	matrixRunURI       = "https://github.com/AutoDevJapan/GoDD-Matrix/actions/runs/44655701843/attempts/1"
	matrixSubjectName  = "godd-matrix-mcp-ece931cb71c191ab1b977629e3fdac1846bb07d1.zip"
	matrixSubjectSHA   = "14fd21b3e610b0cf7b307eb7746dd9d4ae99c4fb7744722f6c9296f096bb20e3"
	matrixRekorIndex   = "4013169934"
	matrixListingURL   = "https://api.github.com/repos/AutoDevJapan/GoDD-Matrix/attestations/sha256:" + matrixSubjectSHA + "?per_page=100"
	matrixReportName   = "matrix-main-attestation-report.json"
	matrixReportDomain = "matrix-main-attestation-actual-v1"
	matrixStatementV1  = "https://in-toto.io/Statement/v1"
	matrixPredicateV1  = "https://slsa.dev/provenance/v1"
	matrixJobBudget    = 20 * time.Minute
)

// The workflow run was created at this time and its job timeout is 20
// minutes, so a signing time outside the window cannot belong to that run.
var matrixRunCreated = time.Date(2026, time.November, 20, 1, 43, 37, 0, time.UTC)

var (
	errMatrixInput     = errors.New("matrix_input_invalid")
	errMatrixRejected  = errors.New("matrix_verification_rejected")
	errMatrixSelection = errors.New("matrix_selection_not_exactly_one")
	errMatrixStatement = errors.New("matrix_statement_mismatch")
	errMatrixTime      = errors.New("matrix_time_window_mismatch")
)

// Every field must stay non-empty: sigstore-go ignores an empty expected
// extension value, so a blank would silently switch that check off.
type matrixPolicy struct {
	Issuer        string
	SAN           string
	ConfigURI     string
	Revision      string
	Ref           string
	Trigger       string
	Runner        string
	Visibility    string
	RepositoryID  string
	RepositoryURI string
	OwnerID       string
	OwnerURI      string
	RunURI        string
	SubjectName   string
	SubjectSHA    string
}

func matrixMainPolicy() matrixPolicy {
	return matrixPolicy{
		Issuer:        matrixIssuer,
		SAN:           matrixWorkflowURI,
		ConfigURI:     matrixWorkflowURI,
		Revision:      matrixSource,
		Ref:           "refs/heads/main",
		Trigger:       "workflow_dispatch",
		Runner:        "github-hosted",
		Visibility:    "public",
		RepositoryID:  matrixRepoIDText,
		RepositoryURI: matrixRepoURI,
		OwnerID:       matrixOwnerIDText,
		OwnerURI:      matrixOwnerURI,
		RunURI:        matrixRunURI,
		SubjectName:   matrixSubjectName,
		SubjectSHA:    matrixSubjectSHA,
	}
}

// The policy fields that the certificate or the artifact digest enforce.
// SubjectName is enforced separately on the signed statement.
func matrixPolicyFieldNames() []string {
	return []string{"Issuer", "SAN", "ConfigURI", "Revision", "Ref", "Trigger", "Runner", "Visibility", "RepositoryID", "RepositoryURI", "OwnerID", "OwnerURI", "RunURI", "SubjectSHA"}
}

func matrixChangedFields(a, b matrixPolicy) []string {
	var out []string
	add := func(name, x, y string) {
		if x != y {
			out = append(out, name)
		}
	}
	add("Issuer", a.Issuer, b.Issuer)
	add("SAN", a.SAN, b.SAN)
	add("ConfigURI", a.ConfigURI, b.ConfigURI)
	add("Revision", a.Revision, b.Revision)
	add("Ref", a.Ref, b.Ref)
	add("Trigger", a.Trigger, b.Trigger)
	add("Runner", a.Runner, b.Runner)
	add("Visibility", a.Visibility, b.Visibility)
	add("RepositoryID", a.RepositoryID, b.RepositoryID)
	add("RepositoryURI", a.RepositoryURI, b.RepositoryURI)
	add("OwnerID", a.OwnerID, b.OwnerID)
	add("OwnerURI", a.OwnerURI, b.OwnerURI)
	add("RunURI", a.RunURI, b.RunURI)
	add("SubjectName", a.SubjectName, b.SubjectName)
	add("SubjectSHA", a.SubjectSHA, b.SubjectSHA)
	return out
}

func matrixPolicyComplete(p matrixPolicy) bool {
	values := []string{p.Issuer, p.SAN, p.ConfigURI, p.Revision, p.Ref, p.Trigger, p.Runner, p.Visibility, p.RepositoryID, p.RepositoryURI, p.OwnerID, p.OwnerURI, p.RunURI, p.SubjectName, p.SubjectSHA}
	for _, v := range values {
		if v == "" {
			return false
		}
	}
	return lowerHex(p.Revision, 40) && lowerHex(p.SubjectSHA, 64)
}

func matrixExtensions(p matrixPolicy) certificate.Extensions {
	return certificate.Extensions{
		BuildSignerURI:                      p.SAN,
		BuildSignerDigest:                   p.Revision,
		RunnerEnvironment:                   p.Runner,
		SourceRepositoryURI:                 p.RepositoryURI,
		SourceRepositoryDigest:              p.Revision,
		SourceRepositoryRef:                 p.Ref,
		SourceRepositoryIdentifier:          p.RepositoryID,
		SourceRepositoryOwnerURI:            p.OwnerURI,
		SourceRepositoryOwnerIdentifier:     p.OwnerID,
		BuildConfigURI:                      p.ConfigURI,
		BuildConfigDigest:                   p.Revision,
		BuildTrigger:                        p.Trigger,
		RunInvocationURI:                    p.RunURI,
		SourceRepositoryVisibilityAtSigning: p.Visibility,
	}
}

type matrixSubject struct {
	Name    string
	SHA256  string
	Digests int
}

type matrixStamp struct {
	Type string
	At   time.Time
}

// The values below are read only from a result that sigstore-go returned after
// it verified the transparency log, the timestamps, the certificate chain, the
// signed certificate timestamp, the DSSE signature, the artifact digest and the
// certificate identity.
type matrixVerified struct {
	StatementType string
	PredicateType string
	Subjects      []matrixSubject
	SAN           string
	Issuer        string
	RunURI        string
	Ref           string
	Trigger       string
	Stamps        []matrixStamp
}

func matrixVerify(ctx context.Context, trusted trustedMaterial, raw []byte, p matrixPolicy) (matrixVerified, error) {
	var out matrixVerified
	if ctx == nil || ctx.Err() != nil || trusted == nil || len(raw) == 0 || len(raw) > 4<<20 || !matrixPolicyComplete(p) {
		return out, errMatrixInput
	}
	digest, err := hex.DecodeString(p.SubjectSHA)
	if err != nil || len(digest) != sha256.Size {
		return out, errMatrixInput
	}
	var signed bundle.Bundle
	if err = signed.UnmarshalJSON(raw); err != nil || signed.Bundle == nil || signed.Bundle.GetDsseEnvelope() == nil {
		return out, errors.Join(errMatrixInput, err)
	}
	san, err := verify.NewSANMatcher(p.SAN, "")
	if err != nil {
		return out, errors.Join(errMatrixInput, err)
	}
	issuer, err := verify.NewIssuerMatcher(p.Issuer, "")
	if err != nil {
		return out, errors.Join(errMatrixInput, err)
	}
	identity, err := verify.NewCertificateIdentity(san, issuer, matrixExtensions(p))
	if err != nil {
		return out, errors.Join(errMatrixInput, err)
	}
	verifier, err := verify.NewVerifier(trusted, verify.WithSignedCertificateTimestamps(1), verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
	if err != nil {
		return out, errors.Join(errMatrixInput, err)
	}
	result, err := verifier.Verify(&signed, verify.NewPolicy(verify.WithArtifactDigest("sha256", digest), verify.WithCertificateIdentity(identity)))
	if err != nil {
		return out, errors.Join(errMatrixRejected, err)
	}
	if result == nil || result.Statement == nil || result.Signature == nil || result.Signature.Certificate == nil || ctx.Err() != nil {
		return out, errMatrixRejected
	}
	out.StatementType = result.Statement.GetType()
	out.PredicateType = result.Statement.GetPredicateType()
	for _, s := range result.Statement.GetSubject() {
		if s == nil {
			return matrixVerified{}, errMatrixRejected
		}
		out.Subjects = append(out.Subjects, matrixSubject{Name: s.GetName(), SHA256: s.GetDigest()["sha256"], Digests: len(s.GetDigest())})
	}
	cert := result.Signature.Certificate
	out.SAN = cert.SubjectAlternativeName
	out.Issuer = cert.Issuer
	out.RunURI = cert.RunInvocationURI
	out.Ref = cert.SourceRepositoryRef
	out.Trigger = cert.BuildTrigger
	for _, ts := range result.VerifiedTimestamps {
		out.Stamps = append(out.Stamps, matrixStamp{Type: ts.Type, At: ts.Timestamp.UTC()})
	}
	return out, nil
}

// An identity rejection means every cryptographic step passed and only the
// certificate identity policy refused the bundle.
func matrixIdentityRejected(err error) bool {
	var noMatch *verify.ErrNoMatchingCertificateIdentity
	return err != nil && errors.As(err, &noMatch)
}

func matrixCheckStatement(v matrixVerified, p matrixPolicy) error {
	if v.StatementType != matrixStatementV1 || v.PredicateType != matrixPredicateV1 {
		return errMatrixStatement
	}
	if len(v.Subjects) != 1 || v.Subjects[0].Name != p.SubjectName || v.Subjects[0].SHA256 != p.SubjectSHA || v.Subjects[0].Digests != 1 {
		return errMatrixStatement
	}
	if v.SAN != p.SAN || v.Issuer != p.Issuer || v.RunURI != p.RunURI || v.Ref != p.Ref || v.Trigger != p.Trigger {
		return errMatrixStatement
	}
	return nil
}

func matrixCheckTime(v matrixVerified) error {
	found := false
	for _, s := range v.Stamps {
		if s.Type != "Tlog" {
			continue
		}
		if s.At.Before(matrixRunCreated) || s.At.After(matrixRunCreated.Add(matrixJobBudget)) {
			return errMatrixTime
		}
		found = true
	}
	if !found {
		return errMatrixTime
	}
	return nil
}

func matrixAssertRejected(t *testing.T, err error, identity bool, want []string) {
	t.Helper()
	if err == nil {
		t.Fatal("rejected_control_was_accepted")
	}
	if got := matrixIdentityRejected(err); got != identity {
		t.Fatalf("rejected_at_unexpected_stage identity_stage=%t want_identity_stage=%t err=%v", got, identity, err)
	}
	if len(want) == 0 {
		return
	}
	text := err.Error()
	for _, w := range want {
		if strings.Contains(text, w) {
			return
		}
	}
	t.Fatalf("rejected_for_unexpected_reason want=%v err=%v", want, err)
}

type matrixEntry struct {
	RepositoryID int64           `json:"repository_id"`
	BundleURL    string          `json:"bundle_url"`
	Bundle       json.RawMessage `json:"bundle"`
}

// The listing is data only. Entries are never trusted until sigstore-go has
// verified them against the policy above.
func matrixParseListing(raw []byte) ([]matrixEntry, error) {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return nil, errMatrixInput
	}
	var listing struct {
		Attestations []matrixEntry `json:"attestations"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&listing); err != nil {
		return nil, errors.Join(errMatrixInput, err)
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errMatrixInput
	}
	if len(listing.Attestations) == 0 || len(listing.Attestations) > 100 {
		return nil, errMatrixInput
	}
	for _, e := range listing.Attestations {
		if len(e.Bundle) == 0 || bytes.Equal(e.Bundle, []byte("null")) {
			return nil, errMatrixInput
		}
	}
	return listing.Attestations, nil
}

// A next page would mean the listing is incomplete, so the one-bundle rule
// could not be judged. Other Link values are informational.
func matrixSinglePage(h http.Header) bool {
	for name, values := range h {
		if !strings.EqualFold(name, "Link") {
			continue
		}
		for _, v := range values {
			if strings.Contains(strings.ToLower(v), `rel="next"`) {
				return false
			}
		}
	}
	return true
}

type matrixFetch struct {
	Body          []byte
	Mode          string
	Status        int
	RateRemaining string
}

// One GET of the fixed listing URL. Redirects are not followed, so a token can
// reach only api.github.com. Neither the body nor the token is logged.
func matrixGetOnce(ctx context.Context, bearer string) ([]byte, int, http.Header, error) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(bounded, http.MethodGet, matrixListingURL, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	client := &http.Client{
		Timeout:       30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
	closeErr := resp.Body.Close()
	if readErr != nil || closeErr != nil || len(body) > 4<<20 {
		return nil, resp.StatusCode, resp.Header, errors.Join(errMatrixInput, readErr, closeErr)
	}
	return body, resp.StatusCode, resp.Header, nil
}

// The job token is used first when GH_TOKEN is set. A refused token request
// falls back to an anonymous one. Transport errors and 5xx answers are retried
// once; nothing is retried without limit.
func matrixFetchListing(ctx context.Context) (matrixFetch, error) {
	token := strings.TrimSpace(os.Getenv("GH_TOKEN"))
	modes := []string{"anonymous"}
	if token != "" {
		modes = []string{"token", "anonymous"}
	}
	var last matrixFetch
	for _, mode := range modes {
		bearer := ""
		if mode == "token" {
			bearer = token
		}
		for attempt := 1; attempt <= 2; attempt++ {
			body, status, header, err := matrixGetOnce(ctx, bearer)
			last = matrixFetch{Mode: mode, Status: status}
			if header != nil {
				last.RateRemaining = header.Get("X-RateLimit-Remaining")
			}
			if err == nil && status == http.StatusOK {
				if !matrixSinglePage(header) {
					return last, errors.Join(errMatrixInput, errors.New("listing_has_next_page"))
				}
				last.Body = body
				return last, nil
			}
			if err == nil && status >= 400 && status < 500 {
				break
			}
			select {
			case <-ctx.Done():
				return last, errors.Join(errMatrixInput, ctx.Err())
			case <-time.After(2 * time.Second):
			}
		}
	}
	return last, errors.Join(errMatrixInput, errors.New("listing_unavailable"))
}

type matrixTlogView struct {
	LogIndex json.Number `json:"logIndex"`
}

type matrixBundleView struct {
	VerificationMaterial struct {
		TlogEntries []matrixTlogView `json:"tlogEntries"`
		Certificate struct {
			RawBytes string `json:"rawBytes"`
		} `json:"certificate"`
	} `json:"verificationMaterial"`
}

// Labels for log lines and for choosing which controls were exercised. They
// come from an unverified bundle and never decide acceptance.
func matrixBundleFacts(raw []byte) (logIndex string, san string) {
	logIndex, san = "unparsed", "unparsed"
	var view matrixBundleView
	if json.Unmarshal(raw, &view) != nil {
		return logIndex, san
	}
	if len(view.VerificationMaterial.TlogEntries) > 0 {
		logIndex = view.VerificationMaterial.TlogEntries[0].LogIndex.String()
	}
	der, err := base64.StdEncoding.DecodeString(view.VerificationMaterial.Certificate.RawBytes)
	if err != nil {
		return logIndex, san
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil || len(cert.URIs) == 0 {
		return logIndex, san
	}
	return logIndex, cert.URIs[0].String()
}

func matrixURLTail(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path == "" {
		return "unparsed"
	}
	return u.Path[strings.LastIndex(u.Path, "/")+1:]
}

type matrixOutcome struct {
	Index     int
	RepoMatch bool
	Raw       []byte
	Verified  matrixVerified
	Err       error
	LogIndex  string
	SAN       string
	URLTail   string
}

func matrixEvaluate(ctx context.Context, trusted trustedMaterial, entries []matrixEntry, p matrixPolicy) []matrixOutcome {
	out := make([]matrixOutcome, 0, len(entries))
	for i, e := range entries {
		o := matrixOutcome{Index: i, RepoMatch: e.RepositoryID == matrixRepoID, URLTail: matrixURLTail(e.BundleURL)}
		var compact bytes.Buffer
		if json.Compact(&compact, e.Bundle) == nil {
			o.Raw = append([]byte(nil), compact.Bytes()...)
		} else {
			o.Raw = append([]byte(nil), e.Bundle...)
		}
		o.LogIndex, o.SAN = matrixBundleFacts(o.Raw)
		if o.RepoMatch {
			o.Verified, o.Err = matrixVerify(ctx, trusted, o.Raw, p)
		} else {
			o.Err = errMatrixInput
		}
		out = append(out, o)
	}
	return out
}

func matrixPullRequestEntry(o matrixOutcome) bool {
	if strings.Contains(o.SAN, "@refs/pull/") {
		return true
	}
	return o.Err != nil && strings.Contains(o.Err.Error(), "@refs/pull/")
}

func matrixOutcomeLabel(o matrixOutcome) string {
	switch {
	case !o.RepoMatch:
		return "ignored_other_repository"
	case o.Err == nil:
		return "verified"
	case matrixIdentityRejected(o.Err):
		return "rejected_by_identity"
	default:
		return "rejected_other"
	}
}

// Exactly one listed bundle of the repository must verify. Selection is the
// result of the cryptographic and identity checks, never a flag set by hand.
func matrixSelectExactlyOne(outcomes []matrixOutcome) (matrixOutcome, error) {
	var picked matrixOutcome
	count := 0
	for _, o := range outcomes {
		if o.RepoMatch && o.Err == nil {
			picked = o
			count++
		}
	}
	if count != 1 {
		return matrixOutcome{}, fmt.Errorf("%w: verified_entries=%d", errMatrixSelection, count)
	}
	return picked, nil
}

func matrixFirstTlogTime(v matrixVerified) string {
	for _, s := range v.Stamps {
		if s.Type == "Tlog" {
			return s.At.Format(time.RFC3339)
		}
	}
	return "none"
}

// Parse, edit and re-encode a bundle. Keys are re-ordered by the encoder, which
// does not change what the verifier reads.
func matrixEditBundle(raw []byte, edit func(map[string]any) bool) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, errors.Join(errMatrixInput, err)
	}
	if root == nil || !edit(root) {
		return nil, errMatrixInput
	}
	changed, err := json.Marshal(root)
	if err != nil {
		return nil, errors.Join(errMatrixInput, err)
	}
	return changed, nil
}

// Replace one character that lies before the final base64 quantum, so the
// decoded bytes always change.
func matrixFlipChar(s string) (string, bool) {
	if len(s) < 12 {
		return s, false
	}
	b := []byte(s)
	i := len(b) - 6
	if b[i] == 'A' {
		b[i] = 'B'
	} else {
		b[i] = 'A'
	}
	return string(b), true
}

func matrixFlipField(m map[string]any, key string) bool {
	s, ok := m[key].(string)
	if !ok {
		return false
	}
	flipped, ok := matrixFlipChar(s)
	if !ok {
		return false
	}
	m[key] = flipped
	return true
}

func matrixBumpLastDigit(s string) (string, bool) {
	if s == "" {
		return s, false
	}
	d := s[len(s)-1]
	if d < '0' || d > '9' {
		return s, false
	}
	return s[:len(s)-1] + string(rune('0'+(d-'0'+1)%10)), true
}

func matrixFirstTlogEntry(root map[string]any) (map[string]any, bool) {
	vm, ok := root["verificationMaterial"].(map[string]any)
	if !ok {
		return nil, false
	}
	entries, ok := vm["tlogEntries"].([]any)
	if !ok || len(entries) != 1 {
		return nil, false
	}
	entry, ok := entries[0].(map[string]any)
	return entry, ok
}

type matrixTamper struct {
	Name string
	Edit func(map[string]any) bool
}

func matrixTamperControls() []matrixTamper {
	return []matrixTamper{
		{Name: "dsse_signature", Edit: func(root map[string]any) bool {
			env, ok := root["dsseEnvelope"].(map[string]any)
			if !ok {
				return false
			}
			sigs, ok := env["signatures"].([]any)
			if !ok || len(sigs) != 1 {
				return false
			}
			first, ok := sigs[0].(map[string]any)
			if !ok {
				return false
			}
			return matrixFlipField(first, "sig")
		}},
		{Name: "dsse_payload", Edit: func(root map[string]any) bool {
			env, ok := root["dsseEnvelope"].(map[string]any)
			if !ok {
				return false
			}
			return matrixFlipField(env, "payload")
		}},
		{Name: "leaf_certificate", Edit: func(root map[string]any) bool {
			vm, ok := root["verificationMaterial"].(map[string]any)
			if !ok {
				return false
			}
			cert, ok := vm["certificate"].(map[string]any)
			if !ok {
				return false
			}
			return matrixFlipField(cert, "rawBytes")
		}},
		{Name: "tlog_log_index", Edit: func(root map[string]any) bool {
			entry, ok := matrixFirstTlogEntry(root)
			if !ok {
				return false
			}
			switch v := entry["logIndex"].(type) {
			case string:
				bumped, ok := matrixBumpLastDigit(v)
				if !ok {
					return false
				}
				entry["logIndex"] = bumped
				return true
			case json.Number:
				bumped, ok := matrixBumpLastDigit(v.String())
				if !ok {
					return false
				}
				entry["logIndex"] = json.Number(bumped)
				return true
			}
			return false
		}},
		{Name: "inclusion_root_hash", Edit: func(root map[string]any) bool {
			entry, ok := matrixFirstTlogEntry(root)
			if !ok {
				return false
			}
			proof, ok := entry["inclusionProof"].(map[string]any)
			if !ok {
				return false
			}
			return matrixFlipField(proof, "rootHash")
		}},
	}
}

type matrixMutation struct {
	Name     string
	Identity bool
	Want     []string
	Mutate   func(*matrixPolicy)
}

// Each mutation changes one policy field to another non-empty value. The real
// bundle must then be refused: at the identity stage (all cryptographic steps
// passed, the policy refused) or, for the digest, at the signature stage.
func matrixMutations() []matrixMutation {
	const pullRef = "refs/pull/120/merge"
	const otherSHA = "0123456789abcdef0123456789abcdef01234567"
	const runPrefix = "https://github.com/AutoDevJapan/GoDD-Matrix/actions/runs/"
	otherWorkflow := strings.Replace(matrixWorkflowURI, "mcp-artifact.yml", "other.yml", 1)
	extension := func(name string) []string { return []string{"expected " + name + " to be"} }
	return []matrixMutation{
		{Name: "san_pull_request_ref", Identity: true, Want: []string{"expected SAN value"}, Mutate: func(p *matrixPolicy) {
			p.SAN = "https://github.com/AutoDevJapan/GoDD-Matrix/.github/workflows/mcp-artifact.yml@" + pullRef
		}},
		{Name: "san_other_workflow", Identity: true, Want: []string{"expected SAN value"}, Mutate: func(p *matrixPolicy) { p.SAN = otherWorkflow }},
		{Name: "san_other_repository", Identity: true, Want: []string{"expected SAN value"}, Mutate: func(p *matrixPolicy) {
			p.SAN = "https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/mcp-artifact.yml@refs/heads/main"
		}},
		{Name: "oidc_issuer", Identity: true, Want: []string{"expected issuer value"}, Mutate: func(p *matrixPolicy) { p.Issuer = "https://issuer.invalid" }},
		{Name: "build_config_uri", Identity: true, Want: extension("BuildConfigURI"), Mutate: func(p *matrixPolicy) { p.ConfigURI = otherWorkflow }},
		{Name: "source_ref_pull_request", Identity: true, Want: extension("SourceRepositoryRef"), Mutate: func(p *matrixPolicy) { p.Ref = pullRef }},
		{Name: "source_ref_other_branch", Identity: true, Want: extension("SourceRepositoryRef"), Mutate: func(p *matrixPolicy) { p.Ref = "refs/heads/other" }},
		{Name: "build_trigger_pull_request", Identity: true, Want: extension("BuildTrigger"), Mutate: func(p *matrixPolicy) { p.Trigger = "pull_request" }},
		{Name: "run_id", Identity: true, Want: extension("RunInvocationURI"), Mutate: func(p *matrixPolicy) { p.RunURI = runPrefix + "44655701844/attempts/1" }},
		{Name: "run_attempt", Identity: true, Want: extension("RunInvocationURI"), Mutate: func(p *matrixPolicy) { p.RunURI = runPrefix + "44655701843/attempts/2" }},
		{Name: "source_revision", Identity: true, Want: []string{"expected BuildSignerDigest to be", "expected SourceRepositoryDigest to be", "expected BuildConfigDigest to be"}, Mutate: func(p *matrixPolicy) { p.Revision = otherSHA }},
		{Name: "repository_id", Identity: true, Want: extension("SourceRepositoryIdentifier"), Mutate: func(p *matrixPolicy) { p.RepositoryID = "1406818713" }},
		{Name: "repository_uri", Identity: true, Want: extension("SourceRepositoryURI"), Mutate: func(p *matrixPolicy) { p.RepositoryURI = "https://github.com/AutoDevJapan/cc-pc-tools-rs" }},
		{Name: "owner_id", Identity: true, Want: extension("SourceRepositoryOwnerIdentifier"), Mutate: func(p *matrixPolicy) { p.OwnerID = "1" }},
		{Name: "owner_uri", Identity: true, Want: extension("SourceRepositoryOwnerURI"), Mutate: func(p *matrixPolicy) { p.OwnerURI = "https://github.com/OtherOwner" }},
		{Name: "runner_environment", Identity: true, Want: extension("RunnerEnvironment"), Mutate: func(p *matrixPolicy) { p.Runner = "self-hosted" }},
		{Name: "repository_visibility", Identity: true, Want: extension("SourceRepositoryVisibilityAtSigning"), Mutate: func(p *matrixPolicy) { p.Visibility = "private" }},
		{Name: "subject_digest", Identity: false, Want: []string{"does not match any digest in statement"}, Mutate: func(p *matrixPolicy) { p.SubjectSHA = strings.Repeat("0", 64) }},
	}
}

// Same shape as a Sigstore bundle, with placeholder content. It checks that the
// tamper edits find their targets and change them; it is never verified.
const matrixSyntheticBundle = `{"mediaType":"x","verificationMaterial":{"certificate":{"rawBytes":"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="},"tlogEntries":[{"logIndex":"4013169934","inclusionProof":{"rootHash":"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="}}]},"dsseEnvelope":{"payload":"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=","payloadType":"t","signatures":[{"sig":"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="}]}}`

func TestMatrixMainTupleIsConsistent(t *testing.T) {
	if matrixSubjectName != "godd-matrix-mcp-"+matrixSource+".zip" {
		t.Fatal("subject name does not follow the fixed source commit")
	}
	if !lowerHex(matrixSource, 40) || !lowerHex(matrixSubjectSHA, 64) {
		t.Fatal("fixed commit or digest is not lower-case hex")
	}
	if matrixRepoIDText != fmt.Sprint(matrixRepoID) {
		t.Fatal("repository id spellings differ")
	}
	if !strings.HasSuffix(matrixRunURI, "/actions/runs/44655701843/attempts/1") || !strings.Contains(matrixListingURL, "sha256:"+matrixSubjectSHA) {
		t.Fatal("run or listing URL does not follow the fixed subject")
	}
	if !strings.HasSuffix(matrixWorkflowURI, "/.github/workflows/mcp-artifact.yml@refs/heads/main") {
		t.Fatal("workflow identity is not the main branch workflow")
	}
}

func TestMatrixPolicyAndMutationsAreEffective(t *testing.T) {
	base := matrixMainPolicy()
	if !matrixPolicyComplete(base) {
		t.Fatal("main policy has an empty or malformed field")
	}
	covered := map[string]bool{}
	for _, m := range matrixMutations() {
		mutated := base
		m.Mutate(&mutated)
		fields := matrixChangedFields(base, mutated)
		if len(fields) != 1 || !matrixPolicyComplete(mutated) {
			t.Fatalf("mutation %s must change exactly one field to a non-empty value, changed=%v", m.Name, fields)
		}
		covered[fields[0]] = true
	}
	for _, name := range matrixPolicyFieldNames() {
		if !covered[name] {
			t.Fatalf("no negative control covers policy field %s", name)
		}
	}
}

func TestMatrixListingParserFailsClosed(t *testing.T) {
	good := `{"attestations":[{"repository_id":1293650669,"bundle_url":"https://example.invalid/a/1.json.sn","bundle":{"mediaType":"m"}}]}`
	entries, err := matrixParseListing([]byte(good))
	if err != nil || len(entries) != 1 || entries[0].RepositoryID != matrixRepoID || matrixURLTail(entries[0].BundleURL) != "1.json.sn" {
		t.Fatalf("valid listing was not accepted err=%v", err)
	}
	bad := map[string]string{
		"empty":           "",
		"not_json":        "x",
		"no_attestations": `{}`,
		"empty_list":      `{"attestations":[]}`,
		"null_bundle":     `{"attestations":[{"repository_id":1,"bundle":null}]}`,
		"missing_bundle":  `{"attestations":[{"repository_id":1}]}`,
		"trailing_data":   good + " {}",
	}
	for name, raw := range bad {
		if _, err := matrixParseListing([]byte(raw)); err == nil {
			t.Fatalf("listing %s was accepted", name)
		}
	}
}

func TestMatrixSinglePageLinkRejectsNextPage(t *testing.T) {
	next := `<https://api.github.com/repositories/1/attestations?page=2>; rel="next"`
	deprecation := `<https://docs.github.com/en/rest/about-the-rest-api/api-versions>; rel="deprecation"; type="text/html"`
	cases := []struct {
		name   string
		header http.Header
		want   bool
	}{
		{"no_link", http.Header{}, true},
		{"deprecation_only", http.Header{"Link": {deprecation}}, true},
		{"next_page", http.Header{"Link": {next}}, false},
		{"next_page_with_deprecation", http.Header{"Link": {deprecation, next}}, false},
		{"upper_case_relation", http.Header{"Link": {strings.ToUpper(next)}}, false},
	}
	for _, c := range cases {
		if got := matrixSinglePage(c.header); got != c.want {
			t.Fatalf("case %s got=%t want=%t", c.name, got, c.want)
		}
	}
}

func TestMatrixSelectionRequiresExactlyOneVerifiedEntry(t *testing.T) {
	good := matrixOutcome{Index: 1, RepoMatch: true}
	rejected := matrixOutcome{Index: 0, RepoMatch: true, Err: errMatrixRejected}
	foreign := matrixOutcome{Index: 2}
	if got, err := matrixSelectExactlyOne([]matrixOutcome{rejected, good, foreign}); err != nil || got.Index != 1 {
		t.Fatalf("one verified entry was not selected err=%v", err)
	}
	for name, set := range map[string][]matrixOutcome{
		"none":      nil,
		"rejected":  {rejected, foreign},
		"duplicate": {good, good},
	} {
		if _, err := matrixSelectExactlyOne(set); err == nil {
			t.Fatalf("selection %s was accepted", name)
		}
	}
}

func TestMatrixTamperEditsAreEffective(t *testing.T) {
	raw := []byte(matrixSyntheticBundle)
	base, err := matrixEditBundle(raw, func(map[string]any) bool { return true })
	if err != nil {
		t.Fatalf("normalising the placeholder bundle failed err=%v", err)
	}
	for _, c := range matrixTamperControls() {
		changed, err := matrixEditBundle(raw, c.Edit)
		if err != nil || bytes.Equal(changed, base) {
			t.Fatalf("tamper %s did not change the placeholder bundle err=%v", c.Name, err)
		}
		if _, err := matrixEditBundle([]byte(`{"mediaType":"x"}`), c.Edit); err == nil {
			t.Fatalf("tamper %s was applied to a bundle without its target", c.Name)
		}
	}
}

const matrixAttestationID = "69945085"

// Only the explicit opt-in MATRIX_MAIN_ATTESTATION_ACTUAL=1 runs the live check.
// Without it the test is skipped, and a skipped run is not an acceptance. The
// hosted job sets the variable for the step that runs this module's tests.
func matrixActualEnabled() bool {
	return os.Getenv("MATRIX_MAIN_ATTESTATION_ACTUAL") == "1"
}

// Fixed fields only. No response body, URL query or token is recorded.
type matrixReport struct {
	Domain              string          `json:"domain"`
	Passed              bool            `json:"passed"`
	AuthMode            string          `json:"api_auth_mode"`
	APIStatus           int             `json:"api_status"`
	APIEntries          int             `json:"api_entries"`
	RepositoryEntries   int             `json:"repository_entries"`
	VerifiedEntries     int             `json:"verified_entries"`
	IdentityRejected    int             `json:"identity_rejected_entries"`
	PullRequestRejected int             `json:"pull_request_entries_rejected_by_identity"`
	RekorLogIndex       string          `json:"rekor_log_index"`
	BundleBytes         int             `json:"bundle_bytes"`
	BundleSHA256        string          `json:"bundle_sha256"`
	SubjectName         string          `json:"subject_name"`
	SubjectSHA256       string          `json:"subject_sha256"`
	VerifiedSAN         string          `json:"verified_san"`
	VerifiedRunURI      string          `json:"verified_run_uri"`
	TlogTime            string          `json:"verified_tlog_time_utc"`
	Controls            map[string]bool `json:"controls"`
}

func matrixWriteReport(t *testing.T, report *matrixReport) {
	dir := os.Getenv("GUARD_ATTESTATION_DIAGNOSTIC_DIR")
	if dir == "" {
		t.Log("matrix_report not_written: no output directory")
		return
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Log("matrix_report not_written: encode_failed")
		return
	}
	file, err := os.OpenFile(filepath.Join(dir, matrixReportName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Log("matrix_report not_written: create_failed")
		return
	}
	_, writeErr := file.Write(append(raw, '\n'))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Log("matrix_report not_written: write_failed")
		return
	}
	t.Logf("matrix_report written name=%s passed=%t", matrixReportName, report.Passed)
}

func TestMatrixMainAttestationActual(t *testing.T) {
	if !matrixActualEnabled() {
		t.Skip("matrix_main_attestation_not_executed: opt-in absent; a skipped run is not an acceptance")
	}
	report := &matrixReport{
		Domain:        matrixReportDomain,
		SubjectName:   matrixSubjectName,
		SubjectSHA256: matrixSubjectSHA,
		Controls:      map[string]bool{},
	}
	defer func() {
		report.Passed = !t.Failed()
		t.Logf("matrix_result passed=%t controls=%d verified_entries=%d rekor_log_index=%s subject_sha256=%s", report.Passed, len(report.Controls), report.VerifiedEntries, report.RekorLogIndex, report.SubjectSHA256)
		matrixWriteReport(t, report)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()

	trusted, err := acquirePublicGoodRoot(ctx)
	if err != nil {
		t.Fatalf("matrix_stage=trusted_root unavailable err=%v", err)
	}
	fetched, err := matrixFetchListing(ctx)
	report.AuthMode, report.APIStatus = fetched.Mode, fetched.Status
	if err != nil {
		t.Fatalf("matrix_stage=listing mode=%s status=%d rate_remaining=%s err=%v", fetched.Mode, fetched.Status, fetched.RateRemaining, err)
	}
	entries, err := matrixParseListing(fetched.Body)
	if err != nil {
		t.Fatalf("matrix_stage=listing_parse bytes=%d err=%v", len(fetched.Body), err)
	}
	policy := matrixMainPolicy()
	outcomes := matrixEvaluate(ctx, trusted, entries, policy)
	report.APIEntries = len(outcomes)
	for _, o := range outcomes {
		if !o.RepoMatch {
			continue
		}
		report.RepositoryEntries++
		switch {
		case o.Err == nil:
			report.VerifiedEntries++
		case matrixIdentityRejected(o.Err):
			report.IdentityRejected++
			if matrixPullRequestEntry(o) {
				report.PullRequestRejected++
			}
		}
	}
	t.Logf("matrix_listing auth_mode=%s status=%d api_entries=%d repository_entries=%d verified=%d identity_rejected=%d pull_request_rejected=%d", fetched.Mode, fetched.Status, report.APIEntries, report.RepositoryEntries, report.VerifiedEntries, report.IdentityRejected, report.PullRequestRejected)
	for _, o := range outcomes {
		t.Logf("matrix_entry index=%d repo_match=%t rekor_log_index=%s bundle_url_tail=%s san=%s result=%s", o.Index, o.RepoMatch, o.LogIndex, o.URLTail, o.SAN, matrixOutcomeLabel(o))
	}
	selected, err := matrixSelectExactlyOne(outcomes)
	if err != nil {
		t.Fatalf("matrix_stage=selection err=%v", err)
	}

	t.Run("selected_entry_matches_fixed_run", func(t *testing.T) {
		if err := matrixCheckStatement(selected.Verified, policy); err != nil {
			t.Fatalf("statement err=%v subjects=%d", err, len(selected.Verified.Subjects))
		}
		if err := matrixCheckTime(selected.Verified); err != nil {
			t.Fatalf("time_window err=%v stamps=%d", err, len(selected.Verified.Stamps))
		}
		if selected.LogIndex != matrixRekorIndex {
			t.Fatalf("rekor_log_index got=%s want=%s", selected.LogIndex, matrixRekorIndex)
		}
		sum := sha256.Sum256(selected.Raw)
		report.BundleBytes = len(selected.Raw)
		report.BundleSHA256 = hex.EncodeToString(sum[:])
		report.RekorLogIndex = selected.LogIndex
		report.VerifiedSAN = selected.Verified.SAN
		report.VerifiedRunURI = selected.Verified.RunURI
		report.TlogTime = matrixFirstTlogTime(selected.Verified)
		subject := selected.Verified.Subjects[0]
		t.Logf("matrix_selected index=%d rekor_log_index=%s bundle_url_tail=%s attestation_id_in_url=%t bundle_bytes=%d bundle_sha256=%s", selected.Index, selected.LogIndex, selected.URLTail, strings.Contains(selected.URLTail, matrixAttestationID), report.BundleBytes, report.BundleSHA256)
		t.Logf("matrix_identity san=%s issuer=%s run_uri=%s ref=%s trigger=%s", selected.Verified.SAN, selected.Verified.Issuer, selected.Verified.RunURI, selected.Verified.Ref, selected.Verified.Trigger)
		t.Logf("matrix_statement type=%s predicate=%s subject=%s subject_sha256=%s tlog_time_utc=%s", selected.Verified.StatementType, selected.Verified.PredicateType, subject.Name, subject.SHA256, report.TlogTime)
		report.Controls["selected_entry_matches_fixed_run"] = true
	})

	t.Run("same_digest_other_entries_rejected_by_identity", func(t *testing.T) {
		if report.PullRequestRejected < 1 {
			t.Fatalf("pull_request_attestation_for_the_same_digest_not_observed repository_entries=%d identity_rejected=%d", report.RepositoryEntries, report.IdentityRejected)
		}
		for _, o := range outcomes {
			if o.RepoMatch && o.Err != nil && !matrixIdentityRejected(o.Err) {
				t.Logf("matrix_note index=%d rejected_outside_identity_stage san=%s", o.Index, o.SAN)
			}
		}
		report.Controls["same_digest_other_entries_rejected_by_identity"] = true
	})

	t.Run("policy_mutations_rejected", func(t *testing.T) {
		for _, m := range matrixMutations() {
			t.Run(m.Name, func(t *testing.T) {
				mutated := policy
				m.Mutate(&mutated)
				if mutated == policy || !matrixPolicyComplete(mutated) {
					t.Fatal("mutation_is_not_effective")
				}
				_, err := matrixVerify(ctx, trusted, selected.Raw, mutated)
				matrixAssertRejected(t, err, m.Identity, m.Want)
				report.Controls["mutation_"+m.Name] = true
			})
		}
	})

	t.Run("bundle_tampering_rejected", func(t *testing.T) {
		base, err := matrixEditBundle(selected.Raw, func(map[string]any) bool { return true })
		if err != nil {
			t.Fatalf("normalising_the_selected_bundle_failed err=%v", err)
		}
		if _, err = matrixVerify(ctx, trusted, base, policy); err != nil {
			t.Fatalf("normalised_selected_bundle_rejected err=%v", err)
		}
		for _, c := range matrixTamperControls() {
			t.Run(c.Name, func(t *testing.T) {
				changed, err := matrixEditBundle(selected.Raw, c.Edit)
				if err != nil || bytes.Equal(changed, base) {
					t.Fatalf("tamper_not_applied err=%v", err)
				}
				_, err = matrixVerify(ctx, trusted, changed, policy)
				matrixAssertRejected(t, err, false, nil)
				report.Controls["tamper_"+c.Name] = true
			})
		}
	})

	t.Run("other_repository_and_other_digest_rejected", func(t *testing.T) {
		guard := actualBundleFixture(t)
		other := policy
		other.SubjectSHA = actualPESHA
		_, err := matrixVerify(ctx, trusted, guard, other)
		matrixAssertRejected(t, err, true, []string{"expected SAN value"})
		_, err = matrixVerify(ctx, trusted, selected.Raw, other)
		matrixAssertRejected(t, err, false, []string{"does not match any digest in statement"})
		report.Controls["other_repository_and_other_digest_rejected"] = true
	})

	t.Run("selection_cardinality", func(t *testing.T) {
		if _, err := matrixSelectExactlyOne([]matrixOutcome{selected, selected}); err == nil {
			t.Fatal("two_verified_entries_accepted")
		}
		var rejected []matrixOutcome
		for _, o := range outcomes {
			if o.Err != nil {
				rejected = append(rejected, o)
			}
		}
		if _, err := matrixSelectExactlyOne(rejected); err == nil {
			t.Fatal("zero_verified_entries_accepted")
		}
		if got, err := matrixSelectExactlyOne(append([]matrixOutcome{selected}, rejected...)); err != nil || got.Index != selected.Index {
			t.Fatalf("one_verified_entry_was_not_selected err=%v", err)
		}
		report.Controls["selection_cardinality"] = true
	})

	t.Run("cancelled_context_rejected", func(t *testing.T) {
		done, stop := context.WithCancel(context.Background())
		stop()
		if _, err := matrixVerify(done, trusted, selected.Raw, policy); err == nil {
			t.Fatal("cancelled_context_accepted")
		}
		report.Controls["cancelled_context_rejected"] = true
	})

	t.Run("statement_fields_are_enforced", func(t *testing.T) {
		other := policy
		other.SubjectName = matrixSubjectName + ".other"
		if matrixCheckStatement(selected.Verified, other) == nil {
			t.Fatal("different_subject_name_accepted")
		}
		other = policy
		other.RunURI = matrixRunURI + "/other"
		if matrixCheckStatement(selected.Verified, other) == nil {
			t.Fatal("different_run_uri_accepted")
		}
		report.Controls["statement_fields_are_enforced"] = true
	})
}
