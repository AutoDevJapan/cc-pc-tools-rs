package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestReviewSourceReferenceDistinguishesTextAndJobSyntaxWithoutIdentityClaims(t *testing.T) {
	const request = "todo29-db-count-review-52aaffb-20261010"
	cases := []struct {
		name, reference, format, stamp string
		present, exact                 bool
	}{
		{"original_client_id", request, "known_request_text", "", true, true},
		{"original_manual_id", "manual:" + request, "known_request_text", "", true, true},
		{"wrapped_request", "queued:manual:" + request, "queue_prefix", "", true, false},
		{"other_queue", "queue:private-other-reference", "queue_prefix", "", false, false},
		{"queue_marker", "queued", "queue_marker", "", false, false},
		{"path_containing_request", "C:/private/" + request + ".md", "other", "", true, false},
		{"job_name", "20261012-131423-private-job-label", "job_name", "2026-10-12 13:14:23", false, false},
		{"leap_day", "20240229-000000-todo-2191", "job_name", "2024-02-29 00:00:00", false, false},
		{"invalid_month", "20261312-131423-private-label", "other", "", false, false},
		{"invalid_time", "20261012-257070-private-label", "other", "", false, false},
		{"path_after_stamp", "20261012-131423-private/label", "other", "", false, false},
		{"non_ascii_label", "20261012-131423-私的な名前", "other", "", false, false},
		{"missing_label", "20261012-131423-", "other", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var db obj
			d := json.NewDecoder(bytes.NewReader(reviewFixture()))
			d.UseNumber()
			if err := d.Decode(&db); err != nil {
				t.Fatal(err)
			}
			recovery := object(object(array(db["items"])[1])["recovery"])
			recovery["request_source_job_id"] = tc.reference
			source, err := json.Marshal(db)
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if code := runReviewMetadataCLI(nil, &out, func() ([]byte, error) { return source, nil }); code != 0 {
				t.Fatalf("projection failed: %d", code)
			}
			var report obj
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			ref := object(object(report["recovery"])["request_source_job_id"])
			if ref["reference_format"] != tc.format || ref["original_request_text_present"] != tc.present || ref["exact_original_request_text"] != tc.exact {
				t.Fatal("reference text or syntax facts were lost or conflated")
			}
			if ref["reference_identity_verified"] != false || ref["reference_opened"] != false || object(report["wait_transition_provenance"])["verified"] != false {
				t.Fatal("reference syntax was promoted into identity, access, or transition proof")
			}
			if tc.stamp == "" {
				if _, exists := ref["job_name_time"]; exists {
					t.Fatal("a path, invalid date, or non-job reference became a job timestamp")
				}
			} else if ref["job_name_time"] != tc.stamp || ref["job_name_time_zone"] != "unspecified" || ref["job_existence_verified"] != false {
				t.Fatal("job-name timestamp lost its uncertainty")
			}
			for _, forbidden := range []string{"private-", "C:/private", "私的", request} {
				if strings.Contains(out.String(), forbidden) {
					t.Fatalf("reference text or another private value leaked: %q", forbidden)
				}
			}
			if _, exists := ref["sha256"]; exists {
				t.Fatal("a reference digest was disclosed")
			}
			for _, field := range []string{"live_state_changed", "data_access_granted", "original_registry_recovered", "review_completed", "console_queue_observed"} {
				if report[field] != false {
					t.Fatalf("text matching was promoted into %s", field)
				}
			}
		})
	}
}

func TestReviewSourceReferenceKeepsMissingAndInvalidValuesUnclassified(t *testing.T) {
	cases := []struct {
		name, want string
		value      any
		absent     bool
	}{
		{"absent", "absent", nil, true},
		{"null", "null", nil, false},
		{"number", "invalid", 2191, false},
		{"empty", "invalid", "", false},
		{"too_long", "invalid", strings.Repeat("x", 4097), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var db obj
			d := json.NewDecoder(bytes.NewReader(reviewFixture()))
			d.UseNumber()
			if err := d.Decode(&db); err != nil {
				t.Fatal(err)
			}
			recovery := object(object(array(db["items"])[1])["recovery"])
			if tc.absent {
				delete(recovery, "request_source_job_id")
			} else {
				recovery["request_source_job_id"] = tc.value
			}
			source, err := json.Marshal(db)
			if err != nil {
				t.Fatal(err)
			}
			report, err := projectReviewMetadata(source)
			if err != nil {
				t.Fatal(err)
			}
			ref := object(object(report["recovery"])["request_source_job_id"])
			if ref["state"] != tc.want {
				t.Fatal("invalid reference became a recorded reference")
			}
			if _, exists := ref["reference_format"]; exists {
				t.Fatal("an unavailable reference was classified by syntax")
			}
		})
	}
}
