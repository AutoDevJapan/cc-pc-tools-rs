package store

import (
	"bytes"
	"strings"
	"testing"
)

func TestCanonicalRequestFingerprintFormatRemainsStable(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{` {"z":[1.00,-0,1e+9,9007199254740993,null,true,false],"a":"<>&\u2028"}`, `{"a":"\u003c\u003e\u0026\u2028","z":[1.00,-0,1e+9,9007199254740993,null,true,false]}`},
		{`{"lastjob":false,"lastJob":null,"empty":{},"array":[]}`, `{"array":[],"empty":{},"lastJob":null,"lastjob":false}`},
		{`[ {"b":2,"a":1},"\ud800"]`, `[{"a":1,"b":2},"�"]`},
	} {
		got, err := canonical([]byte(tt.input))
		if err != nil || string(got) != tt.want {
			t.Fatalf("canonical format changed: %q %v", got, err)
		}
	}
	if _, err := canonical([]byte(`[{"a":1,"\u0061":2}]`)); err == nil {
		t.Fatal("nested duplicate key accepted")
	}
}

func TestCanonicalDeepValidDocumentDoesNotRecopyEveryPrefix(t *testing.T) {
	input := []byte(strings.Repeat("[", 8000) + `{"lastJob":9007199254740993,"lastjob":null}` + strings.Repeat("]", 8000))
	got, err := canonical(input)
	if err != nil || !bytes.Equal(got, input) {
		t.Fatal("deep canonical JSON changed bytes or failed")
	}
}
