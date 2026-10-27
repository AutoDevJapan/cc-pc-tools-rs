package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

// An unrelated compound history element must be discarded without retaining
// the entire object/array. Atomic strings remain subject to the standard
// decoder's buffering; this contract bounds compound values with small tokens.
type ownerProjectionWindowReader struct {
	input     io.Reader
	max       int
	requested *int
}

func (r ownerProjectionWindowReader) Read(p []byte) (int, error) {
	if r.requested != nil && len(p) > *r.requested {
		*r.requested = len(p)
	}
	if len(p) > r.max {
		return 0, errors.New("unrelated metadata exceeded the read window")
	}
	return r.input.Read(p)
}

func TestOwnerProjectionStreamsLargeUnrelatedCompound(t *testing.T) {
	content := strings.Repeat(`"small-metadata-string",`, 400000) + "null"
	for _, envelope := range []struct{ before, after string }{
		{`{"projects":{"fixture":"C:/fixture"},"items":[{"history":[`, `]}]}`},
		{`{"metadata":{"history":[`, `]},"projects":{"fixture":"C:/fixture"}}`},
	} {
		requested := 0
		input := io.MultiReader(strings.NewReader(envelope.before), strings.NewReader(content), strings.NewReader(envelope.after))
		p, ok := ownerProjectProjection(context.Background(), ownerProjectionWindowReader{input, 1 << 20, &requested})
		if !ok || len(p) != 1 || p["fixture"] != "C:/fixture" {
			t.Fatalf("unrelated compound value was retained; requested=%d", requested)
		}
		t.Logf("compound bytes=%d largest read=%d", len(content), requested)
	}
}

func TestOwnerProjectionRetainsIgnoredDepthAndUnicodeContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  bool
	}{
		{"depth-129-array", `{"projects":{},"items":[` + strings.Repeat("[", 127) + "0" + strings.Repeat("]", 127) + `]}`, true},
		{"depth-130-array", `{"projects":{},"items":[` + strings.Repeat("[", 128) + "0" + strings.Repeat("]", 128) + `]}`, false},
		{"depth-129-object", `{"items":{"ignored":` + strings.Repeat(`{"k":`, 127) + "0" + strings.Repeat("}", 127) + `},"projects":{}}`, true},
		{"depth-130-object", `{"items":{"ignored":` + strings.Repeat(`{"k":`, 128) + "0" + strings.Repeat("}", 128) + `},"projects":{}}`, false},
		{"ignored-duplicates", `{"items":[{"a":1,"a":2}],"projects":{}}`, true},
		{"escaped-top-duplicate", `{"projects":{},"pro\u006aects":{}}`, false},
		{"escaped-project-duplicate", `{"projects":{"a":"C:/one","\u0061":"C:/two"}}`, false},
		{"ignored-invalid-utf8", "{\"projects\":{},\"items\":[\"\xff\"]}", true},
		{"project-invalid-utf8", "{\"projects\":{\"a\":\"C:/\xff\"}}", false},
		{"ignored-surrogate", `{"projects":{},"items":["\ud800"]}`, true},
		{"project-surrogate", `{"projects":{"a":"C:/\ud800"}}`, false},
		{"ignored-kinds", `{"projects":{},"a":null,"b":true,"c":3.25e-4,"d":"s","e":[],"f":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := ownerProjectProjection(context.Background(), strings.NewReader(tc.input))
			if ok != tc.want {
				t.Fatalf("projection acceptance=%v want=%v", ok, tc.want)
			}
		})
	}
}

type ownerProjectionTinyReader struct {
	input io.Reader
	chunk int
}

func (r ownerProjectionTinyReader) Read(p []byte) (int, error) {
	if len(p) > r.chunk {
		p = p[:r.chunk]
	}
	return r.input.Read(p)
}

func TestOwnerProjectionDepthScannerAcrossStringChunks(t *testing.T) {
	// Braces inside escaped quotes must not be interpreted as nesting, even
	// when backslashes, escapes and Unicode are split across input reads.
	input := `{"items":[{"note":"` + strings.Repeat(`\\\"[{]}\n日本語`, 200) + `"}],"projects":{"fixture":"C:/fixture"}}`
	for _, chunk := range []int{1, 2, 3, 7, 31} {
		p, ok := ownerProjectProjection(context.Background(), ownerProjectionTinyReader{strings.NewReader(input), chunk})
		if !ok || p["fixture"] != "C:/fixture" {
			t.Fatalf("quoted content changed depth with chunk=%d", chunk)
		}
	}
}
