package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestTaskQuestionPreparedScalarContract(t *testing.T) {
	for _, pair := range [][2]string{
		{"1700000000", "17e8"}, {"1700000000.0", "17e8"}, {"1.7e9", "17e8"},
		{"1700000000.125", "1700000000125e-3"}, {"0.00012345", "12345e-8"}, {"-0.0", "0"},
		{"18446744073709551615", "18446744073709551615"}, {"null", "null"},
		{`"1700000000"`, `"1700000000"`}, {`"<&>\u2028\u2029"`, `"<&>\u2028\u2029"`},
	} {
		if taskQuestionPreparedDigest(json.RawMessage(pair[0])) != bytesHash([]byte(pair[1])) {
			t.Fatal("scalar value changed", pair[0])
		}
	}
	if taskQuestionPreparedDigest(uint64(9007199254740993)) == taskQuestionPreparedDigest(uint64(9007199254740992)) {
		t.Fatal("integer identity rounded through float64")
	}
}

func TestTaskQuestionNativeScalarProducerConsumer(t *testing.T) {
	c, f, _ := taskQuestionReleasedFixture(t, false, taskQuestionNativeSetup(t))
	taskQuestionNativeTransition(t, c, f, "scalar_contract", nil)
	b, err := os.ReadFile(filepath.Join(f.home, "native-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Cases []struct {
			Value          json.RawMessage
			Digest         string
			Scope, Pending json.RawMessage
			Receipt        struct {
				Scope string `json:"scope_sha256"`
				Input string `json:"input_sha256"`
			}
		} `json:"scalar_cases"`
	}
	if json.Unmarshal(b, &out) != nil || len(out.Cases) != 9 {
		t.Fatal("native scalar producer did not run")
	}
	for _, c := range out.Cases {
		if taskQuestionPreparedDigest(c.Value) != c.Digest || taskQuestionPreparedDigest(c.Scope) != c.Receipt.Scope || taskQuestionPreparedDigest(c.Pending) != c.Receipt.Input {
			t.Fatal("producer/consumer scalar mismatch", string(c.Value))
		}
	}
}
