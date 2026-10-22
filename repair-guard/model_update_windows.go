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
	"unsafe"
)

// This purpose is separate from both the generic classifier and ToDo3085's
// capacity update. No request can supply a manifest, digest, source or grant.
type modelUpdateCandidate struct {
	root, path, head, sha256 string
	evidence                 []consoleEvidence
}

type modelUpdateLeaf struct {
	kind, target string
	baselines    []consolePin
	candidate    modelUpdateCandidate
}

const model2903Source = permanent3344Root + "/9fb62b51c7b4f145efadfbd894224795fbc3157e"
const model3082Source = permanent3344Root + "/eae2d2bd7e3e16fe3833b252b065c0ee8ee92a4b"
const model3082Receipt = model3082Source + "/.ci-work/3082-runtime-20261020-232848-todo-3082.private.json"
const model3082ReceiptSHA = "dfda87e2f50ca965f13ba432d7b1d893bd8530b5c8234911ec102423c9beb677"

const model3298Source = permanent3344Root + "/70a7962c36c0b9916ff6865afa9cb606d414962e"
const model3298Head = "70a7962c36c0b9916ff6865afa9cb606d414962e"
const model3298BuildReceipt = model3298Source + "/codex-owned-5ca1100a696bda051f5c813a37f941d9/target/build-receipt.private.json"
const model3081Distribution = model3298Source + "/codex-owned-0c95078c4ebbe13a42f392c2e12e60da"
const model3081Source = permanent3344Root + "/cbddcb32fb8ffc9364862b743c4435739fab1aec"
const model3309Source = permanent3344Root + "/e290e27ffdb63cbd35dcb8af559372293a94de87"
const model3250Source = permanent3344Root + "/7bb3858a81d833dc5bd6e2320c2b81f4a55d0969"
const model3338GuardSource = permanent3344Root + "/a36a77c42690eef7c0147a6a2eee76b496a48249"
const model3344GuardSource = permanent3344Root + "/b4872860870e6832be851d630b2fd4035922a80c"

// The installed baseline has the independent ToDo3177 provenance. ToDo3298
// built the candidate once from the merged source, then ToDo3081 copied it
// without Cargo's second hardlink. Both receipts and the source HEAD stay
// pinned. Discord uses its existing normal classifier; guard stays unbound.
var model3081UpdateLeaves = []modelUpdateLeaf{
	{kind: "todo", target: "D:/CC/Tools/todo/todo.exe", baselines: []consolePin{{
		sha256: "ba1d65381bd31c38d05cf3798973427e2f0271dbc9d664afbfe91501a8de6811",
		evidence: []consoleEvidence{
			{model2903Source, model2903Source + "/.ci-work/todo-pr232-install.private.json", "3fa41d8c38e053473d6f513614b4d3617f574479b31de843e841370bf1ddfcc8"},
			{model2903Source, model2903Source + "/.ci-work/install-todo-pr232.ps1", "bf0bf2dc1cb1b7f2fe0b887fc63772c719d3d00829057bf96d67cfe84efbb238"},
		},
	}}, candidate: modelUpdateCandidate{
		root: model3298Source, path: model3081Distribution + "/todo.exe", head: model3298Head,
		sha256: "5db0e02c09b737a816b31ec811eeba3d129e4a6c0e76ff008692c2b640bbfd4d",
		evidence: []consoleEvidence{
			{model3298Source, model3298BuildReceipt, "fb67ffda1315c1aff8c24d05bdff73f3620385f629b1ad6ec450c538c391ba62"},
			{model3298Source, model3081Distribution + "/staging-receipt.private.json", "317f7e72731c62359e98af327f48772e46c5d61b40bc44a43608830b89da2e27"},
		},
	}},
	{kind: "discord", target: "D:/CC/Tools/discord-ask/discord_ask.exe", baselines: []consolePin{{
		sha256:   "eb14b19b445a247dfd85e498c20de2716a3dcc80bf6b65c188111738f6249295",
		evidence: []consoleEvidence{{model3082Source, model3082Receipt, model3082ReceiptSHA}},
	}}},
	{kind: "guard", target: "D:/CC/Tools/client-records/scrub391.exe", baselines: []consolePin{{
		sha256:   "78d90055f5324366589d6c2bec1ff0d1adc297a60a1fbc34c6de9af0c4ae5069",
		evidence: []consoleEvidence{{model3082Source, model3082Receipt, model3082ReceiptSHA}},
	}, {
		sha256:   "274e8abc5c90d5ca7dcf401b824d5ff50e078c966fd74a35af86f78dd9d900d9",
		evidence: []consoleEvidence{{model3250Source, model3250Source + "/codex-owned-1bd08feb8beaad849537a5ea1b45bdfd/guard-pr351-installed.private.json", "2de2b7789fb32d159f3d1e41e40f828167a10db1f944512d2f30510a4f782115"}},
	}, {
		sha256:   "e99f7cdc857705705871df42aa4cc159e5451df2a958076169425692ad456dc8",
		evidence: []consoleEvidence{{model3338GuardSource, model3338GuardSource + "/guard-artifact-build.private.json", "a3e3e3d64dccab1927157f7802c0c8a57eb679df624f5554d6d9cb039cf567af"}},
	}}, candidate: modelUpdateCandidate{
		root: model3344GuardSource, path: model3344GuardSource + "/guard-artifact.private.exe",
		head: "b4872860870e6832be851d630b2fd4035922a80c", sha256: "a9cb1c42b801972df4c8dad2f4bc23cafd144e80291f1040a28ad4eda3e16c23",
		evidence: []consoleEvidence{{model3344GuardSource, model3344GuardSource + "/guard-artifact-build.private.json", "529d5d0222d98c37702347b51930ae1366d411eec67a2f81a635ef738965c3d4"}},
	}},
	{kind: "jobs", target: jobsRuntime, baselines: []consolePin{{
		sha256:   "7ae20f2bfa824aa6dc27d7046bb997df137bd8980b3291d4e195804714b42c77",
		evidence: []consoleEvidence{{model2903Source, model2903Source + "/.ci-work/3255-existing-runtime3081-current-CAS-drift-20261022.private.json", "92e0ef180fbefa0385ffe36c738323b955edb065622802361178ce1cf038a3ed"}},
	}}, candidate: modelUpdateCandidate{
		root: model3309Source, path: model3309Source + "/codex-owned-f47d20fe42907634de18cbd7f6309d3d/codex_jobs.exe",
		head: "e290e27ffdb63cbd35dcb8af559372293a94de87", sha256: "85beac9a785971e15d452a293c4f5c9d17f5133ef4fac00965dfcf0bd297a71f",
		evidence: []consoleEvidence{
			{model3081Source, model3081Source + "/docs/model3081-3309-build.private.json", "aff3e9dee09275a24996d8a415db638da57e88a4a473eab02686feb5228d0fc6"},
			{model3081Source, model3081Source + "/docs/model3081-3309-result.private.json", "68774dd3285b36a64e8891cd05122732f7718823777de0c260340beb8b125409"},
		},
	}},
}

// The new owner repair is bound to the existing launcher and two actual
// software versions. It does not extend the old finite qualification's clock.
func modelLauncherRuntimeAllowed(path string) bool {
	return modelLauncherQualified(path, model3081RuntimeReceipt)
}

func modelLauncherQualified(path string, verify func(string) object) bool {
	if !same(path, jobsRuntime) {
		return false
	}
	r := verify(path)
	return r["qualified"] == true && r["domain"] == "model-runtime-update-3081-v1" &&
		r["todo_id"] == 3081 && r["target_kind"] == "jobs" && r["data_access_granted"] == false
}

func model3081FlagPresent(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimLeft(arg, "-")
		if arg == "verify-model-runtime-update" || strings.HasPrefix(arg, "verify-model-runtime-update=") {
			return true
		}
	}
	return false
}

// Exactly one purpose flag and one absolute path; no mixed/duplicate flags,
// positionals, manifests, aliases or other purpose can reach the reader.
func runModel3081CLI(args []string, stdout io.Writer, verify func(string) object) int {
	path := ""
	if len(args) == 2 && (args[0] == "--verify-model-runtime-update" || args[0] == "-verify-model-runtime-update") {
		path = args[1]
	} else if len(args) == 1 {
		for _, prefix := range []string{"--verify-model-runtime-update=", "-verify-model-runtime-update="} {
			if strings.HasPrefix(args[0], prefix) {
				path = strings.TrimPrefix(args[0], prefix)
			}
		}
	}
	receipt := object{"qualified": false}
	if filepath.IsAbs(path) {
		receipt = verify(path)
		// Keep only this purpose's bounded software facts on an incomplete
		// candidate. A failed verifier cannot leak arbitrary receipt contents.
		if receipt["qualified"] != true && !(receipt["baseline_verified"] == true && receipt["reason"] == "candidate_unbound") {
			receipt = object{"qualified": false}
		}
	}
	public := object{}
	for _, key := range []string{"qualified", "domain", "todo_id", "target_kind", "current_sha256", "new_sha256", "source_head", "already_current", "baseline_verified", "reason", "data_access_granted", "original_registry_recovered"} {
		if value, ok := receipt[key]; ok {
			public[key] = value
		}
	}
	if json.NewEncoder(stdout).Encode(public) != nil || receipt["qualified"] != true {
		return 1
	}
	return 0
}

func model3081RuntimeReceipt(path string) object {
	return modelRuntimeUpdateWith(path, model3081UpdateLeaves, legacyMatch, permanentRuntimeSourceVerified, modelSourceHead)
}

func modelSourceHead(root, expected string) bool {
	if !civ6Commit.MatchString(expected) {
		return false
	}
	value, err := civ6GitAt(root, "rev-parse", "HEAD")
	return err == nil && strings.TrimSpace(string(value)) == expected
}

func modelRuntimeUpdateWith(path string, leaves []modelUpdateLeaf, protected func(string) bool, source func(string) bool, head func(string, string) bool) object {
	failure := object{"qualified": false}
	for _, leaf := range leaves {
		if !same(path, leaf.target) || !runtimeLeaf(path, filepath.Dir(leaf.target), protected, true) {
			continue
		}
		session := modelPinSession{protected: protected, source: source, head: head}
		defer session.close()
		// Only independently qualified baselines may be read. All receipts
		// stay pinned until the last physical/protection/origin recheck.
		var currentPins []string
		for _, pin := range leaf.baselines {
			if modelDigestValid(pin.sha256) && modelEvidenceIndependent(pin.evidence, path, leaf.candidate.path) && session.evidence(pin.evidence) {
				currentPins = append(currentPins, pin.sha256)
			}
		}
		if len(currentPins) == 0 {
			return failure
		}
		candidate := leaf.candidate
		bound := candidate.root != "" || candidate.path != "" || candidate.head != "" || candidate.sha256 != "" || len(candidate.evidence) != 0
		if bound {
			if !modelDigestValid(candidate.sha256) || !civ6Commit.MatchString(candidate.head) || !within(candidate.root, candidate.path) ||
				!modelCandidateEvidenceIndependent(candidate, leaf) ||
				!session.sourceRoot(candidate.root, candidate.head) || !session.evidence(candidate.evidence) ||
				!session.match(candidate.path, []string{candidate.sha256}) {
				return failure
			}
			// An idempotent match is valid only with the same independently
			// qualified candidate, never with a bare new hash.
			currentPins = append(currentPins, candidate.sha256)
		}
		if !session.match(path, currentPins) || !session.recheck() {
			return failure
		}
		currentSHA := session.files[len(session.files)-1].sha256
		result := object{
			"qualified": bound, "domain": "model-runtime-update-3081-v1", "todo_id": 3081,
			"target_kind": leaf.kind, "current_sha256": currentSHA,
			"data_access_granted": false, "original_registry_recovered": false,
		}
		if !bound {
			result["baseline_verified"] = true
			result["reason"] = "candidate_unbound"
		} else {
			result["new_sha256"] = candidate.sha256
			result["source_head"] = candidate.head
			result["already_current"] = currentSHA == candidate.sha256
		}
		return result
	}
	return failure
}

func modelEvidenceIndependent(evidence []consoleEvidence, artifacts ...string) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, receipt := range evidence {
		for _, artifact := range artifacts {
			if same(receipt.path, artifact) {
				return false
			}
		}
	}
	return true
}

func modelCandidateEvidenceIndependent(candidate modelUpdateCandidate, leaf modelUpdateLeaf) bool {
	if same(candidate.path, leaf.target) || !modelEvidenceIndependent(candidate.evidence, candidate.path, leaf.target) {
		return false
	}
	for _, receipt := range candidate.evidence {
		for _, baseline := range leaf.baselines {
			for _, old := range baseline.evidence {
				if same(receipt.path, old.path) {
					return false
				}
			}
		}
	}
	return true
}

func modelDigestValid(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type modelPinnedFile struct {
	file   *os.File
	path   string
	info   syscall.ByHandleFileInformation
	sha256 string
}

type modelPinSession struct {
	files     []modelPinnedFile
	roots     []struct{ path, head string }
	protected func(string) bool
	source    func(string) bool
	head      func(string, string) bool
}

func (s *modelPinSession) close() {
	for _, file := range s.files {
		file.file.Close()
	}
}

func (s *modelPinSession) sourceRoot(root, head string) bool {
	resolved, err := physical(root)
	if err != nil || !same(resolved, root) || s.protected(root) || !s.source(root) || (head != "" && !s.head(root, head)) {
		return false
	}
	s.roots = append(s.roots, struct{ path, head string }{root, head})
	return true
}

func (s *modelPinSession) evidence(evidence []consoleEvidence) bool {
	if len(evidence) == 0 {
		return false
	}
	for _, receipt := range evidence {
		if !modelDigestValid(receipt.sha256) || !s.sourceRoot(receipt.root, "") || !within(receipt.root, receipt.path) ||
			!s.match(receipt.path, []string{receipt.sha256}) {
			return false
		}
	}
	return true
}

var modelFinalPath = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

func modelHandlePath(h syscall.Handle) (string, bool) {
	buf := make([]uint16, 32768)
	n, _, _ := modelFinalPath.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 || n >= uintptr(len(buf)) {
		return "", false
	}
	path, err := portable(syscall.UTF16ToString(buf[:n]))
	return path, err == nil
}

func (s *modelPinSession) match(path string, pins []string) bool {
	if !runtimeLeaf(path, filepath.Dir(path), s.protected, true) {
		return false
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	// Like consolePinnedFile, but keep this same non-following, non-sharing
	// handle until every receipt/candidate/target has been checked together.
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
	actualPath, ok := modelHandlePath(h)
	if !ok || !same(actualPath, path) {
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
	for _, expected := range pins {
		if pinned.sha256 == expected && s.fileUnchanged(pinned) {
			s.files = append(s.files, pinned)
			kept = true
			return true
		}
	}
	return false
}

func modelFileInfoValid(info syscall.ByHandleFileInformation) bool {
	return info.FileAttributes&(syscall.FILE_ATTRIBUTE_REPARSE_POINT|syscall.FILE_ATTRIBUTE_DIRECTORY) == 0 && info.NumberOfLinks == 1
}

func (s *modelPinSession) fileUnchanged(file modelPinnedFile) bool {
	h := syscall.Handle(file.file.Fd())
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil || !modelFileInfoValid(info) {
		return false
	}
	before := file.info
	before.LastAccessTime = info.LastAccessTime
	resolved, err := physical(file.path)
	actualPath, ok := modelHandlePath(h)
	return before == info && err == nil && same(resolved, file.path) && ok && same(actualPath, file.path) &&
		runtimeLeaf(file.path, filepath.Dir(file.path), s.protected, true)
}

func (s *modelPinSession) recheck() bool {
	for _, root := range s.roots {
		resolved, err := physical(root.path)
		if err != nil || !same(resolved, root.path) || s.protected(root.path) || !s.source(root.path) || (root.head != "" && !s.head(root.path, root.head)) {
			return false
		}
	}
	for _, file := range s.files {
		if !s.fileUnchanged(file) {
			return false
		}
	}
	return true
}
