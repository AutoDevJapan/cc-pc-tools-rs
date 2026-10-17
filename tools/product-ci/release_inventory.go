package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Read-only preparation for ONE approved merge. This report is not a runner
// plan, and can never grant execution or choose a repository/ref from input.
type releaseBlobPin struct{ role, blob, sha256 string }

var releaseBlobPins = []releaseBlobPin{
	{"promotion", "d7ab459899d19b90ebf5387f9d4ad31bb1ef1076", "143e9ef95539bcdda856a076e7b0eaae85e79bbafef02c9c2bacbd43afb7c22e"},
	{"full-verification", "707e19e6a9b2773a2415527c910ab3bcef11e831", "f70c622350eabb6d38f124c321ddc90bc6ccde2eb23e32d6331d76949df358b4"},
	{"backend-integration", "6236fbd54c52c04bf015b66441c03e3d4d0ae23d", "b554cfb12c91db17fff514cde41ba9e56b0a6aa3114a6849863119bf985aede8"},
	{"deployment-guidance", "a54a8f55b2db0bfeb21331018e5145d9fe4ae936", "ed70bb9c631abfaa272ae56c8b55bf68d445f937f00e3d90292c776489093f2f"},
	{"deployment-script", "ac9257a089da3c6f58a2e44a759ec1241ac39f84", "b45fece77b125195baf07a667a510c22091e4bcfa6ff0f433f7a14140cf8b38d"},
	{"workflow-trust", "127dd323392582c4082060e1c1a7b42f13d6085c", "6b2285edc2756d9810b651d874fb6579ab10e788a2f900b3ce326c14e9ac60ef"},
	{"project-rules", "5fd6af223d13886c7d1fd48dc49115788cb473f5", "4d35dd2966022ba3edb11206be30f1b2584f076e27813a393da172c187057709"},
	{"project-guidance", "3981152968f2f2f227a348239aeaab297f316a96", "d34c3c7fde07a58d286fae8a3490858d072139f07b8642a0d1477235e4b06b46"},
}

type releaseBlobReply struct {
	SHA, Content, Encoding string
	Size                   int
}

// Raw bytes and paths have no exported fields and never enter a JSON report.
type frozenReleaseBlob struct {
	pin  releaseBlobPin
	data []byte
}

func gitBlobHash(b []byte) string {
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "blob %d%c", len(b), 0)
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func freezeReleaseBlob(v releaseBlobReply, pin releaseBlobPin) (frozenReleaseBlob, error) {
	if v.Encoding != "base64" || v.SHA != pin.blob || v.Size < 1 || v.Size > 1<<20 || len(v.Content) > 2<<20 {
		return frozenReleaseBlob{}, errors.New("fixed release blob metadata rejected")
	}
	b, err := base64.StdEncoding.DecodeString(v.Content)
	if err != nil || len(b) != v.Size || gitBlobHash(b) != pin.blob || digest(b) != pin.sha256 {
		return frozenReleaseBlob{}, errors.New("fixed release blob bytes rejected")
	}
	return frozenReleaseBlob{pin: pin, data: bytes.Clone(b)}, nil
}

func (b frozenReleaseBlob) verifiedCopy() ([]byte, error) {
	copy := bytes.Clone(b.data)
	if gitBlobHash(copy) != b.pin.blob || digest(copy) != b.pin.sha256 {
		return nil, errors.New("frozen release bytes changed")
	}
	return copy, nil
}

type releaseBinding struct {
	Role, Blob, SHA256 string
	Bytes              int
}

type releaseInventory struct {
	RepoID              int64
	SourceSHA, Tree     string
	AnalysisScope       string
	ExecutionAuthorized bool
	Bindings            []releaseBinding
	Workflows           []releaseWorkflowInventory
}

type releaseWorkflowInventory struct {
	Role, RawSHA256        string
	RootEnvSecretMentions  int
	RootEnvTokenMentions   int
	WorkflowSecretMentions int
	WorkflowTokenMentions  int
	Aliases                int
	DuplicateKeys          int
	Permissions            releasePermissions
	Jobs                   []releaseJobInventory
}

type releaseJobInventory struct {
	Index                         int
	Role, NameSHA256              string
	HostedUbuntu                  bool
	SecretField                   string
	SecretMentions, TokenMentions int
	ConditionSHA256               string
	ReusableReferenceSHA256       string
	Permissions                   releasePermissions
	Steps                         []releaseStepInventory
}

type releaseStepInventory struct {
	Index                 int
	ConditionSHA256       string
	ScriptSHA256          string
	ActionReferenceSHA256 string
	ShellParsed           bool
	Unresolved            string
	GitHubExpressions     int
	DockerCalls           []releaseDockerCall
}

type releasePermission struct{ Capability, Access, KeySHA256 string }

type releasePermissions struct {
	Declaration string
	SHA256      string
	Entries     []releasePermission
	Unresolved  int
}

var releasePermissionKeys = map[string]bool{
	"actions": true, "checks": true, "contents": true, "deployments": true,
	"discussions": true, "id-token": true, "issues": true, "models": true,
	"packages": true, "pages": true, "pull-requests": true,
	"security-events": true, "statuses": true, "attestations": true,
}

func inspectReleasePermissions(n *yaml.Node) releasePermissions {
	p := releasePermissions{Declaration: "inherited", SHA256: releaseScalarDigest(n)}
	if n == nil {
		return p
	}
	if n.Kind == yaml.ScalarNode && (n.Value == "read-all" || n.Value == "write-all") {
		p.Declaration = n.Value
		return p
	}
	if n.Kind != yaml.MappingNode {
		p.Declaration, p.Unresolved = "unresolved", 1
		return p
	}
	p.Declaration = "explicit-mapping"
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1].Value
		e := releasePermission{KeySHA256: digest([]byte(key)), Capability: "opaque", Access: "unresolved"}
		if releasePermissionKeys[key] {
			e.Capability = key
		} else {
			p.Unresolved++
		}
		if value == "read" || value == "write" || value == "none" {
			e.Access = value
		} else {
			p.Unresolved++
		}
		p.Entries = append(p.Entries, e)
	}
	return p
}

func inspectReleaseInventory(ctx context.Context, read api) (releaseInventory, error) {
	r, err := verifyMerged(ctx, mergedCandidate(), read)
	if err != nil {
		return releaseInventory{}, err
	}
	report, err := readReleaseInventoryBlobs(ctx, r, releaseBlobPins, read)
	if err != nil {
		return releaseInventory{}, err
	}
	after, err := verifyMerged(ctx, mergedCandidate(), read)
	if err != nil || after != r {
		return releaseInventory{}, errors.New("release source changed during inventory")
	}
	return report, nil
}

func readReleaseInventoryBlobs(ctx context.Context, r repository, pins []releaseBlobPin, read api) (releaseInventory, error) {
	report := releaseInventory{RepoID: r.ID, SourceSHA: mergedSHA, Tree: mergedTree, AnalysisScope: "literal shell syntax only; no compose resolution, transitive secret proof, semantic trust digest, or execution eligibility"}
	for _, pin := range pins {
		var reply releaseBlobReply
		if err := read(ctx, "repos/"+r.FullName+"/git/blobs/"+pin.blob, &reply); err != nil {
			return releaseInventory{}, mergedReadFailure("release blob", err)
		}
		frozen, err := freezeReleaseBlob(reply, pin)
		if err != nil {
			return releaseInventory{}, err
		}
		// Consume a copy of exactly the verified bytes, without a second API or
		// filesystem read. Future build contexts must use this same boundary.
		data, err := frozen.verifiedCopy()
		if err != nil {
			return releaseInventory{}, err
		}
		report.Bindings = append(report.Bindings, releaseBinding{pin.role, pin.blob, pin.sha256, len(data)})
		if pin.role == "promotion" || pin.role == "full-verification" || pin.role == "backend-integration" {
			workflow, err := inspectReleaseWorkflow(pin.role, data)
			if err != nil {
				return releaseInventory{}, err
			}
			report.Workflows = append(report.Workflows, workflow)
		}
	}
	return report, nil
}

func releaseYAMLField(n *yaml.Node, key string) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) == 1 {
		return releaseYAMLField(n.Content[0], key)
	}
	if n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func releaseYAMLScalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

func releaseScalarDigest(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	b, _ := json.Marshal(n) // Only the hash, never this serialization, leaves RAM.
	return digest(b)
}

func releaseExpressionMentions(n *yaml.Node) (secret, token int) {
	if n == nil {
		return 0, 0
	}
	for _, expression := range releaseGHAExpression.FindAllString(n.Value, -1) {
		secret += len(releaseSecretRoot.FindAllString(expression, -1))
		token += len(releaseTokenRoot.FindAllString(expression, -1))
	}
	for _, child := range n.Content { // Do not follow aliases into a cyclic graph.
		s, t := releaseExpressionMentions(child)
		secret, token = secret+s, token+t
	}
	return secret, token
}

func releaseYAMLShape(n *yaml.Node, depth int, nodes *int) (aliases, duplicates int, err error) {
	*nodes++
	if depth > 128 || *nodes > 100000 {
		return 0, 0, errors.New("release YAML size rejected")
	}
	if n.Kind == yaml.AliasNode {
		aliases++
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i].Value
			if seen[key] {
				duplicates++
			}
			seen[key] = true
		}
	}
	for _, child := range n.Content {
		a, d, e := releaseYAMLShape(child, depth+1, nodes)
		if e != nil {
			return 0, 0, e
		}
		aliases, duplicates = aliases+a, duplicates+d
	}
	return
}

func inspectReleaseWorkflow(role string, data []byte) (releaseWorkflowInventory, error) {
	var root yaml.Node
	if len(data) > 1<<20 || yaml.Unmarshal(data, &root) != nil {
		return releaseWorkflowInventory{}, errors.New("fixed release YAML invalid")
	}
	w := releaseWorkflowInventory{Role: role, RawSHA256: digest(data)}
	nodes := 0
	var err error
	w.Aliases, w.DuplicateKeys, err = releaseYAMLShape(&root, 0, &nodes)
	if err != nil {
		return releaseWorkflowInventory{}, err
	}
	// Reject ambiguous metadata; do not guess GitHub's alias/merge-key behavior.
	if w.Aliases != 0 || w.DuplicateKeys != 0 {
		return releaseWorkflowInventory{}, errors.New("ambiguous release YAML rejected")
	}
	w.RootEnvSecretMentions, w.RootEnvTokenMentions = releaseExpressionMentions(releaseYAMLField(&root, "env"))
	w.WorkflowSecretMentions, w.WorkflowTokenMentions = releaseExpressionMentions(&root)
	w.Permissions = inspectReleasePermissions(releaseYAMLField(&root, "permissions"))
	jobs := releaseYAMLField(&root, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode || len(jobs.Content) == 0 || len(jobs.Content) > 64 {
		return releaseWorkflowInventory{}, errors.New("release job structure rejected")
	}
	defaultShell := releaseYAMLField(releaseYAMLField(releaseYAMLField(&root, "defaults"), "run"), "shell")
	for i := 0; i+1 < len(jobs.Content); i += 2 {
		name, job := jobs.Content[i].Value, jobs.Content[i+1]
		j := releaseJobInventory{Index: i/2 + 1, Role: role, NameSHA256: digest([]byte(name)), SecretField: "absent"}
		if role == "promotion" {
			j.Role = "reusable-verification"
			if name == "preconditions" || name == "promote" {
				j.Role = name
			}
		}
		j.HostedUbuntu = releaseYAMLScalar(releaseYAMLField(job, "runs-on")) == "ubuntu-latest"
		j.ConditionSHA256 = releaseScalarDigest(releaseYAMLField(job, "if"))
		j.ReusableReferenceSHA256 = releaseScalarDigest(releaseYAMLField(job, "uses"))
		j.Permissions = inspectReleasePermissions(releaseYAMLField(job, "permissions"))
		j.SecretMentions, j.TokenMentions = releaseExpressionMentions(job)
		if s := releaseYAMLField(job, "secrets"); s != nil {
			j.SecretField = "other"
			if s.Kind == yaml.MappingNode {
				j.SecretField = "mapping"
			} else if s.Kind == yaml.ScalarNode && s.Value == "inherit" {
				j.SecretField = "inherit"
			}
		}
		jobShell := releaseYAMLField(releaseYAMLField(releaseYAMLField(job, "defaults"), "run"), "shell")
		if jobShell == nil {
			jobShell = defaultShell
		}
		steps := releaseYAMLField(job, "steps")
		if steps != nil && (steps.Kind != yaml.SequenceNode || len(steps.Content) > 200) {
			return releaseWorkflowInventory{}, errors.New("release step structure rejected")
		}
		if steps != nil {
			for index, step := range steps.Content {
				s := releaseStepInventory{Index: index + 1, ConditionSHA256: releaseScalarDigest(releaseYAMLField(step, "if"))}
				s.ActionReferenceSHA256 = releaseScalarDigest(releaseYAMLField(step, "uses"))
				if run := releaseYAMLField(step, "run"); run != nil {
					s.ScriptSHA256 = digest([]byte(run.Value))
					shell := releaseYAMLField(step, "shell")
					if shell == nil {
						shell = jobShell
					}
					shellName := strings.TrimSpace(releaseYAMLScalar(shell))
					if shellName != "" && shellName != "bash" && shellName != "sh" && !strings.HasPrefix(shellName, "bash ") {
						s.Unresolved = "non-shell-script"
					} else {
						s.DockerCalls, s.GitHubExpressions, err = inspectReleaseShell(run.Value)
						if err != nil {
							s.Unresolved = "shell-syntax-unresolved"
						} else {
							s.ShellParsed = true
						}
					}
				}
				j.Steps = append(j.Steps, s)
			}
		}
		w.Jobs = append(w.Jobs, j)
	}
	return w, nil
}
