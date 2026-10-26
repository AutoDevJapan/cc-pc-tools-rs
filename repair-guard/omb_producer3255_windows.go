package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// This is one software-only comparison, not an allowed(path) branch. Its
// independent original-install observation and signed-public-package receipts
// must qualify before any installed component is opened. No caller supplies
// a path, digest, manifest, task, source override or permission.
const omb3255Flag = "--verify-omb-producer-3255"
const omb3255ProofRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3255-godd"

type omb3255Component struct{ path, sha256 string }
type omb3255Spec struct {
	root       string
	evidence   []consoleEvidence
	components []omb3255Component
}

type omb3255Result struct {
	Domain                    string `json:"domain"`
	TodoID                    int    `json:"todo_id"`
	Status                    string `json:"status"`
	SoftwareMatches           *bool  `json:"software_matches"`
	EvidenceVerified          bool   `json:"evidence_verified"`
	ReferenceVersion          string `json:"reference_version"`
	ReferenceSourceCommit     string `json:"reference_source_commit"`
	DataAccessGranted         bool   `json:"data_access_granted"`
	SoftwareAccessGranted     bool   `json:"software_access_granted"`
	SourceAccessGranted       bool   `json:"source_access_granted"`
	SourceWorktreeVerified    bool   `json:"source_worktree_verified"`
	Execution4559Verified     bool   `json:"execution4559_verified"`
	OriginalRegistryRecovered bool   `json:"original_registry_recovered"`
	StateChanged              bool   `json:"state_changed"`
}

func omb3255Unavailable(status string) omb3255Result {
	return omb3255Result{Domain: "omb-producer-software-v1", TodoID: 3255, Status: status,
		ReferenceVersion: "0.1.121", ReferenceSourceCommit: "97a4a1cab96ab3d07b0c7dac275f69afc6e7a8dc"}
}

func omb3255FlagPresent(args []string) bool {
	for _, arg := range args {
		if arg == omb3255Flag || strings.HasPrefix(arg, omb3255Flag+"=") ||
			arg == strings.TrimPrefix(omb3255Flag, "-") || strings.HasPrefix(arg, strings.TrimPrefix(omb3255Flag, "-")+"=") {
			return true
		}
	}
	return false
}

func runOMB3255CLI(args []string, stdout io.Writer, verify func() omb3255Result) int {
	result := omb3255Unavailable("invalid_arguments")
	code := 2
	if len(args) == 1 && args[0] == omb3255Flag {
		result = verify()
		code = 1
		if result.Status == "matched" && result.EvidenceVerified && result.SoftwareMatches != nil && *result.SoftwareMatches {
			code = 0
		}
	}
	if json.NewEncoder(stdout).Encode(result) != nil {
		return 1
	}
	return code
}

func omb3255FixedSpec() omb3255Spec {
	root := omb3255ProofRoot
	return omb3255Spec{root: root, evidence: []consoleEvidence{
		{root, root + "/docs/todo3255-051621-official-provenance-verification.private.json", "27f0eb0a7145f2ab64803fca31e9a734077c1f6f7d2d3cb9a0fe44beb57d9568"},
		{root, root + "/docs/todo3255-051621-verified-public-producer-baseline.private.json", "6aa9d48ed3ce591e8fb2db7c06ee7052ffdc0de959bfd93e3c64557e83fc7c35"},
		{root, root + "/docs/todo3255-065107-installer-original-archive-binding.private.json", "860094ed0530b4302fe1c385f1036e1884aaf42f30aef390b194ba0e244ea8e7"},
	}, components: []omb3255Component{
		{"D:/OMB/headless/package/cli.js", "1873fee9198a8102e0118c2aba19aa7903eb084b249c9105968b458e671bb9a8"},
		{"D:/OMB/headless/package/package.json", "694a7e305216bbb23992422b54443187ea1fd8ba55f6e11d6b9059ef793303b5"},
		{"D:/OMB/headless/package/dist-server/openmausbot.js", "ac105487d60efb32713015abd0e82c28d739ec793959e0047cc4924ad9075e43"},
		{"D:/OMB/headless/package/dist-server/index.js", "c783ab95fbad9651bf8e5bcac8a2c1b5c68ae42daddf2c1f9c57145c7085f15a"},
	}}
}

func verifyOMB3255Software() omb3255Result {
	return omb3255SoftwareWith(omb3255FixedSpec(), legacyMatch, func(root string) bool {
		return same(root, omb3255ProofRoot) && sourceVerified(root) && allowed(root)
	}, allowed)
}

func omb3255SoftwareWith(spec omb3255Spec, protected, source, evidenceAllowed func(string) bool) omb3255Result {
	result := omb3255Unavailable("unavailable")
	if len(spec.evidence) != 3 || len(spec.components) != 4 {
		return result
	}
	seen := make(map[string]bool)
	var targets []string
	for _, leaf := range spec.components {
		key := strings.ToLower(filepath.Clean(leaf.path))
		if !filepath.IsAbs(leaf.path) || !modelDigestValid(leaf.sha256) || seen[key] {
			return result
		}
		seen[key] = true
		targets = append(targets, leaf.path)
	}
	for _, proof := range spec.evidence {
		key := strings.ToLower(filepath.Clean(proof.path))
		if !same(proof.root, spec.root) || !within(spec.root, proof.path) || !modelDigestValid(proof.sha256) ||
			seen[key] || !evidenceAllowed(proof.path) {
			return result
		}
		seen[key] = true
	}
	if !modelEvidenceIndependent(spec.evidence, targets...) {
		return result
	}
	session := modelPinSession{protected: protected, source: source, head: modelSourceHead}
	defer session.close()
	if !session.evidence(spec.evidence) {
		return result
	}
	matched := true
	for _, leaf := range spec.components {
		if !omb3255Observe(&session, leaf.path) {
			return result
		}
		matched = matched && session.files[len(session.files)-1].sha256 == leaf.sha256
	}
	// Reuse the existing same-handle, no-write/no-delete-sharing checks. A
	// successful comparison grants neither data access nor source ownership,
	// and is not an attestation of the running process or a historical model.
	if !session.recheck() {
		return result
	}
	for _, proof := range spec.evidence {
		if !evidenceAllowed(proof.path) {
			return result
		}
	}
	result.Status = "matched"
	if !matched {
		result.Status = "mismatched"
	}
	result.SoftwareMatches, result.EvidenceVerified = &matched, true
	return result
}

// Unlike match, this retains a completely observed regular file even when its
// digest differs. Only the aggregate boolean is returned after all four files
// and the independently pinned evidence pass the final same-handle recheck.
// It does not change match's rejection contract for any existing consumer.
func omb3255Observe(session *modelPinSession, path string) bool {
	if !runtimeLeaf(path, filepath.Dir(path), session.protected, true) {
		return false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, 0x00200000|0x08000000, 0)
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(h), path)
	pinned := modelPinnedFile{file: f, path: path}
	kept := false
	defer func() {
		if !kept {
			f.Close()
		}
	}()
	if syscall.GetFileInformationByHandle(h, &pinned.info) != nil || !modelFileInfoValid(pinned.info) {
		return false
	}
	actual, ok := modelHandlePath(h)
	if !ok || !same(actual, path) {
		return false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 64<<20 {
		return false
	}
	digest := sha256.New()
	n, err := io.Copy(digest, io.LimitReader(f, (64<<20)+1))
	if err != nil || n != info.Size() {
		return false
	}
	pinned.sha256 = hex.EncodeToString(digest.Sum(nil))
	if !session.fileUnchanged(pinned) {
		return false
	}
	session.files = append(session.files, pinned)
	kept = true
	return true
}
