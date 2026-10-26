//go:build windows

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"syscall"
)

const nativeSourceHead = "77b654e688b985efae47ff595151c33454b4a03b"

var nativeLeaf = leaf{
	"safe-delete", evidenceRoot + "/safe-delete-apostrophe.exe",
	"D:/CC/Tools/safe-delete/safe-delete.exe",
	"286a22b24412cc19138c083e09a93f4626de9058f329fad256e3f377b1e3b73b",
	"a413e654c44e3c51342c16b8c8d8416e0bba41befb3f28c69cdb8b5bd1389461",
}

type installProofPin struct{ EventSHA, OutputSHA, Item string }

var nativePrior = installProofPin{
	"d569fd6f47c51dbf0bb4681fcbf3b63f13451b8e42704d381e01cdf3d4135363",
	"8e0071e0b27d1de5e84020e5fdd2ae07e83e72edf298e1b16dd2caaf357d322d",
	"item_18",
}

func readProofEvents(path string) ([]byte, error) {
	ps, e := parents(path)
	if e != nil {
		return nil, e
	}
	defer closePins(ps)
	// The original actor is still running and owns an append handle. Keep
	// the same namespace pinned while taking a bounded byte snapshot. Only
	// the independently pinned complete event line can qualify below; this
	// snapshot is not authority for current or partially appended events.
	p, e := openPin(path, syscall.GENERIC_READ, 3, 3)
	if e != nil {
		return nil, e
	}
	defer p.close()
	if p.info.FileAttributes&0x10 != 0 {
		return nil, errCode("INVALID_PROOF")
	}
	b, e := io.ReadAll(io.LimitReader(p.f, 64<<20+1))
	if e != nil || len(b) > 64<<20 {
		return nil, errCode("PROOF_LIMIT")
	}
	return b, nil
}

// Read the original successful broker command event, not the copied receipt.
// Its pinned bytes are the completed update already observed by this task.
func nativePriorInstall(events []byte, expected installProofPin) error {
	s := bufio.NewScanner(bytes.NewReader(events))
	s.Buffer(make([]byte, 65536), 8<<20)
	found := 0
	for s.Scan() {
		if sha(s.Bytes()) != expected.EventSHA {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Item struct {
				ID, Type, Status string
				Exit             *int   `json:"exit_code"`
				Output           string `json:"aggregated_output"`
			} `json:"item"`
		}
		if json.Unmarshal(s.Bytes(), &event) != nil || event.Type != "item.completed" || event.Item.ID != expected.Item || event.Item.Type != "command_execution" || event.Item.Status != "completed" || event.Item.Exit == nil || *event.Item.Exit != 0 || sha([]byte(event.Item.Output)) != expected.OutputSHA {
			return errCode("PRIOR_SOFTWARE_EVENT_INVALID")
		}
		objects := 0
		for _, line := range strings.Split(event.Item.Output, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var wire struct {
				Domain    string `json:"domain"`
				Head      string `json:"source_head"`
				Qualified bool   `json:"qualified"`
				Results   []struct {
					Kind     string `json:"kind"`
					New      string `json:"new_sha256"`
					Old      string `json:"old_sha256"`
					Updated  bool   `json:"updated"`
					Verified bool   `json:"verified"`
				} `json:"results"`
			}
			if json.Unmarshal([]byte(line), &wire) != nil {
				return errCode("PRIOR_SOFTWARE_OUTPUT_INVALID")
			}
			if wire.Domain != "owned-cleanup-software-3757" || wire.Head != sourceHead || !wire.Qualified || len(wire.Results) != 2 {
				return errCode("PRIOR_SOFTWARE_OUTPUT_INVALID")
			}
			seen := map[string]bool{}
			for _, result := range wire.Results {
				if seen[result.Kind] || !result.Updated || !result.Verified {
					return errCode("PRIOR_SOFTWARE_OUTPUT_INVALID")
				}
				seen[result.Kind] = true
				matched := false
				for _, l := range leaves {
					if result.Kind == l.Kind && result.New == l.New && result.Old == l.Old {
						matched = true
					}
				}
				if !matched {
					return errCode("PRIOR_SOFTWARE_OUTPUT_INVALID")
				}
			}
			objects++
		}
		if objects != 1 {
			return errCode("PRIOR_SOFTWARE_OUTPUT_INVALID")
		}
		found++
	}
	if s.Err() != nil || found != 1 {
		return errCode("PRIOR_SOFTWARE_EVENT_MISSING_OR_AMBIGUOUS")
	}
	return nil
}

func refreshNativeProof(apply bool) ([]map[string]any, error) {
	b, e := readProofEvents("D:/CC/Tools/codex-jobs/jobs/20261025-165120-todo-3757/events.jsonl")
	if e != nil {
		return nil, e
	}
	if e = nativePriorInstall(b, nativePrior); e != nil {
		return nil, e
	}
	if e = verifySource(nativeSourceHead, nativeLeaf.Source); e != nil {
		return nil, e
	}
	// The consumer remains at the installed reviewed bytes. This refresh may
	// not overwrite a later consumer update or repeat the first two-file update.
	ps, e := parents(leaves[1].Target)
	if e != nil {
		return nil, e
	}
	defer closePins(ps)
	consumer, e := openPin(leaves[1].Target, syscall.GENERIC_READ, 1, 3)
	if e != nil {
		return nil, e
	}
	defer consumer.close()
	h, e := consumer.hash()
	if e != nil || h != leaves[1].New {
		return nil, errCode("CURRENT_CONSUMER_CHANGED")
	}
	p, e := prepareAt(nativeLeaf, "todo3757-native-proof")
	if e != nil {
		return nil, e
	}
	defer p.close()
	if !apply {
		return []map[string]any{{"kind": "safe-delete", "qualified": true, "changed": false, "consumer_retained": true}}, nil
	}
	lock, e := openPin("D:/CC/Tools/cleanup-8h/run.lock", syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, 3)
	if e != nil {
		return nil, errCode("HOURLY_CLEANER_LOCK_MISSING_OR_BUSY")
	}
	defer lock.close()
	e = p.install(hooks{})
	row := p.result()
	row["consumer_retained"] = true
	if e != nil {
		row["retained_reason"] = e.Error()
	}
	return []map[string]any{row}, e
}
