package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type historyAttachmentScope struct {
	historyReadScope
	Message    string `json:"message_id"`
	Attachment string `json:"attachment_id"`
	ContentSHA string `json:"expected_content_sha256"`
	Created    string `json:"expected_created_at"`
	Edited     string `json:"expected_edited_timestamp"`
	Size       int64  `json:"expected_size"`
}

type historyOwnerImage struct {
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	URL         string `json:"url"`
}

// URLs exist only in this direct API decoding type, never in a saved receipt.
type historyOwnerImageMessage struct {
	Message
	Type        *int                `json:"type"`
	Attachments []historyOwnerImage `json:"attachments"`
}

func historyAttachmentCLI(args []string, out io.Writer) int {
	return historyAttachmentCLIWith(args, out, loadHistoryRead, historyReadGuard)
}

func historyAttachmentCLIWith(args []string, out io.Writer, load func(context.Context, historyReadScope) (*Runner, error), guard func(context.Context, string) bool) int {
	stage := "arguments"
	httpStatus := 0
	responseLength := int64(-1)
	responseType := "unknown"
	fail := func() int {
		_ = json.NewEncoder(out).Encode(map[string]any{"error": "attachment read unavailable", "stage": stage, "http_status": httpStatus, "response_length": responseLength, "response_media_type": responseType})
		return 1
	}
	var scopePath, output string
	flags := flag.NewFlagSet("history-attachment", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	conversationStringFlag(flags, "scope", &scopePath)
	conversationStringFlag(flags, "output", &output)
	if flags.Parse(args) != nil || flags.NArg() != 0 || scopePath == "" || output == "" {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stage = "private_output"
	mediaType := historyImageOutputType(output)
	if mediaType == "" || !historyPlainParent(output) || !guard(ctx, filepath.Dir(output)) {
		return fail()
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return fail()
	}
	stage = "scope"
	raw, err := historyPrivateInput(ctx, scopePath, guard)
	if err != nil {
		return fail()
	}
	var scope historyAttachmentScope
	if historyStrictJSON(raw, &scope) != nil || !historyAttachmentScopeValid(scope, time.Now()) {
		return fail()
	}
	stage = "configuration"
	runner, err := load(ctx, scope.historyReadScope)
	if err != nil || runner == nil || runner.State == nil || runner.Client == nil || runner.State.Destination.Guild != scope.Guild || runner.State.Destination.Owner != scope.Owner {
		return fail()
	}
	stage = "bot_identity"
	var bot struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if runner.get(ctx, "/users/@me", &bot) != nil || !bot.Bot || bot.ID != runner.State.Destination.Bot {
		return fail()
	}
	stage = "thread_identity"
	threadID := scope.Threads[0]
	var thread Thread
	if runner.get(ctx, "/channels/"+threadID, &thread) != nil || thread.ID != threadID || thread.Guild != scope.Guild || thread.Type != 11 || (thread.Parent != runner.State.Destination.Forum && thread.Parent != requestForum) || (thread.Owner != bot.ID && thread.Owner != scope.Owner) {
		return fail()
	}
	stage = "starter"
	var starter Message
	if runner.get(ctx, "/channels/"+threadID+"/messages/"+threadID, &starter) != nil || !historyMessageValid(starter, threadID, runner) || starter.ID != threadID || (starter.Author.ID != scope.Owner && starter.Author.ID != bot.ID) {
		return fail()
	}
	stage = "owner_message"
	messagePath := "/channels/" + threadID + "/messages/" + scope.Message
	var message historyOwnerImageMessage
	if runner.get(ctx, messagePath, &message) != nil {
		return fail()
	}
	stage = "owner_version_attachment_or_type"
	attachment, ok, reason := historySelectedOwnerImage(scope, message, runner)
	if !ok {
		stage = reason
		return fail()
	}
	stage = "cdn_url"
	imageURL, ok := historyAttachmentURL(attachment.URL, threadID, scope.Attachment)
	if !ok {
		return fail()
	}
	stage = "cdn_request"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return fail()
	}
	// Share only transport, never the bot header, cookie jar or redirect policy.
	client := &http.Client{Transport: runner.Client.Transport, Timeout: 6 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return fail()
	}
	defer response.Body.Close()
	stage = "cdn_response"
	httpStatus = response.StatusCode
	responseLength = response.ContentLength
	if declared, _, err := mime.ParseMediaType(response.Header.Get("Content-Type")); err == nil && historySupportedImageType(declared) {
		responseType = declared
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > 20<<20 {
		return fail()
	}
	stage = "image_bytes"
	data, err := io.ReadAll(io.LimitReader(response.Body, 20<<20+1))
	detectedType := http.DetectContentType(data)
	if err != nil || len(data) > 20<<20 || (response.ContentLength >= 0 && int64(len(data)) != response.ContentLength) || !historySupportedImageType(detectedType) || (mediaType != "image/*" && detectedType != mediaType) {
		return fail()
	}
	mediaType = detectedType
	stage = "owner_version_recheck"
	var current historyOwnerImageMessage
	if runner.get(ctx, messagePath, &current) != nil {
		return fail()
	}
	currentAttachment, ok, _ := historySelectedOwnerImage(scope, current, runner)
	if !ok || ctx.Err() != nil {
		return fail()
	}
	stage = "private_publication"
	if writeHistoryReceipt(output, data) != nil {
		return fail()
	}
	sourceType := "unknown"
	if declared, _, err := mime.ParseMediaType(currentAttachment.ContentType); err == nil && historySupportedImageType(declared) {
		sourceType = declared
	}
	result := map[string]any{"domain": "discord-owner-image-read-receipt-v1", "source": "discord-api-v10", "todo_id": scope.Todo, "guild_id": scope.Guild, "owner_id": scope.Owner, "thread_id": threadID, "message_id": scope.Message, "attachment_id": scope.Attachment, "content_sha256": scope.ContentSHA, "created_at": scope.Created, "edited_timestamp": scope.Edited, "source_size": scope.Size, "source_media_type": sourceType, "size": len(data), "representation": "cdn_response_bytes", "media_type": mediaType, "image_sha256": bytesHash(data), "scope_sha256": bytesHash(raw), "acquired_at": runner.Now().UTC().Format(time.RFC3339Nano), "delivery": "inspection_only", "application": "not_inferred"}
	if json.NewEncoder(out).Encode(result) != nil {
		return 1
	}
	return 0
}

func historyAttachmentScopeValid(scope historyAttachmentScope, now time.Time) bool {
	window := scope.historyReadScope
	window.Domain = "discord-history-read-scope-v1"
	if scope.Domain != "discord-owner-image-read-scope-v1" || len(scope.Threads) != 1 || !historyScopeValid(window, now) || scope.Size <= 0 || scope.Size > 20<<20 || len(scope.ContentSHA) != 64 || strings.ToLower(scope.ContentSHA) != scope.ContentSHA {
		return false
	}
	if b, err := hex.DecodeString(scope.ContentSHA); err != nil || len(b) != 32 {
		return false
	}
	message, valid := number(scope.Message)
	thread, _ := number(scope.Threads[0])
	high, _ := number(historyHighBefore(mustHistoryTime(scope.Until)))
	if _, ok := number(scope.Attachment); !ok || !valid || message < thread || message >= high {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, scope.Created)
	if err != nil || created.Before(mustHistoryTime(scope.Since)) || !created.Before(mustHistoryTime(scope.Until)) {
		return false
	}
	if scope.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, scope.Edited)
		if err != nil || edited.Before(created) || edited.After(now) {
			return false
		}
	}
	return true
}

func historySelectedOwnerImage(scope historyAttachmentScope, message historyOwnerImageMessage, runner *Runner) (historyOwnerImage, bool, string) {
	if message.ID != scope.Message || message.Author.ID != scope.Owner || message.Type == nil || (*message.Type != 0 && *message.Type != 19) || !historyMessageValid(message.Message, scope.Threads[0], runner) {
		return historyOwnerImage{}, false, "owner_message_identity"
	}
	if message.Timestamp != scope.Created || message.Edited != scope.Edited || bytesHash([]byte(message.Content)) != scope.ContentSHA {
		return historyOwnerImage{}, false, "owner_message_version"
	}
	var selected historyOwnerImage
	found := 0
	for _, item := range message.Attachments {
		if item.ID == scope.Attachment {
			selected = item
			found++
		}
	}
	if found != 1 {
		return historyOwnerImage{}, false, "attachment_identity"
	}
	if selected.Size != scope.Size {
		return historyOwnerImage{}, false, "attachment_size"
	}
	return selected, true, ""
}

func historyAttachmentURL(raw, thread, attachment string) (string, bool) {
	u, err := url.Parse(raw)
	prefix := "/attachments/" + thread + "/" + attachment + "/"
	if err != nil || u.Scheme != "https" || u.Host != "cdn.discordapp.com" || u.User != nil || u.Fragment != "" || !strings.HasPrefix(u.EscapedPath(), prefix) || !strings.HasPrefix(u.Path, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(u.Path, prefix)
	return raw, name != "" && name != "." && name != ".." && !strings.Contains(name, "/")
}

func historyImageOutputType(path string) string {
	for suffix, mediaType := range map[string]string{".private.image": "image/*", ".private.png": "image/png", ".private.jpg": "image/jpeg", ".private.jpeg": "image/jpeg", ".private.webp": "image/webp", ".private.gif": "image/gif"} {
		if strings.HasSuffix(path, suffix) {
			return mediaType
		}
	}
	return ""
}

func historySupportedImageType(value string) bool {
	return value == "image/png" || value == "image/jpeg" || value == "image/webp" || value == "image/gif"
}
