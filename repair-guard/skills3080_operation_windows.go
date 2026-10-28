package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

var skillsPublicSources = map[string]string{
	"6mile/skills":                                  "920714f17d81e0a95c7a9bc51faf0a7921cd982d",
	"cloudflare/security-audit-skill":               "c1c8a8c1471069fb0e188eeaff69b8e8db6564a8",
	"docker/skills":                                 "38df58b09720d38eb78c2c674675ddf9fa74d441",
	"hirokaji/jp-ui-contracts":                      "4bc933689ba8391c27f558fa7b8d6905a0a2af90",
	"kepano/obsidian-skills":                        "3ccff5338ea700537839b21900aa5358a0402c98",
	"mattpocock/skills":                             "5beb35b040e3de97e54fa9e386d7e42d15c295c2",
	"MengTo/Skills":                                 "398cd41c86ba42fbb30e944ae507f463648f08b7",
	"mizchi/skills":                                 "ab4eb2f7a92a13fb37c225ab427f2f492143ace1",
	"nateherkai/AIS-OS":                             "ce9cb93a1145da5daba84ea37208ec35fd03606e",
	"Nutlope/hallmark":                              "13ac0ec7e148655948100b6396439e481361d690",
	"obra/superpowers":                              "bb92a77741419a4ab5f06e711a283343f1ada0c3",
	"openai/skills":                                 "49f948faa9258a0c61caceaf225e179651397431",
	"Tencent/teamai-cli":                            "8787405e59353a8da33f7eaf6b9781b0b3188cd2",
	"VoltAgent/awesome-design-md":                   "3b605329240ce735b510037cca322f1831fdd7f8",
	"x1xhlol/system-prompts-and-models-of-ai-tools": "162db92f54d717d8ebd12292e155aeef32bb4b3c",
}

func skillsSourceClaimsPreserved(m skillsManifest) bool {
	outputs := map[string]bool{}
	for _, f := range m.Files {
		outputs[f.SHA] = true
	}
	for _, source := range m.Sources {
		if skillsPublicSources[source.Repository] != source.Commit || source.Acquisition != "already-acquired" || !source.License.Accepted || strings.TrimSpace(source.License.Scope) == "" || (!outputs[source.SHA] && !skillsHasMaskedSourceOutput(source.Path, m.Files)) || !outputs[source.License.SHA] {
			return false
		}
	}
	return true
}
func skillsGitHub(args ...string) ([]byte, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, currentGitHubCLI, args...)
	cmd.Env = currentGitHubCLIEnvironment(os.Environ())
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.WaitDelay = time.Second
	raw, e := cmd.Output()
	return raw, e == nil && len(raw) < 16<<20
}
func skillsMerged(commit string) bool {
	if !civ6Commit.MatchString(commit) {
		return false
	}
	b, ok := skillsGitHub("api", "repos/AutoDevJapan/cc-pc-tools-rs", "--jq", "[.id, .full_name, .private] | @tsv")
	if !ok || strings.TrimSpace(string(b)) != "1406818713\tAutoDevJapan/cc-pc-tools-rs\tfalse" {
		return false
	}
	b, ok = skillsGitHub("api", "repos/AutoDevJapan/cc-pc-tools-rs/compare/"+commit+"...main", "--jq", ".status")
	status := strings.TrimSpace(string(b))
	return ok && (status == "ahead" || status == "identical")
}
func skillsManifestBytes(r skillsRequest) ([]byte, bool) {
	if !skillsRelative(r.ManifestPath) || !strings.HasPrefix(r.ManifestPath, "docs/skills-3080-operations/") || !strings.HasSuffix(r.ManifestPath, ".json") || !skillsDigest(r.ManifestSHA) || !skillsMerged(r.ManifestCommit) || !sourceVerified(r.Writer.Workspace) {
		return nil, false
	}
	// Read the immutable public Git blob, never a mutable pathname a second time.
	b, e := permanentGitAt(r.Writer.Workspace, "cat-file", "blob", r.ManifestCommit+":"+r.ManifestPath)
	return b, e == nil && len(b) <= 16<<20 && hashBytes(b) == r.ManifestSHA
}
func skillsModel(model string) string {
	parts := strings.Split(model, "/")
	last := parts[len(parts)-1]
	switch last {
	case "gpt-6.1-sol", "gpt-6-astra", "gpt-6-luna", "claude-sonnet-5-5", "claude-opus-5-5", "claude-haiku-5-5", "gemini-3.1-pro", "gemini-3.8-flash":
		return last
	}
	return ""
}

var skillsAccountIdentifier = regexp.MustCompile(`^[0-9a-f]{6}$`)

func skillsModelIdentifier(model string) bool {
	if skillsModel(model) == "" {
		return false
	}
	parts := strings.Split(model, "/")
	if len(parts) == 1 {
		return true
	}
	if len(parts) != 2 {
		return false
	}
	prefix := parts[0]
	return prefix == "openai" || prefix == "anthropic" || prefix == "google-antigravity" || prefix == "main" || (len(prefix) == 7 && prefix[0] == 'p' && skillsAccountIdentifier.MatchString(prefix[1:]))
}

// Keep the owner's max requirement for GPT/Claude. The observed existing
// Antigravity catalogue offers Gemini 3.1 Pro only low/high; high is its actual
// highest advertised setting, and is never reported as max.
func skillsReviewEffort(model string) string {
	if skillsModel(model) == "gemini-3.1-pro" || skillsModel(model) == "gemini-3.8-flash" {
		return "high"
	}
	if skillsModel(model) != "" {
		return "max"
	}
	return ""
}
func skillsReviewAccepted(received skillsReceived, manifestSHA, writerModel string) bool {
	// A comparison-only review or an actual-use check is a public raw result,
	// never a fresh assertion that every creation output was content-reviewed.
	if c, ok := skillsParsePublicReview(received.Spec); ok && c.Comparison != nil {
		return false
	}
	if _, ok := skillsParseConsumer(received.Spec); ok {
		return false
	}
	if !strings.Contains(string(received.Spec), "SKILLS3080_MANIFEST_SHA256 "+manifestSHA) || skillsModel(writerModel) == "" || len(received.Native) == 0 {
		return false
	}
	requested := skillsModel(str(received.Metadata["model"]))
	if requested == "" || requested == skillsModel(writerModel) {
		return false
	}
	wantEffort := skillsReviewEffort(requested)
	if received.Metadata["effort"] != wantEffort {
		return false
	}
	for _, native := range received.Native {
		if skillsModel(str(native["model"])) != requested || native["effort"] != wantEffort {
			return false
		}
	}
	found := 0
	for _, line := range strings.Split(string(received.Body), "\n") {
		const prefix = "SKILLS3080_ACCEPTANCE "
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		found++
		var receipt struct {
			Domain   string `json:"domain"`
			SHA      string `json:"manifest_sha256"`
			Accepted bool   `json:"accepted"`
			Must     *int   `json:"must"`
		}
		if !skillsStrict([]byte(strings.TrimPrefix(line, prefix)), &receipt) || receipt.Domain != "skills-3080-review-v1" || receipt.SHA != manifestSHA || !receipt.Accepted || receipt.Must == nil || *receipt.Must != 0 {
			return false
		}
	}
	return found == 1
}

func skillsActorCheck(w skillsWriter, reg skillsRegistry, s *skillsFS, caller string) bool {
	if w.Task == 3080 {
		return skillsWriterCheck(w, reg, s, caller)
	}
	row, ok := reg.Tasks[3686]
	if !ok || skillsHeld(row) || w.Task != 3686 || w.Job != caller || row.Project != "cc-pc-tools-rs" || row.Status != "doing" || row.Scheduler.Job != caller || !skillsHasJob(row, caller) || !skillsLatest(row, caller) || !same(row.Scheduler.Worktree, w.Workspace) || !strings.HasPrefix(row.Scheduler.Branch, "scheduler/todo-3686-") || !skillsTaskSpec(s, row, w.SpecSHA) {
		return false
	}
	job, ok := skillsReadJob(s, caller)
	return ok && skillsJobTask(job, 3686) && job["status"] == "running" && skillsActorLive(job) && skillsActorSource(w.Workspace, row.Scheduler.Branch) && same(str(job["cwd"]), w.Workspace) && job["launch_key"] == row.Scheduler.Key
}
func skillsOperation(op, path string) object {
	if op == "consume-skills3080-admitted" {
		return skillsFreshConsumerOperation()
	}
	fail := func(reason string) object { return object{"qualified": false, "reason": reason} }
	// Caller request must be in the original parent or this repair's assigned
	// workspace. Qualification of one source root never qualifies central data.
	caller := os.Getenv("CC_JOB_ID")
	m := currentSubworkJobName.FindStringSubmatch(caller)
	if len(m) != 3 || (m[2] != "3080" && m[2] != "3686") {
		return fail("writer")
	}
	if !filepath.IsAbs(path) || (!within(skillsParentRoot, path) && !strings.Contains(filepath.ToSlash(path), "/todo-3686-")) || !repairOnlyAllowed(path) {
		return fail("request")
	}
	fs := skillsFS{blocked: skillsBlocked}
	defer fs.close()
	raw, ok := fs.read(path, 1<<20)
	var r skillsRequest
	if !ok || !skillsStrict(raw, &r) || r.Domain != "skills-3080-operation-v1" || !within(r.Writer.Workspace, path) {
		return fail("request")
	}
	scope, ok := fs.read(skillsScopePath, 1<<20)
	if !ok || !skillsScopeBytes(scope) {
		return fail("scope")
	}
	ids := []int{3080, 3686, r.Writer.Task}
	if r.Result != nil {
		ids = append(ids, r.Result.Task)
	}
	if r.Review != nil {
		ids = append(ids, r.Review.Task)
	}
	if r.Runtime != nil && r.Runtime.PeerReview != nil {
		ids = append(ids, r.Runtime.PeerReview.Task)
	}
	reg, ok := skillsRegistryRead(ids)
	if !ok || !skillsActorCheck(r.Writer, reg, &fs, caller) {
		return fail("writer")
	}
	reg.writer = r.Writer
	if strings.HasSuffix(op, "-skills3080-revision") {
		return skillsRevisionOperation(op, path, raw, r, reg, &fs)
	}
	if r.Revision != nil {
		return fail("request")
	}
	result := object{"qualified": true, "domain": "skills-3080-purpose-v1", "todo_id": 3080, "general_access_granted": false, "original_registry_recovered": false, "request_sha256": hashBytes(raw)}
	if op == "receive-skills3080-result" {
		if r.Consume != nil || r.Runtime != nil || r.Result == nil || r.Review != nil || r.ManifestCommit != "" || r.ManifestPath != "" || r.ManifestSHA != "" || !skillsResultID(*r.Result) {
			return fail("request")
		}
		received, reason := skillsReceive(*r.Result, reg, &fs, false)
		if reason != "" {
			return fail(reason)
		}
		fresh, ok := skillsRegistryRead(ids)
		if !ok || !skillsActorCheck(r.Writer, fresh, &fs, caller) {
			return fail("writer")
		}
		result["operation"] = "receive_exact_public_result"
		result["final_response"] = string(received.Body)
		result["metadata"] = received.Metadata
		result["native_turn_context"] = received.Native
		return result
	}
	if op == "verify-skills3080-update" || op == "apply-skills3080-update" || op == "recover-skills3080-update" {
		if r.Consume != nil {
			return fail("request")
		}
		if op != "verify-skills3080-update" {
			return skillsRuntimeApply(op, path, raw, r, reg, &fs, ids)
		}
		return skillsRuntimeQualification(r, reg, &fs)
	}
	if op == "consume-skills3080" {
		return skillsConsumeOperation(r, reg, &fs)
	}
	if r.Consume != nil || r.Runtime != nil || r.Result != nil || r.Review == nil || r.Writer.Task != 3080 || !skillsResultID(*r.Review) {
		return fail("request")
	}
	manifestRaw, ok := skillsManifestBytes(r)
	if !ok {
		return fail("manifest")
	}
	manifest, ok := skillsParseManifest(manifestRaw)
	if !ok || manifest.Writer != r.Writer || !skillsSourcesBound(manifest, &fs) {
		return fail("source")
	}
	review, reason := skillsReceive(*r.Review, reg, &fs, true)
	if reason != "" {
		return fail("review")
	}
	writer, ok := skillsReadJob(&fs, caller)
	if !ok || !skillsReviewAccepted(review, r.ManifestSHA, str(writer["model"])) {
		return fail("review")
	}
	if reason := skillsTargetsAbsent(manifest, skillsCentralRoot, &fs); reason != "" {
		return fail(reason)
	}
	result["operation"] = "CreateNew"
	result["manifest_sha256"] = r.ManifestSHA
	result["source_commit"] = r.ManifestCommit
	result["file_count"] = len(manifest.Files)
	result["created_files"] = 0
	var producer skillsProducer
	if op == "create-skills3080-forward" {
		producer, ok = skillsProducerObservation(&fs)
		if !ok || len(manifest.Files) > 128 || skillsForwardInvocation(path, r.Writer.Workspace) == "" {
			return fail("source")
		}
	}
	if op == "qualify-skills3080" {
		result["forward_native_invocation"] = skillsForwardInvocation(path, r.Writer.Workspace)
	}
	if op == "create-skills3080" || op == "create-skills3080-forward" {
		// Re-read current ownership at the actual mutation boundary, rather than
		// consuming a stale preflight receipt. The same manifest bytes stay held.
		fresh, ok := skillsRegistryRead(ids)
		if !ok || !skillsWriterCheck(r.Writer, fresh, &fs, caller) {
			return fail("writer")
		}
		count, reason := skillsCreate(manifest, skillsCentralRoot, &fs)
		if reason != "" {
			return object{"qualified": false, "reason": reason, "created_files": count, "partial_operation_possible": true}
		}
		result["created_files"] = count
		if op == "create-skills3080-forward" {
			evidence, ok := skillsIssueCreated(r, hashBytes(raw), manifest, skillsCentralRoot, &fs, producer)
			if !ok {
				return object{"qualified": false, "reason": "creation_evidence", "created_files": count, "partial_operation_possible": true}
			}
			result["creation_evidence"] = evidence
		}
	}
	return result
}

// Keep a stable hash over the exact projection without exposing private paths.
func skillsProjectionSHA(v any) string { b, _ := json.Marshal(v); return hashBytes(bytes.TrimSpace(b)) }
