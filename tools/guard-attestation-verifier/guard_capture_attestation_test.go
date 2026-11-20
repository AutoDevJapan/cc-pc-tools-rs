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
	selected, _, err := guardCaptureSelectDetailed(ctx, trusted, entries, p, proof)
	return selected, err
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
	PESelection *guardCaptureSelectionDiagnostic `json:"pe_selection,omitempty"`
	ProofSelection *guardCaptureSelectionDiagnostic `json:"proof_selection,omitempty"`
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
	selected,peDiagnostic,err := guardCaptureSelectDetailed(ctx,trusted,peEntries,p,false)
	report.PESelection=&peDiagnostic
	guardCaptureLogSelection(t,"pe",peDiagnostic)
	if err != nil {
		if peDiagnostic.Stage == "bundle_digest" {
			if guardCapturePreserveVerifiedUnbound(dir,"pe",&peDiagnostic)!=nil { t.Log("guard_capture_unbound subject=pe preservation_failed") }
			t.Logf("guard_capture_unbound subject=pe preserved=%t accepted=false",peDiagnostic.VerifiedUnboundPreserved)
		}
		report.Reason="pe_signature_rejected";t.Fatal("guard_capture_stage=pe_signature")
	}
	proofEntries,err := client.listing(ctx,p.ProofSHA)
	if err != nil { report.Reason="proof_listing_unavailable";t.Fatal("guard_capture_stage=proof_listing") }
	selectedProof,proofDiagnostic,err := guardCaptureSelectDetailed(ctx,trusted,proofEntries,p,true)
	report.ProofSelection=&proofDiagnostic
	guardCaptureLogSelection(t,"proof",proofDiagnostic)
	if err != nil || !guardCaptureSameSeedRepresentation(selected.Raw,selectedProof.Raw,p) {
		if err != nil && proofDiagnostic.Stage == "bundle_digest" {
			if guardCapturePreserveVerifiedUnbound(dir,"proof",&proofDiagnostic)!=nil { t.Log("guard_capture_unbound subject=proof preservation_failed") }
			t.Logf("guard_capture_unbound subject=proof preserved=%t accepted=false",proofDiagnostic.VerifiedUnboundPreserved)
		}
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
	seedRepresentation,seedErr:=guardCaptureSeedRepresentation(selected.Raw,p)
	if seedErr!=nil {t.Fatal("guard_capture_stage=seed_binding")}
	for name,raw:=range map[string][]byte{"guard-capture-bundle.json":selected.Raw,
		"guard-capture-proof-bundle.json":selectedProof.Raw,"guard-capture-seed-representation.json":seedRepresentation} {
		if guardCaptureWrite(dir,name,raw)!=nil {t.Fatal("guard_capture_stage=preserve_bundle")}
	}
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
	for _,proofSubject:=range []bool{false,true}{
		_,detail,err:=guardCaptureSelectDetailed(ctx,trusted,entries,p,proofSubject)
		if err!=nil||detail.Stage!="bound"||detail.Entries!=1||detail.RepositoryEntries!=1||detail.VerifiedEntries!=1||len(detail.Rejections)!=0||detail.ObservedBundleBytes!=0||detail.ObservedBundleSHA256!=""||detail.VerifiedUnboundPreserved||len(detail.verifiedUnbound)!=0||detail.Binding==nil||*detail.Binding!=(guardCaptureBindingDiagnostic{true,true,true,true}){
			t.Fatal("rehearsal_selector_diagnostic_positive_failed")
		}
	}
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


// The following controls run the real metadata/source client methods against
// owned in-memory transports. They never issue a capture or trust fixture data.
type guardCaptureS1Fixture struct {
	pin guardCapturePin
	proof guardCaptureProof
	server guardCaptureServer
	sourceTree guardCaptureTree
	producerTree guardCaptureTree
	archive []byte
	relationStatus string
	relationBase string
	relationMergeBase string
	jobs []guardCaptureJob
	jobTotal int
	archiveURL string
}

func guardCaptureS1FixtureFor(t *testing.T) guardCaptureS1Fixture {
	t.Helper()
	p, raw, capture := guardCaptureTestProof()
	proof, err := guardCaptureProofCheck(raw, capture, p)
	if err != nil {
		t.Fatal("s1_proof_fixture")
	}
	tree, known := guardCaptureTestTree(proof.Before.Inputs)
	p.SourceTree = tree.SHA
	p.RepairTree = known["repair-guard"].SHA
	p.ContractTree = known["tools/civ6-ci-contract"].SHA
	var gitTree bytes.Buffer
	for _, input := range proof.Before.Inputs {
		_, _ = fmt.Fprintf(&gitTree, "100644 blob %s\t%s%c", input.Blob, input.Path, byte(0))
	}
	proof.Before.SourceTree = p.SourceTree
	proof.Before.SourceTreeBefore = p.SourceTree
	proof.Before.GitTreeSHA = guardCaptureHash(gitTree.Bytes())
	proof.After = proof.Before

	producerInputs := append([]guardCaptureInput(nil), proof.Before.Inputs...)
	for _, name := range []string{guardCaptureWorkflow, "tools/guard-gha-capture/main.go"} {
		body := []byte(name)
		input := guardCaptureInput{name, int64(len(body)), guardCaptureHash(body), guardCaptureBlob(body)}
		producerInputs = append(producerInputs, input)
		if name == guardCaptureWorkflow {
			p.WorkflowBlob = input.Blob
		} else {
			p.HelperBlob = input.Blob
		}
	}
	producerTree, _ := guardCaptureTestTree(producerInputs)
	proofRaw := guardCaptureS1JSON(t, proof)
	p.ProofBytes, p.ProofSHA = int64(len(proofRaw)), guardCaptureHash(proofRaw)
	capture = guardCaptureS1JSON(t, guardCaptureManifest{
		"guard-gha-capture-v1", guardCaptureRepoID, p.Source, p.SourceTree,
		p.Producer, p.RunID, p.Attempt, p.JobID, p.CheckRunID, p.ProofSHA,
		proof.Before.ObservationSHA, proof.Before.CommandSHA, proof.Before.ImageSHA,
		"guard.exe", p.PEBytes, p.PESHA,
	})
	p.CaptureBytes, p.CaptureSHA = int64(len(capture)), guardCaptureHash(capture)
	if _, err = guardCaptureProofCheck(proofRaw, capture, p); err != nil {
		t.Fatal("s1_rebound_fixture_proof_rejected")
	}
	server := guardCaptureTestServer(p)
	return guardCaptureS1Fixture{
		pin: p, proof: proof, server: server, sourceTree: tree, producerTree: producerTree,
		archive: guardCaptureTestTar(t, proof.Before.Inputs, false),
		relationStatus: "ahead", relationBase: p.Source, relationMergeBase: p.Source,
		jobs: []guardCaptureJob{server.Job}, jobTotal: 1,
		archiveURL: "https://codeload.github.com/" + guardCaptureRepo + "/legacy.tar.gz/" + p.Source,
	}
}

func guardCaptureS1JSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal("s1_fixture_encode")
	}
	return raw
}

func guardCaptureS1Client(t *testing.T, fixture guardCaptureS1Fixture) (guardCaptureHTTP, *[]string) {
	t.Helper()
	p := fixture.pin
	commit := func(id, tree string) map[string]any {
		return map[string]any{"sha": id, "tree": map[string]string{"sha": tree}}
	}
	apiBodies := map[string][]byte{
		fmt.Sprintf("%s/actions/runs/%d/attempts/%d", guardCaptureAPI, p.RunID, p.Attempt): guardCaptureS1JSON(t, fixture.server.Run),
		fmt.Sprintf("%s/actions/jobs/%d", guardCaptureAPI, p.JobID): guardCaptureS1JSON(t, fixture.server.Job),
		fmt.Sprintf("%s/actions/artifacts/%d", guardCaptureAPI, p.ArtifactID): guardCaptureS1JSON(t, fixture.server.Artifact),
		fmt.Sprintf("%s/actions/runs/%d/attempts/%d/jobs?per_page=100", guardCaptureAPI, p.RunID, p.Attempt):
			guardCaptureS1JSON(t, map[string]any{"total_count": fixture.jobTotal, "jobs": fixture.jobs}),
		guardCaptureAPI + "/git/commits/" + p.Source: guardCaptureS1JSON(t, commit(p.Source, p.SourceTree)),
		guardCaptureAPI + "/git/trees/" + p.SourceTree + "?recursive=1": guardCaptureS1JSON(t, fixture.sourceTree),
		guardCaptureAPI + "/git/commits/" + p.Producer: guardCaptureS1JSON(t, commit(p.Producer, fixture.producerTree.SHA)),
		guardCaptureAPI + "/git/trees/" + fixture.producerTree.SHA + "?recursive=1": guardCaptureS1JSON(t, fixture.producerTree),
		guardCaptureAPI + "/compare/" + p.Source + "..." + p.Producer: guardCaptureS1JSON(t, map[string]any{
			"status": fixture.relationStatus,
			"base_commit": map[string]string{"sha": fixture.relationBase},
			"merge_base_commit": map[string]string{"sha": fixture.relationMergeBase},
		}),
	}
	calls := []string{}
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	api := guardCaptureRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, "api:"+req.URL.String())
		if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer s1-fixture-token" ||
			req.URL.Host != "api.github.com" || req.Header.Get("Cookie") != "" {
			t.Fatal("s1_api_request_boundary")
		}
		if req.URL.String() == guardCaptureAPI+"/tarball/"+p.Source {
			return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {fixture.archiveURL}},
				Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		body, ok := apiBodies[req.URL.String()]
		if !ok {
			t.Fatal("s1_unexpected_api_request")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
			Body: io.NopCloser(bytes.NewReader(body))}, nil
	})
	object := guardCaptureRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, "object:"+req.URL.String())
		if req.Method != http.MethodGet || req.URL.String() != fixture.archiveURL ||
			req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" || req.Header.Get("Accept") != "" {
			t.Fatal("s1_object_request_boundary")
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{},
			Body: io.NopCloser(bytes.NewReader(fixture.archive))}, nil
	})
	return guardCaptureHTTP{
		api: &http.Client{Transport: api, CheckRedirect: noRedirect},
		object: &http.Client{Transport: object, CheckRedirect: noRedirect},
		token: "s1-fixture-token",
	}, &calls
}

func TestGuardCaptureS1HTTPIntegration(t *testing.T) {
	cases := []struct {
		name string
		stage string
		edit func(*guardCaptureS1Fixture)
	}{
		{"normal", "", func(*guardCaptureS1Fixture) {}},
		{"jobs_total_count", "server", func(f *guardCaptureS1Fixture) { f.jobTotal++ }},
		{"job_listing_not_single_job", "server", func(f *guardCaptureS1Fixture) { f.jobs[0].RunnerID++ }},
		{"job_missing_from_attempt", "server", func(f *guardCaptureS1Fixture) { f.jobs[0].ID++ }},
		{"job_duplicated_in_attempt", "server", func(f *guardCaptureS1Fixture) { f.jobs = append(f.jobs, f.jobs[0]); f.jobTotal = 2 }},
		{"compare_not_ahead", "source", func(f *guardCaptureS1Fixture) { f.relationStatus = "diverged" }},
		{"compare_wrong_base", "source", func(f *guardCaptureS1Fixture) { f.relationBase = f.pin.Producer }},
		{"compare_wrong_merge_base", "source", func(f *guardCaptureS1Fixture) { f.relationMergeBase = f.pin.Producer }},
		{"producer_workflow_blob", "source", func(f *guardCaptureS1Fixture) { f.pin.WorkflowBlob = strings.Repeat("0", 40) }},
		{"producer_helper_blob", "source", func(f *guardCaptureS1Fixture) { f.pin.HelperBlob = strings.Repeat("0", 40) }},
		{"git_tree_digest", "source", func(f *guardCaptureS1Fixture) { f.proof.Before.GitTreeSHA = strings.Repeat("0", 64) }},
		{"source_tree_entries", "source", func(f *guardCaptureS1Fixture) { f.sourceTree.Truncated = true }},
		{"source_archive_changed", "source", func(f *guardCaptureS1Fixture) { f.archive = guardCaptureTestTar(t, f.proof.Before.Inputs, true) }},
		{"source_redirect_other_repository", "source", func(f *guardCaptureS1Fixture) { f.archiveURL = "https://codeload.github.com/Other/Repo/archive" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fixture := guardCaptureS1FixtureFor(t)
			c.edit(&fixture)
			client, calls := guardCaptureS1Client(t, fixture)
			got, err := client.server(context.Background(), fixture.pin)
			if c.stage == "server" {
				if err == nil {
					t.Fatal("s1_invalid_server_chain_accepted")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, fixture.server) {
				t.Fatal("s1_valid_server_chain_rejected")
			}
			err = client.source(context.Background(), fixture.pin, fixture.proof)
			if c.stage == "source" {
				if err == nil {
					t.Fatal("s1_invalid_source_chain_accepted")
				}
				return
			}
			if err != nil {
				t.Fatal("s1_valid_source_chain_rejected")
			}
			want := []string{
				fmt.Sprintf("api:%s/actions/runs/%d/attempts/%d", guardCaptureAPI, fixture.pin.RunID, fixture.pin.Attempt),
				fmt.Sprintf("api:%s/actions/jobs/%d", guardCaptureAPI, fixture.pin.JobID),
				fmt.Sprintf("api:%s/actions/artifacts/%d", guardCaptureAPI, fixture.pin.ArtifactID),
				fmt.Sprintf("api:%s/actions/runs/%d/attempts/%d/jobs?per_page=100", guardCaptureAPI, fixture.pin.RunID, fixture.pin.Attempt),
				"api:" + guardCaptureAPI + "/git/commits/" + fixture.pin.Source,
				"api:" + guardCaptureAPI + "/git/trees/" + fixture.pin.SourceTree + "?recursive=1",
				"api:" + guardCaptureAPI + "/tarball/" + fixture.pin.Source,
				"object:" + fixture.archiveURL,
				"api:" + guardCaptureAPI + "/git/commits/" + fixture.pin.Producer,
				"api:" + guardCaptureAPI + "/git/trees/" + fixture.producerTree.SHA + "?recursive=1",
				"api:" + guardCaptureAPI + "/compare/" + fixture.pin.Source + "..." + fixture.pin.Producer,
			}
			if !reflect.DeepEqual(*calls, want) {
				t.Fatal("s1_full_chain_not_observed")
			}
		})
	}
}

func TestGuardCaptureS1GraphInventory(t *testing.T) {
	inputs := map[string]bool{
		"repair-guard/main.go": true,
		"repair-guard/assets/catalog.json": true,
		"repair-guard/object.syso": true,
		"tools/civ6-ci-contract/check.go": true,
		"tools/civ6-ci-contract/data.json": true,
		"tools/civ6-ci-contract/object.syso": true,
	}
	packages := []guardCaptureGraphPackage{
		{ImportPath: "cc/civ6-ci-contract", GoFiles: []string{"check.go"}, EmbedFiles: []string{"data.json"}, SysoFiles: []string{"object.syso"}},
		{ImportPath: guardCaptureModule, GoFiles: []string{"main.go"}, EmbedFiles: []string{"assets/catalog.json"}, SysoFiles: []string{"object.syso"}},
	}
	good := guardCaptureS1JSON(t, packages)
	if guardCaptureGraph(good, inputs) != nil {
		t.Fatal("s1_graph_inventory_positive_rejected")
	}
	for _, module := range []int{0, 1} {
		for _, field := range []string{"GoFiles", "EmbedFiles", "SysoFiles"} {
			for _, bad := range []string{"absent.go", "../outside.go", "dir\\file.go"} {
				name := fmt.Sprintf("module_%d/%s/%s", module, field, strings.ReplaceAll(bad, "/", "_"))
				t.Run(name, func(t *testing.T) {
					var changed []guardCaptureGraphPackage
					if json.Unmarshal(good, &changed) != nil {
						t.Fatal("s1_graph_fixture_decode")
					}
					switch field {
					case "GoFiles":
						changed[module].GoFiles = []string{bad}
					case "EmbedFiles":
						changed[module].EmbedFiles = []string{bad}
					case "SysoFiles":
						changed[module].SysoFiles = []string{bad}
					}
					if guardCaptureGraph(guardCaptureS1JSON(t, changed), inputs) == nil {
						t.Fatal("s1_graph_outside_or_malformed_input_accepted")
					}
				})
			}
		}
	}
}

func TestGuardCaptureS1SourceURLBoundary(t *testing.T) {
	for _, raw := range []string{
		"https://codeload.github.com/AutoDevJapan/cc-pc-tools-rs/legacy.tar.gz/fixed",
		"https://codeload.github.com:443/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed",
	} {
		if !guardCaptureObjectURL(raw, true) {
			t.Fatal("s1_fixed_source_url_rejected")
		}
	}
	for name, raw := range map[string]string{
		"other_host": "https://codeload.github.com.evil.invalid/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed",
		"other_repo": "https://codeload.github.com/AutoDevJapan/another-repo/tar.gz/fixed",
		"prefix_only": "https://codeload.github.com/AutoDevJapan/cc-pc-tools-rs-extra/tar.gz/fixed",
		"http": "http://codeload.github.com/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed",
		"port": "https://codeload.github.com:444/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed",
		"userinfo": "https://user@codeload.github.com/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed",
		"fragment": "https://codeload.github.com/AutoDevJapan/cc-pc-tools-rs/tar.gz/fixed#fragment",
	} {
		t.Run(name, func(t *testing.T) {
			if guardCaptureObjectURL(raw, true) {
				t.Fatal("s1_invalid_source_url_accepted")
			}
		})
	}
}

type guardCaptureS1ZIPMember struct {
	name string
	body []byte
	mode os.FileMode
}

func guardCaptureS1ZIP(t *testing.T, members []guardCaptureS1ZIPMember) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, member := range members {
		header := zip.FileHeader{Name: member.name, Method: zip.Store}
		header.SetMode(member.mode)
		file, err := writer.CreateHeader(&header)
		if err != nil {
			t.Fatal("s1_zip_header")
		}
		if _, err = file.Write(member.body); err != nil {
			t.Fatal("s1_zip_body")
		}
	}
	if writer.Close() != nil {
		t.Fatal("s1_zip_close")
	}
	return output.Bytes()
}

func TestGuardCaptureS1ZIPAbnormalMembers(t *testing.T) {
	p := guardCaptureTestPin()
	members := []guardCaptureS1ZIPMember{
		{"guard.exe", []byte("fixturePE"), 0600},
		{"source-proof.json", []byte("{}"), 0600},
		{"capture.json", []byte("{}"), 0600},
	}
	p.PEBytes, p.PESHA = int64(len(members[0].body)), guardCaptureHash(members[0].body)
	p.ProofBytes, p.ProofSHA = 2, guardCaptureHash(members[1].body)
	p.CaptureBytes, p.CaptureSHA = 2, guardCaptureHash(members[2].body)
	for _, name := range []string{"normal", "encrypted", "directory", "symlink", "declared_size", "extra_member"} {
		t.Run(name, func(t *testing.T) {
			changed := append([]guardCaptureS1ZIPMember(nil), members...)
			switch name {
			case "directory":
				changed[0].mode = os.ModeDir | 0700
			case "symlink":
				changed[0].mode = os.ModeSymlink | 0600
			case "extra_member":
				changed = append(changed, guardCaptureS1ZIPMember{"extra.json", []byte("{}"), 0600})
			}
			raw := guardCaptureS1ZIP(t, changed)
			edits := 0
			if name == "encrypted" || name == "declared_size" {
				for i := 0; i+46 <= len(raw); i++ {
					switch {
					case name == "encrypted" && bytes.Equal(raw[i:i+4], []byte{'P', 'K', 3, 4}):
						raw[i+6] |= 1
						edits++
					case bytes.Equal(raw[i:i+4], []byte{'P', 'K', 1, 2}):
						if name == "encrypted" {
							raw[i+8] |= 1
						} else {
							raw[i+24]++
						}
						edits++
					}
				}
				wantEdits := 3
				if name == "encrypted" {
					wantEdits = 6
				}
				if edits != wantEdits {
					t.Fatal("s1_zip_fault_not_applied_to_exact_inventory")
				}
			}
			current := p
			current.ArchiveBytes, current.ArchiveSHA = int64(len(raw)), guardCaptureHash(raw)
			got, err := guardCaptureReadZIP(raw, current)
			if name == "normal" {
				if err != nil || !bytes.Equal(got.PE, members[0].body) {
					t.Fatal("s1_zip_positive_rejected")
				}
			} else if err == nil {
				t.Fatal("s1_abnormal_zip_accepted")
			}
		})
	}
}

func guardCaptureS1Tar(t *testing.T, inputs []guardCaptureInput, fault string) []byte {
	t.Helper()
	var plain bytes.Buffer
	writer := tar.NewWriter(&plain)
	for i, input := range inputs {
		body := []byte(input.Path)
		header := tar.Header{Name: "fixture-root/" + input.Path, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(body))}
		if i == 0 {
			switch fault {
			case "symlink":
				header.Typeflag, header.Size, header.Linkname, body = tar.TypeSymlink, 0, "elsewhere", nil
			case "hardlink":
				header.Typeflag, header.Size, header.Linkname, body = tar.TypeLink, 0, "elsewhere", nil
			case "header_size":
				header.Size++
				body = append(body, 'x')
			}
		}
		if i == 1 && fault == "prefix" {
			header.Name = "different-root/" + input.Path
		}
		if writer.WriteHeader(&header) != nil {
			t.Fatal("s1_tar_header")
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal("s1_tar_body")
		}
	}
	if writer.Close() != nil {
		t.Fatal("s1_tar_close")
	}
	expanded := plain.Bytes()
	if fault == "truncated_body" {
		// A valid tar header advertises the original file size; the stream ends
		// before that first body is complete. gzip remains well formed.
		expanded = expanded[:512+len(inputs[0].Path)-1]
	}
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(expanded); err != nil {
		t.Fatal("s1_gzip_body")
	}
	if gz.Close() != nil {
		t.Fatal("s1_gzip_close")
	}
	return compressed.Bytes()
}

func TestGuardCaptureS1TarAbnormalMembers(t *testing.T) {
	p, raw, capture := guardCaptureTestProof()
	proof, err := guardCaptureProofCheck(raw, capture, p)
	if err != nil {
		t.Fatal("s1_tar_proof_fixture")
	}
	for _, fault := range []string{"normal", "symlink", "hardlink", "prefix", "header_size", "truncated_body"} {
		t.Run(fault, func(t *testing.T) {
			err := guardCaptureSourceArchive(guardCaptureS1Tar(t, proof.Before.Inputs, fault), proof.Before.Inputs)
			if fault == "normal" {
				if err != nil {
					t.Fatal("s1_tar_positive_rejected")
				}
			} else if err == nil {
				t.Fatal("s1_abnormal_tar_accepted")
			}
		})
	}
}

func TestGuardCaptureS1ServerMetadataFields(t *testing.T) {
	p := guardCaptureTestPin()
	base := guardCaptureTestServer(p)
	clone := func() guardCaptureServer {
		raw := guardCaptureS1JSON(t, base)
		var out guardCaptureServer
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal("s1_server_fixture_decode")
		}
		return out
	}
	if guardCaptureServerCheck(base, p) != nil {
		t.Fatal("s1_server_metadata_positive_rejected")
	}
	edits := map[string]func(*guardCaptureServer){
		"run_attempt": func(s *guardCaptureServer) { s.Run.Attempt++ },
		"run_branch": func(s *guardCaptureServer) { s.Run.Branch = "other" },
		"run_workflow_path": func(s *guardCaptureServer) { s.Run.Path = ".github/workflows/other.yml" },
		"run_status": func(s *guardCaptureServer) { s.Run.Status = "in_progress" },
		"run_conclusion": func(s *guardCaptureServer) { s.Run.Conclusion = "failure" },
		"run_repository_name": func(s *guardCaptureServer) { s.Run.Repository.FullName = "Other/Repository" },
		"run_private": func(s *guardCaptureServer) { s.Run.Repository.Private = true },
		"run_head_repository": func(s *guardCaptureServer) { s.Run.HeadRepository.ID++ },
		"job_run": func(s *guardCaptureServer) { s.Job.RunID++ },
		"job_head": func(s *guardCaptureServer) { s.Job.HeadSHA = p.Source },
		"job_name": func(s *guardCaptureServer) { s.Job.Name = "other job" },
		"job_status": func(s *guardCaptureServer) { s.Job.Status = "queued" },
		"job_conclusion": func(s *guardCaptureServer) { s.Job.Conclusion = "cancelled" },
		"job_started": func(s *guardCaptureServer) { s.Job.Started = "2026-01-01T00:00:01Z" },
		"job_completed": func(s *guardCaptureServer) { s.Job.Completed = "2026-01-01T00:09:59Z" },
		"job_runner": func(s *guardCaptureServer) { s.Job.RunnerID = 0 },
		"artifact_name": func(s *guardCaptureServer) { s.Artifact.Name += "-other" },
		"artifact_size": func(s *guardCaptureServer) { s.Artifact.Bytes++ },
		"artifact_created": func(s *guardCaptureServer) { s.Artifact.Created = "2026-01-01T00:08:59Z" },
		"artifact_expires": func(s *guardCaptureServer) { s.Artifact.Expires = "2026-01-02T00:08:59Z" },
		"artifact_repository": func(s *guardCaptureServer) { s.Artifact.Run.RepositoryID++ },
		"artifact_head_repository": func(s *guardCaptureServer) { s.Artifact.Run.HeadRepositoryID++ },
		"artifact_branch": func(s *guardCaptureServer) { s.Artifact.Run.Branch = "other" },
		"step_missing": func(s *guardCaptureServer) { s.Job.Steps = s.Job.Steps[:4] },
		"step_in_progress": func(s *guardCaptureServer) { s.Job.Steps[1].Status = "in_progress" },
		"step_number_swapped": func(s *guardCaptureServer) {
			s.Job.Steps[0].Number, s.Job.Steps[1].Number = s.Job.Steps[1].Number, s.Job.Steps[0].Number
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			current := clone()
			edit(&current)
			if guardCaptureServerCheck(current, p) == nil {
				t.Fatal("s1_single_metadata_mismatch_accepted")
			}
		})
	}
}


// Fixed diagnostic categories only. Library error text, URLs and input bodies
// are never returned or printed by the guard capture failure report.
type guardCaptureSelectionDiagnostic struct {
	Stage string `json:"stage"`
	Entries int `json:"entries"`
	RepositoryEntries int `json:"repository_entries"`
	VerifiedEntries int `json:"verified_entries"`
	Rejections map[string]int `json:"rejections"`
	ObservedBundleBytes int `json:"observed_bundle_bytes,omitempty"`
	ObservedBundleSHA256 string `json:"observed_bundle_sha256,omitempty"`
	VerifiedUnboundPreserved bool `json:"verified_unbound_preserved,omitempty"`
	Binding *guardCaptureBindingDiagnostic `json:"binding,omitempty"`
	verifiedUnbound []byte
}

func guardCaptureVerificationCategory(err error) string {
	if err == nil {
		return "verified"
	}
	if matrixIdentityRejected(err) {
		return "identity"
	}
	if errors.Is(err, errMatrixInput) {
		return "verification_input"
	}
	// These are fixed prefixes from the locked sigstore-go verifier. Unknown
	// errors keep their own category; no free-form suffix is ever retained.
	message := err.Error()
	for _, c := range []struct{ prefix, category string }{
		{"failed to verify log inclusion:", "transparency_log"},
		{"failed to verify timestamps:", "observer_timestamp"},
		{"failed to verify leaf certificate:", "certificate_chain"},
		{"failed to verify signed certificate timestamp:", "certificate_sct"},
		{"failed to verify certificate identity:", "identity"},
		{"failed to fetch envelope statement:", "statement_decode"},
		{"failed to fetch signature content:", "signature_content"},
		{"failed to get signature verifier:", "signature_verifier"},
		{"failed to fetch entity version:", "bundle_version"},
	} {
		if strings.Contains(message, c.prefix) {
			return c.category
		}
	}
	if strings.Contains(message, "failed to verify signature:") {
		if strings.Contains(message, "does not match any digest in statement") {
			return "subject_digest"
		}
		return "signature"
	}
	return "verification_other"
}


// Only the direct verificationMaterial member order may be projected back to
// the retained seed. Values stay as exact RawMessage spans; this is not JSON
// semantic equivalence or a general canonicalization algorithm.
type guardCaptureRawMember struct {
	name string
	value []byte
	start, end int
}

func guardCaptureRawObject(raw []byte) ([]guardCaptureRawMember, error) {
	if len(raw) == 0 || int64(len(raw)) > guardCaptureMaxJSON { return nil, errGuardCapture }
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil || !bytes.Equal(compact.Bytes(), raw) { return nil, errGuardCapture }
	check := json.NewDecoder(bytes.NewReader(raw))
	check.UseNumber()
	if guardCaptureJSONValue(check, 0) != nil || check.Decode(new(any)) != io.EOF { return nil, errGuardCapture }
	dec := json.NewDecoder(bytes.NewReader(raw))
	first, err := dec.Token()
	if err != nil || first != json.Delim('{') { return nil, errGuardCapture }
	var members []guardCaptureRawMember
	for dec.More() {
		keyStart := int(dec.InputOffset())
		if keyStart < len(raw) && raw[keyStart] == ',' { keyStart++ }
		keyToken, keyErr := dec.Token()
		key, ok := keyToken.(string)
		keyEnd := int(dec.InputOffset())
		if keyErr != nil || !ok || keyStart >= keyEnd || keyEnd >= len(raw) || raw[keyEnd] != ':' ||
			!bytes.Equal(raw[keyStart:keyEnd], []byte("\""+key+"\"")) { return nil, errGuardCapture }
		var value json.RawMessage
		if dec.Decode(&value) != nil { return nil, errGuardCapture }
		end := int(dec.InputOffset())
		start := keyEnd+1
		if start >= end || end > len(raw) || !bytes.Equal(raw[start:end], value) { return nil, errGuardCapture }
		members = append(members, guardCaptureRawMember{key, raw[start:end], start, end})
	}
	last, err := dec.Token()
	if err != nil || last != json.Delim('}') || dec.Decode(new(any)) != io.EOF { return nil, errGuardCapture }
	return members, nil
}

func guardCaptureSeedRepresentation(raw []byte, p guardCapturePin) ([]byte, error) {
	if len(raw) == 0 || int64(len(raw)) > guardCaptureMaxJSON || int64(len(raw)) != p.BundleBytes { return nil, errGuardCapture }
	if guardCaptureHash(raw) == p.BundleSHA { return raw, nil }
	root, err := guardCaptureRawObject(raw)
	if err != nil || len(root) != 3 || root[0].name != "mediaType" ||
		root[1].name != "verificationMaterial" || root[2].name != "dsseEnvelope" { return nil, errGuardCapture }
	vm, err := guardCaptureRawObject(root[1].value)
	if err != nil || len(vm) != 3 || vm[0].name != "tlogEntries" ||
		vm[1].name != "timestampVerificationData" || vm[2].name != "certificate" { return nil, errGuardCapture }
	want := []string{"certificate", "tlogEntries", "timestampVerificationData"}
	values := make(map[string][]byte, len(vm))
	for _, m := range vm { values[m.name] = m.value }
	if len(values["certificate"]) == 0 || values["certificate"][0] != '{' ||
		len(values["tlogEntries"]) == 0 || values["tlogEntries"][0] != '[' ||
		len(values["timestampVerificationData"]) == 0 || values["timestampVerificationData"][0] != '{' { return nil, errGuardCapture }
	var ordered bytes.Buffer
	ordered.WriteByte('{')
	for i, key := range want {
		value, ok := values[key]
		if !ok { return nil, errGuardCapture }
		if i != 0 { ordered.WriteByte(',') }
		ordered.WriteByte('"'); ordered.WriteString(key); ordered.WriteString("\":")
		ordered.Write(value)
	}
	ordered.WriteByte('}')
	if ordered.Len() != len(root[1].value) { return nil, errGuardCapture }
	restored := make([]byte, 0, len(raw))
	restored = append(restored, raw[:root[1].start]...)
	restored = append(restored, ordered.Bytes()...)
	restored = append(restored, raw[root[1].end:]...)
	if int64(len(restored)) != p.BundleBytes || guardCaptureHash(restored) != p.BundleSHA { return nil, errGuardCapture }
	return restored, nil
}

type guardCaptureBindingDiagnostic struct {
	BundleSizeMatch bool `json:"bundle_size_match"`
	SeedRepresentationMatch bool `json:"seed_representation_match"`
	RekorIndexMatch bool `json:"rekor_index_match"`
	AttestationRecordMatch bool `json:"attestation_record_match"`
}

func guardCaptureBindingFacts(selected matrixOutcome, rawURL string, p guardCapturePin) guardCaptureBindingDiagnostic {
	_, err := guardCaptureSeedRepresentation(selected.Raw, p)
	return guardCaptureBindingDiagnostic{
		BundleSizeMatch: int64(len(selected.Raw)) == p.BundleBytes,
		SeedRepresentationMatch: err == nil,
		RekorIndexMatch: selected.LogIndex == p.RekorIndex,
		AttestationRecordMatch: guardCaptureBundleID(rawURL, p.AttestationID),
	}
}

func guardCaptureBindingStage(f guardCaptureBindingDiagnostic) string {
	if !f.BundleSizeMatch { return "bundle_size" }
	if !f.SeedRepresentationMatch { return "bundle_digest" }
	if !f.RekorIndexMatch { return "rekor_index" }
	if !f.AttestationRecordMatch { return "attestation_record" }
	return "bound"
}

func guardCaptureSelectedBinding(selected matrixOutcome, rawURL string, p guardCapturePin) string {
	return guardCaptureBindingStage(guardCaptureBindingFacts(selected, rawURL, p))
}

func guardCaptureSameSeedRepresentation(left, right []byte, p guardCapturePin) bool {
	leftSeed, leftErr := guardCaptureSeedRepresentation(left, p)
	rightSeed, rightErr := guardCaptureSeedRepresentation(right, p)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftSeed, rightSeed)
}

func guardCaptureSelectDetailed(ctx context.Context, trusted trustedMaterial, entries []matrixEntry, p guardCapturePin, proof bool) (matrixOutcome, guardCaptureSelectionDiagnostic, error) {
	diagnostic := guardCaptureSelectionDiagnostic{Stage: "selection_not_completed", Entries: len(entries), Rejections: map[string]int{}}
	policy := guardCapturePolicy(p, proof)
	var outcomes []matrixOutcome
	for i, e := range entries {
		o := matrixOutcome{Index: i, RepoMatch: e.RepositoryID == guardCaptureRepoID, Err: errGuardCapture}
		var compact bytes.Buffer
		if json.Compact(&compact, e.Bundle) != nil {
			diagnostic.Stage = "bundle_encoding"
			return matrixOutcome{}, diagnostic, errGuardCapture
		}
		o.Raw = append([]byte(nil), compact.Bytes()...)
		category := "other_repository"
		if o.RepoMatch {
			diagnostic.RepositoryEntries++
			category = "bundle_structure"
			if guardCaptureBundleStructure(o.Raw) == nil {
				o.Verified, o.Err = matrixVerify(ctx, trusted, o.Raw, policy)
				category = guardCaptureVerificationCategory(o.Err)
				if o.Err == nil {
					o.Err = guardCaptureStatement(o.Verified, p)
					if o.Err != nil {
						category = "verified_statement_or_time"
					}
				}
			}
		}
		if o.RepoMatch && o.Err == nil {
			diagnostic.VerifiedEntries++
		} else {
			diagnostic.Rejections[category]++
		}
		o.LogIndex, o.SAN = matrixBundleFacts(o.Raw)
		o.URLTail = matrixURLTail(e.BundleURL)
		outcomes = append(outcomes, o)
	}
	selected, err := matrixSelectExactlyOne(outcomes)
	if err != nil {
		diagnostic.Stage = "selection_zero"
		if diagnostic.VerifiedEntries > 1 {
			diagnostic.Stage = "selection_multiple"
		}
		return matrixOutcome{}, diagnostic, errGuardCapture
	}
	binding := guardCaptureBindingFacts(selected, entries[selected.Index].BundleURL, p)
	diagnostic.Binding = &binding
	diagnostic.Stage = guardCaptureBindingStage(binding)
	if diagnostic.Stage == "bundle_digest" {
		diagnostic.ObservedBundleBytes = len(selected.Raw)
		diagnostic.ObservedBundleSHA256 = guardCaptureHash(selected.Raw)
		diagnostic.verifiedUnbound = selected.Raw
	}
	if diagnostic.Stage != "bound" {
		return matrixOutcome{}, diagnostic, errGuardCapture
	}
	return selected, diagnostic, nil
}

func guardCaptureLogSelection(t *testing.T, subject string, d guardCaptureSelectionDiagnostic) {
	t.Helper()
	// The subject labels are fixed at the two call sites below.
	t.Logf("guard_capture_selection subject=%s stage=%s entries=%d repository_entries=%d verified_entries=%d",
		subject, d.Stage, d.Entries, d.RepositoryEntries, d.VerifiedEntries)
	if d.Binding != nil {
		t.Logf("guard_capture_binding subject=%s bundle_size_match=%t seed_representation_match=%t rekor_index_match=%t attestation_record_match=%t",
			subject,d.Binding.BundleSizeMatch,d.Binding.SeedRepresentationMatch,d.Binding.RekorIndexMatch,d.Binding.AttestationRecordMatch)
	}
	for _, category := range []string{"other_repository", "bundle_structure", "verification_input", "transparency_log",
		"observer_timestamp", "certificate_chain", "certificate_sct", "identity", "statement_decode", "signature_content",
		"signature_verifier", "bundle_version", "subject_digest", "signature", "verification_other", "verified_statement_or_time"} {
		if count := d.Rejections[category]; count != 0 {
			t.Logf("guard_capture_rejection subject=%s category=%s count=%d", subject, category, count)
		}
	}
}

func TestGuardCaptureSelectionDiagnosticCategories(t *testing.T) {
	for name, c := range map[string]struct{ err error; want string }{
		"nil": {nil, "verified"},
		"invalid_input": {errMatrixInput, "verification_input"},
		"log": {errors.New("failed to verify log inclusion: private detail"), "transparency_log"},
		"timestamp": {errors.New("failed to verify timestamps: private detail"), "observer_timestamp"},
		"chain": {errors.New("failed to verify leaf certificate: private detail"), "certificate_chain"},
		"sct": {errors.New("failed to verify signed certificate timestamp: private detail"), "certificate_sct"},
		"identity": {errors.New("failed to verify certificate identity: private detail"), "identity"},
		"statement": {errors.New("failed to fetch envelope statement: private detail"), "statement_decode"},
		"signature_content": {errors.New("failed to fetch signature content: private detail"), "signature_content"},
		"signature_verifier": {errors.New("failed to get signature verifier: private detail"), "signature_verifier"},
		"version": {errors.New("failed to fetch entity version: private detail"), "bundle_version"},
		"digest": {errors.New("failed to verify signature: digest does not match any digest in statement"), "subject_digest"},
		"signature": {errors.New("failed to verify signature: private detail"), "signature"},
		"unknown": {errors.New("arbitrary private detail"), "verification_other"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := guardCaptureVerificationCategory(c.err); got != c.want {
				t.Fatal("selection_diagnostic_category_mismatch")
			}
		})
	}
}

func TestGuardCaptureSelectionBindingDiagnostics(t *testing.T) {
	p := guardCaptureTestPin()
	raw := []byte("binding fixture only")
	p.BundleBytes, p.BundleSHA = int64(len(raw)), guardCaptureHash(raw)
	selected := matrixOutcome{Raw: raw, LogIndex: p.RekorIndex}
	u := "https://fixture.blob.core.windows.net/" + p.AttestationID + ".json.sn"
	if guardCaptureSelectedBinding(selected, u, p) != "bound" {
		t.Fatal("selection_binding_positive_rejected")
	}
	for name, edit := range map[string]func(*guardCapturePin){
		"bundle_size": func(p *guardCapturePin) { p.BundleBytes++ },
		"bundle_digest": func(p *guardCapturePin) { p.BundleSHA = strings.Repeat("0", 64) },
		"rekor_index": func(p *guardCapturePin) { p.RekorIndex += "0" },
		"attestation_record": func(p *guardCapturePin) { p.AttestationID += "0" },
	} {
		t.Run(name, func(t *testing.T) {
			current := p
			edit(&current)
			if guardCaptureSelectedBinding(selected, u, current) != name {
				t.Fatal("selection_binding_failure_not_distinguished")
			}
		})
	}
}

func TestGuardCaptureSelectionFailureDiagnosticDoesNotAccept(t *testing.T) {
	p := guardCaptureTestPin()
	for name, c := range map[string]struct {
		entries []matrixEntry
		stage string
		repositoryEntries int
		rejections map[string]int
	}{
		"empty": {nil, "selection_zero", 0, map[string]int{}},
		"other_repository": {[]matrixEntry{{RepositoryID: 1, Bundle: []byte("{}")}},
			"selection_zero", 0, map[string]int{"other_repository": 1}},
		"structure": {[]matrixEntry{{RepositoryID: guardCaptureRepoID, Bundle: []byte("{}")}},
			"selection_zero", 1, map[string]int{"bundle_structure": 1}},
		"encoding": {[]matrixEntry{{RepositoryID: guardCaptureRepoID, Bundle: []byte("not json")}},
			"bundle_encoding", 0, map[string]int{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, detail, err := guardCaptureSelectDetailed(context.Background(), nil, c.entries, p, false)
			if err == nil || detail.VerifiedEntries != 0 || detail.Stage == "bound" {
				t.Fatal("selection_failure_diagnostic_became_acceptance")
			}
			if detail.Stage != c.stage || detail.Entries != len(c.entries) ||
				detail.RepositoryEntries != c.repositoryEntries || !reflect.DeepEqual(detail.Rejections, c.rejections) ||
				detail.ObservedBundleBytes != 0 || detail.ObservedBundleSHA256 != "" ||
				detail.VerifiedUnboundPreserved || len(detail.verifiedUnbound) != 0 || detail.Binding != nil {
				t.Fatal("selection_failure_diagnostic_incomplete_or_incorrect")
			}
			report := guardCaptureReport{Reason: "pe_signature_rejected", PESelection: &detail}
			encoded, encodeErr := json.Marshal(report)
			if encodeErr != nil || report.Passed || report.SignatureChecked || report.UpdateAuthorized ||
				bytes.Contains(encoded, []byte("arbitrary private detail")) ||
				bytes.Contains(encoded, []byte("not json")) || bytes.Contains(encoded, []byte("blob.core.windows.net")) {
				t.Fatal("selection_diagnostic_exposes_input_or_accepts")
			}
		})
	}
}

// Retain only a single cryptographically verified, statement-checked selection
// that failed the pinned bundle digest. This is diagnostic evidence, never an
// accepted bundle, an alternate hash or an update capability.
func guardCapturePreserveVerifiedUnbound(dir, subject string, d *guardCaptureSelectionDiagnostic) error {
	if d == nil || d.Stage != "bundle_digest" || d.VerifiedEntries != 1 ||
		len(d.verifiedUnbound) == 0 || int64(len(d.verifiedUnbound)) > guardCaptureMaxJSON ||
		d.ObservedBundleBytes != len(d.verifiedUnbound) ||
		d.ObservedBundleSHA256 != guardCaptureHash(d.verifiedUnbound) {
		return errGuardCapture
	}
	name := ""
	switch subject {
	case "pe":
		name = "guard-capture-pe-verified-unbound-bundle.json"
	case "proof":
		name = "guard-capture-proof-verified-unbound-bundle.json"
	default:
		return errGuardCapture
	}
	defer func() {
		clear(d.verifiedUnbound)
		d.verifiedUnbound = nil
	}()
	if err := guardCaptureWrite(dir, name, d.verifiedUnbound); err != nil {
		return err
	}
	d.VerifiedUnboundPreserved = true
	return nil
}

func TestGuardCaptureVerifiedUnboundRetention(t *testing.T) {
	for _, name := range []string{"pe", "proof", "unverified", "wrong_stage", "wrong_bytes", "wrong_digest", "unknown_subject", "collision", "oversize"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			raw := []byte("synthetic verified-unbound retention fixture")
			if name == "oversize" {
				raw = bytes.Repeat([]byte{'x'}, int(guardCaptureMaxJSON)+1)
			}
			d := guardCaptureSelectionDiagnostic{
				Stage: "bundle_digest", VerifiedEntries: 1,
				ObservedBundleBytes: len(raw), ObservedBundleSHA256: guardCaptureHash(raw),
				verifiedUnbound: append([]byte(nil), raw...),
			}
			subject := "pe"
			if name == "proof" {
				subject = "proof"
			}
			fileName := "guard-capture-" + subject + "-verified-unbound-bundle.json"
			switch name {
			case "unverified":
				d.VerifiedEntries = 0
			case "wrong_stage":
				d.Stage = "selection_zero"
			case "wrong_bytes":
				d.ObservedBundleBytes++
			case "wrong_digest":
				d.ObservedBundleSHA256 = strings.Repeat("0", 64)
			case "unknown_subject":
				subject = "../untrusted"
			case "collision":
				if guardCaptureWrite(dir, fileName, []byte("previous evidence")) != nil {
					t.Fatal("unbound_collision_fixture_failed")
				}
			}
			err := guardCapturePreserveVerifiedUnbound(dir, subject, &d)
			filePath := filepath.Join(dir, fileName)
			if name == "pe" || name == "proof" {
				got, readErr := os.ReadFile(filePath)
				if err != nil || readErr != nil || !bytes.Equal(got, raw) ||
					!d.VerifiedUnboundPreserved || d.verifiedUnbound != nil {
					t.Fatal("verified_unbound_same_buffer_not_preserved")
				}
				report := guardCaptureReport{Reason: "pe_signature_rejected", PESelection: &d}
				encoded, encodeErr := json.Marshal(report)
				if encodeErr != nil || report.Passed || report.SignatureChecked || report.UpdateAuthorized ||
					bytes.Contains(encoded, raw) || !bytes.Contains(encoded, []byte(d.ObservedBundleSHA256)) {
					t.Fatal("unbound_retention_became_acceptance_or_exposed_body")
				}
				return
			}
			if err == nil || d.VerifiedUnboundPreserved {
				t.Fatal("ineligible_unbound_evidence_was_preserved")
			}
			if name == "collision" {
				got, readErr := os.ReadFile(filePath)
				if readErr != nil || string(got) != "previous evidence" {
					t.Fatal("unbound_collision_changed_prior_evidence")
				}
			} else if _, statErr := os.Stat(filePath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatal("ineligible_unbound_evidence_created_file")
			}
		})
	}
	if guardCapturePreserveVerifiedUnbound(t.TempDir(), "pe", nil) == nil {
		t.Fatal("nil_unbound_diagnostic_accepted")
	}
}

// Parser/binding data only; this fixture has no signature or verification authority.
func guardCaptureRepresentationFixture() ([]byte, []byte) {
	certificate := `{"rawBytes":"Q0VSVA=="}`
	tlog := `[{"logIndex":"4023990479","integratedTime":1.0,"canonicalizedBody":"Ym9keQ=="}]`
	timestamp := `{"rfc3161Timestamps":[]}`
	envelope := `{"payloadType":"application/vnd.in-toto+json","payload":"YWJ\u006a","signatures":[{"sig":"c2ln","keyid":""}]}`
	seed := []byte(`{"mediaType":"fixture/v0","verificationMaterial":{"certificate":`+certificate+
		`,"tlogEntries":`+tlog+`,"timestampVerificationData":`+timestamp+`},"dsseEnvelope":`+envelope+`}`)
	live := []byte(`{"mediaType":"fixture/v0","verificationMaterial":{"tlogEntries":`+tlog+
		`,"timestampVerificationData":`+timestamp+`,"certificate":`+certificate+`},"dsseEnvelope":`+envelope+`}`)
	return seed, live
}

func TestGuardCaptureSeedRepresentationPreservesRawMaterial(t *testing.T) {
	seed, live := guardCaptureRepresentationFixture()
	p := guardCaptureTestPin()
	p.BundleBytes, p.BundleSHA = int64(len(seed)), guardCaptureHash(seed)
	for name, raw := range map[string][]byte{"seed_exact":seed,"api_vm_order":live} {
		t.Run(name, func(t *testing.T) {
			before := append([]byte(nil), raw...)
			restored, err := guardCaptureSeedRepresentation(raw, p)
			if err != nil || !bytes.Equal(restored, seed) || !bytes.Equal(raw, before) { t.Fatal("seed_projection_positive_failed") }
			if !guardCaptureSameSeedRepresentation(seed, raw, p) { t.Fatal("same_seed_projection_rejected") }
		})
	}
	for name, edit := range map[string]func(string)string{
		"payload":func(s string)string{return strings.Replace(s,`YWJ\u006a`,`YWJ\u006b`,1)},
		"signature":func(s string)string{return strings.Replace(s,"c2ln","c2lo",1)},
		"certificate":func(s string)string{return strings.Replace(s,"Q0VSVA==","Q0VSVB==",1)},
		"tlog_body":func(s string)string{return strings.Replace(s,"Ym9keQ==","Ym9keR==",1)},
		"tlog_number":func(s string)string{return strings.Replace(s,"1.0","2.0",1)},
		"number_spelling":func(s string)string{return strings.Replace(s,"1.0","1e0",1)},
		"string_spelling":func(s string)string{return strings.Replace(s,`\u006a`,`\u006A`,1)},
		"nested_order":func(s string)string{return strings.Replace(s,`"sig":"c2ln","keyid":""`,`"keyid":"","sig":"c2ln"`,1)},
		"root_order":func(s string)string{return "{"+strings.TrimSuffix(strings.TrimPrefix(s,`{"mediaType":"fixture/v0",`),"}")+`,"mediaType":"fixture/v0"}`},
		"unknown_vm":func(s string)string{return strings.Replace(s,`"certificate":`,`"extra":0,"certificate":`,1)},
		"unknown_same_length":func(s string)string{return strings.Replace(s,`"certificate":`,`"certificatE":`,1)},
		"missing_vm":func(s string)string{return strings.Replace(s,`,"certificate":{"rawBytes":"Q0VSVA=="}`,"",1)},
		"wrong_vm_type":func(s string)string{return strings.Replace(s,`{"rawBytes":"Q0VSVA=="}`,`["rawBytes","Q0VSVA=="]`,1)},
		"other_direct_order":func(s string)string{return strings.Replace(s,`"timestampVerificationData":{"rfc3161Timestamps":[]},"certificate":{"rawBytes":"Q0VSVA=="}`,`"certificate":{"rawBytes":"Q0VSVA=="},"timestampVerificationData":{"rfc3161Timestamps":[]}`,1)},
		"duplicate_vm":func(s string)string{return strings.Replace(s,`"certificate":`,`"certificate":{},"certificate":`,1)},
		"casefold_vm":func(s string)string{return strings.Replace(s,`"certificate":`,`"Certificate":{},"certificate":`,1)},
		"escaped_key":func(s string)string{return strings.Replace(s,`"certificate":`,`"\u0063ertificate":`,1)},
		"tail":func(s string)string{return s+"{}"},
		"whitespace_not_compact":func(s string)string{return " "+s},
		"invalid":func(s string)string{return s[:len(s)-1]},
	} {
		t.Run(name, func(t *testing.T) {
			changed := []byte(edit(string(live)))
			if bytes.Equal(changed, live) { t.Fatal("ineffective_representation_negative") }
			if _, err := guardCaptureSeedRepresentation(changed, p); err == nil { t.Fatal("changed_raw_material_accepted") }
			if guardCaptureSameSeedRepresentation(live,changed,p) { t.Fatal("different_seed_material_accepted") }
		})
	}

	certificate:=`{"rawBytes":"Q0VSVA=="}`
	tlog:=`[{"logIndex":"4023990479","integratedTime":1.0,"canonicalizedBody":"Ym9keQ=="}]`
	timestamp:=`{"rfc3161Timestamps":[]}`
	members:=[]string{`"certificate":`+certificate,`"tlogEntries":`+tlog,`"timestampVerificationData":`+timestamp}
	liveRoot,readErr:=guardCaptureRawObject(live)
	if readErr!=nil||len(liveRoot)!=3{t.Fatal("permutation_fixture_invalid")}
	for i,order:=range [][]int{{0,2,1},{1,0,2},{2,0,1},{2,1,0}} {
		t.Run(fmt.Sprintf("unsupported_order_%d",i),func(t *testing.T){
			vm:=[]byte("{"+members[order[0]]+","+members[order[1]]+","+members[order[2]]+"}")
			changed:=append(append(append([]byte(nil),live[:liveRoot[1].start]...),vm...),live[liveRoot[1].end:]...)
			if len(changed)!=len(seed)||bytes.Equal(changed,live)||bytes.Equal(changed,seed){t.Fatal("ineffective_order_negative")}
			if _,err:=guardCaptureSeedRepresentation(changed,p);err==nil{t.Fatal("unsupported_vm_order_accepted")}
		})
	}
	wrong := p; wrong.BundleSHA = strings.Repeat("0",64)
	if _,err:=guardCaptureSeedRepresentation(live,wrong);err==nil {t.Fatal("caller_hash_without_seed_accepted")}
	selected := matrixOutcome{Raw:live,LogIndex:p.RekorIndex}
	if guardCaptureSelectedBinding(selected,"https://fixture.blob.core.windows.net/"+p.AttestationID+".json.sn",p)!="bound" {t.Fatal("projected_binding_rejected")}
}

func TestGuardCaptureRawObjectRejectsAmbiguity(t *testing.T) {
	for name, raw := range map[string]string{
		"duplicate":`{"a":1,"a":2}`,
		"casefold":`{"a":1,"A":2}`,
		"escaped_key":`{"\u0061":1}`,
		"nested_duplicate":`{"a":{"b":1,"b":2}}`,
		"tail":`{"a":1}{}`,
		"nonobject":`[]`,
		"invalid":`{"a":}`,
		"depth":strings.Repeat("[",66)+"0"+strings.Repeat("]",66),
	} {
		t.Run(name,func(t *testing.T){if _,err:=guardCaptureRawObject([]byte(raw));err==nil{t.Fatal("ambiguous_raw_object_accepted")}})
	}
	raw := []byte(`{"a":9007199254740993,"b":"\\u0061","c":{"rawBytes":"Q0VSVA=="}}`)
	members,err:=guardCaptureRawObject(raw)
	if err!=nil||len(members)!=3||string(members[0].value)!="9007199254740993"||string(members[1].value)!=`"\\u0061"`||
		string(members[2].value)!=`{"rawBytes":"Q0VSVA=="}`{t.Fatal("raw_value_spans_not_preserved")}
}

func TestGuardCaptureBindingChecksAreIndependent(t *testing.T) {
	seed,live:=guardCaptureRepresentationFixture()
	p:=guardCaptureTestPin();p.BundleBytes,p.BundleSHA=int64(len(seed)),guardCaptureHash(seed)
	for name, c:=range map[string]struct{raw []byte; index string; url string;want guardCaptureBindingDiagnostic;stage string}{
		"all_true":{live,p.RekorIndex,"https://fixture.blob.core.windows.net/"+p.AttestationID+".json.sn",guardCaptureBindingDiagnostic{true,true,true,true},"bound"},
		"digest_only":{bytes.Replace(live,[]byte("c2ln"),[]byte("c2lo"),1),p.RekorIndex,"https://fixture.blob.core.windows.net/"+p.AttestationID+".json.sn",guardCaptureBindingDiagnostic{true,false,true,true},"bundle_digest"},
		"digest_and_index":{bytes.Replace(live,[]byte("c2ln"),[]byte("c2lo"),1),p.RekorIndex+"0","https://fixture.blob.core.windows.net/"+p.AttestationID+".json.sn",guardCaptureBindingDiagnostic{true,false,false,true},"bundle_digest"},
		"digest_and_record":{bytes.Replace(live,[]byte("c2ln"),[]byte("c2lo"),1),p.RekorIndex,"https://fixture.blob.core.windows.net/other.json.sn",guardCaptureBindingDiagnostic{true,false,true,false},"bundle_digest"},
		"all_false":{[]byte("{}"),p.RekorIndex+"0","https://fixture.blob.core.windows.net/other.json.sn",guardCaptureBindingDiagnostic{false,false,false,false},"bundle_size"},
	} {
		t.Run(name,func(t *testing.T){
			selected:=matrixOutcome{Raw:c.raw,LogIndex:c.index}
			got:=guardCaptureBindingFacts(selected,c.url,p)
			if got!=c.want||guardCaptureBindingStage(got)!=c.stage{t.Fatal("binding_facts_short_circuited_or_incorrect")}
			d:=guardCaptureSelectionDiagnostic{Stage:c.stage,Binding:&got}
			report:=guardCaptureReport{PESelection:&d}
			encoded,err:=json.Marshal(report)
			if err!=nil||report.Passed||report.UpdateAuthorized||bytes.Contains(encoded,c.raw)||bytes.Contains(encoded,[]byte(c.url)){t.Fatal("binding_facts_granted_or_exposed_input")}
			for key,want:=range map[string]bool{"bundle_size_match":c.want.BundleSizeMatch,"seed_representation_match":c.want.SeedRepresentationMatch,"rekor_index_match":c.want.RekorIndexMatch,"attestation_record_match":c.want.AttestationRecordMatch}{
				if !bytes.Contains(encoded,[]byte(fmt.Sprintf("%q:%t",key,want))){t.Fatal("binding_fact_not_serialized")}
			}
		})
	}
}

// Public hosted diagnostic bytes from the same fixed capture, not a verifier result.
func TestGuardCaptureRetainedLiveRepresentationMatchesSeedPin(t *testing.T) {
	raw:=[]byte(`{"mediaType":"application/vnd.dev.sigstore.bundle.v0.3+json","verificationMaterial":{"tlogEntries":[{"logIndex":"4023990479","logId":{"keyId":"wNI9atQGlz+VWfO6LRygH4QUfY/8W4RFwiT5i5WRgB0="},"kindVersion":{"kind":"dsse","version":"0.0.1"},"integratedTime":"1795173556","inclusionPromise":{"signedEntryTimestamp":"MEUCIGlXQcpNSRASflF6+09FK5nIxFzVmG2q3Rl/YclVzaKnAiEAmCw33wLwDfgTsQynDKdsECp+DhdLvOINzhu04SZCf5U="},"inclusionProof":{"logIndex":"3902086217","rootHash":"Ovag7dTikNgXHz9jhU1BIUeAbBRXst1Tq6uLy/hPfT8=","treeSize":"3902086223","hashes":["NL379pjJi7EwVdq0Q/eUDwB+Uc1o8KQY/qFhyUHQAjs=","qIMiqzprhoZBapctDWau9rX0tqroqebtwZ7G3J8APSg=","mVQFCru5QnB3FYYYvXpZ0Y8WhB6QpxR0odbMbb7AaN4=","Vrk/ETv8MiL/5tALP1NoX7p+JYj8yrtU17N74Ns72qo=","siD+aw8+fImlBIcCHxkSNSiBC+UZpMn0fdKeQQMX4hI=","XjQ/urXfhdBveFqLn0r+LGnqYUbEDtBXxOaAZIyivQ0=","pV31HEAxz2yjRgmqShks9jtbOdLjFvQXPri86OeZiHQ=","226ZxtCzkCbMrL6oUeehXQ2TkGMgnS2lU6IqrjSI5y4=","TjfjRmgxdQIq1L+RcZmf8x6T3EVddeiKg4DM5d1X8Ks=","zg6onwKXIBaa9MXMOObR1FahT1U7IDutCqv2BGQPDMc=","TkKgr5bFMXLxX0gcNTmH5fNaDfGP7sVqKcdaCKNfNJY=","bpivoC31PB5djfiSn6cC5GXF2dPZuOJdVxDafoiGndU=","XYkf+Vk9hVBebwq5KRpgAFIE2Jk4aAgO1jFr22tSd4k=","k0+Z1T6dI0Tgluh54oO/hnxtuoW22MqFIjVTPYeUJGU=","1DaDKzrCNNBhmSdY5vFjz+NpAmxmYfYEgBCdPLrMP1A=","xH/DCseLHr9eKoYT8qsORZK7zVdEGYWHuVtsVrD95wY="],"checkpoint":{"envelope":"rekor.sigstore.dev - 1193050959916656506\n3902086223\nOvag7dTikNgXHz9jhU1BIUeAbBRXst1Tq6uLy/hPfT8=\n\n— rekor.sigstore.dev wNI9ajBEAiBs6bZOYVVaARvtAkg3YmzB19jwSazooviqWfsjLV0ypQIgJEOg9a8yvD1qmgl+nhVeGgd0bay0PYrno/XJAQiK8B0=\n"}},"canonicalizedBody":"eyJhcGlWZXJzaW9uIjoiMC4wLjEiLCJraW5kIjoiZHNzZSIsInNwZWMiOnsiZW52ZWxvcGVIYXNoIjp7ImFsZ29yaXRobSI6InNoYTI1NiIsInZhbHVlIjoiOGUwMWFkM2I2YjZkMzQwNjA0NjlmYWFhZDA4OTE2ZWIyNDFhZjZhNzdjOGZiNDU5ZmM0YTczZGRiOGJhMDRiNyJ9LCJwYXlsb2FkSGFzaCI6eyJhbGdvcml0aG0iOiJzaGEyNTYiLCJ2YWx1ZSI6IjVkODM0YjMzNmEzODhjNDY2ZmY1MzQzZjM3NWFhNjM4OTRhN2QxN2M1OGFhNWYzODNlNWVlNjdlN2Q4ZDNkMWMifSwic2lnbmF0dXJlcyI6W3sic2lnbmF0dXJlIjoiTUVVQ0lCWHIvSzNsblZNTnNnaUNDVDF5TWwyTXFWY2JmYnpmbVJwR1F1cFZUU1QrQWlFQXJ3bGtsWjVlbkwwU1ZJRFB1dkJvSWJxSUprVjNCSmd1UlVOY0JCVXhNQmc9IiwidmVyaWZpZXIiOiJMUzB0TFMxQ1JVZEpUaUJEUlZKVVNVWkpRMEZVUlMwdExTMHRDazFKU1Voa1ZFTkRRblo1WjBGM1NVSkJaMGxWVWxCelRuQjFiRkpHTTBabGFEVmxhM0Z5V2tKblZURkdRbWxuZDBObldVbExiMXBKZW1vd1JVRjNUWGNLVG5wRlZrMUNUVWRCTVZWRlEyaE5UV015Ykc1ak0xSjJZMjFWZFZwSFZqSk5ValIzU0VGWlJGWlJVVVJGZUZaNllWZGtlbVJIT1hsYVV6RndZbTVTYkFwamJURnNXa2RzYUdSSFZYZElhR05PVFdwWmVFMVVTWGROVkVWNFQxUkZNbGRvWTA1TmFsbDRUVlJKZDAxVVJYbFBWRVV5VjJwQlFVMUdhM2RGZDFsSUNrdHZXa2w2YWpCRFFWRlpTVXR2V2tsNmFqQkVRVkZqUkZGblFVVm5aMFJrV2t0bFMyaDBPVzFRUm5GbFNHWXllRUk1YUhKaE5EUlVUWGhrUm5kbWNuQUtWMGg2ZGs1SGNVUnBVa05RYVRoNk9HUlZWMlY2TkdoUFpFWkNTR2t5WjI1Q1RsSk1Xa1ZWYlZsSVZrUjNXbFZZT0RaUFEwSm9jM2RuWjFsWVRVRTBSd3BCTVZWa1JIZEZRaTkzVVVWQmQwbElaMFJCVkVKblRsWklVMVZGUkVSQlMwSm5aM0pDWjBWR1FsRmpSRUY2UVdaQ1owNVdTRk5OUlVkRVFWZG5RbFJtQ2pBcmJsQldhVkZTYkhadGJ6SlBhMjlXWVV4SFRHaG9hMUI2UWpOQ1owNVdTRkpGUWtGbU9FVmlWRUp5YUcxc2IyUklVbmRqZW05MlRESmtjR1JIYURFS1dXazFhbUl5TUhaUldGWXdZakJTYkdScmNHaGpSMFoxVERKT2FreFlRbXBNV0ZKMllqSjRla3hZU25wTWVUVnVZVmhTYjJSWFNYWmtNamw1WVRKYWN3cGlNMlI2VEROV2RWbFlVakJhVnpWcldsZFJkR050Vm1oYVIyeDFXbGhPZWt4dWJIUmlSVUo1V2xkYWVrd3lhR3haVjFKNlRESXhhR0ZYTkhkUFVWbExDa3QzV1VKQ1FVZEVkbnBCUWtGUlVYSmhTRkl3WTBoTk5reDVPVEJpTW5Sc1ltazFhRmt6VW5CaU1qVjZURzFrY0dSSGFERlpibFo2V2xoS2FtSXlOVEFLV2xjMU1FeHRUblppVkVGbVFtZHZja0puUlVWQldVOHZUVUZGUTBKQ1JqTmlNMHB5V20xNGRtUXhPV3RoV0U1M1dWaFNhbUZFUVRKQ1oyOXlRbWRGUlFwQldVOHZUVUZGUkVKRFozZFBWMFYzV2tkVk1VNHlUVEJQVkU1c1RqSk5OVTV0UlhoYWJVbDRUVlJOZWsxRVdUUmFSRTVzVFcxRk5FMXFVVEpQUkVFeUNrMURaMGREYVhOSFFWRlJRbWMzT0hkQlVWRkZSMnhXZFZsWVVqQmFWelZyV2xkUloyTnRWbWhhUjJ4MVdsaE9la2xJVW5aaU1uaDZUVU5yUjBOcGMwY0tRVkZSUW1jM09IZEJVVlZGUnpCR01XUkhPVVZhV0ZwTFdWaENhR0pwT1dwWmVURjNXWGt4TUdJeU9YTmplVEY1WTNwQlpFSm5iM0pDWjBWRlFWbFBMd3BOUVVWSFFrRTVlVnBYV25wTU1taHNXVmRTZWt3eU1XaGhWelIzVDNkWlMwdDNXVUpDUVVkRWRucEJRa05CVVhSRVEzUnZaRWhTZDJONmIzWk1NMUoyQ21FeVZuVk1iVVpxWkVkc2RtSnVUWFZhTW13d1lVaFdhV1JZVG14amJVNTJZbTVTYkdKdVVYVlpNamwwVFVoclIwTnBjMGRCVVZGQ1p6YzRkMEZSYTBVS1lYZDRjR0ZJVWpCalNFMDJUSGs1Ym1GWVVtOWtWMGwxV1RJNWRFd3dSakZrUnpsRldsaGFTMWxZUW1oaWFUbHFXWGt4ZDFsNU1UQmlNamx6WTNreGVRcGplVGgxV2pKc01HRklWbWxNTTJSMlkyMTBiV0pIT1ROamVUa3hZbTFHTUdSSFZuVmFSMVpyVEZoS2JGbFhVbkJpYlZaNlkzazFOV0pYZUVGamJWWnRDbU41T1c5YVYwWnJZM2s1ZEZsWGJIVk5SR2RIUTJselIwRlJVVUpuTnpoM1FWRnZSVXRuZDI5TlJHeG9UVWRTYkU1VVpHcE9SR3Q2V2xSa2FrOVVXbWdLVFZkYWFVMVVSWHBOZWtFeVQwZFJlbHBVU21oUFJFa3dUbXBuZDA1cVFXUkNaMjl5UW1kRlJVRlpUeTlOUVVWTVFrRTRUVVJYWkhCa1IyZ3hXV2t4YndwaU0wNHdXbGRSZDFCbldVdExkMWxDUWtGSFJIWjZRVUpFUVZGM1JFTTFiMlJJVW5kamVtOTJUREprY0dSSGFERlphVFZxWWpJd2RsRllWakJpTUZKc0NtUnJjR2hqUjBaMVRESk9ha3hZUW1wTVdGSjJZako0ZWt4WVNucE5SR2RIUTJselIwRlJVVUpuTnpoM1FWRXdSVXRuZDI5TlJHeG9UVWRTYkU1VVpHb0tUa1JyZWxwVVpHcFBWRnBvVFZkYWFVMVVSWHBOZWtFeVQwZFJlbHBVU21oUFJFa3dUbXBuZDA1cVFXWkNaMjl5UW1kRlJVRlpUeTlOUVVWUFFrSkZUUXBFTTBwc1dtNU5kbUZIVm1oYVNFMTJZbGRHY0dKcVFXRkNaMjl5UW1kRlJVRlpUeTlOUVVWUVFrRjNUVU5xUlRCTlJGazBUVlJuTTAxVVRYZE1kMWxMQ2t0M1dVSkNRVWRFZG5wQlFrVkJVV2hFUWpsdlpFaFNkMk42YjNaTU1tUndaRWRvTVZscE5XcGlNakIyVVZoV01HSXdVbXhrYTNCb1kwZEdkVTFDYTBjS1EybHpSMEZSVVVKbk56aDNRVkpGUlVOM2QwcE5hbEV5VDBSTk1FOVVaM3BOU0d0SFEybHpSMEZSVVVKbk56aDNRVkpKUldGM2VIQmhTRkl3WTBoTk5ncE1lVGx1WVZoU2IyUlhTWFZaTWpsMFREQkdNV1JIT1VWYVdGcExXVmhDYUdKcE9XcFplVEYzV1hreE1HSXlPWE5qZVRGNVkzazRkVm95YkRCaFNGWnBDa3d6WkhaamJYUnRZa2M1TTJONU9URmliVVl3WkVkV2RWcEhWbXRNV0Vwc1dWZFNjR0p0Vm5wamVUVTFZbGQ0UVdOdFZtMWplVGx2V2xkR2EyTjVPWFFLV1Zkc2RVMUVaMGREYVhOSFFWRlJRbWMzT0hkQlVrMUZTMmQzYjAxRWJHaE5SMUpzVGxSa2FrNUVhM3BhVkdScVQxUmFhRTFYV21sTlZFVjZUWHBCTWdwUFIxRjZXbFJLYUU5RVNUQk9hbWQzVG1wQmFFSm5iM0pDWjBWRlFWbFBMMDFCUlZWQ1FrMU5SVmhrZG1OdGRHMWlSemt6V0RKU2NHTXpRbWhrUjA1dkNrMUhTVWREYVhOSFFWRlJRbWMzT0hkQlVsVkZWa0Y0VTJGSVVqQmpTRTAyVEhrNWJtRllVbTlrVjBsMVdUSTVkRXd3UmpGa1J6bEZXbGhhUzFsWVFtZ0tZbWs1YWxsNU1YZFplVEV3WWpJNWMyTjVNWGxqZVRsb1dUTlNjR0l5TlhwTU0wb3hZbTVOZGs1RVVUTk5hbGt3VDBSSk1rMUVRWFpaV0ZJd1dsY3hkd3BrU0UxMlRWUkJWMEpuYjNKQ1owVkZRVmxQTDAxQlJWZENRV2ROUW01Q01WbHRlSEJaZWtKYVFtZHZja0puUlVWQldVOHZUVUZGV1VKRmMwMVRXRXBzQ21OSE9EWlJXRll3WWpCU2JHUnJjR2hqUjBaMVVVUkpNRTVxWjNwT1JHczBUWGs1YWxsNU1YZFplVEV3WWpJNWMyTjVNWGxqTUVGNFRrUkJNazlFUlRRS1RucEZlazl1U214YWFuQjVXbGRhZWt3eWFHeFpWMUo2VERJeGFHRlhOSGRuV1c5SFEybHpSMEZSVVVJeGJtdERRa0ZKUldaQlVqWkJTR2RCWkdkRVpBcFFWRUp4ZUhOalVrMXRUVnBJYUhsYVducGpRMjlyY0dWMVRqUTRjbVlyU0dsdVMwRk1lVzUxYW1kQlFVRmhTRFJ5Y1VoblFVRkJSVUYzUWtoTlJWVkRDa2xSUkhkaFRqSkZRa3c0VTBVNGFESlJRbTFzS3k4MWQwOHdhVkUzYVhsdFJUaFNibVZhY0RKUmQxWnJaVkZKWjA5WGRrMVFaVnBhYTJJMGNTOVRPRElLZVhjM1R6WjVUblZZYkhrMFEwOWtUekV4TlZocmQwcHZjbVJCZDBObldVbExiMXBKZW1vd1JVRjNUVVJhZDBGM1drRkpkMWhhTm1oVk1EWXhNbXR1TUFwaVFYVlNla3RwVldnMlNEQjRVRTl0V2xac09DczNkVXh5VUZReWNWQnpSblZhYTFKdWJtNTVWbEY0TmxwQ1VVOU1ZbWxGUVdwQmIzbHBhMUZqZURCbENtWTBjazB4YzFvdldIZzNRa1pDZEZBM1JrZ3JjWEZ4Y2tsWlUyTk9iaTlRT0V0VVdGcFRTbUZNTlM4MFNtNXZVV2wyZWtOalFqZzlDaTB0TFMwdFJVNUVJRU5GVWxSSlJrbERRVlJGTFMwdExTMEsifV19fQ=="}],"timestampVerificationData":{},"certificate":{"rawBytes":"MIIHdTCCBvygAwIBAgIURPsNpulRF3Feh5ekqrZBgU1FBigwCgYIKoZIzj0EAwMwNzEVMBMGA1UEChMMc2lnc3RvcmUuZGV2MR4wHAYDVQQDExVzaWdzdG9yZS1pbnRlcm1lZGlhdGUwHhcNMjYxMTIwMTExOTE2WhcNMjYxMTIwMTEyOTE2WjAAMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEggDdZKeKht9mPFqeHf2xB9hra44TMxdFwfrpWHzvNGqDiRCPi8z8dUWez4hOdFBHi2gnBNRLZEUmYHVDwZUX86OCBhswggYXMA4GA1UdDwEB/wQEAwIHgDATBgNVHSUEDDAKBggrBgEFBQcDAzAfBgNVHSMEGDAWgBTf0+nPViQRlvmo2OkoVaLGLhhkPzB3BgNVHREBAf8EbTBrhmlodHRwczovL2dpdGh1Yi5jb20vQXV0b0RldkphcGFuL2NjLXBjLXRvb2xzLXJzLy5naXRodWIvd29ya2Zsb3dzL3VuYXR0ZW5kZWQtcmVhZGluZXNzLnltbEByZWZzL2hlYWRzL21haW4wOQYKKwYBBAGDvzABAQQraHR0cHM6Ly90b2tlbi5hY3Rpb25zLmdpdGh1YnVzZXJjb250ZW50LmNvbTAfBgorBgEEAYO/MAECBBF3b3JrZmxvd19kaXNwYXRjaDA2BgorBgEEAYO/MAEDBCgwOWEwZGU1N2M0OTNlN2M5NmExZmIxMTMzMDY4ZDNlMmE4MjQ2ODA2MCgGCisGAQQBg78wAQQEGlVuYXR0ZW5kZWQgcmVhZGluZXNzIHRvb2xzMCkGCisGAQQBg78wAQUEG0F1dG9EZXZKYXBhbi9jYy1wYy10b29scy1yczAdBgorBgEEAYO/MAEGBA9yZWZzL2hlYWRzL21haW4wOwYKKwYBBAGDvzABCAQtDCtodHRwczovL3Rva2VuLmFjdGlvbnMuZ2l0aHVidXNlcmNvbnRlbnQuY29tMHkGCisGAQQBg78wAQkEawxpaHR0cHM6Ly9naXRodWIuY29tL0F1dG9EZXZKYXBhbi9jYy1wYy10b29scy1ycy8uZ2l0aHViL3dvcmtmbG93cy91bmF0dGVuZGVkLXJlYWRpbmVzcy55bWxAcmVmcy9oZWFkcy9tYWluMDgGCisGAQQBg78wAQoEKgwoMDlhMGRlNTdjNDkzZTdjOTZhMWZiMTEzMzA2OGQzZTJhODI0NjgwNjAdBgorBgEEAYO/MAELBA8MDWdpdGh1Yi1ob3N0ZWQwPgYKKwYBBAGDvzABDAQwDC5odHRwczovL2dpdGh1Yi5jb20vQXV0b0RldkphcGFuL2NjLXBjLXRvb2xzLXJzMDgGCisGAQQBg78wAQ0EKgwoMDlhMGRlNTdjNDkzZTdjOTZhMWZiMTEzMzA2OGQzZTJhODI0NjgwNjAfBgorBgEEAYO/MAEOBBEMD3JlZnMvaGVhZHMvbWFpbjAaBgorBgEEAYO/MAEPBAwMCjE0MDY4MTg3MTMwLwYKKwYBBAGDvzABEAQhDB9odHRwczovL2dpdGh1Yi5jb20vQXV0b0RldkphcGFuMBkGCisGAQQBg78wAREECwwJMjQ2ODM0OTgzMHkGCisGAQQBg78wARIEawxpaHR0cHM6Ly9naXRodWIuY29tL0F1dG9EZXZKYXBhbi9jYy1wYy10b29scy1ycy8uZ2l0aHViL3dvcmtmbG93cy91bmF0dGVuZGVkLXJlYWRpbmVzcy55bWxAcmVmcy9oZWFkcy9tYWluMDgGCisGAQQBg78wARMEKgwoMDlhMGRlNTdjNDkzZTdjOTZhMWZiMTEzMzA2OGQzZTJhODI0NjgwNjAhBgorBgEEAYO/MAEUBBMMEXdvcmtmbG93X2Rpc3BhdGNoMGIGCisGAQQBg78wARUEVAxSaHR0cHM6Ly9naXRodWIuY29tL0F1dG9EZXZKYXBhbi9jYy1wYy10b29scy1ycy9hY3Rpb25zL3J1bnMvNDQ3MjY0ODI2MDAvYXR0ZW1wdHMvMTAWBgorBgEEAYO/MAEWBAgMBnB1YmxpYzBZBgorBgEEAYO/MAEYBEsMSXJlcG86QXV0b0RldkphcGFuQDI0NjgzNDk4My9jYy1wYy10b29scy1yc0AxNDA2ODE4NzEzOnJlZjpyZWZzL2hlYWRzL21haW4wgYoGCisGAQQB1nkCBAIEfAR6AHgAdgDdPTBqxscRMmMZHhyZZzcCokpeuN48rf+HinKALynujgAAAaH4rqHgAAAEAwBHMEUCIQDwaN2EBL8SE8h2QBml+/5wO0iQ7iymE8RneZp2QwVkeQIgOWvMPeZZkb4q/S82yw7O6yNuXly4COdO115XkwJordAwCgYIKoZIzj0EAwMDZwAwZAIwXZ6hU0612kn0bAuRzKiUh6H0xPOmZVl8+7uLrPT2qPsFuZkRnnnyVQx6ZBQOLbiEAjAoyikQcx0ef4rM1sZ/Xx7BFBtP7FH+qqqrIYScNn/P8KTXZSJaL5/4JnoQivzCcB8="}},"dsseEnvelope":{"payload":"eyJfdHlwZSI6Imh0dHBzOi8vaW4tdG90by5pby9TdGF0ZW1lbnQvdjEiLCJzdWJqZWN0IjpbeyJuYW1lIjoiZ3VhcmQuZXhlIiwiZGlnZXN0Ijp7InNoYTI1NiI6IjljNDMxYzU4MjQ0YTIyMmMwYTRiNmJmOGZkMjQ0OWVlMGY1OWExYjc2NTNiZjM0MzkzZTRhODkwY2UzYTM1ODkifX0seyJuYW1lIjoic291cmNlLXByb29mLmpzb24iLCJkaWdlc3QiOnsic2hhMjU2IjoiMTNiNzA0Nzc4M2EzM2NkMzQ2Zjc4ZDlhMDQ5NmE4OWFlZWUyYTk3NzRiZjlmOWQ5MjAxMjQ3YmFjZjljYzMxMiJ9fV0sInByZWRpY2F0ZVR5cGUiOiJodHRwczovL3Nsc2EuZGV2L3Byb3ZlbmFuY2UvdjEiLCJwcmVkaWNhdGUiOnsiYnVpbGREZWZpbml0aW9uIjp7ImJ1aWxkVHlwZSI6Imh0dHBzOi8vYWN0aW9ucy5naXRodWIuaW8vYnVpbGR0eXBlcy93b3JrZmxvdy92MSIsImV4dGVybmFsUGFyYW1ldGVycyI6eyJ3b3JrZmxvdyI6eyJyZWYiOiJyZWZzL2hlYWRzL21haW4iLCJyZXBvc2l0b3J5IjoiaHR0cHM6Ly9naXRodWIuY29tL0F1dG9EZXZKYXBhbi9jYy1wYy10b29scy1ycyIsInBhdGgiOiIuZ2l0aHViL3dvcmtmbG93cy91bmF0dGVuZGVkLXJlYWRpbmVzcy55bWwifX0sImludGVybmFsUGFyYW1ldGVycyI6eyJnaXRodWIiOnsiZXZlbnRfbmFtZSI6IndvcmtmbG93X2Rpc3BhdGNoIiwicmVwb3NpdG9yeV9pZCI6IjE0MDY4MTg3MTMiLCJyZXBvc2l0b3J5X293bmVyX2lkIjoiMjQ2ODM0OTgzIiwicnVubmVyX2Vudmlyb25tZW50IjoiZ2l0aHViLWhvc3RlZCJ9fSwicmVzb2x2ZWREZXBlbmRlbmNpZXMiOlt7InVyaSI6ImdpdCtodHRwczovL2dpdGh1Yi5jb20vQXV0b0RldkphcGFuL2NjLXBjLXRvb2xzLXJzQHJlZnMvaGVhZHMvbWFpbiIsImRpZ2VzdCI6eyJnaXRDb21taXQiOiIwOWEwZGU1N2M0OTNlN2M5NmExZmIxMTMzMDY4ZDNlMmE4MjQ2ODA2In19XX0sInJ1bkRldGFpbHMiOnsiYnVpbGRlciI6eyJpZCI6Imh0dHBzOi8vZ2l0aHViLmNvbS9BdXRvRGV2SmFwYW4vY2MtcGMtdG9vbHMtcnMvLmdpdGh1Yi93b3JrZmxvd3MvdW5hdHRlbmRlZC1yZWFkaW5lc3MueW1sQHJlZnMvaGVhZHMvbWFpbiJ9LCJtZXRhZGF0YSI6eyJpbnZvY2F0aW9uSWQiOiJodHRwczovL2dpdGh1Yi5jb20vQXV0b0RldkphcGFuL2NjLXBjLXRvb2xzLXJzL2FjdGlvbnMvcnVucy80NDcyNjQ4MjYwMC9hdHRlbXB0cy8xIn19fX0=","payloadType":"application/vnd.in-toto+json","signatures":[{"sig":"MEUCIBXr/K3lnVMNsgiCCT1yMl2MqVcbfbzfmRpGQupVTST+AiEArwlklZ5enL0SVIDPuvBoIbqIJkV3BJguRUNcBBUxMBg="}]}}`)
	if int64(len(raw))!=11231||guardCaptureHash(raw)!="7053712e67c573649755595edb137af7aa6e7cacbc16e490d1bcc568e8dcab2f"{t.Fatal("retained_live_fixture_changed")}
	p:=guardCaptureNext
	if p.BundleBytes!=11231||p.BundleSHA!="e7834c6fc21c5bed1d17507c929c7bc25f8c6c7322226383b26d5a037b9d0771"{t.Fatal("original_seed_pin_changed")}
	restored,err:=guardCaptureSeedRepresentation(raw,p)
	if err!=nil||int64(len(restored))!=p.BundleBytes||guardCaptureHash(restored)!=p.BundleSHA {t.Fatal("retained_live_seed_binding_failed")}
	root,err:=guardCaptureRawObject(restored)
	if err!=nil||len(root)!=3{t.Fatal("retained_seed_structure_changed")}
	vm,err:=guardCaptureRawObject(root[1].value)
	if err!=nil||len(vm)!=3{t.Fatal("retained_seed_material_changed")}
	order:=[]int{1,2,0}
	var back bytes.Buffer;back.WriteByte('{')
	for i,j:=range order {
		if i!=0 {back.WriteByte(',')}
		back.WriteByte('"');back.WriteString(vm[j].name);back.WriteString("\":");back.Write(vm[j].value)
	}
	back.WriteByte('}')
	roundtrip:=append(append(append([]byte(nil),restored[:root[1].start]...),back.Bytes()...),restored[root[1].end:]...)
	if !bytes.Equal(roundtrip,raw){t.Fatal("retained_live_roundtrip_changed_raw_material")}
}
