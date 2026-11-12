package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// This only binds retry inputs. The existing sender independently checks the
// original live guard, source receipt and exact upload bytes before any POST.
func conversationQuestionImages(paths []string, check ...func(string) (conversationMediaVerification, error)) ([]string, []string, error) {
	var verify func(string) (conversationMediaVerification, error)
	if len(check) == 1 {
		verify = check[0]
	}
	return conversationQuestionImagesWithRead(paths, verify, conversationMediaRead)
}

func conversationQuestionImagesWithRead(paths []string, verify func(string) (conversationMediaVerification, error), read func(string, int64) ([]byte, os.FileInfo, error)) ([]string, []string, error) {
	if len(paths) > 10 {
		return nil, nil, errors.New("conversation question image limit exceeded")
	}
	var canonical, hashes []string
	var total int
	seen := make(map[string]bool)
	for _, path := range paths {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, nil, errConversationMedia
		}
		var proof *conversationMediaVerification
		if strings.EqualFold(filepath.Ext(absolute), ".mp4") {
			if verify == nil {
				return nil, nil, errConversationMedia
			}
			p, err := verify(absolute)
			if err != nil || !p.Verified || p.Domain != conversationMediaDomain || p.MIME != "video/mp4" || p.Extension != "mp4" || p.Size < 1 || p.Size > conversationVideoLimit || !shaPattern.MatchString(p.SHA256) || p.QuestionQualification != nil {
				return nil, nil, errConversationMedia
			}
			proof = &p
		}
		data, _, err := read(absolute, conversationAttachmentLimit(absolute))
		if err != nil || len(data) == 0 || seen[absolute] {
			clear(data)
			return nil, nil, errConversationMedia
		}
		if _, _, err = conversationAttachmentMagic(data, absolute); err != nil {
			clear(data)
			return nil, nil, err
		}
		if proof != nil && (proof.Size != int64(len(data)) || proof.SHA256 != bytesHash(data)) {
			clear(data)
			return nil, nil, errConversationMedia
		}
		total += len(data)
		if total > 23<<20 {
			clear(data)
			return nil, nil, errConversationMedia
		}
		seen[absolute] = true
		canonical = append(canonical, absolute)
		hashes = append(hashes, bytesHash(data))
		clear(data)
	}
	return canonical, hashes, nil
}

func conversationQuestionImageRecord(q ConversationQuestion) bool {
	if len(q.Attachments) != len(q.AttachmentHashes) || len(q.Attachments) > 10 {
		return false
	}
	for i, path := range q.Attachments {
		if !filepath.IsAbs(path) || path != filepath.Clean(path) || !shaPattern.MatchString(q.AttachmentHashes[i]) {
			return false
		}
	}
	return true
}

func conversationQuestionRetryImages(index *conversationIndex, input ConversationAskInput, check ...func(string) (conversationMediaVerification, error)) ([]string, []string, error) {
	q, exists := index.Questions[input.RequestID]
	if alias, ok := index.Aliases[input.RequestID]; ok {
		q = index.Questions[alias.Target]
		exists = true
		if alias.ImagePaths != nil || alias.ImageHashes != nil {
			q.Attachments, q.AttachmentHashes = alias.ImagePaths, alias.ImageHashes
		} else if alias.Intent != nil {
			q.Attachments, q.AttachmentHashes = alias.Intent.Attachments, alias.Intent.AttachmentHashes
		}
	}
	if !exists {
		return conversationQuestionImages(input.Attachments, check...)
	}
	if !conversationQuestionImageRecord(q) || len(input.Attachments) != len(q.Attachments) {
		return nil, nil, errConversationMedia
	}
	for i, path := range input.Attachments {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, nil, errConversationMedia
		}
		given, err := conversationMediaPath(absolute)
		if err != nil {
			return nil, nil, errConversationMedia
		}
		saved, err := conversationMediaPath(q.Attachments[i])
		if err != nil || !conversationMediaSamePath(given, saved) {
			return nil, nil, errConversationMedia
		}
	}
	return q.Attachments, q.AttachmentHashes, nil
}

// The normal sender's read-only media verifier qualifies the original source
// before this fresh-question path reads or hashes MP4 bytes. It also resolves
// a bridge snapshot's source descriptor and exact sender namespace.
func (r *Runner) conversationQuestionVideoCheck(ctx context.Context, input ConversationAskInput) func(string) (conversationMediaVerification, error) {
	return func(path string) (conversationMediaVerification, error) {
		deps := conversationMediaDependencies{Home: filepath.Dir(r.Config.Home), VideoTitle: input.Title, VideoOrigin: input.OriginThread}
		deps.Guard = func(c context.Context, original string) (bool, error) {
			if r.Run == nil || c.Err() != nil {
				return false, errConversationMedia
			}
			raw, err := r.Run(c, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", original)
			var p struct {
				Allowed    *bool `json:"access_allowed"`
				Client     *bool `json:"client_resource"`
				Unverified *bool `json:"classification_unverified"`
			}
			var fields map[string]json.RawMessage
			if err != nil || len(raw) > 64<<10 || historyStrictJSON([]byte(raw), &fields) != nil || json.Unmarshal([]byte(raw), &p) != nil || p.Allowed == nil || !*p.Allowed || p.Client == nil || p.Unverified == nil {
				return false, errConversationMedia
			}
			for key := range fields {
				switch key {
				case "ok", "access_allowed", "client_resource", "classification_unverified", "classification_basis", "classification_policy", "ci_retirement_qualified", "original_registry_recovered", "protection_categories", "protection_status", "scope":
				default:
					return false, errConversationMedia
				}
			}
			return !*p.Client && !*p.Unverified, nil
		}
		deps.VideoQuestion = func(_ context.Context, snapshot, sender string, _ conversationMediaSource, _ *generatedQuestionScope) error {
			if !strings.HasPrefix(input.RequestID, "normal-ask-") || sender != "conversation-v1-"+bytesHash([]byte(input.RequestID))[:32] || filepath.Base(filepath.Dir(snapshot)) != input.RequestID {
				return errConversationMedia
			}
			return nil
		}
		return verifyConversationMedia(ctx, path, "conversation-v1-"+bytesHash([]byte(input.RequestID))[:32], deps)
	}
}

// The external sender requires the actual normal-question reservation, not a
// caller-selected identifier prefix. Read only this selected question's fields.
func registeredConversationVideoQuestion(home string) func(context.Context, string, string, conversationMediaSource, *generatedQuestionScope) error {
	return registeredConversationVideoQuestionWithContext(home, "", "")
}

func registeredConversationVideoQuestionWithContext(home, title, origin string) func(context.Context, string, string, conversationMediaSource, *generatedQuestionScope) error {
	return func(ctx context.Context, path, request string, source conversationMediaSource, scope *generatedQuestionScope) error {
		if scope == nil || ctx.Err() != nil {
			return errConversationMedia
		}
		key := filepath.Base(filepath.Dir(path))
		if !strings.HasPrefix(key, "normal-ask-") || !conversationMediaNormal.MatchString(key) || request != "conversation-v1-"+bytesHash([]byte(key))[:32] {
			return errConversationMedia
		}
		raw, _, err := conversationMediaRead(filepath.Join(home, "unattended", "conversation-questions.json"), maxState)
		if err != nil {
			return errConversationMedia
		}
		defer clear(raw)
		var index conversationIndex
		if historyStrictJSON(raw, &index) != nil || index.Domain != conversationDomain || index.Destination.Owner != scope.Owner || index.Destination.Guild != scope.Guild || index.Destination.Forum != scope.Forum {
			return errConversationMedia
		}
		q, ok := index.Questions[key]
		if !ok || q.RequestID != key || q.SenderID != request || q.Todo != scope.Todo || q.Phase != "reserved" || q.Project == "" || title == "" || origin == "" || q.OriginThread != origin || len(q.Attachments) != len(q.AttachmentHashes) {
			return errConversationMedia
		}
		matched := false
		for i, p := range q.Attachments {
			if conversationMediaSamePath(p, path) && q.AttachmentHashes[i] == source.SHA256 {
				matched = true
			}
		}
		if !matched {
			return errConversationMedia
		}
		dir := filepath.Join(home, "unattended", "conversations", key)
		if q.BodyFile != filepath.Join(dir, "question.txt") || q.DecisionFile != filepath.Join(dir, "decision.json") {
			return errConversationMedia
		}
		body, _, err := conversationMediaRead(q.BodyFile, 128<<10)
		if err != nil {
			return errConversationMedia
		}
		defer clear(body)
		decision, _, err := conversationMediaRead(q.DecisionFile, 128<<10)
		if err != nil {
			return errConversationMedia
		}
		defer clear(decision)
		var value json.RawMessage
		if historyStrictJSON(decision, &value) != nil || bytesHash([]byte(strings.TrimLeft(string(body), "\ufeff"))) != scope.Body || normalQuestionDecisionHash(value) != scope.Decision || normalQuestionVideoTitle(q.Title, body) != title {
			return errConversationMedia
		}
		return nil
	}
}

func normalQuestionVideoTitle(title string, body []byte) string {
	if title != "" {
		return strings.TrimSpace(title)
	}
	for _, line := range strings.Split(strings.TrimLeft(string(body), "\ufeff"), "\n") {
		if strings.TrimSpace(line) != "" {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// Rust serde_json emits these Unicode separators literally; Go's JSON encoder
// escapes them even with EscapeHTML=false. Unescape only the actual JSON
// escape, keeping an escaped literal backslash followed by "u2028" distinct.
func normalQuestionDecisionHash(raw json.RawMessage) string {
	var value any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return ""
	}
	var encoded bytes.Buffer
	e := json.NewEncoder(&encoded)
	e.SetEscapeHTML(false)
	if e.Encode(value) != nil {
		return ""
	}
	b := bytes.TrimSuffix(encoded.Bytes(), []byte("\n"))
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\\' && i+1 < len(b) {
			if i+6 <= len(b) && (string(b[i:i+6]) == `\u2028` || string(b[i:i+6]) == `\u2029`) {
				if b[i+5] == '8' {
					out = append(out, []byte("\u2028")...)
				} else {
					out = append(out, []byte("\u2029")...)
				}
				i += 5
				continue
			}
			out = append(out, b[i], b[i+1])
			i++
			continue
		}
		out = append(out, b[i])
	}
	return bytesHash(out)
}
