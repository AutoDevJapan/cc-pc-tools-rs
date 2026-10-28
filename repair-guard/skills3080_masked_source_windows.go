package main

import (
	"bytes"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const skillsModificationNoticePrefix = "Modification notice: The distributor changed this reference only by replacing withheld source lines with \"[WITHHELD physical line N]\" markers. All remaining source lines and their original positions are preserved below.\n\n"

func skillsMaskedSourceTarget(sourcePath string) (string, bool) {
	if !skillsRelative(sourcePath) || path.Base(sourcePath) != "SKILL.md" {
		return "", false
	}
	name := path.Base(path.Dir(sourcePath))
	if !skillsName.MatchString(name) || len(name) > 80 {
		return "", false
	}
	return name + "/references/original-skill.masked.txt", true
}

// This is a shape check only. The saved catalog/index/tree/commit and raw
// source object still bind the original SHA, role, bytes and exact view below.
func skillsHasMaskedSourceOutput(sourcePath string, outputs []skillsOutput) bool {
	target, ok := skillsMaskedSourceTarget(sourcePath)
	if !ok {
		return false
	}
	for _, output := range outputs {
		if output.Target == target && len(output.body) > 0 && hashBytes(output.body) == output.SHA {
			return true
		}
	}
	return false
}

func skillsMaskedSourceBytes(raw, view []byte) bool {
	if !utf8.Valid(raw) || !utf8.Valid(view) || bytes.IndexByte(raw, 0) >= 0 || bytes.IndexByte(view, 0) >= 0 || bytes.IndexByte(view, '\r') >= 0 {
		return false
	}
	// First preserve a genuine legacy payload, including one whose original
	// first physical line happens to equal the fixed annotation text.
	if skillsMaskedSourcePayload(raw, view) {
		return true
	}
	// Strip at most the one exact fixed representation prefix. Every original
	// byte/line/marker rule below still applies to the unchanged payload. An
	// unknown, changed or duplicated prefix remains unmatched source content.
	if !bytes.HasPrefix(view, []byte(skillsModificationNoticePrefix)) {
		return false
	}
	return skillsMaskedSourcePayload(raw, bytes.TrimPrefix(view, []byte(skillsModificationNoticePrefix)))
}
func skillsMaskedSourcePayload(raw, view []byte) bool {
	original := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.ContainsRune(original, '\r') || strings.HasSuffix(original, "\n") != bytes.HasSuffix(view, []byte{'\n'}) {
		return false
	}
	lines := strings.Split(strings.TrimSuffix(original, "\n"), "\n")
	displayed := strings.Split(strings.TrimSuffix(string(view), "\n"), "\n")
	if len(lines) != len(displayed) {
		return false
	}
	masked, visible := 0, 0
	for i, line := range lines {
		// A marker already present in the source hides nothing. It also cannot
		// serve as the sole visible content in an otherwise all-marker view.
		if displayed[i] == line {
			reserved := strings.HasPrefix(line, "[WITHHELD physical line ") && strings.HasSuffix(line, "]")
			if strings.TrimSpace(line) != "" && !reserved {
				visible++
			}
			continue
		}
		if displayed[i] == "[WITHHELD physical line "+strconv.Itoa(i+1)+"]" {
			masked++
			continue
		}
		return false
	}
	return masked > 0 && visible > 0
}

func skillsMaskedSourceObject(row skillsSavedSource, raw []byte, outputs []skillsOutput) bool {
	if row.Role != "skill_entry" {
		return false
	}
	target, ok := skillsMaskedSourceTarget(row.Path)
	if !ok {
		return false
	}
	for _, output := range outputs {
		if output.Target == target && hashBytes(output.body) == output.SHA && skillsMaskedSourceBytes(raw, output.body) {
			return true
		}
	}
	return false
}
