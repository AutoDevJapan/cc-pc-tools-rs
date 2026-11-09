package main

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// The accepted async call must immediately follow its previews, with only
// native reasoning/bookkeeping between them. A different user or tool event
// breaks the association even if the later title happens to match.
func nativeQuestionAssociationGap(e nativeQuestionEvidence) error {
	physical, before, err := conversationMediaPhysical(filepath.FromSlash(e.Rollout))
	if err != nil || !before.Mode().IsRegular() {
		return errConversationMedia
	}
	f, err := os.Open(physical)
	if err != nil {
		return errConversationMedia
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errConversationMedia
	}
	err = nativeQuestionAssociationScan(f, e.PreviewResult, e.Async)
	_, named, pathErr := conversationMediaPhysical(physical)
	if err != nil || pathErr != nil || !os.SameFile(opened, named) {
		return errConversationMedia
	}
	return nil
}

func nativeQuestionAssociationScan(input io.Reader, preview, async nativeQuestionEvent) error {
	if preview.Line < 1 || async.Line <= preview.Line || async.Line-preview.Line > 100 {
		return errConversationMedia
	}
	scan := bufio.NewScanner(io.LimitReader(input, generatedNativeScanLimit))
	scan.Buffer(make([]byte, 65536), generatedNativeLineLimit)
	line := 0
	for scan.Scan() {
		line++
		if line < preview.Line {
			continue
		}
		b := scan.Bytes()
		if line == preview.Line {
			if bytesHash(b) != preview.SHA {
				return errConversationMedia
			}
			continue
		}
		if line == async.Line {
			if bytesHash(b) == async.SHA {
				return nil
			}
			return errConversationMedia
		}
		var event struct {
			Type    string
			Payload struct {
				Type string
				Item struct{ Type string }
			}
		}
		if json.Unmarshal(b, &event) != nil {
			return errConversationMedia
		}
		if (event.Type == "response_item" && event.Payload.Type == "reasoning") || (event.Type == "event_msg" && (event.Payload.Type == "token_count" || (event.Payload.Type == "item_completed" && event.Payload.Item.Type == "Reasoning"))) {
			continue
		}
		return errConversationMedia
	}
	return errConversationMedia
}
