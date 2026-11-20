package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"debug/buildinfo"
	"debug/pe"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	guardCaptureRepo = "AutoDevJapan/cc-pc-tools-rs"
	guardCaptureRepoID = int64(1406818713)
	guardCaptureOwnerID = "246834983"
	guardCaptureRepoURI = "https://github.com/" + guardCaptureRepo
	guardCaptureAPI = "https://api.github.com/repos/" + guardCaptureRepo
	guardCaptureWorkflow = ".github/workflows/unattended-readiness.yml"
	guardCaptureSAN = guardCaptureRepoURI + "/" + guardCaptureWorkflow + "@refs/heads/main"
	guardCaptureModule = "github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard"
	guardCaptureMaxZIP = int64(48 << 20)
	guardCaptureMaxProof = int64(8 << 20)
	guardCaptureMaxPE = int64(32 << 20)
	guardCaptureMaxJSON = int64(4 << 20)
	guardCaptureMaxLog = int64(16 << 20)
)
var errGuardCapture = errors.New("guard_capture_check_rejected")

func guardCaptureHash(raw []byte) string { return subjectSHA(raw) }

func guardCaptureBlob(raw []byte) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d%c", len(raw), byte(0))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

func guardCaptureTime(s string) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, s)
	return at, err == nil && at.Location() == time.UTC && !at.IsZero()
}

func guardCaptureArtifactName(p guardCapturePin) string {
	return fmt.Sprintf("guard-bootstrap-%d-%d-%d", p.RunID, p.Attempt, p.JobID)
}

func guardCapturePinValid(p guardCapturePin) bool {
	if !p.Issued || p.InputCount < 1 || p.InputCount > 4096 ||
		p.RunID < 1 || p.Attempt < 1 || p.JobID < 1 || p.CheckRunID < 1 || p.ArtifactID < 1 ||
		p.ArtifactName != guardCaptureArtifactName(p) || p.Producer == p.Source {
		return false
	}
	for _, s := range []string{p.Source, p.SourceTree, p.RepairTree, p.ContractTree, p.Producer, p.WorkflowBlob, p.HelperBlob} {
		if !lowerHex(s, 40) { return false }
	}
	for _, s := range []string{p.ArchiveSHA, p.PESHA, p.ProofSHA, p.CaptureSHA, p.BundleSHA} {
		if !lowerHex(s, 64) { return false }
	}
	if p.ArchiveBytes < 1 || p.ArchiveBytes > guardCaptureMaxZIP ||
		p.PEBytes < 1 || p.PEBytes > guardCaptureMaxPE ||
		p.ProofBytes < 1 || p.ProofBytes > guardCaptureMaxProof ||
		p.CaptureBytes < 1 || p.CaptureBytes > 64<<10 ||
		p.BundleBytes < 1 || p.BundleBytes > guardCaptureMaxJSON ||
		len(p.AttestationID) < 1 || len(p.AttestationID) > 128 || p.RekorIndex == "" {
		return false
	}
	for _, r := range p.AttestationID {
		if !((r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_' || r == '-') { return false }
	}
	for _, r := range p.RekorIndex { if r < '0' || r > '9' { return false } }
	start, a := guardCaptureTime(p.JobStarted)
	end, b := guardCaptureTime(p.JobCompleted)
	created, c := guardCaptureTime(p.ArtifactCreated)
	expires, d := guardCaptureTime(p.ArtifactExpires)
	return a && b && c && d && !end.Before(start) && end.Sub(start) <= 15*time.Minute &&
		!created.Before(start) && !created.After(end) && expires.After(end)
}

// Canonical producer JSON rejects aliases, duplicate keys, unknown keys,
// reordered fields and trailing data through a typed round trip.
func guardCaptureCanonical(raw []byte, out any, limit int64) error {
	if len(raw) == 0 || int64(len(raw)) > limit { return errGuardCapture }
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF { return errGuardCapture }
	encoded, err := json.Marshal(out)
	if err != nil || !bytes.Equal(encoded, raw) { return errGuardCapture }
	return nil
}

// GitHub metadata permits additional server fields, but no ambiguous duplicate
// or case-folded field names. It is not an authorization document.
func guardCaptureJSON(raw []byte, out any) error {
	if len(raw) == 0 || int64(len(raw)) > guardCaptureMaxLog { return errGuardCapture }
	dec := json.NewDecoder(bytes.NewReader(raw))
	if guardCaptureJSONValue(dec, 0) != nil || dec.Decode(new(any)) != io.EOF { return errGuardCapture }
	dec = json.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(out) != nil || dec.Decode(new(any)) != io.EOF { return errGuardCapture }
	return nil
}

func guardCaptureJSONValue(dec *json.Decoder, depth int) error {
	if depth > 64 { return errGuardCapture }
	tok, err := dec.Token()
	if err != nil { return errGuardCapture }
	d, ok := tok.(json.Delim)
	if !ok { return nil }
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			key, ok := k.(string)
			if err != nil || !ok || seen[strings.ToLower(key)] { return errGuardCapture }
			seen[strings.ToLower(key)] = true
			if guardCaptureJSONValue(dec, depth+1) != nil { return errGuardCapture }
		}
		end, err := dec.Token()
		if err != nil || end != json.Delim('}') { return errGuardCapture }
	case '[':
		for dec.More() { if guardCaptureJSONValue(dec, depth+1) != nil { return errGuardCapture } }
		end, err := dec.Token()
		if err != nil || end != json.Delim(']') { return errGuardCapture }
	default:
		return errGuardCapture
	}
	return nil
}

type guardCaptureRecord struct {
	Domain string `json:"domain"`
	RepositoryID int64 `json:"repository_id"`
	Source string `json:"source"`
	SourceTree string `json:"source_tree"`
	Producer string `json:"producer_revision"`
	RunID int64 `json:"run_id"`
	Attempt int `json:"run_attempt"`
	JobID int64 `json:"job_id"`
	CheckRunID int64 `json:"check_run_id"`
	ArtifactID int64 `json:"artifact_id"`
	ArtifactName string `json:"artifact_name"`
	ArchiveSHA string `json:"archive_sha256"`
	PESHA string `json:"output_sha256"`
	CaptureSHA string `json:"capture_sha256"`
	ProofSHA string `json:"source_manifest_sha256"`
	AttestationID string `json:"attestation_id"`
}

func guardCaptureExpectedRecord(p guardCapturePin) guardCaptureRecord {
	return guardCaptureRecord{"guard-gha-publication-v1", guardCaptureRepoID, p.Source, p.SourceTree,
		p.Producer, p.RunID, p.Attempt, p.JobID, p.CheckRunID, p.ArtifactID, p.ArtifactName,
		p.ArchiveSHA, p.PESHA, p.CaptureSHA, p.ProofSHA, p.AttestationID}
}

func guardCapturePublication(raw []byte, p guardCapturePin) ([]byte, error) {
	if len(raw) == 0 || int64(len(raw)) > guardCaptureMaxLog { return nil, errGuardCapture }
	var found []byte
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		// The Actions log endpoint prefixes each line with its UTC timestamp.
		if len(line) > 20 && line[4] == '-' {
			if i := bytes.IndexByte(line, ' '); i > 0 {
				if _, ok := guardCaptureTime(string(line[:i])); !ok { continue }
				line = line[i+1:]
			}
		}
		const marker = "guard_gha_publication_v1="
		if !bytes.HasPrefix(line, []byte(marker)) { continue }
		if found != nil { return nil, errGuardCapture }
		found = append([]byte(nil), line[len(marker):]...)
	}
	var record guardCaptureRecord
	if guardCaptureCanonical(found, &record, 8192) != nil || record != guardCaptureExpectedRecord(p) {
		return nil, errGuardCapture
	}
	return found, nil
}

type guardCaptureCommand struct {
	Directory string `json:"directory"`
	Executable string `json:"executable"`
	Arguments []string `json:"arguments"`
}
type guardCaptureImage struct {
	OS string `json:"os"`
	Version string `json:"version"`
	RunnerOS string `json:"runner_os"`
	RunnerArch string `json:"runner_arch"`
}
type guardCaptureInput struct {
	Path string `json:"path"`
	Size int64 `json:"size"`
	SHA string `json:"sha256"`
	Blob string `json:"git_blob"`
}
type guardCaptureBefore struct {
	Domain string `json:"domain"`
	RepositoryID int64 `json:"repository_id"`
	Source string `json:"source"`
	RunID int64 `json:"run_id"`
	Attempt int `json:"run_attempt"`
	Event string `json:"event"`
	SourceTree string `json:"source_tree"`
	Producer string `json:"producer_revision"`
	CheckRunID int64 `json:"check_run_id"`
	InventorySHA string `json:"inventory_sha256"`
	ObservationSHA string `json:"runner_toolchain_observation_sha256"`
	Observation json.RawMessage `json:"runner_toolchain_observation"`
	BuildGraph json.RawMessage `json:"build_graph"`
	SourceTreeBefore string `json:"source_tree_before"`
	GitTreeSHA string `json:"git_tree_sha256"`
	GitArchiveSHA string `json:"git_archive_sha256"`
	Command guardCaptureCommand `json:"compile_command"`
	ImageSHA string `json:"runner_image_sha256"`
	Image guardCaptureImage `json:"runner_image"`
	BuildGraphSHA string `json:"build_graph_sha256"`
	CommandSHA string `json:"compile_command_sha256"`
	Inputs []guardCaptureInput `json:"inputs"`
}
type guardCaptureProof struct {
	Domain string `json:"domain"`
	Before guardCaptureBefore `json:"before"`
	After guardCaptureBefore `json:"after"`
	CompilerExit int `json:"compiler_exit"`
}
type guardCaptureManifest struct {
	Domain string `json:"domain"`
	RepositoryID int64 `json:"repository_id"`
	Source string `json:"source"`
	SourceTree string `json:"source_tree"`
	Producer string `json:"producer_revision"`
	RunID int64 `json:"run_id"`
	Attempt int `json:"run_attempt"`
	JobID int64 `json:"job_id"`
	CheckRunID int64 `json:"check_run_id"`
	ProofSHA string `json:"source_manifest_sha256"`
	ObservationSHA string `json:"runner_toolchain_observation_sha256"`
	CommandSHA string `json:"compile_command_sha256"`
	ImageSHA string `json:"runner_image_sha256"`
	OutputName string `json:"output_name"`
	PEBytes int64 `json:"output_bytes"`
	PESHA string `json:"output_sha256"`
}

func guardCaptureDigestJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil { return "" }
	return guardCaptureHash(raw)
}

func guardCaptureProofCheck(raw, captureRaw []byte, p guardCapturePin) (guardCaptureProof, error) {
	var proof guardCaptureProof
	var capture guardCaptureManifest
	if int64(len(raw)) != p.ProofBytes || guardCaptureHash(raw) != p.ProofSHA ||
		int64(len(captureRaw)) != p.CaptureBytes || guardCaptureHash(captureRaw) != p.CaptureSHA ||
		guardCaptureCanonical(raw, &proof, guardCaptureMaxProof) != nil ||
		guardCaptureCanonical(captureRaw, &capture, 64<<10) != nil {
		return proof, errGuardCapture
	}
	b := proof.Before
	if proof.Domain != "guard-gha-source-proof-v1" || proof.CompilerExit != 0 ||
		!reflect.DeepEqual(proof.Before, proof.After) ||
		b.Domain != "guard-gha-source-before-v1" || b.RepositoryID != guardCaptureRepoID ||
		b.Source != p.Source || b.SourceTree != p.SourceTree || b.SourceTreeBefore != p.SourceTree ||
		b.Producer != p.Producer || b.RunID != p.RunID || b.Attempt != p.Attempt ||
		b.CheckRunID != p.CheckRunID || b.Event != "workflow_dispatch" ||
		len(b.Inputs) != p.InputCount || guardCaptureDigestJSON(b.Inputs) != b.InventorySHA {
		return proof, errGuardCapture
	}
	seen := map[string]bool{}
	previous := ""
	total := int64(0)
	for _, in := range b.Inputs {
		if in.Path <= previous || !guardCaptureInputPath(in.Path) || seen[strings.ToLower(in.Path)] ||
			in.Size < 0 || in.Size > 8<<20 || !lowerHex(in.SHA, 64) || !lowerHex(in.Blob, 40) {
			return proof, errGuardCapture
		}
		previous = in.Path
		seen[strings.ToLower(in.Path)] = true
		total += in.Size
	}
	if total > 64<<20 { return proof, errGuardCapture }
	for _, name := range []string{"repair-guard/go.mod", "repair-guard/go.sum", "repair-guard/owner_catalog.json",
		"tools/civ6-ci-contract/go.mod", "tools/civ6-ci-contract/go.sum"} {
		if !seen[name] { return proof, errGuardCapture }
	}
	wantCommand := guardCaptureCommand{"repair-guard", "go", []string{"build", "-mod=readonly", "-pgo=off", "-trimpath", "-o", "<fixed-attempt-directory>/guard-build.exe", "."}}
	if !reflect.DeepEqual(b.Command, wantCommand) || b.CommandSHA != guardCaptureDigestJSON(b.Command) ||
		b.Image.OS == "" || b.Image.Version == "" || b.Image.RunnerOS != "Windows" ||
		b.Image.RunnerArch != "X64" || b.ImageSHA != guardCaptureDigestJSON(b.Image) ||
		!lowerHex(b.GitTreeSHA, 64) || !lowerHex(b.GitArchiveSHA, 64) ||
		guardCaptureHash(b.Observation) != b.ObservationSHA || guardCaptureHash(b.BuildGraph) != b.BuildGraphSHA ||
		guardCaptureObservation(b.Observation) != nil || guardCaptureGraph(b.BuildGraph, seen) != nil {
		return proof, errGuardCapture
	}
	want := guardCaptureManifest{"guard-gha-capture-v1", guardCaptureRepoID, p.Source, p.SourceTree,
		p.Producer, p.RunID, p.Attempt, p.JobID, p.CheckRunID, p.ProofSHA,
		b.ObservationSHA, b.CommandSHA, b.ImageSHA, "guard.exe", p.PEBytes, p.PESHA}
	if capture != want { return proof, errGuardCapture }
	return proof, nil
}

func guardCaptureInputPath(s string) bool {
	return s != "" && path.Clean(s) == s && !strings.ContainsAny(s, "\\:\r\n\x00") &&
		(strings.HasPrefix(s, "repair-guard/") || strings.HasPrefix(s, "tools/civ6-ci-contract/"))
}

func guardCaptureObservation(raw []byte) error {
	var o struct {
		Version string `json:"version"`
		Env map[string]string `json:"env"`
	}
	if guardCaptureCanonical(raw, &o, 64<<10) != nil || !strings.HasPrefix(o.Version, "go version go") || len(o.Env) != 12 {
		return errGuardCapture
	}
	want := map[string]string{"GOOS":"windows", "GOARCH":"amd64", "GOTOOLCHAIN":"local", "GOWORK":"off",
		"GOFLAGS":"", "CGO_ENABLED":"0", "GOROOT":"<runner-goroot>", "GOMODCACHE":"<runner-module-cache>",
		"GOCACHE":"<runner-build-cache>", "GOPROXY":"https://proxy.golang.org", "GOSUMDB":"sum.golang.org"}
	for k, v := range want { if got, ok := o.Env[k]; !ok || got != v { return errGuardCapture } }
	if !strings.HasPrefix(o.Env["GOVERSION"], "go") { return errGuardCapture }
	return nil
}

type guardCaptureGraphModule struct {
	Path, Version, Sum, GoModSum string
	Main bool
	Replace *guardCaptureGraphModule
}
type guardCaptureGraphPackage struct {
	ImportPath string
	Standard bool
	Module *guardCaptureGraphModule
	Error *json.RawMessage
	DepsErrors []json.RawMessage
	GoFiles, CgoFiles, CFiles, CXXFiles, HFiles, SFiles, SysoFiles, EmbedFiles []string
	Imports []string
}

func guardCaptureGraph(raw []byte, inputs map[string]bool) error {
	var packages []guardCaptureGraphPackage
	if guardCaptureCanonical(raw, &packages, guardCaptureMaxProof) != nil || len(packages) == 0 || len(packages) > 4096 {
		return errGuardCapture
	}
	last := ""
	mainSeen, replaceSeen := false, false
	for _, pkg := range packages {
		if pkg.ImportPath <= last || pkg.Error != nil || len(pkg.DepsErrors) != 0 { return errGuardCapture }
		last = pkg.ImportPath
		prefix := ""
		switch {
		case pkg.ImportPath == guardCaptureModule:
			prefix, mainSeen = "repair-guard", true
		case strings.HasPrefix(pkg.ImportPath, guardCaptureModule+"/"):
			prefix = "repair-guard/" + strings.TrimPrefix(pkg.ImportPath, guardCaptureModule+"/")
		case pkg.ImportPath == "cc/civ6-ci-contract":
			prefix, replaceSeen = "tools/civ6-ci-contract", true
		case strings.HasPrefix(pkg.ImportPath, "cc/civ6-ci-contract/"):
			prefix = "tools/civ6-ci-contract/" + strings.TrimPrefix(pkg.ImportPath, "cc/civ6-ci-contract/")
		}
		if prefix == "" { continue }
		for _, names := range [][]string{pkg.GoFiles, pkg.CgoFiles, pkg.CFiles, pkg.CXXFiles, pkg.HFiles, pkg.SFiles, pkg.SysoFiles, pkg.EmbedFiles} {
			for _, name := range names {
				if name == "" || strings.ContainsAny(name, "\\:\r\n") || path.Clean(name) != name || strings.HasPrefix(name, "../") ||
					!inputs[path.Join(prefix, name)] { return errGuardCapture }
			}
		}
	}
	if !mainSeen || !replaceSeen { return errGuardCapture }
	return nil
}

type guardCapturePayload struct { PE, Proof, Capture []byte }

func guardCaptureReadZIP(raw []byte, p guardCapturePin) (guardCapturePayload, error) {
	var out guardCapturePayload
	if int64(len(raw)) != p.ArchiveBytes || int64(len(raw)) > guardCaptureMaxZIP || guardCaptureHash(raw) != p.ArchiveSHA {
		return out, errGuardCapture
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil || len(zr.File) != 3 { return out, errGuardCapture }
	type entryPin struct { size int64; sha string; limit int64; dest *[]byte }
	entries := map[string]entryPin{
		"guard.exe": {p.PEBytes, p.PESHA, guardCaptureMaxPE, &out.PE},
		"source-proof.json": {p.ProofBytes, p.ProofSHA, guardCaptureMaxProof, &out.Proof},
		"capture.json": {p.CaptureBytes, p.CaptureSHA, 64<<10, &out.Capture},
	}
	for _, f := range zr.File {
		expected, ok := entries[f.Name]
		if !ok || f.Flags&1 != 0 || !f.Mode().IsRegular() ||
			f.UncompressedSize64 != uint64(expected.size) || expected.size < 1 || expected.size > expected.limit {
			return guardCapturePayload{}, errGuardCapture
		}
		delete(entries, f.Name)
		r, err := f.Open()
		if err != nil { return guardCapturePayload{}, errGuardCapture }
		body, readErr := io.ReadAll(io.LimitReader(r, expected.limit+1))
		closeErr := r.Close()
		if readErr != nil || closeErr != nil || int64(len(body)) != expected.size || guardCaptureHash(body) != expected.sha {
			return guardCapturePayload{}, errGuardCapture
		}
		*expected.dest = body
	}
	if len(entries) != 0 { return guardCapturePayload{}, errGuardCapture }
	return out, nil
}

func guardCaptureBuildInfo(info *debug.BuildInfo, p guardCapturePin) error {
	if info == nil || info.Path != guardCaptureModule || info.Main.Path != guardCaptureModule { return errGuardCapture }
	settings := map[string]string{}
	for _, v := range info.Settings {
		if _, ok := settings[v.Key]; ok { return errGuardCapture }
		settings[v.Key] = v.Value
	}
	for k, want := range map[string]string{"vcs":"git", "vcs.revision":p.Source, "vcs.modified":"false",
		"GOOS":"windows", "GOARCH":"amd64", "-trimpath":"true", "CGO_ENABLED":"0"} {
		if settings[k] != want { return errGuardCapture }
	}
	// Go omits -pgo from build info when it is off; a profile is refused.
	if _, present := settings["-pgo"]; present { return errGuardCapture }
	return nil
}

func guardCaptureBinary(raw []byte, p guardCapturePin, goVersion string) error {
	if int64(len(raw)) != p.PEBytes || guardCaptureHash(raw) != p.PESHA { return errGuardCapture }
	file, err := pe.NewFile(bytes.NewReader(raw))
	if err != nil { return errGuardCapture }
	machine := file.FileHeader.Machine
	closeErr := file.Close()
	if machine != pe.IMAGE_FILE_MACHINE_AMD64 || closeErr != nil { return errGuardCapture }
	info, err := buildinfo.Read(bytes.NewReader(raw))
	if err != nil || goVersion == "" || info.GoVersion != goVersion { return errGuardCapture }
	return guardCaptureBuildInfo(info, p)
}

type guardCaptureHTTP struct {
	api *http.Client
	object *http.Client
	token string
}

func guardCaptureLiveHTTP(token string) guardCaptureHTTP {
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return guardCaptureHTTP{
		api: &http.Client{Timeout: 30*time.Second, CheckRedirect: noRedirect},
		object: &http.Client{Timeout: 60*time.Second, CheckRedirect: noRedirect},
		token: token,
	}
}

func guardCaptureObjectURL(raw string, source bool) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" ||
		u.Opaque != "" || (u.Port() != "" && u.Port() != "443") { return false }
	host := strings.ToLower(u.Hostname())
	if source { return host == "codeload.github.com" && strings.HasPrefix(u.Path, "/"+guardCaptureRepo+"/") }
	return strings.HasSuffix(host, ".blob.core.windows.net") || strings.HasSuffix(host, ".actions.githubusercontent.com")
}

func guardCaptureBody(resp *http.Response, limit int64) ([]byte, error) {
	if resp == nil || resp.Body == nil { return nil, errGuardCapture }
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	closeErr := resp.Body.Close()
	if err != nil || closeErr != nil || int64(len(body)) > limit { return nil, errGuardCapture }
	return body, nil
}

// No retry, alternate identity or credential fallback. Only a reviewed fixed
// repository endpoint reaches this function. Raw error text is never logged.
func (c guardCaptureHTTP) get(ctx context.Context, endpoint string, limit int64, redirect, source bool) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil || c.api == nil || c.object == nil || c.token == "" ||
		!strings.HasPrefix(endpoint, guardCaptureAPI+"/") || limit < 1 { return nil, errGuardCapture }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil { return nil, errGuardCapture }
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.api.Do(req)
	if err != nil { return nil, errGuardCapture }
	if !redirect {
		if resp.StatusCode != http.StatusOK || !matrixSinglePage(resp.Header) {
			_ = resp.Body.Close()
			return nil, errGuardCapture
		}
		return guardCaptureBody(resp, limit)
	}
	if resp.StatusCode != http.StatusFound {
		_ = resp.Body.Close()
		return nil, errGuardCapture
	}
	location := resp.Header.Get("Location")
	if resp.Body.Close() != nil || !guardCaptureObjectURL(location, source) { return nil, errGuardCapture }
	// A new request and a separate client carry no API token, cookies or headers.
	objReq, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil { return nil, errGuardCapture }
	resp, err = c.object.Do(objReq)
	if err != nil { return nil, errGuardCapture }
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, errGuardCapture
	}
	return guardCaptureBody(resp, limit)
}

func (c guardCaptureHTTP) json(ctx context.Context, suffix string, out any) error {
	raw, err := c.get(ctx, guardCaptureAPI+suffix, guardCaptureMaxJSON, false, false)
	if err != nil { return err }
	return guardCaptureJSON(raw, out)
}

type guardCaptureRun struct {
	ID int64 `json:"id"`
	Attempt int `json:"run_attempt"`
	HeadSHA string `json:"head_sha"`
	Branch string `json:"head_branch"`
	Event string `json:"event"`
	Path string `json:"path"`
	Status string `json:"status"`
	Conclusion string `json:"conclusion"`
	Repository struct { ID int64 `json:"id"`; FullName string `json:"full_name"`; Private bool `json:"private"` } `json:"repository"`
	HeadRepository struct { ID int64 `json:"id"` } `json:"head_repository"`
}
type guardCaptureStep struct {
	Name string `json:"name"`
	Number int `json:"number"`
	Status string `json:"status"`
	Conclusion string `json:"conclusion"`
}
type guardCaptureJob struct {
	ID int64 `json:"id"`
	RunID int64 `json:"run_id"`
	HeadSHA string `json:"head_sha"`
	Name string `json:"name"`
	Status string `json:"status"`
	Conclusion string `json:"conclusion"`
	CheckRunURL string `json:"check_run_url"`
	Started string `json:"started_at"`
	Completed string `json:"completed_at"`
	RunnerID int64 `json:"runner_id"`
	Steps []guardCaptureStep `json:"steps"`
}
type guardCaptureArtifact struct {
	ID int64 `json:"id"`
	Name string `json:"name"`
	Bytes int64 `json:"size_in_bytes"`
	Digest string `json:"digest"`
	Expired bool `json:"expired"`
	Created string `json:"created_at"`
	Expires string `json:"expires_at"`
	Run struct {
		ID int64 `json:"id"`
		RepositoryID int64 `json:"repository_id"`
		HeadRepositoryID int64 `json:"head_repository_id"`
		HeadSHA string `json:"head_sha"`
		Branch string `json:"head_branch"`
	} `json:"workflow_run"`
}
type guardCaptureServer struct {
	Run guardCaptureRun
	Job guardCaptureJob
	Artifact guardCaptureArtifact
}

func guardCaptureServerCheck(s guardCaptureServer, p guardCapturePin) error {
	r, j, a := s.Run, s.Job, s.Artifact
	if r.ID != p.RunID || r.Attempt != p.Attempt || r.HeadSHA != p.Producer ||
		r.Branch != "main" || r.Event != "workflow_dispatch" || r.Path != guardCaptureWorkflow ||
		r.Status != "completed" || r.Conclusion != "success" || r.Repository.ID != guardCaptureRepoID ||
		r.Repository.FullName != guardCaptureRepo || r.Repository.Private || r.HeadRepository.ID != guardCaptureRepoID ||
		j.ID != p.JobID || j.RunID != p.RunID || j.HeadSHA != p.Producer ||
		j.Name != "Guard bootstrap artifact" || j.Status != "completed" || j.Conclusion != "success" ||
		j.Started != p.JobStarted || j.Completed != p.JobCompleted || j.RunnerID < 1 ||
		j.CheckRunURL != fmt.Sprintf("%s/check-runs/%d", guardCaptureAPI, p.CheckRunID) {
		return errGuardCapture
	}
	last := 0
	for _, name := range []string{"Capture the fixed guard build", "Check the exact captured files",
		"Attest the guard and source evidence", "Upload the fixed guard capture", "Record the server-assigned capture artifact"} {
		count := 0
		for _, step := range j.Steps {
			if step.Name == name {
				count++
				if step.Status != "completed" || step.Conclusion != "success" || step.Number <= last { return errGuardCapture }
				last = step.Number
			}
		}
		if count != 1 { return errGuardCapture }
	}
	if a.ID != p.ArtifactID || a.Name != p.ArtifactName || a.Bytes != p.ArchiveBytes ||
		a.Digest != "sha256:"+p.ArchiveSHA || a.Created != p.ArtifactCreated || a.Expires != p.ArtifactExpires ||
		a.Run.ID != p.RunID || a.Run.RepositoryID != guardCaptureRepoID || a.Run.HeadRepositoryID != guardCaptureRepoID ||
		a.Run.HeadSHA != p.Producer || a.Run.Branch != "main" {
		return errGuardCapture
	}
	return nil
}

func (c guardCaptureHTTP) server(ctx context.Context, p guardCapturePin) (guardCaptureServer, error) {
	var s guardCaptureServer
	if !guardCapturePinValid(p) { return s, errGuardCapture }
	if c.json(ctx, fmt.Sprintf("/actions/runs/%d/attempts/%d", p.RunID, p.Attempt), &s.Run) != nil ||
		c.json(ctx, fmt.Sprintf("/actions/jobs/%d", p.JobID), &s.Job) != nil ||
		c.json(ctx, fmt.Sprintf("/actions/artifacts/%d", p.ArtifactID), &s.Artifact) != nil ||
		guardCaptureServerCheck(s, p) != nil { return guardCaptureServer{}, errGuardCapture }
	// Resolve this job among the exact run attempt, not by its name alone.
	var jobs struct { Total int `json:"total_count"`; Jobs []guardCaptureJob `json:"jobs"` }
	if c.json(ctx, fmt.Sprintf("/actions/runs/%d/attempts/%d/jobs?per_page=100", p.RunID, p.Attempt), &jobs) != nil ||
		jobs.Total != len(jobs.Jobs) || jobs.Total > 100 { return guardCaptureServer{}, errGuardCapture }
	count := 0
	for _, job := range jobs.Jobs {
		if job.ID == p.JobID {
			count++
			if !reflect.DeepEqual(job, s.Job) { return guardCaptureServer{}, errGuardCapture }
		}
	}
	if count != 1 { return guardCaptureServer{}, errGuardCapture }
	return s, nil
}

type guardCaptureTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA string `json:"sha"`
	Size int64 `json:"size"`
}
type guardCaptureTree struct {
	SHA string `json:"sha"`
	Truncated bool `json:"truncated"`
	Entries []guardCaptureTreeEntry `json:"tree"`
}

// Rebuild every tree object from the non-truncated recursive response. Merely
// echoing the response's "sha" field would not bind its entries to the pin.
func guardCaptureTreeCheck(tree guardCaptureTree, want string) (map[string]guardCaptureTreeEntry, error) {
	if tree.Truncated || tree.SHA != want || !lowerHex(want, 40) || len(tree.Entries) > 20000 { return nil, errGuardCapture }
	known := map[string]guardCaptureTreeEntry{}
	children := map[string][]guardCaptureTreeEntry{}
	for _, e := range tree.Entries {
		if e.Path == "" || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "/") ||
			strings.HasPrefix(e.Path, "../") || strings.ContainsAny(e.Path, "\\\r\n\x00:") ||
			!lowerHex(e.SHA, 40) { return nil, errGuardCapture }
		if _, ok := known[e.Path]; ok { return nil, errGuardCapture }
		switch e.Type {
		case "blob":
			if e.Mode != "100644" && e.Mode != "100755" && e.Mode != "120000" { return nil, errGuardCapture }
			if e.Size < 0 { return nil, errGuardCapture }
		case "tree":
			if e.Mode != "040000" { return nil, errGuardCapture }
		case "commit":
			if e.Mode != "160000" { return nil, errGuardCapture }
		default:
			return nil, errGuardCapture
		}
		known[e.Path] = e
		dir := path.Dir(e.Path)
		if dir == "." { dir = "" }
		children[dir] = append(children[dir], e)
	}
	for name, entry := range known {
		if entry.Type == "tree" { if _, ok := children[name]; !ok { children[name] = nil } }
		dir := path.Dir(name)
		if dir != "." {
			if parent, ok := known[dir]; !ok || parent.Type != "tree" { return nil, errGuardCapture }
		}
	}
	for dir, list := range children {
		sort.Slice(list, func(i,j int) bool {
			a, b := path.Base(list[i].Path), path.Base(list[j].Path)
			if list[i].Type == "tree" { a += "/" }
			if list[j].Type == "tree" { b += "/" }
			return a < b
		})
		var raw bytes.Buffer
		for _, e := range list {
			mode := strings.TrimLeft(e.Mode, "0")
			_, _ = fmt.Fprintf(&raw, "%s %s%c", mode, path.Base(e.Path), byte(0))
			id, _ := hex.DecodeString(e.SHA)
			_, _ = raw.Write(id)
		}
		h := sha1.New()
		_, _ = fmt.Fprintf(h, "tree %d%c", raw.Len(), byte(0))
		_, _ = h.Write(raw.Bytes())
		expected := want
		if dir != "" { expected = known[dir].SHA }
		if hex.EncodeToString(h.Sum(nil)) != expected { return nil, errGuardCapture }
	}
	if _, ok := children[""]; !ok { return nil, errGuardCapture }
	return known, nil
}

func guardCaptureSourceEntries(entries map[string]guardCaptureTreeEntry, p guardCapturePin, inputs []guardCaptureInput) error {
	if entries["repair-guard"].SHA != p.RepairTree || entries["tools/civ6-ci-contract"].SHA != p.ContractTree ||
		len(inputs) != p.InputCount { return errGuardCapture }
	selected := map[string]guardCaptureTreeEntry{}
	for name, e := range entries {
		if !guardCaptureInputPath(name) || e.Type == "tree" { continue }
		if e.Type != "blob" || e.Mode != "100644" { return errGuardCapture }
		selected[name] = e
	}
	if len(selected) != p.InputCount { return errGuardCapture }
	var gitTree bytes.Buffer
	for _, in := range inputs {
		e, ok := selected[in.Path]
		if !ok || e.SHA != in.Blob || e.Size != in.Size { return errGuardCapture }
		delete(selected, in.Path)
		_, _ = fmt.Fprintf(&gitTree, "100644 blob %s\t%s%c", in.Blob, in.Path, byte(0))
	}
	if len(selected) != 0 { return errGuardCapture }
	return nil
}

// A normal source archive is untrusted data. Selected file bytes must have both
// the Git blob ID from the pinned tree and the SHA-256 from the signed proof.
// The archive is never extracted and unrelated members are not retained.
func guardCaptureSourceArchive(raw []byte, inputs []guardCaptureInput) error {
	if len(raw) == 0 || len(raw) > 128<<20 || len(inputs) == 0 { return errGuardCapture }
	gz, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil { return errGuardCapture }
	gz.Multistream(false)
	expanded, readErr := io.ReadAll(io.LimitReader(gz, (256<<20)+1))
	closeErr := gz.Close()
	if readErr != nil || closeErr != nil || len(expanded) > 256<<20 { return errGuardCapture }
	defer clear(expanded)
	wanted := map[string]guardCaptureInput{}
	for _, in := range inputs {
		if _, ok := wanted[in.Path]; ok || !guardCaptureInputPath(in.Path) { return errGuardCapture }
		wanted[in.Path] = in
	}
	reader := tar.NewReader(bytes.NewReader(expanded))
	prefix := ""
	for {
		h, err := reader.Next()
		if err == io.EOF { break }
		if err != nil { return errGuardCapture }
		if h.Typeflag == tar.TypeXGlobalHeader { continue }
		name := strings.TrimSuffix(h.Name, "/")
		first, rest, hasSlash := strings.Cut(name, "/")
		if first == "" || first == "." || first == ".." || strings.ContainsAny(name, "\\\r\n\x00") { return errGuardCapture }
		if prefix == "" { prefix = first }
		if first != prefix || (hasSlash && path.Clean(rest) != rest) { return errGuardCapture }
		if !hasSlash || !guardCaptureInputPath(rest) || h.Typeflag == tar.TypeDir { continue }
		in, ok := wanted[rest]
		if !ok || h.Typeflag != tar.TypeReg || h.Size != in.Size || h.Size < 0 || h.Size > 8<<20 {
			return errGuardCapture
		}
		body, err := io.ReadAll(io.LimitReader(reader, (8<<20)+1))
		if err != nil || int64(len(body)) != in.Size || guardCaptureBlob(body) != in.Blob || guardCaptureHash(body) != in.SHA {
			return errGuardCapture
		}
		delete(wanted, rest)
	}
	if len(wanted) != 0 { return errGuardCapture }
	return nil
}

func (c guardCaptureHTTP) source(ctx context.Context, p guardCapturePin, proof guardCaptureProof) error {
	var commit struct { SHA string `json:"sha"`; Tree struct { SHA string `json:"sha"` } `json:"tree"` }
	if c.json(ctx, "/git/commits/"+p.Source, &commit) != nil || commit.SHA != p.Source || commit.Tree.SHA != p.SourceTree { return errGuardCapture }
	var tree guardCaptureTree
	if c.json(ctx, "/git/trees/"+p.SourceTree+"?recursive=1", &tree) != nil { return errGuardCapture }
	entries, err := guardCaptureTreeCheck(tree, p.SourceTree)
	if err != nil || guardCaptureSourceEntries(entries, p, proof.Before.Inputs) != nil { return errGuardCapture }
	var gitTree bytes.Buffer
	for _, in := range proof.Before.Inputs { _, _ = fmt.Fprintf(&gitTree, "100644 blob %s\t%s%c", in.Blob, in.Path, byte(0)) }
	if guardCaptureHash(gitTree.Bytes()) != proof.Before.GitTreeSHA { return errGuardCapture }
	archive, err := c.get(ctx, guardCaptureAPI+"/tarball/"+p.Source, 128<<20, true, true)
	if err != nil || guardCaptureSourceArchive(archive, proof.Before.Inputs) != nil { return errGuardCapture }

	var producer struct { SHA string `json:"sha"`; Tree struct { SHA string `json:"sha"` } `json:"tree"` }
	if c.json(ctx, "/git/commits/"+p.Producer, &producer) != nil || producer.SHA != p.Producer || !lowerHex(producer.Tree.SHA,40) { return errGuardCapture }
	var producerTree guardCaptureTree
	if c.json(ctx, "/git/trees/"+producer.Tree.SHA+"?recursive=1", &producerTree) != nil { return errGuardCapture }
	pe, err := guardCaptureTreeCheck(producerTree, producer.Tree.SHA)
	if err != nil || pe["repair-guard"].SHA != p.RepairTree || pe["tools/civ6-ci-contract"].SHA != p.ContractTree ||
		pe[guardCaptureWorkflow].SHA != p.WorkflowBlob || pe["tools/guard-gha-capture/main.go"].SHA != p.HelperBlob { return errGuardCapture }
	var relation struct {
		Status string `json:"status"`
		Base struct { SHA string `json:"sha"` } `json:"base_commit"`
		MergeBase struct { SHA string `json:"sha"` } `json:"merge_base_commit"`
	}
	if c.json(ctx, "/compare/"+p.Source+"..."+p.Producer, &relation) != nil || relation.Status != "ahead" ||
		relation.Base.SHA != p.Source || relation.MergeBase.SHA != p.Source { return errGuardCapture }
	return nil
}

func guardCapturePolicy(p guardCapturePin, proof bool) matrixPolicy {
	name, sha := "guard.exe", p.PESHA
	if proof { name, sha = "source-proof.json", p.ProofSHA }
	return matrixPolicy{
		Issuer: "https://token.actions.githubusercontent.com", SAN: guardCaptureSAN, ConfigURI: guardCaptureSAN,
		Revision: p.Producer, Ref: "refs/heads/main", Trigger: "workflow_dispatch", Runner: "github-hosted",
		Visibility: "public", RepositoryID: strconv.FormatInt(guardCaptureRepoID,10), RepositoryURI: guardCaptureRepoURI,
		OwnerID: guardCaptureOwnerID, OwnerURI: "https://github.com/AutoDevJapan",
		RunURI: fixedRunURI(p.RunID,p.Attempt), SubjectName: name, SubjectSHA: sha,
	}
}

func guardCaptureStatement(v matrixVerified, p guardCapturePin) error {
	if v.StatementType != matrixStatementV1 || v.PredicateType != matrixPredicateV1 || len(v.Subjects) != 2 ||
		v.Subjects[0] != (matrixSubject{"guard.exe",p.PESHA,1}) ||
		v.Subjects[1] != (matrixSubject{"source-proof.json",p.ProofSHA,1}) ||
		v.SAN != guardCaptureSAN || v.Issuer != "https://token.actions.githubusercontent.com" ||
		v.RunURI != fixedRunURI(p.RunID,p.Attempt) || v.Ref != "refs/heads/main" || v.Trigger != "workflow_dispatch" {
		return errGuardCapture
	}
	start, a := guardCaptureTime(p.JobStarted)
	end, b := guardCaptureTime(p.JobCompleted)
	if !a || !b { return errGuardCapture }
	tlogs := 0
	for _, s := range v.Stamps {
		if s.Type != "Tlog" { continue }
		tlogs++
		if s.At.Before(start) || s.At.After(end) { return errGuardCapture }
	}
	if tlogs != 1 { return errGuardCapture }
	return nil
}

func guardCaptureBundleID(rawURL string, id string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" { return false }
	// Repository listing URL tails identify the server record; the cryptographic
	// identity and the signed log index remain separate mandatory checks.
	return path.Base(u.Path) == id+".json" || path.Base(u.Path) == id+".json.sn" || path.Base(u.Path) == id
}

func guardCaptureSelect(ctx context.Context, trusted trustedMaterial, entries []matrixEntry, p guardCapturePin, proof bool) (matrixOutcome, error) {
	policy := guardCapturePolicy(p, proof)
	var outcomes []matrixOutcome
	for i, e := range entries {
		o := matrixOutcome{Index:i, RepoMatch:e.RepositoryID == guardCaptureRepoID, Err:errGuardCapture}
		var compact bytes.Buffer
		if json.Compact(&compact, e.Bundle) != nil { return matrixOutcome{},errGuardCapture }
		o.Raw = append([]byte(nil), compact.Bytes()...)
		if o.RepoMatch && guardCaptureBundleStructure(o.Raw)==nil {
			o.Verified,o.Err = matrixVerify(ctx,trusted,o.Raw,policy)
			if o.Err == nil { o.Err = guardCaptureStatement(o.Verified,p) }
		}
		o.LogIndex,o.SAN = matrixBundleFacts(o.Raw)
		o.URLTail = matrixURLTail(e.BundleURL)
		outcomes = append(outcomes,o)
	}
	selected, err := matrixSelectExactlyOne(outcomes)
	if err != nil || int64(len(selected.Raw)) != p.BundleBytes || guardCaptureHash(selected.Raw) != p.BundleSHA ||
		selected.LogIndex != p.RekorIndex || !guardCaptureBundleID(entries[selected.Index].BundleURL,p.AttestationID) {
		return matrixOutcome{}, errGuardCapture
	}
	return selected,nil
}

func (c guardCaptureHTTP) listing(ctx context.Context, digest string) ([]matrixEntry,error) {
	if !lowerHex(digest,64) { return nil,errGuardCapture }
	raw,err := c.get(ctx,guardCaptureAPI+"/attestations/sha256:"+digest+"?per_page=100",guardCaptureMaxJSON,false,false)
	if err != nil { return nil,err }
	var listing struct { Entries []matrixEntry `json:"attestations"` }
	if guardCaptureJSON(raw,&listing)!=nil || len(listing.Entries)==0 || len(listing.Entries)>100 { return nil,errGuardCapture }
	// Current and legacy API representations are both untrusted data. A URL
	// from this listing is fetched without credentials, bounded and never
	// followed. It cannot supply the expected identity or subject digest.
	totalBundleBytes:=0
	for i:=range listing.Entries {
		entry:=&listing.Entries[i]
		if len(entry.Bundle)==0 || bytes.Equal(bytes.TrimSpace(entry.Bundle),[]byte("null")) {
			if !guardCaptureObjectURL(entry.BundleURL,false) || ctx.Err()!=nil {return nil,errGuardCapture}
			req,err:=http.NewRequestWithContext(ctx,http.MethodGet,entry.BundleURL,nil)
			if err!=nil{return nil,errGuardCapture}
			resp,err:=c.object.Do(req)
			if err!=nil{return nil,errGuardCapture}
			if resp.StatusCode!=http.StatusOK{_ = resp.Body.Close();return nil,errGuardCapture}
			entry.Bundle,err=guardCaptureBody(resp,guardCaptureMaxJSON)
			if err!=nil{return nil,errGuardCapture}
		}
		totalBundleBytes+=len(entry.Bundle)
		if int64(totalBundleBytes)>guardCaptureMaxJSON{return nil,errGuardCapture}
	}
	// Reuse the established bounded parser after populating a URL-only entry.
	normalized,err:=json.Marshal(struct{Entries []matrixEntry `json:"attestations"`}{listing.Entries})
	if err!=nil{return nil,errGuardCapture}
	return matrixParseListing(normalized)
}

func guardCaptureMutations(base matrixPolicy) []matrixMutation {
	extension := func(s string) []string { return []string{"expected "+s+" to be"} }
	otherWorkflow := strings.Replace(guardCaptureSAN,"unattended-readiness.yml","other.yml",1)
	return []matrixMutation{
		{Name:"san_pull_request",Identity:true,Want:[]string{"expected SAN value"},Mutate:func(p *matrixPolicy){p.SAN=strings.Replace(guardCaptureSAN,"refs/heads/main","refs/pull/1/merge",1)}},
		{Name:"san_other_workflow",Identity:true,Want:[]string{"expected SAN value"},Mutate:func(p *matrixPolicy){p.SAN=otherWorkflow}},
		{Name:"san_other_repository",Identity:true,Want:[]string{"expected SAN value"},Mutate:func(p *matrixPolicy){p.SAN=strings.Replace(guardCaptureSAN,"cc-pc-tools-rs","other-repository",1)}},
		{Name:"issuer",Identity:true,Want:[]string{"expected issuer value"},Mutate:func(p *matrixPolicy){p.Issuer="https://issuer.invalid"}},
		{Name:"config_uri",Identity:true,Want:extension("BuildConfigURI"),Mutate:func(p *matrixPolicy){p.ConfigURI=otherWorkflow}},
		{Name:"ref_pull_request",Identity:true,Want:extension("SourceRepositoryRef"),Mutate:func(p *matrixPolicy){p.Ref="refs/pull/1/merge"}},
		{Name:"ref_other_branch",Identity:true,Want:extension("SourceRepositoryRef"),Mutate:func(p *matrixPolicy){p.Ref="refs/heads/other"}},
		{Name:"trigger",Identity:true,Want:extension("BuildTrigger"),Mutate:func(p *matrixPolicy){p.Trigger="pull_request"}},
		{Name:"run",Identity:true,Want:extension("RunInvocationURI"),Mutate:func(p *matrixPolicy){p.RunURI=strings.Replace(base.RunURI,"/actions/runs/","/actions/runs/9",1)}},
		{Name:"attempt",Identity:true,Want:extension("RunInvocationURI"),Mutate:func(p *matrixPolicy){p.RunURI=base.RunURI+"0"}},
		{Name:"revision",Identity:true,Want:extension("BuildSignerDigest"),Mutate:func(p *matrixPolicy){p.Revision=strings.Repeat("0",40)}},
		{Name:"repository_id",Identity:true,Want:extension("SourceRepositoryIdentifier"),Mutate:func(p *matrixPolicy){p.RepositoryID="1"}},
		{Name:"repository_uri",Identity:true,Want:extension("SourceRepositoryURI"),Mutate:func(p *matrixPolicy){p.RepositoryURI="https://github.com/Other/Repo"}},
		{Name:"owner_id",Identity:true,Want:extension("SourceRepositoryOwnerIdentifier"),Mutate:func(p *matrixPolicy){p.OwnerID="1"}},
		{Name:"owner_uri",Identity:true,Want:extension("SourceRepositoryOwnerURI"),Mutate:func(p *matrixPolicy){p.OwnerURI="https://github.com/Other"}},
		{Name:"runner",Identity:true,Want:extension("RunnerEnvironment"),Mutate:func(p *matrixPolicy){p.Runner="self-hosted"}},
		{Name:"visibility",Identity:true,Want:extension("SourceRepositoryVisibilityAtSigning"),Mutate:func(p *matrixPolicy){p.Visibility="private"}},
		{Name:"subject_digest",Want:[]string{"does not match any digest in statement"},Mutate:func(p *matrixPolicy){p.SubjectSHA=strings.Repeat("0",64)}},
	}
}

type guardCaptureReport struct {
	Domain string `json:"domain"`
	Passed bool `json:"passed"`
	Reason string `json:"reason"`
	NewArtifactTupleIssued bool `json:"new_artifact_tuple_issued"`
	SignatureChecked bool `json:"signature_checked"`
	ZIPChecked bool `json:"zip_checked"`
	SourceClosureChecked bool `json:"source_closure_checked"`
	BuildInfoChecked bool `json:"build_info_checked"`
	UpdateAuthorized bool `json:"update_authorized"`
	DataAccessGranted bool `json:"data_access_granted"`
	InstalledTested bool `json:"installed_tested"`
	HostsTested bool `json:"hosts_tested"`
	CompilerClosureComplete bool `json:"compiler_closure_complete"`
	Pin guardCapturePin `json:"pin"`
	BundleSHA string `json:"bundle_sha256"`
	PublicationSHA string `json:"publication_sha256"`
	Controls map[string]bool `json:"controls"`
}

func guardCaptureWrite(dir,name string,raw []byte) error {
	if dir == "" || path.Base(name) != name || strings.ContainsAny(name,"\\:\r\n") { return errGuardCapture }
	file,err := os.OpenFile(filepath.Join(dir,name),os.O_WRONLY|os.O_CREATE|os.O_EXCL,0600)
	if err != nil { return errGuardCapture }
	n,writeErr := file.Write(raw)
	if writeErr == nil && n != len(raw) { writeErr = io.ErrShortWrite }
	if writeErr == nil { writeErr = file.Sync() }
	closeErr := file.Close()
	return errors.Join(writeErr,closeErr)
}

// One result ties metadata, both verified subjects and the exact downloaded
// bytes together. It never invokes a payload or returns an update permit.
func TestGuardCaptureAttestationActual(t *testing.T) {
	if os.Getenv("GUARD_CAPTURE_ATTESTATION_ACTUAL") != "1" {
		t.Skip("guard_capture_not_executed: opt-in absent")
	}
	p := guardCaptureNext
	report := guardCaptureReport{Domain:"guard-capture-actual-v1",Reason:"not_completed",
		NewArtifactTupleIssued:p.Issued,Pin:p,Controls:map[string]bool{}}
	dir := os.Getenv("GUARD_ATTESTATION_DIAGNOSTIC_DIR")
	defer func(){
		if t.Failed(){report.Passed=false}
		raw,err := json.MarshalIndent(report,"","  ")
		if err != nil || guardCaptureWrite(dir,"guard-capture-report.json",append(raw,'\n')) != nil {
			report.Passed=false
			t.Error("guard_capture_report_preservation_failed")
		}
		t.Logf("guard_capture_result passed=%t signature_checked=%t zip_checked=%t source_closure_checked=%t build_info_checked=%t controls=%d update_authorized=false data_access_granted=false",
			report.Passed,report.SignatureChecked,report.ZIPChecked,report.SourceClosureChecked,report.BuildInfoChecked,len(report.Controls))
	}()
	if !guardCapturePinValid(p) { report.Reason="tuple_not_issued_or_invalid";t.Fatal("guard_capture_stage=pin") }
	ctx,cancel := context.WithTimeout(context.Background(),4*time.Minute)
	defer cancel()
	client := guardCaptureLiveHTTP(strings.TrimSpace(os.Getenv("GH_TOKEN")))
	var server guardCaptureServer
	var publication []byte
	var err error
	expires,_:=guardCaptureTime(p.ArtifactExpires)
	// After expiry, only the public signed claims are replayed. Missing expired
	// Actions payloads or logs do not become permanent failures or acceptances.
	if time.Now().UTC().Before(expires) {
		server,err=client.server(ctx,p)
		if err != nil { report.Reason="server_binding_rejected";t.Fatal("guard_capture_stage=server") }
		log,readErr:=client.get(ctx,fmt.Sprintf("%s/actions/jobs/%d/logs",guardCaptureAPI,p.JobID),guardCaptureMaxLog,true,false)
		if readErr != nil { report.Reason="publication_unavailable";t.Fatal("guard_capture_stage=job_log") }
		publication,err=guardCapturePublication(log,p)
		clear(log)
		if err != nil { report.Reason="publication_rejected";t.Fatal("guard_capture_stage=publication") }
		report.PublicationSHA=guardCaptureHash(publication)
	}
	trusted,err := acquirePublicGoodRoot(ctx)
	if err != nil { report.Reason="trusted_root_unavailable";t.Fatal("guard_capture_stage=root") }
	peEntries,err := client.listing(ctx,p.PESHA)
	if err != nil { report.Reason="pe_listing_unavailable";t.Fatal("guard_capture_stage=pe_listing") }
	selected,err := guardCaptureSelect(ctx,trusted,peEntries,p,false)
	if err != nil { report.Reason="pe_signature_rejected";t.Fatal("guard_capture_stage=pe_signature") }
	proofEntries,err := client.listing(ctx,p.ProofSHA)
	if err != nil { report.Reason="proof_listing_unavailable";t.Fatal("guard_capture_stage=proof_listing") }
	selectedProof,err := guardCaptureSelect(ctx,trusted,proofEntries,p,true)
	if err != nil || !bytes.Equal(selected.Raw,selectedProof.Raw) {
		report.Reason="subjects_not_same_verified_bundle";t.Fatal("guard_capture_stage=same_bundle")
	}
	report.SignatureChecked=true
	report.BundleSHA=guardCaptureHash(selected.Raw)
	policy:=guardCapturePolicy(p,false)
	for _,m:=range guardCaptureMutations(policy) {
		t.Run("policy_"+m.Name,func(t *testing.T){
			changed:=policy;m.Mutate(&changed)
			if len(matrixChangedFields(policy,changed))!=1 {t.Fatal("ineffective_policy_mutation")}
			_,err:=matrixVerify(ctx,trusted,selected.Raw,changed)
			matrixAssertRejected(t,err,m.Identity,m.Want)
			report.Controls["policy_"+m.Name]=true
		})
	}
	normalized,err:=matrixEditBundle(selected.Raw,func(map[string]any)bool{return true})
	if err!=nil {t.Fatal("guard_capture_stage=normalize")}
	if _,err=matrixVerify(ctx,trusted,normalized,policy);err!=nil {t.Fatal("unmodified_bundle_reencoding_rejected")}
	for _,control:=range matrixTamperControls() {
		t.Run("tamper_"+control.Name,func(t *testing.T){
			changed,err:=matrixEditBundle(selected.Raw,control.Edit)
			if err!=nil||bytes.Equal(changed,normalized){t.Fatal("ineffective_bundle_edit")}
			_,err=matrixVerify(ctx,trusted,changed,policy)
			matrixAssertRejected(t,err,false,nil)
			report.Controls["tamper_"+control.Name]=true
		})
	}
	t.Run("verified_entry_cardinality",func(t *testing.T){
		duplicated:=append([]matrixEntry(nil),peEntries...)
		duplicated=append(duplicated,peEntries[selected.Index])
		if _,err:=guardCaptureSelect(ctx,trusted,duplicated,p,false);err==nil{t.Fatal("duplicate_verified_bundle_accepted")}
		var none []matrixEntry
		for i,e:=range peEntries{if i!=selected.Index{none=append(none,e)}}
		if _,err:=guardCaptureSelect(ctx,trusted,none,p,false);err==nil{t.Fatal("zero_verified_bundle_accepted")}
		report.Controls["verified_entry_cardinality"]=true
	})
	t.Run("cancelled_verification",func(t *testing.T){
		done,stop:=context.WithCancel(context.Background());stop()
		if _,err:=guardCaptureSelect(done,trusted,peEntries,p,false);err==nil{t.Fatal("cancelled_verification_accepted")}
		report.Controls["cancelled_verification"]=true
	})
	old:=actualBundleFixture(t)
	oldPolicy:=guardCapturePolicy(p,false)
	oldPolicy.SubjectSHA=actualPESHA
	_,err=matrixVerify(ctx,trusted,old,oldPolicy)
	matrixAssertRejected(t,err,true,[]string{"expected"})
	report.Controls["old_rehearsal_not_new_capture"]=true
	if t.Failed(){report.Reason="crypto_control_failed";return}
	if guardCaptureWrite(dir,"guard-capture-bundle.json",selected.Raw)!=nil {t.Fatal("guard_capture_stage=preserve_bundle")}
	if len(publication)>0&&guardCaptureWrite(dir,"guard-capture-publication.json",publication)!=nil {t.Fatal("guard_capture_stage=preserve_publication")}

	if server.Artifact.Expired||!time.Now().UTC().Before(expires) {
		report.Reason="artifact_expired_payload_not_executed"
		t.Skip("guard_capture_payload_not_executed: artifact expired; signature result retained, acceptance=false")
	}
	archive,err:=client.get(ctx,fmt.Sprintf("%s/actions/artifacts/%d/zip",guardCaptureAPI,p.ArtifactID),guardCaptureMaxZIP,true,false)
	if err!=nil {report.Reason="archive_unavailable";t.Fatal("guard_capture_stage=archive")}
	defer clear(archive)
	payload,err:=guardCaptureReadZIP(archive,p)
	if err!=nil {report.Reason="archive_rejected";t.Fatal("guard_capture_stage=archive_bytes")}
	defer clear(payload.PE);defer clear(payload.Proof);defer clear(payload.Capture)
	proof,err:=guardCaptureProofCheck(payload.Proof,payload.Capture,p)
	if err!=nil {report.Reason="proof_rejected";t.Fatal("guard_capture_stage=proof")}
	if client.source(ctx,p,proof)!=nil {report.Reason="source_closure_rejected";t.Fatal("guard_capture_stage=source_closure")}
	report.SourceClosureChecked=true
	var observed struct { Version string `json:"version"`; Env map[string]string `json:"env"` }
	_ = json.Unmarshal(proof.Before.Observation,&observed)
	if guardCaptureBinary(payload.PE,p,observed.Env["GOVERSION"])!=nil {report.Reason="binary_rejected";t.Fatal("guard_capture_stage=build_info")}
	report.BuildInfoChecked=true
	last,err:=client.server(ctx,p)
	if err!=nil||!reflect.DeepEqual(last,server)||ctx.Err()!=nil {report.Reason="final_binding_changed";t.Fatal("guard_capture_stage=final_binding")}
	// Preserve actual bytes before claiming the payload check. A later expiry
	// cannot turn the retained payload into a newly executed check.
	for name,raw:=range map[string][]byte{"guard-capture-artifact.zip":archive,"guard-capture-guard.exe":payload.PE,
		"guard-capture-source-proof.json":payload.Proof,"guard-capture-capture.json":payload.Capture} {
		if guardCaptureWrite(dir,name,raw)!=nil {report.Reason="payload_preservation_failed";t.Fatal("guard_capture_stage=preserve_payload")}
	}
	if ctx.Err()!=nil{report.Reason="cancelled_before_result";t.Fatal("guard_capture_stage=final_context")}
	report.ZIPChecked=true
	report.Passed=true
	report.Reason="fixed_capture_verified_without_update_authority"
}

func guardCaptureTestPin() guardCapturePin {
	return guardCapturePin{Issued:true,Source:strings.Repeat("1",40),SourceTree:strings.Repeat("2",40),
		RepairTree:strings.Repeat("3",40),ContractTree:strings.Repeat("4",40),InputCount:5,
		Producer:strings.Repeat("5",40),WorkflowBlob:strings.Repeat("6",40),HelperBlob:strings.Repeat("7",40),
		RunID:101,Attempt:1,JobID:202,CheckRunID:303,JobStarted:"2026-01-01T00:00:00Z",JobCompleted:"2026-01-01T00:10:00Z",
		ArtifactID:404,ArtifactName:"guard-bootstrap-101-1-202",ArtifactCreated:"2026-01-01T00:09:00Z",ArtifactExpires:"2026-01-02T00:09:00Z",
		ArchiveBytes:1,ArchiveSHA:strings.Repeat("a",64),PEBytes:1,PESHA:strings.Repeat("b",64),
		ProofBytes:1,ProofSHA:strings.Repeat("c",64),CaptureBytes:1,CaptureSHA:strings.Repeat("d",64),
		AttestationID:"505",RekorIndex:"606",BundleBytes:1,BundleSHA:strings.Repeat("e",64)}
}

func TestGuardCapturePinFailsClosed(t *testing.T) {
	good:=guardCaptureTestPin()
	if !guardCapturePinValid(good){t.Fatal("synthetic_complete_pin_rejected")}
	unissued:=guardCaptureNext;unissued.Issued=false
	if guardCapturePinValid(unissued){t.Fatal("unissued_tuple_accepted")}
	if guardCaptureNext.Issued&&!guardCapturePinValid(guardCaptureNext){t.Fatal("issued_tuple_invalid")}
	for name,edit:=range map[string]func(*guardCapturePin){
		"unissued":func(p *guardCapturePin){p.Issued=false},
		"run":func(p *guardCapturePin){p.RunID++},
		"job":func(p *guardCapturePin){p.JobID++},
		"zero_artifact":func(p *guardCapturePin){p.ArtifactID=0},
		"source_producer_swap":func(p *guardCapturePin){p.Producer=p.Source},
		"empty_digest":func(p *guardCapturePin){p.ProofSHA=""},
		"upper_digest":func(p *guardCapturePin){p.ArchiveSHA=strings.Repeat("A",64)},
		"bad_attestation":func(p *guardCapturePin){p.AttestationID="../x"},
		"bad_log_index":func(p *guardCapturePin){p.RekorIndex="1x"},
		"late_job":func(p *guardCapturePin){p.JobCompleted="2026-01-01T00:16:00Z"},
		"early_artifact":func(p *guardCapturePin){p.ArtifactCreated="2025-12-31T23:59:59Z"},
		"expired_before_upload":func(p *guardCapturePin){p.ArtifactExpires=p.ArtifactCreated},
	}{
		t.Run(name,func(t *testing.T){p:=good;edit(&p);if guardCapturePinValid(p){t.Fatal("invalid_pin_accepted")}})
	}
}

func TestGuardCapturePublicationExactAndUnambiguous(t *testing.T) {
	p:=guardCaptureTestPin()
	raw,_:=json.Marshal(guardCaptureExpectedRecord(p))
	line:=append([]byte("2026-01-01T00:09:59.000Z guard_gha_publication_v1="),raw...)
	if got,err:=guardCapturePublication(line,p);err!=nil||!bytes.Equal(got,raw){t.Fatal("valid_publication_rejected")}
	for name,body:=range map[string][]byte{
		"duplicate":append(append(append([]byte(nil),line...),'\n'),line...),
		"not_marker":append([]byte("quoted: guard_gha_publication_v1="),raw...),
		"trailing":append(append([]byte(nil),line...),[]byte(" {}")...),
		"unknown":bytes.Replace(line,[]byte("\"domain\":"),[]byte("\"unknown\":true,\"domain\":"),1),
		"null":bytes.Replace(line,[]byte("\"run_id\":101"),[]byte("\"run_id\":null"),1),
		"case_alias":bytes.Replace(line,[]byte("\"run_id\":101"),[]byte("\"Run_ID\":101"),1),
		"wrong_id":bytes.Replace(line,[]byte("\"run_id\":101"),[]byte("\"run_id\":102"),1),
		"duplicate_key":bytes.Replace(line,[]byte("\"run_id\":101"),[]byte("\"run_id\":101,\"run_id\":101"),1),
	}{
		t.Run(name,func(t *testing.T){if _,err:=guardCapturePublication(body,p);err==nil{t.Fatal("invalid_publication_accepted")}})
	}
	var value any
	for _,raw:=range []string{`{"id":1,"ID":1}`,`{"x":{"id":1,"id":1}}`,`{} {}`}{
		if guardCaptureJSON([]byte(raw),&value)==nil{t.Fatal("ambiguous_metadata_accepted")}
	}
}

func guardCaptureTestProof() (guardCapturePin,[]byte,[]byte) {
	p:=guardCaptureTestPin()
	inputs:=[]guardCaptureInput{}
	for _,name:=range []string{"repair-guard/go.mod","repair-guard/go.sum","repair-guard/owner_catalog.json","tools/civ6-ci-contract/go.mod","tools/civ6-ci-contract/go.sum"}{
		raw:=[]byte(name)
		inputs=append(inputs,guardCaptureInput{name,int64(len(raw)),guardCaptureHash(raw),guardCaptureBlob(raw)})
	}
	obs,_:=json.Marshal(struct{Version string `json:"version"`;Env map[string]string `json:"env"`}{
		"go version go1.27.4 windows/amd64",map[string]string{"GOOS":"windows","GOARCH":"amd64","GOVERSION":"go1.27.4",
			"GOROOT":"<runner-goroot>","GOTOOLCHAIN":"local","GOWORK":"off","GOFLAGS":"","CGO_ENABLED":"0",
			"GOMODCACHE":"<runner-module-cache>","GOCACHE":"<runner-build-cache>","GOPROXY":"https://proxy.golang.org","GOSUMDB":"sum.golang.org"}})
	graph,_:=json.Marshal([]guardCaptureGraphPackage{{ImportPath:"cc/civ6-ci-contract"},{ImportPath:guardCaptureModule}})
	command:=guardCaptureCommand{"repair-guard","go",[]string{"build","-mod=readonly","-pgo=off","-trimpath","-o","<fixed-attempt-directory>/guard-build.exe","."}}
	image:=guardCaptureImage{"win25","1","Windows","X64"}
	b:=guardCaptureBefore{Domain:"guard-gha-source-before-v1",RepositoryID:guardCaptureRepoID,Source:p.Source,SourceTree:p.SourceTree,
		SourceTreeBefore:p.SourceTree,Producer:p.Producer,RunID:p.RunID,Attempt:p.Attempt,CheckRunID:p.CheckRunID,Event:"workflow_dispatch",
		Inputs:inputs,InventorySHA:guardCaptureDigestJSON(inputs),Observation:obs,ObservationSHA:guardCaptureHash(obs),
		BuildGraph:graph,BuildGraphSHA:guardCaptureHash(graph),Command:command,CommandSHA:guardCaptureDigestJSON(command),
		Image:image,ImageSHA:guardCaptureDigestJSON(image),GitTreeSHA:strings.Repeat("a",64),GitArchiveSHA:strings.Repeat("b",64)}
	proof,_:=json.Marshal(guardCaptureProof{"guard-gha-source-proof-v1",b,b,0})
	p.ProofBytes,p.ProofSHA=int64(len(proof)),guardCaptureHash(proof)
	capture,_:=json.Marshal(guardCaptureManifest{"guard-gha-capture-v1",guardCaptureRepoID,p.Source,p.SourceTree,p.Producer,
		p.RunID,p.Attempt,p.JobID,p.CheckRunID,p.ProofSHA,b.ObservationSHA,b.CommandSHA,b.ImageSHA,"guard.exe",p.PEBytes,p.PESHA})
	p.CaptureBytes,p.CaptureSHA=int64(len(capture)),guardCaptureHash(capture)
	return p,proof,capture
}

func TestGuardCaptureProofAndCaptureBindings(t *testing.T) {
	p,raw,capture:=guardCaptureTestProof()
	good,err:=guardCaptureProofCheck(raw,capture,p)
	if err!=nil{t.Fatal("valid_synthetic_proof_rejected")}
	for name,edit:=range map[string]func(*guardCaptureProof){
		"exit":func(v *guardCaptureProof){v.CompilerExit=1},
		"after":func(v *guardCaptureProof){v.After.Source=strings.Repeat("0",40)},
		"producer":func(v *guardCaptureProof){v.Before.Producer=v.Before.Source;v.After=v.Before},
		"inventory_count":func(v *guardCaptureProof){v.Before.Inputs=v.Before.Inputs[:4];v.After=v.Before},
		"inventory_hash":func(v *guardCaptureProof){v.Before.InventorySHA=strings.Repeat("0",64);v.After=v.Before},
		"compile_command":func(v *guardCaptureProof){v.Before.Command.Arguments=[]string{"build","."};v.After=v.Before},
		"graph_hash":func(v *guardCaptureProof){v.Before.BuildGraphSHA=strings.Repeat("0",64);v.After=v.Before},
		"image":func(v *guardCaptureProof){v.Before.Image.RunnerOS="Linux";v.After=v.Before},
	}{
		t.Run(name,func(t *testing.T){
			var changed guardCaptureProof
			_ = json.Unmarshal(raw,&changed);edit(&changed)
			body,_:=json.Marshal(changed)
			np:=p;np.ProofBytes,np.ProofSHA=int64(len(body)),guardCaptureHash(body)
			var cm guardCaptureManifest;_ = json.Unmarshal(capture,&cm);cm.ProofSHA=np.ProofSHA
			cr,_:=json.Marshal(cm);np.CaptureBytes,np.CaptureSHA=int64(len(cr)),guardCaptureHash(cr)
			if _,err:=guardCaptureProofCheck(body,cr,np);err==nil{t.Fatal("mutated_proof_accepted")}
		})
	}
	if !reflect.DeepEqual(good.Before,good.After){t.Fatal("test_fixture_not_same_before_after")}
	for _,edit:=range []func(*guardCaptureManifest){
		func(v *guardCaptureManifest){v.PESHA=strings.Repeat("0",64)},
		func(v *guardCaptureManifest){v.CheckRunID++},
		func(v *guardCaptureManifest){v.OutputName="other.exe"},
		func(v *guardCaptureManifest){v.ObservationSHA=strings.Repeat("0",64)},
	}{
		var v guardCaptureManifest;_ = json.Unmarshal(capture,&v);edit(&v)
		body,_:=json.Marshal(v);np:=p;np.CaptureBytes,np.CaptureSHA=int64(len(body)),guardCaptureHash(body)
		if _,err:=guardCaptureProofCheck(raw,body,np);err==nil{t.Fatal("mutated_capture_accepted")}
	}
}

func guardCaptureTestZIP(t *testing.T,names []string,contents map[string][]byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	w:=zip.NewWriter(&raw)
	for _,name:=range names{
		f,err:=w.Create(name);if err!=nil{t.Fatal("zip_fixture_create")}
		if _,err=f.Write(contents[name]);err!=nil{t.Fatal("zip_fixture_write")}
	}
	if w.Close()!=nil{t.Fatal("zip_fixture_close")}
	return raw.Bytes()
}

func TestGuardCaptureZIPUsesSameBytesAndExactInventory(t *testing.T) {
	p:=guardCaptureTestPin()
	contents:=map[string][]byte{"guard.exe":[]byte("PE fixture"),"source-proof.json":[]byte("{}"),"capture.json":[]byte("{}")}
	p.PEBytes,p.PESHA=int64(len(contents["guard.exe"])),guardCaptureHash(contents["guard.exe"])
	p.ProofBytes,p.ProofSHA=2,guardCaptureHash(contents["source-proof.json"])
	p.CaptureBytes,p.CaptureSHA=2,guardCaptureHash(contents["capture.json"])
	names:=[]string{"guard.exe","source-proof.json","capture.json"}
	for _,bad:=range []bool{false,true}{
		selected:=names;if bad{selected=[]string{"guard.exe","source-proof.json","capture.json","guard.exe"}}
		raw:=guardCaptureTestZIP(t,selected,contents)
		p.ArchiveBytes,p.ArchiveSHA=int64(len(raw)),guardCaptureHash(raw)
		got,err:=guardCaptureReadZIP(raw,p)
		if bad{if err==nil{t.Fatal("duplicate_member_accepted")};continue}
		if err!=nil||!bytes.Equal(got.PE,contents["guard.exe"]){t.Fatal("valid_same_buffer_zip_rejected")}
		wrong:=p;wrong.ProofSHA=strings.Repeat("0",64)
		if _,err=guardCaptureReadZIP(raw,wrong);err==nil{t.Fatal("wrong_member_digest_accepted")}
		if _,err=guardCaptureReadZIP(raw[:len(raw)-1],p);err==nil{t.Fatal("truncated_zip_accepted")}
	}
	for _,names:=range [][]string{{"guard.exe","source-proof.json"},{"guard.exe","source-proof.json","../capture.json"}}{
		raw:=guardCaptureTestZIP(t,names,contents);p.ArchiveBytes,p.ArchiveSHA=int64(len(raw)),guardCaptureHash(raw)
		if _,err:=guardCaptureReadZIP(raw,p);err==nil{t.Fatal("wrong_inventory_accepted")}
	}
}

func guardCaptureTestServer(p guardCapturePin) guardCaptureServer {
	var s guardCaptureServer
	s.Run.ID,s.Run.Attempt,s.Run.HeadSHA,s.Run.Branch=p.RunID,p.Attempt,p.Producer,"main"
	s.Run.Event,s.Run.Path,s.Run.Status,s.Run.Conclusion="workflow_dispatch",guardCaptureWorkflow,"completed","success"
	s.Run.Repository.ID,s.Run.Repository.FullName,s.Run.Repository.Private=guardCaptureRepoID,guardCaptureRepo,false
	s.Run.HeadRepository.ID=guardCaptureRepoID
	s.Job=guardCaptureJob{ID:p.JobID,RunID:p.RunID,HeadSHA:p.Producer,Name:"Guard bootstrap artifact",
		Status:"completed",Conclusion:"success",Started:p.JobStarted,Completed:p.JobCompleted,RunnerID:1,
		CheckRunURL:fmt.Sprintf("%s/check-runs/%d",guardCaptureAPI,p.CheckRunID)}
	for i,name:=range []string{"Capture the fixed guard build","Check the exact captured files","Attest the guard and source evidence",
		"Upload the fixed guard capture","Record the server-assigned capture artifact"}{
		s.Job.Steps=append(s.Job.Steps,guardCaptureStep{Name:name,Number:i+1,Status:"completed",Conclusion:"success"})
	}
	s.Artifact.ID,s.Artifact.Name,s.Artifact.Bytes,s.Artifact.Digest=p.ArtifactID,p.ArtifactName,p.ArchiveBytes,"sha256:"+p.ArchiveSHA
	s.Artifact.Created,s.Artifact.Expires=p.ArtifactCreated,p.ArtifactExpires
	s.Artifact.Run.ID,s.Artifact.Run.RepositoryID,s.Artifact.Run.HeadRepositoryID=p.RunID,guardCaptureRepoID,guardCaptureRepoID
	s.Artifact.Run.HeadSHA,s.Artifact.Run.Branch=p.Producer,"main"
	return s
}

func TestGuardCaptureServerBindingSeparatesRevisionsAndJob(t *testing.T) {
	p:=guardCaptureTestPin()
	good:=guardCaptureTestServer(p)
	if guardCaptureServerCheck(good,p)!=nil{t.Fatal("valid_server_fixture_rejected")}
	for name,edit:=range map[string]func(*guardCaptureServer){
		"source_is_not_producer":func(s *guardCaptureServer){s.Run.HeadSHA=p.Source},
		"pull_request":func(s *guardCaptureServer){s.Run.Event="pull_request"},
		"other_repo":func(s *guardCaptureServer){s.Run.Repository.ID++},
		"other_job":func(s *guardCaptureServer){s.Job.ID++},
		"other_check_run":func(s *guardCaptureServer){s.Job.CheckRunURL+="0"},
		"step_skip":func(s *guardCaptureServer){s.Job.Steps[0].Conclusion="skipped"},
		"step_duplicate":func(s *guardCaptureServer){s.Job.Steps=append(s.Job.Steps,s.Job.Steps[0])},
		"wrong_archive_digest":func(s *guardCaptureServer){s.Artifact.Digest="sha256:"+strings.Repeat("0",64)},
		"other_artifact_run":func(s *guardCaptureServer){s.Artifact.Run.ID++},
		"other_artifact_head":func(s *guardCaptureServer){s.Artifact.Run.HeadSHA=p.Source},
	}{
		t.Run(name,func(t *testing.T){
			body,_:=json.Marshal(good);var changed guardCaptureServer;_ = json.Unmarshal(body,&changed);edit(&changed)
			if guardCaptureServerCheck(changed,p)==nil{t.Fatal("mutated_server_binding_accepted")}
		})
	}
}

type guardCaptureRoundTrip func(*http.Request)(*http.Response,error)
func (f guardCaptureRoundTrip) RoundTrip(r *http.Request)(*http.Response,error){return f(r)}
type guardCaptureCloseFault struct {io.Reader}
func (guardCaptureCloseFault) Close()error{return errGuardCapture}

func TestGuardCaptureHTTPSeparatesCredentialsAndBounds(t *testing.T) {
	for _,name:=range []string{"normal","forbidden","bad_redirect","object_redirect","overflow","close_error","cancelled"}{
		t.Run(name,func(t *testing.T){
			apiCalls,objectCalls:=0,0
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			if name=="cancelled"{cancel()}
			api:=guardCaptureRoundTrip(func(req *http.Request)(*http.Response,error){
				apiCalls++
				if req.URL.Host!="api.github.com"||req.Header.Get("Authorization")!="Bearer fixture-only-token"{t.Fatal("token_not_confined_to_api")}
				status:=http.StatusFound;location:="https://fixture.blob.core.windows.net/blob?temporary=fixture"
				if name=="forbidden"{status=http.StatusForbidden}
				if name=="bad_redirect"{location="https://notallowed.invalid/blob"}
				return &http.Response{StatusCode:status,Header:http.Header{"Location":{location}},Body:io.NopCloser(strings.NewReader(""))},nil
			})
			object:=guardCaptureRoundTrip(func(req *http.Request)(*http.Response,error){
				objectCalls++
				if req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""||req.Header.Get("Accept")!=""{t.Fatal("credential_or_api_header_reached_object")}
				body:=io.ReadCloser(io.NopCloser(strings.NewReader("abc")))
				if name=="overflow"{body=io.NopCloser(strings.NewReader("abcd"))}
				if name=="close_error"{body=guardCaptureCloseFault{strings.NewReader("abc")}}
				status:=http.StatusOK;if name=="object_redirect"{status=http.StatusFound}
				return &http.Response{StatusCode:status,Header:http.Header{},Body:body},nil
			})
			c:=guardCaptureHTTP{api:&http.Client{Transport:api,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}},
				object:&http.Client{Transport:object,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}},token:"fixture-only-token"}
			body,err:=c.get(ctx,guardCaptureAPI+"/actions/artifacts/404/zip",3,true,false)
			if name=="normal"{if err!=nil||string(body)!="abc"||apiCalls!=1||objectCalls!=1{t.Fatal("normal_bounded_redirect_rejected")}}else{
				if err==nil{t.Fatal("negative_transport_accepted")}
				if name=="cancelled"&&apiCalls!=0{t.Fatal("cancelled_request_started")}
				if (name=="forbidden"||name=="bad_redirect")&&objectCalls!=0{t.Fatal("rejected_api_followed")}
			}
		})
	}
	for _,raw:=range []string{"http://x.blob.core.windows.net/a","https://blob.core.windows.net.evil.invalid/a",
		"https://user@x.blob.core.windows.net/a","https://x.blob.core.windows.net:444/a","https://x.blob.core.windows.net/a#frag"}{
		if guardCaptureObjectURL(raw,false){t.Fatal("unsafe_redirect_accepted")}
	}
}

func guardCaptureTestTree(inputs []guardCaptureInput)(guardCaptureTree,map[string]guardCaptureTreeEntry) {
	known:=map[string]guardCaptureTreeEntry{}
	for _,in:=range inputs{
		known[in.Path]=guardCaptureTreeEntry{Path:in.Path,Mode:"100644",Type:"blob",SHA:in.Blob,Size:in.Size}
		for dir:=path.Dir(in.Path);dir!=".";dir=path.Dir(dir){
			known[dir]=guardCaptureTreeEntry{Path:dir,Mode:"040000",Type:"tree"}
		}
	}
	var build func(string)string
	build=func(dir string)string{
		var list []guardCaptureTreeEntry
		for name,e:=range known{
			parent:=path.Dir(name);if parent=="."{parent=""}
			if parent!=dir{continue}
			if e.Type=="tree"{e.SHA=build(name);known[name]=e}
			list=append(list,e)
		}
		sort.Slice(list,func(i,j int)bool{
			a,b:=path.Base(list[i].Path),path.Base(list[j].Path)
			if list[i].Type=="tree"{a+="/"};if list[j].Type=="tree"{b+="/"};return a<b
		})
		var raw bytes.Buffer
		for _,e:=range list{
			_,_=fmt.Fprintf(&raw,"%s %s%c",strings.TrimLeft(e.Mode,"0"),path.Base(e.Path),byte(0))
			id,_:=hex.DecodeString(e.SHA);_,_=raw.Write(id)
		}
		h:=sha1.New();_,_=fmt.Fprintf(h,"tree %d%c",raw.Len(),byte(0));_,_=h.Write(raw.Bytes())
		return hex.EncodeToString(h.Sum(nil))
	}
	root:=build("")
	tree:=guardCaptureTree{SHA:root}
	var names []string;for name:=range known{names=append(names,name)};sort.Strings(names)
	for _,name:=range names{tree.Entries=append(tree.Entries,known[name])}
	return tree,known
}

func guardCaptureTestTar(t *testing.T,inputs []guardCaptureInput,mutate bool) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz:=gzip.NewWriter(&compressed)
	tw:=tar.NewWriter(gz)
	for i,in:=range inputs{
		body:=[]byte(in.Path);if mutate&&i==0{body=append(body,'x')}
		if tw.WriteHeader(&tar.Header{Name:"fixture-root/"+in.Path,Mode:0600,Size:int64(len(body)),Typeflag:tar.TypeReg})!=nil{t.Fatal("tar_header")}
		if _,err:=tw.Write(body);err!=nil{t.Fatal("tar_write")}
	}
	if tw.Close()!=nil||gz.Close()!=nil{t.Fatal("tar_close")}
	return compressed.Bytes()
}

func TestGuardCaptureSourceClosureUsesMerkleAndActualBytes(t *testing.T) {
	p,proofRaw,captureRaw:=guardCaptureTestProof()
	proof,err:=guardCaptureProofCheck(proofRaw,captureRaw,p)
	if err!=nil{t.Fatal("proof_fixture")}
	tree,known:=guardCaptureTestTree(proof.Before.Inputs)
	p.SourceTree,p.RepairTree,p.ContractTree=tree.SHA,known["repair-guard"].SHA,known["tools/civ6-ci-contract"].SHA
	verified,err:=guardCaptureTreeCheck(tree,p.SourceTree)
	if err!=nil||guardCaptureSourceEntries(verified,p,proof.Before.Inputs)!=nil{t.Fatal("valid_merkle_inventory_rejected")}
	for name,edit:=range map[string]func(*guardCaptureTree){
		"truncated":func(t *guardCaptureTree){t.Truncated=true},
		"blob":func(t *guardCaptureTree){for i:=range t.Entries{if t.Entries[i].Type=="blob"{t.Entries[i].SHA=strings.Repeat("0",40);break}}},
		"duplicate":func(t *guardCaptureTree){t.Entries=append(t.Entries,t.Entries[0])},
		"mode":func(t *guardCaptureTree){t.Entries[0].Mode="100755"},
	}{
		t.Run(name,func(t *testing.T){
			changed:=tree;changed.Entries=append([]guardCaptureTreeEntry(nil),tree.Entries...);edit(&changed)
			if _,err:=guardCaptureTreeCheck(changed,p.SourceTree);err==nil{t.Fatal("mutated_tree_accepted")}
		})
	}
	archive:=guardCaptureTestTar(t,proof.Before.Inputs,false)
	if guardCaptureSourceArchive(archive,proof.Before.Inputs)!=nil{t.Fatal("valid_selected_source_bytes_rejected")}
	if guardCaptureSourceArchive(guardCaptureTestTar(t,proof.Before.Inputs,true),proof.Before.Inputs)==nil{t.Fatal("mutated_selected_source_bytes_accepted")}
	if guardCaptureSourceArchive(archive,proof.Before.Inputs[:4])==nil{t.Fatal("extra_selected_source_accepted")}
	wrong:=append([]guardCaptureInput(nil),proof.Before.Inputs...);wrong[0].SHA=strings.Repeat("0",64)
	if guardCaptureSourceArchive(archive,wrong)==nil{t.Fatal("wrong_source_sha_accepted")}
	if guardCaptureSourceArchive(archive[:len(archive)-1],proof.Before.Inputs)==nil{t.Fatal("truncated_source_archive_accepted")}
}

func TestGuardCaptureBuildInfoAndTwoSubjectStatement(t *testing.T) {
	p:=guardCaptureTestPin()
	info:=&debug.BuildInfo{Path:guardCaptureModule,Main:debug.Module{Path:guardCaptureModule}}
	for k,v:=range map[string]string{"vcs":"git","vcs.revision":p.Source,"vcs.modified":"false","GOOS":"windows","GOARCH":"amd64","-trimpath":"true","CGO_ENABLED":"0"}{
		info.Settings=append(info.Settings,debug.BuildSetting{Key:k,Value:v})
	}
	if guardCaptureBuildInfo(info,p)!=nil{t.Fatal("valid_build_info_rejected")}
	for name,edit:=range map[string]func(*debug.BuildInfo){
		"wrong_path":func(b *debug.BuildInfo){b.Path+="other"},
		"modified_source":func(b *debug.BuildInfo){for i:=range b.Settings{if b.Settings[i].Key=="vcs.modified"{b.Settings[i].Value="true"}}},
		"missing_setting":func(b *debug.BuildInfo){b.Settings=nil},
		"duplicate_setting":func(b *debug.BuildInfo){b.Settings=append(b.Settings,b.Settings[0])},
		"pgo_profile":func(b *debug.BuildInfo){b.Settings=append(b.Settings,debug.BuildSetting{Key:"-pgo",Value:"profile.pprof"})},
	}{
		changed:=*info;changed.Settings=append([]debug.BuildSetting(nil),info.Settings...);edit(&changed)
		if guardCaptureBuildInfo(&changed,p)==nil{t.Fatalf("invalid_build_info_accepted control=%s",name)}
	}
	if guardCaptureBinary([]byte("not a PE"),p,"go1.27.4")==nil{t.Fatal("non_pe_accepted")}
	at,_:=guardCaptureTime(p.JobStarted)
	v:=matrixVerified{StatementType:matrixStatementV1,PredicateType:matrixPredicateV1,
		Subjects:[]matrixSubject{{"guard.exe",p.PESHA,1},{"source-proof.json",p.ProofSHA,1}},
		SAN:guardCaptureSAN,Issuer:"https://token.actions.githubusercontent.com",RunURI:fixedRunURI(p.RunID,p.Attempt),
		Ref:"refs/heads/main",Trigger:"workflow_dispatch",Stamps:[]matrixStamp{{Type:"Tlog",At:at}}}
	if guardCaptureStatement(v,p)!=nil{t.Fatal("valid_two_subject_statement_rejected")}
	for name,edit:=range map[string]func(*matrixVerified){
		"one_subject":func(v *matrixVerified){v.Subjects=v.Subjects[:1]},
		"wrong_second_subject":func(v *matrixVerified){v.Subjects[1].SHA256=strings.Repeat("0",64)},
		"wrong_order":func(v *matrixVerified){v.Subjects[0],v.Subjects[1]=v.Subjects[1],v.Subjects[0]},
		"outside_job":func(v *matrixVerified){v.Stamps[0].At=at.Add(-time.Second)},
		"missing_tlog":func(v *matrixVerified){v.Stamps=nil},
		"duplicate_tlog":func(v *matrixVerified){v.Stamps=append(v.Stamps,v.Stamps[0])},
		"pr_ref":func(v *matrixVerified){v.Ref="refs/pull/1/merge"},
	}{
		changed:=v;changed.Subjects=append([]matrixSubject(nil),v.Subjects...);changed.Stamps=append([]matrixStamp(nil),v.Stamps...);edit(&changed)
		if guardCaptureStatement(changed,p)==nil{t.Fatalf("invalid_statement_accepted control=%s",name)}
	}
	policy:=guardCapturePolicy(p,false)
	covered:=map[string]bool{}
	for _,m:=range guardCaptureMutations(policy){
		changed:=policy;m.Mutate(&changed)
		fields:=matrixChangedFields(policy,changed)
		if len(fields)!=1||!matrixPolicyComplete(changed){t.Fatal("ineffective_identity_mutation")}
		covered[fields[0]]=true
	}
	for _,name:=range matrixPolicyFieldNames(){if !covered[name]{t.Fatalf("missing_policy_control=%s",name)}}
}

// The published old bundle is a cryptographic rehearsal only. No payload,
// server tuple, new capture or installed target is accepted by this test.
func TestGuardCaptureTwoSubjectCryptographicRehearsal(t *testing.T) {
	raw:=actualBundleFixture(t)
	ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
	trusted,err:=acquirePublicGoodRoot(ctx)
	if err!=nil{t.Fatal("guard_rehearsal_root_unavailable")}
	if guardCaptureBundleStructure(raw)!=nil{t.Fatal("rehearsal_log_components_missing")}
	old:=actualPolicy()
	p:=guardCapturePin{Producer:old.WorkflowRevision,RunID:old.RunID,Attempt:old.RunAttempt,PESHA:actualPESHA,ProofSHA:actualProofSHA}
	var rehearsalVerified matrixVerified
	for _,proof:=range []bool{false,true}{
		policy:=guardCapturePolicy(p,proof)
		v,err:=matrixVerify(ctx,trusted,raw,policy)
		if err!=nil||len(v.Subjects)!=2||v.Subjects[0]!=(matrixSubject{"guard.exe",actualPESHA,1})||
			v.Subjects[1]!=(matrixSubject{"source-proof.json",actualProofSHA,1}){t.Fatal("guard_two_subject_rehearsal_failed")}
		rehearsalVerified=v
	}
	// This exercises the real selector with cryptographically verified old
	// bytes; the temporary pin is only a rehearsal input, not an issued tuple.
	var compact bytes.Buffer
	if json.Compact(&compact,raw)!=nil{t.Fatal("rehearsal_compact")}
	p.BundleBytes,p.BundleSHA=int64(compact.Len()),guardCaptureHash(compact.Bytes())
	p.RekorIndex,_=matrixBundleFacts(raw)
	p.AttestationID="rehearsal"
	for _,stamp:=range rehearsalVerified.Stamps{
		if stamp.Type=="Tlog"{
			p.JobStarted=stamp.At.Add(-time.Minute).Format(time.RFC3339)
			p.JobCompleted=stamp.At.Add(time.Minute).Format(time.RFC3339)
		}
	}
	entries:=[]matrixEntry{{RepositoryID:guardCaptureRepoID,BundleURL:"https://fixture.blob.core.windows.net/rehearsal.json.sn",Bundle:raw}}
	if _,err:=guardCaptureSelect(ctx,trusted,entries,p,false);err!=nil{t.Fatal("rehearsal_selector_positive_failed")}
	if _,err:=guardCaptureSelect(ctx,trusted,entries,p,true);err!=nil{t.Fatal("rehearsal_second_subject_selector_failed")}
	if _,err:=guardCaptureSelect(ctx,trusted,append(entries,entries[0]),p,false);err==nil{t.Fatal("rehearsal_duplicate_verified_entries_accepted")}
	if _,err:=guardCaptureSelect(ctx,trusted,nil,p,false);err==nil{t.Fatal("rehearsal_zero_verified_entries_accepted")}
	policy:=guardCapturePolicy(p,false)
	for _,m:=range guardCaptureMutations(policy){
		t.Run(m.Name,func(t *testing.T){
			changed:=policy;m.Mutate(&changed)
			_,err:=matrixVerify(ctx,trusted,raw,changed)
			matrixAssertRejected(t,err,m.Identity,m.Want)
		})
	}
	// This is an effective counter to treating the old fixture as a new
	// attestation: its non-empty, different producer identity must be refused.
	// Re-encoding is a positive control for every byte-level edit.
	normalized,err:=matrixEditBundle(raw,func(map[string]any)bool{return true})
	if err!=nil{t.Fatal("rehearsal_normalization")}
	if _,err=matrixVerify(ctx,trusted,normalized,policy);err!=nil{t.Fatal("rehearsal_normalized_bundle_rejected")}
	for _,control:=range matrixTamperControls(){
		t.Run("tamper_"+control.Name,func(t *testing.T){
			changed,err:=matrixEditBundle(raw,control.Edit)
			if err!=nil||bytes.Equal(changed,normalized){t.Fatal("ineffective_rehearsal_tamper")}
			_,err=matrixVerify(ctx,trusted,changed,policy)
			matrixAssertRejected(t,err,false,nil)
		})
	}
	other:=policy;other.Revision=guardCaptureNext.Source
	_,err=matrixVerify(ctx,trusted,raw,other)
	matrixAssertRejected(t,err,true,[]string{"expected"})
	ctxCancelled,stop:=context.WithCancel(context.Background());stop()
	if _,err=matrixVerify(ctxCancelled,trusted,raw,policy);err==nil{t.Fatal("cancelled_crypto_accepted")}
	t.Log("guard_capture_rehearsal signature_only=true new_capture_accepted=false payload_checked=false update_authorized=false")
}

func TestGuardCaptureEvidenceWriteIsExclusive(t *testing.T) {
	dir:=t.TempDir()
	if guardCaptureWrite(dir,"record.json",[]byte("first"))!=nil{t.Fatal("initial_evidence_write_failed")}
	if guardCaptureWrite(dir,"record.json",[]byte("second"))==nil{t.Fatal("evidence_collision_overwritten")}
	got,err:=os.ReadFile(filepath.Join(dir,"record.json"))
	if err!=nil||string(got)!="first"{t.Fatal("collision_changed_original_evidence")}
	if guardCaptureWrite(dir,"../outside.json",[]byte("{}"))==nil{t.Fatal("relative_escape_accepted")}
}

// Rekor v1 must carry both the signed entry timestamp and the inclusion
// proof/checkpoint. The library verifies these when present; requiring their
// presence here avoids silently accepting a promise-only bundle.
func guardCaptureBundleStructure(raw []byte) error {
	var value any
	if guardCaptureJSON(raw,&value)!=nil{return errGuardCapture}
	root,ok:=value.(map[string]any);if !ok{return errGuardCapture}
	vm,ok:=root["verificationMaterial"].(map[string]any);if !ok{return errGuardCapture}
	list,ok:=vm["tlogEntries"].([]any);if !ok||len(list)!=1{return errGuardCapture}
	entry,ok:=list[0].(map[string]any);if !ok{return errGuardCapture}
	promise,ok:=entry["inclusionPromise"].(map[string]any);if !ok{return errGuardCapture}
	set,ok:=promise["signedEntryTimestamp"].(string);if !ok||set==""{return errGuardCapture}
	proof,ok:=entry["inclusionProof"].(map[string]any);if !ok{return errGuardCapture}
	hash,ok:=proof["rootHash"].(string);if !ok||hash==""{return errGuardCapture}
	hashes,ok:=proof["hashes"].([]any);if !ok||len(hashes)>128{return errGuardCapture}
	for _,h:=range hashes{if text,ok:=h.(string);!ok||text==""{return errGuardCapture}}
	checkpoint,ok:=proof["checkpoint"].(map[string]any);if !ok{return errGuardCapture}
	envelope,ok:=checkpoint["envelope"].(string);if !ok||envelope==""{return errGuardCapture}
	return nil
}

func TestGuardCaptureLogProofComponentsRequired(t *testing.T) {
	raw:=[]byte(`{"verificationMaterial":{"tlogEntries":[{"inclusionPromise":{"signedEntryTimestamp":"fixture"},"inclusionProof":{"rootHash":"fixture","hashes":[],"checkpoint":{"envelope":"fixture"}}}]}}`)
	if guardCaptureBundleStructure(raw)!=nil{t.Fatal("structural_fixture_rejected")}
	for _,field:=range []string{"inclusionPromise","inclusionProof","checkpoint"}{
		var root map[string]any
		_ = json.Unmarshal(raw,&root)
		vm:=root["verificationMaterial"].(map[string]any)
		entry:=vm["tlogEntries"].([]any)[0].(map[string]any)
		if field=="checkpoint"{delete(entry["inclusionProof"].(map[string]any),field)}else{delete(entry,field)}
		body,_:=json.Marshal(root)
		if guardCaptureBundleStructure(body)==nil{t.Fatal("missing_log_component_accepted")}
	}
}

func TestGuardCaptureURLOnlyListingIsBoundedAndTokenless(t *testing.T) {
	for _,mode:=range []string{"embedded","url_only","bad_url","overflow","second_redirect","duplicate_keys"}{
		t.Run(mode,func(t *testing.T){
			objectCalls:=0
			objects:=guardCaptureRoundTrip(func(req *http.Request)(*http.Response,error){
				objectCalls++
				if req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""{t.Fatal("listing_token_reached_object")}
				body:=`{"mediaType":"fixture"}`
				status:=http.StatusOK
				if mode=="overflow"{body=strings.Repeat("x",int(guardCaptureMaxJSON)+1)}
				if mode=="second_redirect"{status=http.StatusFound}
				return &http.Response{StatusCode:status,Header:http.Header{},Body:io.NopCloser(strings.NewReader(body))},nil
			})
			api:=guardCaptureRoundTrip(func(req *http.Request)(*http.Response,error){
				if req.URL.Host!="api.github.com"||req.Header.Get("Authorization")!="Bearer fixture-only-token"{t.Fatal("listing_api_binding")}
				body:=`{"attestations":[{"repository_id":1406818713,"bundle_url":"https://fixture.blob.core.windows.net/a.json.sn","bundle":null}]}`
				if mode=="embedded"{body=strings.Replace(body,`"bundle":null`,`"bundle":{"mediaType":"fixture"}`,1)}
				if mode=="bad_url"{body=strings.Replace(body,"fixture.blob.core.windows.net","evil.invalid",1)}
				if mode=="duplicate_keys"{body=strings.Replace(body,`"attestations":`,`"attestations":[],"attestations":`,1)}
				return &http.Response{StatusCode:http.StatusOK,Header:http.Header{},Body:io.NopCloser(strings.NewReader(body))},nil
			})
			noRedirect:=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
			c:=guardCaptureHTTP{api:&http.Client{Transport:api,CheckRedirect:noRedirect},object:&http.Client{Transport:objects,CheckRedirect:noRedirect},token:"fixture-only-token"}
			entries,err:=c.listing(context.Background(),strings.Repeat("a",64))
			if mode=="embedded"||mode=="url_only"{
				if err!=nil||len(entries)!=1||string(entries[0].Bundle)!=`{"mediaType":"fixture"}`{t.Fatal("normal_listing_rejected")}
				want:=1;if mode=="embedded"{want=0};if objectCalls!=want{t.Fatal("listing_object_call_count")}
			}else if err==nil{t.Fatal("invalid_listing_object_accepted")}
		})
	}
}
