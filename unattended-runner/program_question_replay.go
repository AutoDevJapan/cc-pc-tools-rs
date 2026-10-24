package main

// A replay receipt is minted only after an isolated, public-checksum-verified
// build reproduces every original PNG byte. It is not historical environment
// reconstruction, imagegen evidence, approval, or a new source classification.
import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const programReplayDomain = "owner-program-drawing-replay-v1"

type programReplayFile struct {
	Path string `json:"path"`
	SHA  string `json:"sha256"`
}
type programReplayInput struct {
	Binding string              `json:"binding_sha256"`
	Program string              `json:"program_sha256"`
	Module  string              `json:"module_sha256"`
	GoTool  string              `json:"go_tool_sha256"`
	Fonts   []programReplayFile `json:"fonts"`
}
type programReplayModule struct{ Path, Version, Sum, GoModSum string }
type programReplayReceipt struct {
	Domain  string                 `json:"domain"`
	Input   programReplayInput     `json:"input"`
	SumSHA  string                 `json:"go_sum_sha256"`
	Modules []programReplayModule  `json:"modules"`
	Images  []programQuestionImage `json:"original_images"`
	At      time.Time              `json:"replayed_at"`
}
type programReplayEnvelope struct {
	Payload json.RawMessage `json:"payload"`
	MAC     string          `json:"mac"`
}
type programReplayOwned struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}
type programReplayRegistry struct {
	Home, GoTool       string
	Read               func(string, int64) ([]byte, error)
	Protect, Unprotect func([]byte) ([]byte, error)
	Produce            func(context.Context, programQuestionEvidence, []byte, []byte) ([]programReplayModule, string, *programReplayOwned, error)
	Owned              *programReplayOwned
}

func programReplayProduction() *programReplayRegistry {
	return &programReplayRegistry{Home: "D:/CC/Tools/discord-ask/unattended/program-question-replays", GoTool: "C:/Program Files/Go/bin/go.exe", Read: generatedQuestionRead, Protect: generatedQuestionProtect, Unprotect: generatedQuestionUnprotect, Produce: programReplayProduce}
}
func programReplayMAC(key, payload []byte) string {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write([]byte(programReplayDomain + "\x00"))
	_, _ = m.Write(payload)
	return hex.EncodeToString(m.Sum(nil))
}
func (r *programReplayRegistry) input(e programQuestionEvidence, source, module []byte) (programReplayInput, error) {
	input := programReplayInput{Binding: canonicalHash(e), Program: bytesHash(source), Module: bytesHash(module)}
	tool, err := r.Read(r.GoTool, 128<<20)
	if err != nil {
		return input, errConversationMedia
	}
	input.GoTool = bytesHash(tool)
	font := regexp.MustCompile(`"(C:/Windows/Fonts/[A-Za-z0-9_-]+\.ttf)"`)
	seen := map[string]bool{}
	for _, match := range font.FindAllSubmatch(source, -1) {
		path := string(match[1])
		if seen[path] {
			continue
		}
		seen[path] = true
		b, err := r.Read(path, 32<<20)
		if err != nil {
			return input, errConversationMedia
		}
		input.Fonts = append(input.Fonts, programReplayFile{Path: path, SHA: bytesHash(b)})
	}
	sort.Slice(input.Fonts, func(i, j int) bool { return input.Fonts[i].Path < input.Fonts[j].Path })
	if len(input.Fonts) == 0 || !programModule(module) {
		return input, errConversationMedia
	}
	return input, nil
}
func (r *programReplayRegistry) key(create bool) ([]byte, error) {
	if create {
		if _, err := os.Lstat(filepath.Join(r.Home, "purpose-key.dpapi")); os.IsNotExist(err) {
			existing, scanErr := filepath.Glob(filepath.Join(r.Home, "*.json"))
			if scanErr != nil || len(existing) > 0 {
				return nil, errConversationMedia
			}
		}
		if os.MkdirAll(r.Home, 0700) != nil {
			return nil, errConversationMedia
		}
	}
	// Reuse only the DPAPI/key-publication primitive, with a separate store and
	// MAC domain. No imagegen bundle or generation receipt is manufactured.
	k := generatedQuestionRegistry{Home: r.Home, Read: r.Read, Protect: r.Protect, Unprotect: r.Unprotect}
	return k.key(create)
}
func (r *programReplayRegistry) receiptPath(input programReplayInput) string {
	return filepath.Join(r.Home, canonicalHash(input)+".json")
}
func (r *programReplayRegistry) check(ctx context.Context, e programQuestionEvidence, source, module []byte) error {
	input, err := r.input(e, source, module)
	if err != nil || ctx.Err() != nil {
		return errConversationMedia
	}
	return r.checkInput(e, input)
}
func (r *programReplayRegistry) checkInput(e programQuestionEvidence, input programReplayInput) error {
	key, err := r.key(false)
	if err != nil {
		return errConversationMedia
	}
	defer eraseGeneratedKey(key)
	b, err := r.Read(r.receiptPath(input), 1<<20)
	if err != nil {
		return errConversationMedia
	}
	var envelope programReplayEnvelope
	if historyStrictJSON(b, &envelope) != nil || !hmac.Equal([]byte(programReplayMAC(key, envelope.Payload)), []byte(envelope.MAC)) {
		return errConversationMedia
	}
	var receipt programReplayReceipt
	if historyStrictJSON(envelope.Payload, &receipt) != nil || receipt.Domain != programReplayDomain || canonicalHash(receipt.Input) != canonicalHash(input) || canonicalHash(receipt.Images) != canonicalHash(e.Images) || !shaPattern.MatchString(receipt.SumSHA) || !programReplayModules(receipt.Modules) || receipt.At.IsZero() {
		return errConversationMedia
	}
	return nil
}
func (r *programReplayRegistry) register(ctx context.Context, e programQuestionEvidence, source, module []byte) error {
	input, err := r.input(e, source, module)
	if err != nil {
		return errConversationMedia
	}
	if r.checkInput(e, input) == nil {
		return nil
	}
	if _, err := os.Lstat(r.receiptPath(input)); !os.IsNotExist(err) {
		return errConversationMedia
	}
	modules, sum, owned, err := r.Produce(ctx, e, source, module)
	r.Owned = owned
	if err != nil || !programReplayModules(modules) || !shaPattern.MatchString(sum) {
		return errConversationMedia
	}
	after, err := r.input(e, source, module)
	if err != nil || canonicalHash(after) != canonicalHash(input) {
		return errConversationMedia
	}
	key, err := r.key(true)
	if err != nil {
		return errConversationMedia
	}
	defer eraseGeneratedKey(key)
	payload, _ := json.Marshal(programReplayReceipt{Domain: programReplayDomain, Input: input, SumSHA: sum, Modules: modules, Images: e.Images, At: time.Now().UTC()})
	b, _ := json.Marshal(programReplayEnvelope{Payload: payload, MAC: programReplayMAC(key, payload)})
	if err = generatedWriteNew(r.receiptPath(input), b); err != nil && !os.IsExist(err) {
		return errConversationMedia
	}
	return r.checkInput(e, input)
}
func programReplayModules(modules []programReplayModule) bool {
	// x/image's authenticated graph also includes Go's tooling modules even
	// when they are not linked into the illustration. These remain public,
	// pinned module archives, never local or ambient replacement directories.
	public := map[string]bool{"golang.org/x/image": true, "golang.org/x/text": true, "golang.org/x/tools": true, "golang.org/x/mod": true, "golang.org/x/sync": true}
	if len(modules) < 1 || len(modules) > len(public) {
		return false
	}
	seen := map[string]bool{}
	for _, m := range modules {
		if seen[m.Path] || !public[m.Path] || !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(m.Version) || !regexp.MustCompile(`^h1:[A-Za-z0-9+/]{43}=$`).MatchString(m.Sum) || !regexp.MustCompile(`^h1:[A-Za-z0-9+/]{43}=$`).MatchString(m.GoModSum) {
			return false
		}
		seen[m.Path] = true
	}
	return seen["golang.org/x/image"]
}
func programReplayDecodeModules(raw []byte) ([]programReplayModule, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	var result []programReplayModule
	for {
		var record struct {
			Path, Version, Sum, GoModSum string
			Replace, Error               json.RawMessage
		}
		err := d.Decode(&record)
		if err == io.EOF {
			break
		}
		if err != nil || len(record.Replace) > 0 || len(record.Error) > 0 {
			return nil, errConversationMedia
		}
		result = append(result, programReplayModule{record.Path, record.Version, record.Sum, record.GoModSum})
	}
	if !programReplayModules(result) {
		return nil, errConversationMedia
	}
	return result, nil
}
func programReplayRegisterCLI(args []string, out io.Writer) int {
	r := programReplayProduction()
	d := programQuestionProduction()
	d.Replay = r.register
	d.Budget = 2 * time.Minute
	// The normal check path validates task/source/native records first. Only
	// this explicit registration can execute the isolated reproducibility run.
	var buffer bytes.Buffer
	code := programQuestionCLI(args, &buffer, d)
	var proof programQuestionProof
	if code != 0 || json.Unmarshal(buffer.Bytes(), &proof) != nil {
		_ = json.NewEncoder(out).Encode(map[string]any{"domain": programReplayDomain, "registered": false, "replay_ownership": r.Owned})
		return 1
	}
	if json.NewEncoder(out).Encode(map[string]any{"domain": programReplayDomain, "registered": true, "qualification": proof, "replay_ownership": r.Owned}) != nil {
		return 1
	}
	return 0
}
