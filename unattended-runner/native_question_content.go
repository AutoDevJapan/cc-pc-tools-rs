package main

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// Recover literal body/decision creation from the original native command.
// This parser never evaluates PowerShell, variables, pipelines, or expressions.
type nativePS struct {
	text  string
	at    int
	depth int
}

func (p *nativePS) space() {
	for p.at < len(p.text) && strings.ContainsRune(" \r\n\t", rune(p.text[p.at])) {
		p.at++
	}
}
func (p *nativePS) take(s string) bool {
	p.space()
	if strings.HasPrefix(p.text[p.at:], s) {
		p.at += len(s)
		return true
	}
	return false
}
func (p *nativePS) literal() (any, error) {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > 32 {
		return nil, errConversationMedia
	}
	p.space()
	if p.at >= len(p.text) {
		return nil, errConversationMedia
	}
	if p.take("[ordered]") {
		return p.literal()
	}
	if p.take("@{") {
		v := map[string]any{}
		for !p.take("}") {
			key := p.word()
			if key == "" || v[key] != nil || !p.take("=") {
				return nil, errConversationMedia
			}
			x, e := p.literal()
			if e != nil {
				return nil, e
			}
			v[key] = x
			if p.take("}") {
				return v, nil
			}
			if !p.take(";") {
				return nil, errConversationMedia
			}
		}
		return v, nil
	}
	if p.take("@(") {
		v := []any{}
		for !p.take(")") {
			x, e := p.literal()
			if e != nil {
				return nil, e
			}
			v = append(v, x)
			if p.take(")") {
				return v, nil
			}
			if !p.take(",") {
				return nil, errConversationMedia
			}
		}
		return v, nil
	}
	p.space()
	if p.text[p.at] == '\'' {
		p.at++
		start := p.at
		for p.at < len(p.text) && p.text[p.at] != '\'' {
			p.at++
		}
		if p.at == len(p.text) {
			return nil, errConversationMedia
		}
		s := p.text[start:p.at]
		p.at++
		return s, nil
	}
	start := p.at
	for p.at < len(p.text) && p.text[p.at] >= '0' && p.text[p.at] <= '9' {
		p.at++
	}
	if start == p.at {
		return nil, errConversationMedia
	}
	n, e := strconv.ParseUint(p.text[start:p.at], 10, 64)
	if e != nil {
		return nil, errConversationMedia
	}
	return float64(n), nil
}
func (p *nativePS) word() string {
	p.space()
	start := p.at
	for p.at < len(p.text) {
		c := p.text[p.at]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (p.at > start && c >= '0' && c <= '9') {
			p.at++
		} else {
			break
		}
	}
	return p.text[start:p.at]
}
func nativeQuestionContent(e nativeQuestionEvidence, body, decision []byte, d nativeQuestionDeps) error {
	if e.Content.Line < 2 || e.Content.Line >= e.Question.Line || !shaPattern.MatchString(e.Content.SHA) {
		return errConversationMedia
	}
	records, err := d.Records(e.Rollout, []nativeQuestionEvent{e.Content})
	if err != nil || len(records) != 1 {
		return errConversationMedia
	}
	var record nativeQuestionRecord
	if json.Unmarshal(records[0], &record) != nil || record.Type != "response_item" || record.Payload.Type != "custom_tool_call" || record.Payload.Name != "exec" {
		return errConversationMedia
	}
	code := nativeQuestionMetadata(record.Payload.Input)
	commands, err := nativeQuestionTokenCommands(code)
	if err != nil {
		return err
	}
	found := 0
	for _, cmd := range commands {
		a := strings.Index(cmd, "@'\n")
		if a < 0 {
			continue
		}
		b := strings.Index(cmd[a+3:], "\n'@ | Set-Content -LiteralPath '")
		if b < 0 {
			continue
		}
		text := cmd[a+3:a+3+b] + "\n"
		// Set-Content emits the host newline; compare the original saved body
		// without treating arbitrary newline edits to a retry as interchangeable.
		normalized := func(b []byte) string {
			return strings.ReplaceAll(string(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))), "\r\n", "\n")
		}
		if normalized(body) != text {
			continue
		}
		a = strings.Index(cmd, "$decision=[ordered]@{")
		if a < 0 {
			continue
		}
		tail := cmd[a+len("$decision="):]
		p := nativePS{text: tail}
		v, err := p.literal()
		if err != nil {
			continue
		}
		p.space()
		if !strings.HasPrefix(p.text[p.at:], "$decision|ConvertTo-Json -Depth ") {
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			continue
		}
		raw, err = canonicalJSON(raw)
		if err != nil || !bytes.Equal(raw, decision) {
			continue
		}
		found++
	}
	if found != 1 {
		return errConversationMedia
	}
	return nil
}
