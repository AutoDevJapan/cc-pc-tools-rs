package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const conversationMediaDomain = "discord-conversation-media-v1"
const conversationMediaSourceDomain = "discord-conversation-media-source-v1"
const conversationMediaLimit int64 = 10 << 20

// Normal MP4 uploads use Discord's documented default per-file limit. This
// does not enlarge the generated-image qualification or image byte ceiling.
const conversationVideoLimit int64 = 20 << 20

var errConversationMedia = errors.New("conversation media verification unavailable")
var conversationMediaSender = regexp.MustCompile(`^conversation-(log|v1)-[a-f0-9]{32}$`)
var conversationMediaNormal = regexp.MustCompile(`^normal-(log|ask)-[a-f0-9]{64}$`)
var conversationMediaAttachment = regexp.MustCompile(`^attachment-([0-9])\.(png|jpg|jpeg|gif|webp|bmp|tif|tiff|mp4)$`)

type conversationMediaDependencies struct {
	Home                    string
	Guard                   func(context.Context, string) (bool, error)
	Question                *generatedQuestionScope
	VideoTitle, VideoOrigin string
	Qualify                 func(context.Context, string, generatedQuestionScope) (*generatedImageQualification, error)
	VideoQuestion           func(context.Context, string, string, conversationMediaSource, *generatedQuestionScope) error
}

type conversationMediaSource struct {
	Domain       string `json:"domain"`
	SourcePath   string `json:"source_path"`
	SnapshotPath string `json:"snapshot_path"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
}

type conversationMediaVerification struct {
	Domain                string                       `json:"domain"`
	Verified              bool                         `json:"verified"`
	SHA256                string                       `json:"sha256"`
	Size                  int64                        `json:"size"`
	MIME                  string                       `json:"mime"`
	Extension             string                       `json:"extension"`
	QuestionQualification *generatedImageQualification `json:"question_qualification,omitempty"`
}

func conversationMediaObject(data []byte, fields ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errConversationMedia
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return errConversationMedia
		}
		seen[key] = true
		var raw json.RawMessage
		if decoder.Decode(&raw) != nil {
			return errConversationMedia
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF || len(seen) != len(fields) {
		return errConversationMedia
	}
	for _, field := range fields {
		if !seen[field] {
			return errConversationMedia
		}
	}
	return nil
}

func conversationMediaPath(path string) (string, error) {
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(path, `\\?\UNC\`) {
			path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
		} else if strings.HasPrefix(path, `\\?\`) {
			path = strings.TrimPrefix(path, `\\?\`)
			if len(path) < 3 || path[1] != ':' || path[2] != '\\' {
				return "", errConversationMedia
			}
		}
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) || path != filepath.Clean(path) {
		return "", errConversationMedia
	}
	for _, part := range strings.Split(strings.ReplaceAll(strings.ToLower(path), `\`, "/"), "/") {
		if strings.HasPrefix(part, ".env") || strings.Contains(part, "password") || strings.Contains(part, "secret") || strings.Contains(part, "token") {
			return "", errConversationMedia
		}
		switch part {
		case ".ssh", ".aws", ".azure", ".gnupg", "credentials", "id_rsa", "id_ed25519":
			return "", errConversationMedia
		}
		if runtime.GOOS == "windows" && (strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || (strings.Contains(part, ":") && part != strings.ToLower(filepath.VolumeName(path)))) {
			return "", errConversationMedia
		}
	}
	return path, nil
}

func conversationMediaReparse(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	// FileInfo.Sys exposes Win32FileAttributeData on Windows. Reflection keeps
	// this read-only attribute check portable without platform-specific files.
	value := reflect.ValueOf(info.Sys())
	if value.IsValid() && value.Kind() == reflect.Pointer && !value.IsNil() {
		value = value.Elem()
	}
	if value.IsValid() && value.Kind() == reflect.Struct {
		attribute := value.FieldByName("FileAttributes")
		if attribute.IsValid() && attribute.CanUint() && attribute.Uint()&0x400 != 0 {
			return true
		}
	}
	return false
}

func conversationMediaSamePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func conversationMediaPhysical(path string) (string, os.FileInfo, error) {
	path, err := conversationMediaPath(path)
	if err != nil {
		return "", nil, errConversationMedia
	}
	var leaf os.FileInfo
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || conversationMediaReparse(info) {
			return "", nil, errConversationMedia
		}
		if leaf == nil {
			leaf = info
		}
		if filepath.Dir(current) == current {
			break
		}
	}
	physical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", nil, errConversationMedia
	}
	physical, err = conversationMediaPath(physical)
	if err != nil || !conversationMediaSamePath(path, physical) {
		return "", nil, errConversationMedia
	}
	return physical, leaf, nil
}

func conversationMediaRead(path string, ceiling int64) ([]byte, os.FileInfo, error) {
	physical, before, err := conversationMediaPhysical(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > ceiling {
		return nil, nil, errConversationMedia
	}
	f, err := os.Open(physical)
	if err != nil {
		return nil, nil, errConversationMedia
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) || opened.Size() > ceiling {
		return nil, nil, errConversationMedia
	}
	b, err := io.ReadAll(io.LimitReader(f, ceiling+1))
	after, statErr := f.Stat()
	_, named, pathErr := conversationMediaPhysical(physical)
	if err != nil || statErr != nil || pathErr != nil || int64(len(b)) > ceiling || int64(len(b)) != opened.Size() || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) || !os.SameFile(opened, named) {
		return nil, nil, errConversationMedia
	}
	return b, opened, nil
}

func conversationMediaInside(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	return rel, err == nil && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func conversationMediaMagic(b []byte) (string, string, error) {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", "png", nil
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		return "image/jpeg", "jpg", nil
	case bytes.HasPrefix(b, []byte("GIF87a")) || bytes.HasPrefix(b, []byte("GIF89a")):
		return "image/gif", "gif", nil
	case len(b) >= 12 && bytes.HasPrefix(b, []byte("RIFF")) && string(b[8:12]) == "WEBP":
		return "image/webp", "webp", nil
	case bytes.HasPrefix(b, []byte("BM")):
		return "image/bmp", "bmp", nil
	case bytes.HasPrefix(b, []byte("II\x2a\x00")) || bytes.HasPrefix(b, []byte("MM\x00\x2a")):
		return "image/tiff", "tiff", nil
	default:
		return "", "", errConversationMedia
	}
}

// Parse the bounded ISO BMFF box structure, rather than trusting the suffix or
// an ftyp substring. A video track and nonempty media data are required; this
// container check is not a claim that every codec can be decoded by Discord.
func conversationVideoMagic(b []byte) bool {
	if len(b) < 24 || string(b[4:8]) != "ftyp" {
		return false
	}
	brand, video, media := false, false, false
	var walk func([]byte, string, int) bool
	walk = func(data []byte, parent string, depth int) bool {
		if depth > 4 {
			return false
		}
		for len(data) > 0 {
			if len(data) < 8 {
				return false
			}
			n, header := uint64(binary.BigEndian.Uint32(data)), uint64(8)
			kind := string(data[4:8])
			if n == 1 {
				if len(data) < 16 {
					return false
				}
				n = binary.BigEndian.Uint64(data[8:16])
				header = 16
			}
			if n == 0 {
				n = uint64(len(data))
			}
			if n < header || n > uint64(len(data)) {
				return false
			}
			payload := data[header:n]
			switch {
			case depth == 0 && kind == "ftyp":
				if brand || len(payload) < 8 || (len(payload)-8)%4 != 0 {
					return false
				}
				allowed := func(s string) bool {
					return s == "isom" || s == "iso2" || s == "mp41" || s == "mp42" || s == "avc1" || s == "dash" || s == "M4V "
				}
				brand = allowed(string(payload[:4]))
				for p := 8; p+4 <= len(payload); p += 4 {
					brand = brand || allowed(string(payload[p:p+4]))
				}
				if !brand {
					return false
				}
			case depth == 0 && kind == "mdat":
				media = media || len(payload) > 0
			case depth == 0 && kind == "moov", parent == "moov" && kind == "trak", parent == "trak" && kind == "mdia":
				if !walk(payload, kind, depth+1) {
					return false
				}
			case parent == "mdia" && kind == "hdlr":
				video = video || len(payload) >= 12 && string(payload[8:12]) == "vide"
			}
			data = data[n:]
		}
		return true
	}
	return walk(b, "", 0) && brand && video && media
}

func conversationAttachmentLimit(path string) int64 {
	if strings.EqualFold(filepath.Ext(path), ".mp4") {
		return conversationVideoLimit
	}
	return conversationMediaLimit
}

func conversationAttachmentMagic(b []byte, path string) (string, string, error) {
	if strings.EqualFold(filepath.Ext(path), ".mp4") {
		if int64(len(b)) <= conversationVideoLimit && conversationVideoMagic(b) {
			return "video/mp4", "mp4", nil
		}
		return "", "", errConversationMedia
	}
	return conversationMediaMagic(b)
}

func verifyConversationMedia(ctx context.Context, path, request string, deps conversationMediaDependencies) (conversationMediaVerification, error) {
	var result conversationMediaVerification
	if deps.Guard == nil || !conversationMediaSender.MatchString(request) {
		return result, errConversationMedia
	}
	if strings.EqualFold(filepath.Ext(path), ".mp4") && !strings.HasPrefix(request, "conversation-v1-") {
		return result, errConversationMedia
	}
	home, info, err := conversationMediaPhysical(filepath.Clean(filepath.FromSlash(deps.Home)))
	if err != nil || !info.IsDir() {
		return result, errConversationMedia
	}
	modeBytes, _, err := conversationMediaRead(filepath.Join(home, "conversation-mode.json"), 4096)
	if err != nil || conversationMediaObject(modeBytes, "domain", "enabled") != nil {
		return result, errConversationMedia
	}
	var mode struct {
		Domain  string `json:"domain"`
		Enabled bool   `json:"enabled"`
	}
	if json.Unmarshal(modeBytes, &mode) != nil || mode.Domain != "discord-conversation-mode-v1" || !mode.Enabled {
		return result, errConversationMedia
	}
	path, info, err = conversationMediaPhysical(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > conversationAttachmentLimit(path) {
		return result, errConversationMedia
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".mp4":
	default:
		return result, errConversationMedia
	}
	sourcePath := path
	var source conversationMediaSource
	bridge := filepath.Join(home, "conversation-bridge")
	if rel, inside := conversationMediaInside(bridge, path); inside {
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 2 || !conversationMediaNormal.MatchString(parts[0]) || !conversationMediaAttachment.MatchString(parts[1]) {
			return result, errConversationMedia
		}
		prefix := "conversation-log-"
		if strings.HasPrefix(parts[0], "normal-ask-") {
			prefix = "conversation-v1-"
		}
		if request != prefix+bytesHash([]byte(parts[0]))[:32] {
			return result, errConversationMedia
		}
		descriptor := strings.TrimSuffix(path, filepath.Ext(path)) + ".source.json"
		b, _, err := conversationMediaRead(descriptor, 64<<10)
		if err != nil || conversationMediaObject(b, "domain", "source_path", "snapshot_path", "sha256", "size") != nil || json.Unmarshal(b, &source) != nil || source.Domain != conversationMediaSourceDomain || !shaPattern.MatchString(source.SHA256) || source.Size < 1 || source.Size > conversationAttachmentLimit(path) {
			return result, errConversationMedia
		}
		snapshot, err := conversationMediaPath(source.SnapshotPath)
		if err != nil || !conversationMediaSamePath(snapshot, path) {
			return result, errConversationMedia
		}
		sourcePath, _, err = conversationMediaPhysical(source.SourcePath)
		if err != nil {
			return result, errConversationMedia
		}
		switch strings.ToLower(filepath.Ext(sourcePath)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff", ".mp4":
		default:
			return result, errConversationMedia
		}
		if strings.EqualFold(filepath.Ext(sourcePath), ".mp4") != strings.EqualFold(filepath.Ext(path), ".mp4") {
			return result, errConversationMedia
		}
		if _, inside := conversationMediaInside(bridge, sourcePath); inside {
			return result, errConversationMedia
		}
	}
	// MP4 support belongs to normal questions with a retained source receipt,
	// not an arbitrary recovery caller choosing the conversation-v1 prefix.
	if strings.EqualFold(filepath.Ext(path), ".mp4") && source.Domain == "" {
		return result, errConversationMedia
	}
	if strings.EqualFold(filepath.Ext(path), ".mp4") {
		if deps.VideoTitle == "" || deps.VideoOrigin == "" || deps.VideoQuestion == nil || deps.VideoQuestion(ctx, path, request, source, deps.Question) != nil {
			return result, errConversationMedia
		}
	}
	allowed, err := deps.Guard(ctx, sourcePath)
	if err != nil || ctx.Err() != nil {
		return result, errConversationMedia
	}
	var qualification *generatedImageQualification
	if !allowed {
		if strings.EqualFold(filepath.Ext(path), ".mp4") {
			return result, errConversationMedia
		}
		if deps.Question == nil || deps.Qualify == nil || !strings.HasPrefix(request, "conversation-v1-") {
			return result, errConversationMedia
		}
		qualification, err = deps.Qualify(ctx, sourcePath, *deps.Question)
		if err != nil || qualification == nil || qualification.Domain != generatedQuestionDomain || !qualification.Qualified || qualification.Scope != *deps.Question {
			return result, errConversationMedia
		}
	}
	b, snapshotInfo, err := conversationMediaRead(path, conversationAttachmentLimit(path))
	if err != nil {
		return result, errConversationMedia
	}
	defer clear(b)
	if source.Domain != "" {
		original, originalInfo, err := conversationMediaRead(sourcePath, conversationAttachmentLimit(sourcePath))
		defer clear(original)
		if err != nil || os.SameFile(snapshotInfo, originalInfo) || source.Size != int64(len(b)) || source.SHA256 != bytesHash(b) || !bytes.Equal(original, b) {
			return result, errConversationMedia
		}
	}
	mime, extension, err := conversationAttachmentMagic(b, path)
	if err != nil || ctx.Err() != nil {
		return result, errConversationMedia
	}
	if qualification != nil && (qualification.SHA256 != bytesHash(b) || qualification.Size != int64(len(b)) || qualification.MIME != mime || qualification.Extension != extension) {
		return result, errConversationMedia
	}
	return conversationMediaVerification{Domain: conversationMediaDomain, Verified: true, SHA256: bytesHash(b), Size: int64(len(b)), MIME: mime, Extension: extension, QuestionQualification: qualification}, nil
}

func conversationMediaLiveGuard(ctx context.Context, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	var proof struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
	}
	if err != nil || len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &proof) != nil || proof.Client == nil || proof.Unverified == nil {
		return false, errConversationMedia
	}
	return !*proof.Client && !*proof.Unverified, nil
}

func conversationMediaCLI(args []string, out io.Writer) int {
	home, exists := os.LookupEnv("CC_ASK_HOME")
	if !exists {
		home = "D:/CC/Tools/discord-ask"
	}
	return conversationMediaCLIWithDependencies(args, out, conversationMediaDependencies{Home: home, Guard: conversationMediaLiveGuard, VideoQuestion: registeredConversationVideoQuestion(home), Qualify: func(ctx context.Context, path string, scope generatedQuestionScope) (*generatedImageQualification, error) {
		return qualifyGeneratedQuestionImage(ctx, path, scope, generatedQuestionProduction())
	}})
}

func conversationMediaCLIWithDependencies(args []string, out io.Writer, deps conversationMediaDependencies) int {
	fs := flag.NewFlagSet("verify-conversation-media", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var path, request string
	conversationStringFlag(fs, "path", &path)
	conversationStringFlag(fs, "request-id", &request)
	conversationStringFlag(fs, "question-title", &deps.VideoTitle)
	conversationStringFlag(fs, "question-origin-thread", &deps.VideoOrigin)
	getScope := generatedQuestionScopeFlags(fs)
	if fs.Parse(args) != nil || fs.NArg() != 0 || path == "" || request == "" {
		_, _ = io.WriteString(out, "invalid conversation media arguments\n")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	scope, valid := getScope()
	if !valid {
		_, _ = io.WriteString(out, "invalid conversation media arguments\n")
		return 1
	}
	deps.Question = scope
	if strings.EqualFold(filepath.Ext(path), ".mp4") {
		deps.VideoQuestion = registeredConversationVideoQuestionWithContext(deps.Home, deps.VideoTitle, deps.VideoOrigin)
	}
	verified, err := verifyConversationMedia(ctx, path, request, deps)
	if err != nil {
		_, _ = io.WriteString(out, "conversation media verification unavailable\n")
		return 1
	}
	b, err := json.Marshal(verified)
	if err != nil {
		_, _ = io.WriteString(out, "conversation media verification unavailable\n")
		return 1
	}
	b = append(b, '\n')
	n, err := out.Write(b)
	if err != nil || n != len(b) {
		return 1
	}
	return 0
}
