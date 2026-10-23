//go:build !windows

package main

// The live owner sender uses the Windows user-bound store. Unit tests inject
// isolated key protection; another platform cannot silently create a plain key.
func generatedQuestionProtect([]byte) ([]byte, error)   { return nil, errConversationMedia }
func generatedQuestionUnprotect([]byte) ([]byte, error) { return nil, errConversationMedia }
