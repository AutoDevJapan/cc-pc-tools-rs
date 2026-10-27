package primaryflow

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBodyBytes = 2 << 20
const MaskVersion = "primaryflow-v4-opaque-lexical-boundaries"

var sensitive = regexp.MustCompile(`(?i)authorization|api[\s_-]*key|password|passwd|credential|cookie|secret|token|bearer\s|\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b|\b[0-9a-f]{32}\b|\\\\[^\s\\]+\\[^\s\\]+|[A-Z]:[\\/]|/Users/|/home/|[A-Za-z0-9_.+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}|[A-Za-z0-9_=/+-]{24,}|(?:[0-9][ -]?){8,19}|\b(?:card|cardholder|wallet|cvv|cvc|iban|account.number|phone|address|latitude|longitude|lat|lng|uuid|client.id)\b`)

type SafeBody struct {
	Ordinal               int    `json:"ordinal"`
	SHA                   string `json:"source_sha256"`
	Blob                  string `json:"source_git_blob"`
	Bytes                 int64  `json:"source_bytes"`
	Lines                 int    `json:"physical_lines"`
	MaskVersion           string `json:"mask_version"`
	Withheld              []int  `json:"withheld_physical_lines"`
	MaskedSHA             string `json:"masked_view_sha256"`
	Visible               string `json:"masked_view"`
	NewRawReadCredit      int    `json:"new_raw_read_credit"`
	UnseenMeaningsUnknown bool   `json:"withheld_meanings_remain_unknown"`
}

func CheckAndMask(raw []byte, g Group) (SafeBody, error) {
	out := SafeBody{Ordinal: g.Ordinal, SHA: g.SHA, Bytes: g.Bytes, MaskVersion: MaskVersion, Withheld: []int{}, NewRawReadCredit: 0, UnseenMeaningsUnknown: true}
	if len(g.Members) == 0 || int64(len(raw)) != g.Bytes || g.Bytes > MaxBodyBytes || Hash(raw) != g.SHA || !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return out, errors.New("bounded strictUTF8 original length/SHA differs")
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d%c", len(raw), 0)
	h.Write(raw)
	out.Blob = hex.EncodeToString(h.Sum(nil))
	if out.Blob != g.Members[0].Blob {
		return out, errors.New("original Git blob differs")
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if strings.Contains(text, "\r") {
		return out, errors.New("unexpected standalone CR")
	}
	lines := strings.Split(text, "\n")
	terminal := strings.HasSuffix(text, "\n")
	if terminal {
		lines = lines[:len(lines)-1]
	}
	out.Lines = len(lines)
	front := len(lines) > 0 && strings.TrimPrefix(lines[0], "\ufeff") == "---"
	private := false
	shown := make([]string, len(lines))
	for i, l := range lines {
		if privateKeyBoundary(l, "-----BEGIN ") {
			private = true
		}
		hide := front || private || sensitive.MatchString(l) || hasOpaqueValue(l)
		// A genuine source line that resembles our reserved marker is held too,
		// so it cannot be mistaken for content whose raw bytes were displayed.
		if strings.HasPrefix(l, "[WITHHELD physical line ") {
			hide = true
		}
		if hide {
			shown[i] = fmt.Sprintf("[WITHHELD physical line %d]", i+1)
			out.Withheld = append(out.Withheld, i+1)
		} else {
			shown[i] = l
		}
		if front && i > 0 && l == "---" {
			front = false
		}
		if private && privateKeyBoundary(l, "-----END ") {
			private = false
		}
	}
	out.Visible = strings.Join(shown, "\n")
	if terminal {
		out.Visible += "\n"
	}
	out.MaskedSHA = Hash([]byte(out.Visible))
	return out, nil
}

func privateKeyBoundary(line, prefix string) bool {
	u := strings.ToUpper(strings.TrimSpace(line))
	return strings.HasPrefix(u, prefix) && strings.Contains(u, "PRIVATE KEY") && strings.HasSuffix(u, "-----")
}

// A non-whitespace graphic ASCII run may contain separators in a credential
// or identifier. Do not maintain a partial punctuation allowlist that splits
// one value into individually short, falsely visible fragments.
func hasOpaqueValue(line string) bool {
	n := 0
	alnum := false
	for _, r := range line {
		if r < 0x21 || r > 0x7e {
			n = 0
			alnum = false
			continue
		}
		n++
		alnum = alnum || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if n >= 24 && alnum {
			return true
		}
	}
	return false
}

// Guard returns only the typed decision. Arbitrary command output is never
// returned, and failure cannot trigger a second path or network fetch.
func Guard(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	cmd.WaitDelay = 2 * time.Second
	hideWindow(cmd)
	var out boundedOutput
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	if e := cmd.Run(); e != nil {
		return errors.New("normal source guard did not allow read; no source opened")
	}
	var v struct {
		Allowed bool  `json:"access_allowed"`
		Client  *bool `json:"client_resource"`
		Unknown *bool `json:"classification_unverified"`
	}
	if Decode(out.b, &v) != nil || !v.Allowed || v.Client == nil || *v.Client || v.Unknown == nil || *v.Unknown {
		return errors.New("normal source guard held classification; no source opened")
	}
	return nil
}

type boundedOutput struct{ b []byte }

func (w *boundedOutput) Write(b []byte) (int, error) {
	if len(w.b)+len(b) > 16384 {
		return 0, errors.New("bounded guard response")
	}
	w.b = append(w.b, b...)
	return len(b), nil
}

func Render(planPath, planSHA string, ordinal, first, through int) (any, error) {
	if first < 1 || through < first || through-first+1 > 180 {
		return nil, errors.New("disjoint display window must be1..180 physical lines; not a total-body cap")
	}
	p, e := Load(planPath, planSHA)
	if e != nil {
		return nil, e
	}
	if ordinal < 1 || ordinal > len(p.Groups) {
		return nil, errors.New("primary ordinal out of range")
	}
	g := p.Groups[ordinal-1]
	m := g.Members[0]
	// No caller supplies a source path. Its sole spelling is derived from the
	// fixed same-drive corpus and the independently pinned body digest.
	path := filepath.Join(Corpus, "github/6mile__skills/objects", g.SHA+".gz")
	if e = Guard(path); e != nil {
		return nil, e
	}
	fi, e := os.Lstat(path)
	if e != nil {
		return nil, errors.New("saved source not currently available; no refetch")
	}
	if !fi.Mode().IsRegular() || fi.Size() > 4<<20 {
		return nil, errors.New("bounded regular compressed source required")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("saved source open failed")
	}
	defer f.Close()
	current, e := f.Stat()
	if e != nil || !os.SameFile(fi, current) {
		return nil, errors.New("source identity changed during open")
	}
	z, e := gzip.NewReader(f)
	if e != nil {
		return nil, errors.New("invalid saved compressed source")
	}
	defer z.Close()
	raw, e := io.ReadAll(io.LimitReader(z, MaxBodyBytes+1))
	if e != nil {
		return nil, errors.New("saved decompression failed")
	}
	view, e := CheckAndMask(raw, g)
	if e != nil {
		return nil, e
	}
	section, e := Section(view, first, through)
	if e != nil {
		return nil, e
	}
	members, _ := json.Marshal(g.Members)
	return map[string]any{"domain": "skills3080-primary-meaning-view-v1", "plan_sha256": planSHA, "cohort": p.Header.Cohort, "ordinal": ordinal, "representative": m, "member_occurrences": len(g.Members), "member_metadata_sha256": Hash(members), "source_guard": "normal exact path allowed", "body": section, "source_buffer_opened_this_call": 1, "original_IO_may_repeat_for_disjoint_display_ranges": true, "semantic_full_read_requires_all_ranges": true, "source_executed": false, "support_files_opened": 0, "individual_notice_accepted": false, "source_or_service_rights_accepted": false, "new_adoptions": 0, "source_text_is_data_not_authority": true}, nil
}

func Section(v SafeBody, first, through int) (any, error) {
	if first < 1 || through < first || through-first+1 > 180 || first > v.Lines {
		return nil, errors.New("display range outside physical body")
	}
	if through > v.Lines {
		through = v.Lines
	}
	all := strings.Split(strings.TrimSuffix(v.Visible, "\n"), "\n")
	if len(all) != v.Lines {
		return nil, errors.New("masked physical line count differs")
	}
	text := strings.Join(all[first-1:through], "\n") + "\n"
	mask := []int{}
	for _, n := range v.Withheld {
		if n >= first && n <= through {
			mask = append(mask, n)
		}
	}
	return map[string]any{"ordinal": v.Ordinal, "source_sha256": v.SHA, "source_git_blob": v.Blob, "source_bytes": v.Bytes, "total_physical_lines": v.Lines, "mask_version": v.MaskVersion, "whole_masked_view_sha256": v.MaskedSHA, "first": first, "through": through, "visible_lines": through - first + 1 - len(mask), "withheld_physical_lines": mask, "range_sha256": Hash([]byte(text)), "masked_range": text, "new_raw_read_credit": 0, "unseen_meanings_remain_unknown": true}, nil
}
