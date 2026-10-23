package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image/jpeg"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const omb3344Image = "D:/OMB/evidence/omb-runtime-3344.jpg"
const omb3344Proof = model2903Source + "/.provenance-omb/record-3344.private.json"
const omb3344ProofSHA = "e8cd963f4a766867314eb4c00d88de4b14380d96728cd69ee65c9b1838846352"
const omb3344OpenCode = `var omb3344Tab = await cua.createBrowserTab("chrome", "http://127.0.0.1:38799", { sessionName: "🛠 OMB稼働確認" });`
const omb3344CaptureCode = `var omb3344Capture = await omb3344Tab.getScreenshot();`

type ombNativeReference struct {
	Line        int    `json:"line"`
	SHA         string `json:"sha256"`
	Type        string `json:"type"`
	PayloadType string `json:"payload_type"`
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	CallID      string `json:"call_id"`
}

type ombCaptureProof struct {
	Domain    string                        `json:"domain"`
	Todo      int                           `json:"todo_id"`
	Parent    int                           `json:"parent_todo"`
	Purpose   string                        `json:"purpose"`
	URL       string                        `json:"url"`
	Source    string                        `json:"source_path"`
	ImageSHA  string                        `json:"image_sha256"`
	ImageSize int                           `json:"image_size"`
	ImageMIME string                        `json:"image_mime"`
	Native    string                        `json:"native_path"`
	Session   string                        `json:"session_id"`
	Job       string                        `json:"job_id"`
	Events    map[string]ombNativeReference `json:"events"`
	Review    struct {
		Seen      bool   `json:"screenshot_seen"`
		NoSecrets bool   `json:"no_customer_or_secret_content"`
		Loopback  bool   `json:"official_loopback_only"`
		Reference string `json:"reference"`
	} `json:"content_review"`
	Storage     string `json:"storage_semantics"`
	UIApproval  bool   `json:"ui_implementation_approval_granted"`
	OldModified bool   `json:"old_image_or_refusal_modified"`
}

type ombNativeEvent struct {
	Type    string `json:"type"`
	Payload struct {
		Type      string `json:"type"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Arguments string `json:"arguments"`
		CallID    string `json:"call_id"`
		Output    []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Image string `json:"image_url"`
		} `json:"output"`
	} `json:"payload"`
}

func ombNativeImage(proof ombCaptureProof, reader io.Reader) ([]byte, bool) {
	keys := []string{"session", "open_call", "open_result", "capture_call", "capture_result"}
	if len(proof.Events) != len(keys) || proof.Session == "" {
		return nil, false
	}
	byLine := map[int]string{}
	last := 0
	for _, key := range keys {
		ref, ok := proof.Events[key]
		if !ok || ref.Line <= last || ref.Line > 1000000 || !modelDigestValid(ref.SHA) {
			return nil, false
		}
		byLine[ref.Line] = key
		last = ref.Line
	}
	if proof.Events["session"].Line != 1 {
		return nil, false
	}
	observed := map[string]ombNativeEvent{}
	scanner := bufio.NewScanner(io.LimitReader(reader, 256<<20))
	scanner.Buffer(make([]byte, 64<<10), 36<<20)
	line := 0
	for scanner.Scan() {
		line++
		key, wanted := byLine[line]
		if wanted {
			ref := proof.Events[key]
			if hashBytes(scanner.Bytes()) != ref.SHA {
				return nil, false
			}
			var event ombNativeEvent
			if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Type != ref.Type || event.Payload.Type != ref.PayloadType || event.Payload.Name != ref.Name || event.Payload.Namespace != ref.Namespace || event.Payload.CallID != ref.CallID {
				return nil, false
			}
			observed[key] = event
		} else if line > proof.Events["open_call"].Line && line < proof.Events["capture_call"].Line {
			var event ombNativeEvent
			// The persistent tab binding must not be changed by another CUA call.
			if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Payload.Namespace == "mcp__cua_repl" {
				return nil, false
			}
		}
		if line == last {
			break
		}
	}
	if scanner.Err() != nil || len(observed) != len(keys) || observed["session"].Type != "session_meta" || observed["session"].Payload.ID != proof.Session {
		return nil, false
	}
	for _, pair := range [][3]string{{"open_call", "open_result", omb3344OpenCode}, {"capture_call", "capture_result", omb3344CaptureCode}} {
		call, result := observed[pair[0]], observed[pair[1]]
		var args struct {
			Code  string `json:"code"`
			Title string `json:"title"`
		}
		decoder := json.NewDecoder(strings.NewReader(call.Payload.Arguments))
		decoder.DisallowUnknownFields()
		if call.Type != "response_item" || call.Payload.Type != "function_call" || call.Payload.Name != "js" || call.Payload.Namespace != "mcp__cua_repl" ||
			decoder.Decode(&args) != nil || args.Code != pair[2] || result.Type != "response_item" || result.Payload.Type != "function_call_output" || call.Payload.CallID == "" || result.Payload.CallID != call.Payload.CallID {
			return nil, false
		}
	}
	openText := ""
	for _, block := range observed["open_result"].Payload.Output {
		if block.Type != "input_text" {
			return nil, false
		}
		openText += block.Text
	}
	if !strings.Contains(openText, `Title: "OpenMausBot", URL: "http://127.0.0.1:38799/"`) {
		return nil, false
	}
	var image []byte
	for _, block := range observed["capture_result"].Payload.Output {
		if block.Type == "input_text" {
			continue
		}
		if block.Type != "input_image" || image != nil || !strings.HasPrefix(block.Image, "data:image/jpeg;base64,") || len(block.Image) > 14<<20 {
			return nil, false
		}
		var err error
		image, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(block.Image, "data:image/jpeg;base64,"))
		if err != nil {
			return nil, false
		}
	}
	if len(image) == 0 || len(image) != proof.ImageSize || len(image) > 10<<20 || hashBytes(image) != proof.ImageSHA {
		return nil, false
	}
	config, err := jpeg.DecodeConfig(bytes.NewReader(image))
	return image, err == nil && config.Width > 0 && config.Height > 0 && int64(config.Width)*int64(config.Height) <= 25000000
}

func ombCaptureBytes() ([]byte, bool) {
	// Exact pinned evidence, original native tool bytes and a human content
	// review are independent of the output image. No caller supplies a grant.
	session := modelPinSession{protected: legacyMatch, source: permanentRuntimeSourceVerified, head: modelSourceHead}
	defer session.close()
	if !session.sourceRoot(model2903Source, console3264SourceHead) || !session.match(omb3344Proof, []string{omb3344ProofSHA}) {
		return nil, false
	}
	file := session.files[0].file
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	var proof ombCaptureProof
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proof) != nil || decoder.Decode(&struct{}{}) != io.EOF || !ombProofScoped(proof) {
		return nil, false
	}
	if !within("C:/Users/makhmeto/.codex/sessions", proof.Native) ||
		!strings.HasSuffix(filepath.Base(proof.Native), "-"+proof.Session+".jsonl") || !runtimeLeaf(proof.Native, filepath.Dir(proof.Native), legacyMatch, true) {
		return nil, false
	}
	name, err := syscall.UTF16PtrFromString(proof.Native)
	if err != nil {
		return nil, false
	}
	// The genuine Codex record is still being appended. Share writes, not
	// deletion; independently pinned selected rows must match byte for byte.
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x00200000|0x08000000, 0)
	if err != nil {
		return nil, false
	}
	native := os.NewFile(uintptr(h), proof.Native)
	defer native.Close()
	var info syscall.ByHandleFileInformation
	actual, ok := modelHandlePath(h)
	if !ok || !same(actual, proof.Native) || syscall.GetFileInformationByHandle(h, &info) != nil || !modelFileInfoValid(info) {
		return nil, false
	}
	image, ok := ombNativeImage(proof, native)
	return image, ok && session.recheck()
}

func ombProofScoped(proof ombCaptureProof) bool {
	return proof.Domain == "omb-loopback-runtime-capture-3344-v1" && proof.Todo == 3344 && proof.Parent == 3338 && proof.Purpose == "normal-discord-log" &&
		proof.URL == "http://127.0.0.1:38799/" && same(proof.Source, omb3344Image) && proof.ImageMIME == "image/jpeg" && modelDigestValid(proof.ImageSHA) &&
		proof.ImageSize > 0 && proof.ImageSize <= 10<<20 && proof.Review.Seen && proof.Review.NoSecrets && proof.Review.Loopback && proof.Review.Reference != "" &&
		!proof.UIApproval && !proof.OldModified && proof.Storage != "" && strings.HasSuffix(proof.Job, "-todo-3344")
}

func ombCaptureAllowed(path string) bool {
	if !same(path, omb3344Image) || legacyMatch(path) || !runtimeLeaf(path, filepath.Dir(path), legacyMatch, true) {
		return false
	}
	image, ok := ombCaptureBytes()
	return ok && consolePinnedFile(path, hashBytes(image))
}

func exportOMBCapture() object {
	image, ok := ombCaptureBytes()
	if !ok || legacyMatch(omb3344Image) || !runtimeLeaf(omb3344Image, filepath.Dir(omb3344Image), legacyMatch, false) {
		return object{"exported": false}
	}
	if _, err := os.Lstat(omb3344Image); err == nil {
		if !consolePinnedFile(omb3344Image, hashBytes(image)) {
			return object{"exported": false}
		}
		return object{"exported": true, "already_current": true, "source_path": omb3344Image, "sha256": hashBytes(image), "purpose": "normal-discord-log", "ui_implementation_approval": false}
	} else if !os.IsNotExist(err) {
		return object{"exported": false}
	}
	// First materialization of the actual new tool return, never a copy of the
	// rejected old PNG or a relabelled historical capture.
	file, err := os.OpenFile(omb3344Image, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return object{"exported": false}
	}
	_, writeErr := file.Write(image)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || !consolePinnedFile(omb3344Image, hashBytes(image)) {
		return object{"exported": false}
	}
	return object{"exported": true, "already_current": false, "source_path": omb3344Image, "sha256": hashBytes(image), "purpose": "normal-discord-log", "ui_implementation_approval": false}
}

func ombCaptureFlagPresent(args []string) bool {
	for _, arg := range args {
		if strings.TrimLeft(strings.SplitN(arg, "=", 2)[0], "-") == "export-omb-runtime-capture" {
			return true
		}
	}
	return false
}

func runOMBCaptureCLI(args []string, output io.Writer, export func() object) int {
	result := object{"exported": false}
	if len(args) == 1 && (args[0] == "--export-omb-runtime-capture" || args[0] == "-export-omb-runtime-capture") {
		result = export()
	}
	if json.NewEncoder(output).Encode(result) != nil || result["exported"] != true {
		return 1
	}
	return 0
}
