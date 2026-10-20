package main

import (
	"errors"
	"path/filepath"
)

// This only binds retry inputs. The existing sender independently checks the
// original live guard, source receipt and exact upload bytes before any POST.
func conversationQuestionImages(paths []string) ([]string, []string, error) {
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
		data, _, err := conversationMediaRead(absolute, conversationMediaLimit)
		if err != nil || len(data) == 0 || seen[absolute] {
			return nil, nil, errConversationMedia
		}
		if _, _, err = conversationMediaMagic(data); err != nil {
			return nil, nil, err
		}
		total += len(data)
		if total > 23<<20 {
			return nil, nil, errConversationMedia
		}
		seen[absolute] = true
		canonical = append(canonical, absolute)
		hashes = append(hashes, bytesHash(data))
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

func conversationQuestionRetryImages(index *conversationIndex, input ConversationAskInput) ([]string, []string, error) {
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
		return conversationQuestionImages(input.Attachments)
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
