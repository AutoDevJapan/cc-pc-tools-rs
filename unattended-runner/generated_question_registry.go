package main

// This purpose-specific store contains signed selections of native evidence,
// never an allow-list of source classifications. Registration and consumption
// both validate the original evidence and bytes.
import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const generatedEvidenceDomain = "owner-generated-question-evidence-v1"

var generatedUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type generatedContentReview struct {
	New            bool   `json:"new_generation"`
	Fictional      bool   `json:"fictional_data"`
	NoConfidential bool   `json:"no_customer_or_secret_content"`
	Reference      string `json:"reference"`
}
type generatedQuestionBundle struct {
	Domain     string                 `json:"domain"`
	Scope      generatedQuestionScope `json:"scope"`
	Session    string                 `json:"session"`
	Proof      string                 `json:"proof_path"`
	ProofSHA   string                 `json:"proof_sha256"`
	Rollout    string                 `json:"rollout_path"`
	Created    time.Time              `json:"created_at"`
	Expires    time.Time              `json:"expires_at"`
	Cell       string                 `json:"exec_cell_id"`
	Content    generatedContentReview `json:"content_review"`
	Images     []generatedImageBound  `json:"images"`
	Events     []generatedEventBound  `json:"events"`
	SessionSHA string                 `json:"session_record_sha256"`
	Assignment generatedEventBound    `json:"assignment_event"`
}
type generatedQuestionEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	MAC     string          `json:"mac"`
}
type generatedQuestionRegistry struct {
	Home, Artifacts, Sessions string
	Guard                     func(context.Context, string) (bool, error)
	Read                      func(string, int64) ([]byte, error)
	Task                      func(uint64) bool
	Destination               func(generatedQuestionScope) bool
	SourceClear               func(string) bool
	Now                       func() time.Time
	Protect, Unprotect        func([]byte) ([]byte, error)
}

func generatedQuestionRegistryProduction() generatedQuestionRegistry {
	return generatedQuestionRegistry{
		Home:      "D:/CC/Tools/discord-ask/unattended/generated-question-media",
		Artifacts: "C:/Users/makhmeto/.codex/generated_images", Sessions: "C:/Users/makhmeto/.codex/sessions",
		Guard: conversationMediaLiveGuard, Read: generatedQuestionRead, Task: generatedQuestionTask,
		Destination: func(scope generatedQuestionScope) bool {
			var cfg Config
			var dst Destination
			return readJSON(conversationDefaultConfig, 1<<20, &cfg) == nil && readJSON(cfg.NotifyConfig, 1<<20, &dst) == nil && scope.Owner == dst.Owner && scope.Guild == dst.Guild && scope.Forum == dst.Forum
		}, SourceClear: generatedQuestionSourceClear, Now: time.Now, Protect: generatedQuestionProtect, Unprotect: generatedQuestionUnprotect,
	}
}

func generatedStrictJSON(b []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return errConversationMedia
	}
	if d.Decode(new(any)) != io.EOF {
		return errConversationMedia
	}
	return nil
}

func generatedWithin(root, path string) bool {
	r, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(path))
	return err == nil && r != "." && r != ".." && !filepath.IsAbs(r) && !strings.HasPrefix(r, ".."+string(filepath.Separator))
}

func (r generatedQuestionRegistry) spec(b generatedQuestionBundle) (generatedQuestionSpec, error) {
	s := generatedQuestionSpec{}
	for _, id := range []string{b.Scope.Owner, b.Scope.Guild, b.Scope.Forum} {
		if _, ok := number(id); !ok {
			return s, errConversationMedia
		}
	}
	if b.Domain != generatedEvidenceDomain || b.Scope.Todo == 0 || !shaPattern.MatchString(b.Scope.Body) || !shaPattern.MatchString(b.Scope.Decision) || !generatedUUID.MatchString(b.Session) || !b.Content.New || !b.Content.Fictional || !b.Content.NoConfidential || strings.TrimSpace(b.Content.Reference) == "" || !r.Destination(b.Scope) || !r.Task(b.Scope.Todo) || !shaPattern.MatchString(b.ProofSHA) || len(b.Images) == 0 || len(b.Images) > 10 || (len(b.Events) != 2 && len(b.Events) != 4) || b.Cell == "" {
		return s, errConversationMedia
	}
	if !generatedWithin(r.Sessions, b.Rollout) || !strings.HasSuffix(filepath.Base(filepath.FromSlash(b.Rollout)), "-"+b.Session+".jsonl") {
		return s, errConversationMedia
	}
	seen := map[string]bool{}
	for i, image := range b.Images {
		parent := filepath.Join(filepath.FromSlash(r.Artifacts), b.Session)
		name := filepath.Base(filepath.FromSlash(image.Path))
		if !conversationMediaSamePath(filepath.Dir(filepath.FromSlash(image.Path)), parent) || !strings.HasPrefix(name, "exec-") || !strings.HasSuffix(name, ".png") || !generatedUUID.MatchString(strings.TrimSuffix(strings.TrimPrefix(name, "exec-"), ".png")) || seen[strings.ToLower(image.Path)] || !shaPattern.MatchString(image.SHA256) || !shaPattern.MatchString(image.RecordSHA) || !shaPattern.MatchString(image.PromptSHA) || image.Size <= 0 || image.Size > conversationMediaLimit || image.Order != i+1 || image.Revision < 1 || image.Option == "" {
			return s, errConversationMedia
		}
		seen[strings.ToLower(image.Path)] = true
	}
	previous := 0
	for _, event := range b.Events {
		if event.Line <= previous || !shaPattern.MatchString(event.SHA) || event.Call == "" {
			return s, errConversationMedia
		}
		previous = event.Line
	}
	s = generatedQuestionSpec{Scope: b.Scope, Session: b.Session, Proof: b.Proof, ProofSHA: b.ProofSHA, Rollout: b.Rollout, Images: b.Images, Events: b.Events, Cell: b.Cell, Created: b.Created, Expires: b.Expires, Read: r.Read, Guard: r.Guard, Records: generatedQuestionRecords, Task: func() bool { return r.Task(b.Scope.Todo) && r.Destination(b.Scope) }, SourceClear: r.SourceClear, Now: r.Now}
	s.SessionSHA = b.SessionSHA
	s.Assignment = b.Assignment
	if !shaPattern.MatchString(s.SessionSHA) || s.Assignment.Line <= 1 || !shaPattern.MatchString(s.Assignment.SHA) || s.Assignment.Line >= s.Events[0].Line {
		return generatedQuestionSpec{}, errConversationMedia
	}
	return s, nil
}

func generatedReceiptName(scope generatedQuestionScope, path string) string {
	b, _ := json.Marshal(scope)
	return bytesHash(append(append(b, 0), []byte(strings.ToLower(strings.ReplaceAll(filepath.Clean(filepath.FromSlash(path)), `\`, "/")))...)) + ".json"
}

func (r generatedQuestionRegistry) key(create bool) ([]byte, error) {
	path := filepath.Join(filepath.FromSlash(r.Home), "purpose-key.dpapi")
	_, statErr := os.Lstat(path)
	if statErr == nil {
		b, err := r.Read(path, 4096)
		if err != nil {
			return nil, errConversationMedia
		}
		key, e := r.Unprotect(b)
		if e != nil || len(key) != 32 {
			return nil, errConversationMedia
		}
		return key, nil
	}
	if !create || !os.IsNotExist(statErr) {
		return nil, errConversationMedia
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, errConversationMedia
	}
	sealed, err := r.Protect(key)
	if err != nil {
		return nil, errConversationMedia
	}
	err = generatedWriteNew(path, sealed)
	if os.IsExist(err) {
		return r.key(false)
	}
	if err != nil {
		return nil, errConversationMedia
	}
	return key, nil
}

func generatedWriteNew(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".purpose-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Publishing a finished inode is atomic and never replaces an existing key
	// or receipt. A crash cannot leave an empty final file that blocks recovery.
	return os.Link(f.Name(), path)
}

func generatedMAC(key, payload []byte) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(generatedEvidenceDomain + "\x00"))
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}
func eraseGeneratedKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

func (r generatedQuestionRegistry) check(ctx context.Context, evidence string) (generatedQuestionBundle, error) {
	empty := generatedQuestionBundle{}
	allowed, err := r.Guard(ctx, evidence)
	if err != nil || !allowed {
		return empty, errConversationMedia
	}
	b, err := r.Read(evidence, 512<<10)
	if err != nil {
		return empty, errConversationMedia
	}
	var bundle generatedQuestionBundle
	if generatedStrictJSON(b, &bundle) != nil {
		return empty, errConversationMedia
	}
	s, err := r.spec(bundle)
	if err != nil {
		return empty, errConversationMedia
	}
	for _, image := range bundle.Images {
		if _, err = qualifyGeneratedQuestionImage(ctx, image.Path, bundle.Scope, s); err != nil {
			return empty, errConversationMedia
		}
	}
	return bundle, nil
}

func (r generatedQuestionRegistry) register(ctx context.Context, evidence string) (int, error) {
	bundle, err := r.check(ctx, evidence)
	if err != nil {
		return 0, errConversationMedia
	}
	if err = os.MkdirAll(filepath.FromSlash(r.Home), 0700); err != nil {
		return 0, errConversationMedia
	}
	physical, _, err := conversationMediaPhysical(filepath.FromSlash(r.Home))
	if err != nil || !conversationMediaSamePath(physical, filepath.FromSlash(r.Home)) {
		return 0, errConversationMedia
	}
	key, err := r.key(true)
	if err != nil {
		return 0, errConversationMedia
	}
	defer eraseGeneratedKey(key)
	payload, err := json.Marshal(bundle)
	if err != nil {
		return 0, errConversationMedia
	}
	envelope, _ := json.Marshal(generatedQuestionEnvelope{Payload: payload, MAC: generatedMAC(key, payload)})
	for _, image := range bundle.Images {
		path := filepath.Join(physical, generatedReceiptName(bundle.Scope, image.Path))
		openErr := generatedWriteNew(path, envelope)
		if os.IsExist(openErr) {
			old, readErr := r.Read(path, 512<<10)
			if readErr != nil || !bytes.Equal(old, envelope) {
				return 0, errConversationMedia
			}
			continue
		}
		if openErr != nil {
			return 0, errConversationMedia
		}
	}
	return len(bundle.Images), nil
}

func (r generatedQuestionRegistry) load(ctx context.Context, path string, scope generatedQuestionScope) (generatedQuestionSpec, error) {
	empty := generatedQuestionSpec{}
	// Rust canonicalize uses the Windows verbatim spelling. Normalize only its
	// syntax before the local receipt lookup; never stat an unknown caller path.
	path, pathErr := conversationMediaPath(path)
	if pathErr != nil {
		return empty, errConversationMedia
	}
	root, _, err := conversationMediaPhysical(filepath.FromSlash(r.Home))
	if err != nil || !conversationMediaSamePath(root, filepath.FromSlash(r.Home)) {
		return empty, errConversationMedia
	}
	b, err := r.Read(filepath.Join(root, generatedReceiptName(scope, path)), 512<<10)
	if err != nil {
		return empty, errConversationMedia
	}
	var e generatedQuestionEnvelope
	if generatedStrictJSON(b, &e) != nil {
		return empty, errConversationMedia
	}
	key, err := r.key(false)
	if err != nil {
		return empty, errConversationMedia
	}
	defer eraseGeneratedKey(key)
	actual, err := hex.DecodeString(e.MAC)
	expected, _ := hex.DecodeString(generatedMAC(key, e.Payload))
	if err != nil || !hmac.Equal(actual, expected) {
		return empty, errConversationMedia
	}
	var bundle generatedQuestionBundle
	if generatedStrictJSON(e.Payload, &bundle) != nil || bundle.Scope != scope || ctx.Err() != nil {
		return empty, errConversationMedia
	}
	return r.spec(bundle)
}
