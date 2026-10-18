package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

type conversationMediaFixture struct {
	root, home, source, snapshot, descriptor, normal, sender string
	data                                                     []byte
	guardPaths                                               []string
	deny                                                     bool
	onGuard                                                  func()
}

func mediaFixture(t *testing.T) *conversationMediaFixture {
	t.Helper()
	root, err := os.MkdirTemp("", "cc-media-fixture-")
	if err != nil {
		t.Fatal("could not create media fixture")
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal("could not resolve media fixture")
	}
	f := &conversationMediaFixture{root: root, home: filepath.Join(root, "tool-home"), source: filepath.Join(root, "original.png"), normal: "normal-log-" + strings.Repeat("a", 64), data: []byte("\x89PNG\r\n\x1a\nfixture-image")}
	f.sender = "conversation-log-" + bytesHash([]byte(f.normal))[:32]
	f.snapshot = filepath.Join(f.home, "conversation-bridge", f.normal, "attachment-0.png")
	f.descriptor = filepath.Join(filepath.Dir(f.snapshot), "attachment-0.source.json")
	if os.MkdirAll(filepath.Dir(f.snapshot), 0700) != nil {
		t.Fatal("could not create media fixture directory")
	}
	mediaJSON(t, filepath.Join(f.home, "conversation-mode.json"), map[string]any{"domain": "discord-conversation-mode-v1", "enabled": true})
	f.write(t)
	return f
}

func mediaJSON(t *testing.T, path string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil || os.WriteFile(path, b, 0600) != nil {
		t.Fatal("could not save private media fixture")
	}
}

func (f *conversationMediaFixture) write(t *testing.T) {
	t.Helper()
	if os.WriteFile(f.source, f.data, 0600) != nil || os.WriteFile(f.snapshot, f.data, 0600) != nil {
		t.Fatal("could not save image fixture")
	}
	mediaJSON(t, f.descriptor, conversationMediaSource{Domain: conversationMediaSourceDomain, SourcePath: f.source, SnapshotPath: f.snapshot, SHA256: bytesHash(f.data), Size: int64(len(f.data))})
}

func (f *conversationMediaFixture) deps() conversationMediaDependencies {
	return conversationMediaDependencies{Home: f.home, Guard: func(ctx context.Context, path string) (bool, error) {
		f.guardPaths = append(f.guardPaths, path)
		if f.onGuard != nil {
			f.onGuard()
		}
		return !f.deny, nil
	}}
}

func (f *conversationMediaFixture) verify(t *testing.T, path string) conversationMediaVerification {
	t.Helper()
	result, err := verifyConversationMedia(context.Background(), path, f.sender, f.deps())
	if err != nil || !result.Verified || result.Domain != conversationMediaDomain || result.SHA256 != bytesHash(f.data) || result.Size != int64(len(f.data)) {
		t.Fatal("qualified image did not return its metadata proof")
	}
	return result
}

func TestConversationMediaDirectAndBridgeSourcesAreCheckedLive(t *testing.T) {
	f := mediaFixture(t)
	f.verify(t, f.source)
	f.verify(t, f.snapshot)
	if len(f.guardPaths) != 2 || !conversationMediaSamePath(f.guardPaths[0], f.source) || !conversationMediaSamePath(f.guardPaths[1], f.source) {
		t.Fatal("snapshot verification lost the original source guard")
	}
	f.deny = true
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("protected source was accepted from an old private receipt")
	}
}

func TestConversationMediaCLIOnlyReturnsFixedMetadataAndNeverUsesRunnerLock(t *testing.T) {
	f := mediaFixture(t)
	unlock, err := acquire(filepath.Join(f.home, "runner.lock"))
	if err != nil {
		t.Fatal("could not hold media fixture runner lock")
	}
	defer unlock()
	before, err := os.ReadDir(f.home)
	if err != nil {
		t.Fatal("could not inspect media fixture")
	}
	var out bytes.Buffer
	code := conversationMediaCLIWithDependencies([]string{"--path", f.snapshot, "--request-id", f.sender}, &out, f.deps())
	var result map[string]any
	if code != 0 || json.Unmarshal(out.Bytes(), &result) != nil || len(result) != 6 || result["verified"] != true || result["sha256"] != bytesHash(f.data) {
		t.Fatal("media CLI did not return its fixed proof schema")
	}
	if bytes.Contains(out.Bytes(), []byte(f.root)) || bytes.Contains(out.Bytes(), f.data) || bytes.Contains(out.Bytes(), []byte("fixture-token")) {
		t.Fatal("media CLI exposed a private path, image or credential")
	}
	after, _ := os.ReadDir(f.home)
	if len(after) != len(before) {
		t.Fatal("read-only verification created a state artifact")
	}
}

func TestConversationMediaCLIRejectsInvalidArgumentsWithoutGuard(t *testing.T) {
	for _, args := range [][]string{nil, {"--path", "private-path"}, {"--request-id", "private"}, {"--path=a", "--path=b", "--request-id=x"}, {"--path=a", "--request-id=x", "--request-id=y"}, {"--path=a", "--request-id=x", "extra"}, {"--path=a", "--request-id=x", "--unknown=fixture-token"}} {
		f := mediaFixture(t)
		var out bytes.Buffer
		if conversationMediaCLIWithDependencies(args, &out, f.deps()) != 1 || out.String() != "invalid conversation media arguments\n" || len(f.guardPaths) != 0 {
			t.Fatal("invalid media arguments were echoed or reached the guard")
		}
	}
}

func TestConversationMediaRequiresNewSenderAndStrictEnabledMode(t *testing.T) {
	for _, mode := range []string{`null`, `{}`, `{"domain":"wrong","enabled":true}`, `{"domain":"discord-conversation-mode-v1","enabled":false}`, `{"domain":"discord-conversation-mode-v1","enabled":"true"}`, `{"domain":"discord-conversation-mode-v1","enabled":true,"extra":0}`, `{"domain":"discord-conversation-mode-v1","enabled":false,"enabled":true}`} {
		f := mediaFixture(t)
		if os.WriteFile(filepath.Join(f.home, "conversation-mode.json"), []byte(mode), 0600) != nil {
			t.Fatal("mode fixture write failed")
		}
		if _, err := verifyConversationMedia(context.Background(), f.source, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("invalid or disabled mode authorized an image")
		}
	}
	for _, request := range []string{"legacy-request", "conversation-log-abc", "conversation-log-" + strings.Repeat("A", 32), "conversation-log-" + strings.Repeat("a", 33)} {
		f := mediaFixture(t)
		if _, err := verifyConversationMedia(context.Background(), f.source, request, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("legacy or malformed request entered the new media namespace")
		}
	}
}

func TestConversationMediaBridgeDescriptorIsStrictAndBoundToSnapshotAndSender(t *testing.T) {
	for _, field := range []string{"domain", "source_path", "snapshot_path", "sha256", "size", "extra", "source_is_snapshot", "other_sender"} {
		t.Run(field, func(t *testing.T) {
			f := mediaFixture(t)
			value := map[string]any{"domain": conversationMediaSourceDomain, "source_path": f.source, "snapshot_path": f.snapshot, "sha256": bytesHash(f.data), "size": len(f.data)}
			switch field {
			case "size":
				value[field] = len(f.data) + 1
			case "extra":
				value[field] = true
			case "source_is_snapshot":
				value["source_path"] = f.snapshot
			case "other_sender":
				f.sender = "conversation-log-" + strings.Repeat("b", 32)
			default:
				value[field] = "different"
			}
			mediaJSON(t, f.descriptor, value)
			if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
				t.Fatal("corrupt or mismatched provenance was accepted")
			}
		})
	}
	for _, data := range []string{"null", "{}", "{", `{"domain":"discord-conversation-media-source-v1","domain":"discord-conversation-media-source-v1"}`} {
		f := mediaFixture(t)
		_ = os.WriteFile(f.descriptor, []byte(data), 0600)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
			t.Fatal("malformed source receipt was accepted")
		}
	}
}

func TestConversationMediaChangedOrMissingOriginalAndSnapshotAreRejected(t *testing.T) {
	for _, which := range []string{"source_changed", "snapshot_changed", "source_missing", "receipt_missing", "changed_in_guard"} {
		t.Run(which, func(t *testing.T) {
			f := mediaFixture(t)
			switch which {
			case "source_changed":
				_ = os.WriteFile(f.source, append(f.data, 'x'), 0600)
			case "snapshot_changed":
				_ = os.WriteFile(f.snapshot, append(f.data, 'x'), 0600)
			case "source_missing":
				_ = os.Remove(f.source)
			case "receipt_missing":
				_ = os.Remove(f.descriptor)
			case "changed_in_guard":
				f.onGuard = func() { _ = os.WriteFile(f.source, []byte("changed-current-image"), 0600) }
			}
			if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
				t.Fatal("missing or changed source bytes were authorized")
			}
		})
	}
}

func TestConversationMediaPathBoundariesRejectHiddenOriginsAndAliases(t *testing.T) {
	for _, component := range []string{".env-images", ".ssh", "credentials", "private-secrets", "access-token", "password-images"} {
		f := mediaFixture(t)
		f.source = filepath.Join(f.root, component, "original.png")
		_ = os.Mkdir(filepath.Dir(f.source), 0700)
		f.write(t)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("sensitive original path was hidden by a generic snapshot name")
		}
	}
	f := mediaFixture(t)
	alias := filepath.Dir(f.snapshot) + string(filepath.Separator) + ".." + string(filepath.Separator) + f.normal + string(filepath.Separator) + filepath.Base(f.snapshot)
	if _, err := verifyConversationMedia(context.Background(), alias, f.sender, f.deps()); err == nil {
		t.Fatal("path alias entered the snapshot namespace")
	}
	other := filepath.Join(filepath.Dir(f.snapshot), "different.png")
	_ = os.WriteFile(other, f.data, 0600)
	if _, err := verifyConversationMedia(context.Background(), other, f.sender, f.deps()); err == nil {
		t.Fatal("unknown bridge path fell back to direct source authority")
	}
}

func TestConversationMediaLinksAndNonregularFilesAreRejected(t *testing.T) {
	f := mediaFixture(t)
	link := filepath.Join(f.root, "linked.png")
	if err := os.Symlink(f.source, link); err != nil {
		if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.Errno(1314)) {
			t.Skip("symbolic links are unavailable for this test account")
		}
		t.Fatal("could not create link fixture")
	}
	if _, err := verifyConversationMedia(context.Background(), link, f.sender, f.deps()); err == nil {
		t.Fatal("symbolic image alias was accepted")
	}
	_ = os.Remove(f.snapshot)
	if os.Link(f.source, f.snapshot) != nil {
		t.Fatal("could not create physical alias fixture")
	}
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("a hard link was treated as an independent source snapshot")
	}
	directory := filepath.Join(f.root, "folder.png")
	_ = os.Mkdir(directory, 0700)
	if _, err := verifyConversationMedia(context.Background(), directory, f.sender, f.deps()); err == nil {
		t.Fatal("directory was accepted as image bytes")
	}
}

func TestConversationMediaSizeAndFormatLimits(t *testing.T) {
	for _, pathKind := range []string{"source", "snapshot"} {
		f := mediaFixture(t)
		path := f.source
		if pathKind == "snapshot" {
			path = f.snapshot
		}
		file, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err != nil || file.Truncate(conversationMediaLimit+1) != nil {
			t.Fatal("could not create oversized image fixture")
		}
		_ = file.Close()
		if _, err := verifyConversationMedia(context.Background(), path, f.sender, f.deps()); err == nil || len(f.guardPaths) != 0 {
			t.Fatal("oversized input reached image byte processing")
		}
	}
	for _, data := range [][]byte{[]byte("PK\x03\x04code-archive"), []byte("MZexecutable"), []byte("package main"), []byte("RIFFbad")} {
		f := mediaFixture(t)
		f.data = data
		f.write(t)
		if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
			t.Fatal("code or non-image bytes were accepted")
		}
	}
	f := mediaFixture(t)
	f.source = filepath.Join(f.root, "code.zip")
	f.write(t)
	if _, err := verifyConversationMedia(context.Background(), f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("an archive origin was hidden behind an image suffix")
	}
}

func TestConversationMediaRecognizesOnlyExistingImageMagic(t *testing.T) {
	for _, test := range []struct {
		bytes           []byte
		mime, extension string
	}{
		{[]byte("\x89PNG\r\n\x1a\nimage"), "image/png", "png"}, {[]byte("\xff\xd8\xffimage"), "image/jpeg", "jpg"}, {[]byte("GIF89aimage"), "image/gif", "gif"}, {[]byte("RIFF1234WEBPimage"), "image/webp", "webp"}, {[]byte("BMimage"), "image/bmp", "bmp"}, {[]byte("II\x2a\x00image"), "image/tiff", "tiff"},
	} {
		f := mediaFixture(t)
		f.data = test.bytes
		f.write(t)
		result := f.verify(t, f.snapshot)
		if result.MIME != test.mime || result.Extension != test.extension {
			t.Fatal("image proof format differs from sender magic detection")
		}
	}
}

type conversationMediaReparseInfo struct{ os.FileInfo }

func (conversationMediaReparseInfo) Sys() any { return &struct{ FileAttributes uint32 }{0x400} }

func TestConversationMediaReparseAttributesAreRejectedPortably(t *testing.T) {
	f := mediaFixture(t)
	info, err := os.Stat(f.source)
	if err != nil || !conversationMediaReparse(conversationMediaReparseInfo{info}) {
		t.Fatal("Windows reparse attributes were not detected")
	}
}

func TestConversationMediaGuardFailureAndCancellationHaveFixedOutput(t *testing.T) {
	f := mediaFixture(t)
	deps := f.deps()
	deps.Guard = func(context.Context, string) (bool, error) {
		return false, errors.New("fixture-token private-origin-path")
	}
	var out bytes.Buffer
	if conversationMediaCLIWithDependencies([]string{"--path", f.source, "--request-id", f.sender}, &out, deps) != 1 || out.String() != "conversation media verification unavailable\n" {
		t.Fatal("guard failure exposed private diagnostics")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := verifyConversationMedia(ctx, f.snapshot, f.sender, f.deps()); err == nil {
		t.Fatal("canceled media verification succeeded")
	}
}
