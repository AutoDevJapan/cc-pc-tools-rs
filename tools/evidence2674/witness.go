package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var sessionRE = regexp.MustCompile(`(?i)(?:session_id|session id)\D{0,8}([0-9]+)`)

// Deliberately only non-expanding literals with an explicit absolute target.
// Extracting a literal does not prove that its surrounding code was executed.
var hereLiteral = regexp.MustCompile(`(?is)@'\r?\n(.*?)\r?\n'@\s*\|\s*Set-Content\s+(?:-LiteralPath|-Path)\s+['"]D:[/\\]CC[/\\]Tools[/\\]client-projects\.txt['"]`)
var valueLiteral = regexp.MustCompile(`(?i)Set-Content\s+(?:-LiteralPath|-Path)\s+'D:[/\\]CC[/\\]Tools[/\\]client-projects\.txt'\s+-Value\s+'([^'\r\n]*)'`)
var goLiteral = regexp.MustCompile(`(?i)os\.WriteFile\(\s*"D:[/\\]+CC[/\\]+Tools[/\\]+client-projects\.txt"\s*,\s*\[\]byte\(("(?:[^"\\]|\\.)*")\)`)

func literals(command string) [][]byte {
	result := [][]byte{}
	for _, m := range hereLiteral.FindAllStringSubmatch(command, -1) {
		result = append(result, []byte(m[1]))
	}
	for _, m := range valueLiteral.FindAllStringSubmatch(command, -1) {
		result = append(result, []byte(m[1]))
	}
	for _, m := range goLiteral.FindAllStringSubmatch(command, -1) {
		if v, e := strconv.Unquote(m[1]); e == nil {
			result = append(result, []byte(v))
		}
	}
	return result
}
func witness(private string) {
	f, e := os.Open(filepath.Join(private, "joined.private.jsonl"))
	if e != nil {
		fail()
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 65536), 64*1024*1024)
	raw := newPrivate(filepath.Join(private, "literal-witnesses.private.jsonl"))
	defer raw.Close()
	counts := map[string]int{}
	versions := map[string]bool{}
	for s.Scan() {
		var g Joined
		if json.Unmarshal(s.Bytes(), &g) != nil {
			fail()
		}
		for _, r := range g.Receipts {
			if !directWrite.MatchString(r.Command) {
				continue
			}
			counts["write_receipts"]++
			ls := literals(r.Command)
			if len(ls) == 0 {
				counts["literal_not_resolved"]++
			}
			for _, v := range ls {
				sum := hash(v)
				versions[sum] = true
				record := map[string]any{"record": g.Record, "source_sha256": g.SourceHash, "call_sha256": g.CallHash, "literal_sha256": sum, "content": string(v), "original_proven": false, "target_file_bytes_proven": false, "empty": len(strings.TrimSpace(string(v))) == 0}
				if json.NewEncoder(raw).Encode(record) != nil {
					fail()
				}
				counts["literal_syntax"]++
			}
		}
	}
	if s.Err() != nil || raw.Sync() != nil {
		fail()
	}
	counts["literal_versions"] = len(versions)
	out := newPrivate(filepath.Join(private, "witness-summary.json"))
	defer out.Close()
	if json.NewEncoder(out).Encode(counts) != nil {
		fail()
	}
	emit(counts)
}
