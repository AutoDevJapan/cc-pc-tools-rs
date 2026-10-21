package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

// Requests contain IDs/hashes and the controller's explicit expectation only.
// This read-only command neither registers runners nor silently chooses a head.
func normalCIJobCLI(ctx context.Context, file string) error {
	f, e := os.Open(file)
	if e != nil {
		return errors.New("normal CI binding request unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 32769))
	if e != nil || len(raw) > 32768 {
		return errors.New("normal CI binding request bound rejected")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var p normalCIJobPin
	if d.Decode(&p) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("normal CI binding request rejected")
	}
	b, e := normalCIJobPlan(ctx, p, get)
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(b)
}
