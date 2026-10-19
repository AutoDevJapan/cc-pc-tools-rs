package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestSourceBlobReadersBoundedAndDrained(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			data := map[string][]byte{}
			var hashes []string
			for i := 0; i < 12; i++ {
				b := []byte(fmt.Sprint(i))
				h := objectID("blob", b)
				hashes = append(hashes, h)
				data[h] = b
			}
			started := make(chan struct{}, 12)
			release := make(chan struct{})
			var mu sync.Mutex
			active, peak, finished := 0, 0, 0
			read := func(path string, v any) error {
				mu.Lock()
				active++
				if active > peak {
					peak = active
				}
				mu.Unlock()
				started <- struct{}{}
				<-release
				defer func() { mu.Lock(); active--; finished++; mu.Unlock() }()
				h := path[strings.LastIndex(path, "/")+1:]
				if fail && h == hashes[0] {
					return errors.New("synthetic refusal")
				}
				b := data[h]
				reply, _ := json.Marshal(map[string]any{"sha": h, "encoding": "base64", "size": len(b), "content": base64.StdEncoding.EncodeToString(b)})
				return json.Unmarshal(reply, v)
			}
			type result struct {
				objects map[string]sourceObject
				err     error
			}
			done := make(chan result, 1)
			go func() { o, e := fetchSourceBlobs(hashes, read); done <- result{o, e} }()
			for i := 0; i < 4; i++ {
				<-started
			}
			mu.Lock()
			observed := active
			mu.Unlock()
			if observed != 4 {
				t.Errorf("parallel readers=%d", observed)
			}
			close(release)
			r := <-done
			mu.Lock()
			a, p, f := active, peak, finished
			mu.Unlock()
			if a != 0 || p > 4 || f < 4 {
				t.Fatalf("undrained or unbounded: active=%d peak=%d finished=%d", a, p, f)
			}
			if fail {
				if r.err == nil || r.objects != nil {
					t.Fatal("partial source accepted")
				}
			} else {
				if r.err != nil || len(r.objects) != len(hashes) {
					t.Fatal("complete source not collected")
				}
				for h, b := range data {
					if string(r.objects[h].Bytes) != string(b) {
						t.Fatal("blob changed")
					}
				}
			}
		})
	}
}
