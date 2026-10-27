package main

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func validateNext3FlowProof(installation, creation, readback []byte) error {
	var install struct {
		Batches *int `json:"normal_read_batches"`
	}
	var read struct {
		Batches *int `json:"bounded_normal_batches"`
		Rows    []struct {
			Target       string `json:"target"`
			Files, Bytes *int
			Indices      []int  `json:"indices"`
			Qualified    *bool  `json:"qualified"`
			Response     string `json:"response_sha256"`
		} `json:"batches"`
	}
	var native struct {
		At     string `json:"native_call_timestamp"`
		Failed []struct {
			Call     string `json:"native_call_id"`
			At       string `json:"timestamp"`
			SHA      string `json:"CLI_stdout_sha256"`
			Reason   string `json:"reason"`
			Created  *bool  `json:"created_evidence_returned"`
			Prewrite *bool  `json:"source_or_review_reason_branches_precede_skillsCreate_in_reviewed_deb430_code"`
		} `json:"prior_failed_exact_invocation_attempts"`
	}
	if json.Unmarshal(installation, &install) != nil || json.Unmarshal(readback, &read) != nil || json.Unmarshal(creation, &native) != nil || !next3Value(install.Batches, 3) || !next3Value(read.Batches, 3) || len(read.Rows) != 3 || len(native.Failed) != 1 {
		return errors.New("next3 three read batches and original failed attempt must remain explicit")
	}
	names := []string{"hotels", "lounges", "wallet"}
	sizes := []int{24311, 20020, 21235}
	responses := []string{"e505a26d8ff8d6c296c10e015e0bd59bd582f78192c75baacacdd1135b741c53", "65c824078f31dfd4e97047fde9f82f5a03a1a0db087835afd0c43d8be17d7202", "801ab6d279f9e01adb6f390b947dc53bbb8d0b56006d0bfd48f0945c1fb80ef3"}
	for i, row := range read.Rows {
		if row.Target != names[i] || !next3Value(row.Files, 7) || !next3Value(row.Bytes, sizes[i]) || !recordedTrue(row.Qualified) || len(row.Indices) != 7 || row.Response != responses[i] {
			return errors.New("next3 bounded read batch identity differs")
		}
		for j, index := range row.Indices {
			if index != i*7+j {
				return errors.New("next3 read batch indices overlap, skip, or reorder")
			}
		}
	}
	f := native.Failed[0]
	before, e1 := time.Parse(time.RFC3339Nano, f.At)
	after, e2 := time.Parse(time.RFC3339Nano, native.At)
	if f.Call != "call_3f19872af67b4c598df946770bbc3381" || f.SHA != "cc7288e5b39781026d250a19148760bdd979bae563a5051b013200b4dd45d74c" || f.Reason != "review" || !recordedFalse(f.Created) || !recordedTrue(f.Prewrite) || e1 != nil || e2 != nil || !before.Before(after) {
		return errors.New("next3 original prewrite failure identity or sequence differs")
	}
	return nil
}

func next3Value(value *int, want int) bool { return value != nil && *value == want }

func validateNext3Precreation(raw []byte) error {
	var proof struct {
		Domain         string `json:"domain"`
		Request        string `json:"request_sha256"`
		Manifest       string `json:"manifest_sha256"`
		CurrentAbsence *bool  `json:"current_absence_asserted"`
		Revalidated    *bool  `json:"mutation_revalidated_before_CreateNew"`
		Calls          *int   `json:"new_Create_calls_by_publication"`
		Observations   []struct {
			Phase      string `json:"phase"`
			Wrapper    string `json:"original_complete_wrapper_sha256"`
			Invocation string `json:"exact_forward_invocation_sha256"`
			Projection *bool  `json:"projection_is_not_a_new_issuer_receipt"`
			Response   struct {
				Qualified *bool  `json:"qualified"`
				Files     *int   `json:"file_count"`
				Created   *int   `json:"created_files"`
				Request   string `json:"request_sha256"`
				Manifest  string `json:"manifest_sha256"`
				General   *bool  `json:"general_access_granted"`
				Registry  *bool  `json:"original_registry_recovered"`
			} `json:"response_projection"`
		} `json:"normal_readonly_qualification_observations"`
	}
	if json.Unmarshal(raw, &proof) != nil || proof.Domain != "skills3080-next3-precreation-qualification-history-v1" || proof.Request != "b74fe84e8da49c63f75cfe41cb905c0493d60eeaa51edfa31fd9182874a0dda2" || proof.Manifest != next3OperationSHA || !recordedFalse(proof.CurrentAbsence) || !recordedTrue(proof.Revalidated) || !next3Value(proof.Calls, 0) || len(proof.Observations) != 2 {
		return errors.New("next3 precreation history cannot become current absence or new creation authority")
	}
	for i, observation := range proof.Observations {
		phase := []string{"before_first_native_attempt", "after_prewrite_failure_before_single_retry"}[i]
		r := observation.Response
		if observation.Phase != phase || observation.Wrapper != "1d5a0a1b069ce61f8c3d0d563508e62f2bc740585df416c63975366ffec7d3db" || observation.Invocation != "2da7e9a8bf4388fac8b62cad5e6f920df5703d16796e568d87b80d5bbae402b0" || !recordedTrue(observation.Projection) || !recordedTrue(r.Qualified) || !next3Value(r.Files, 21) || !next3Value(r.Created, 0) || r.Request != proof.Request || r.Manifest != proof.Manifest || !recordedFalse(r.General) || !recordedFalse(r.Registry) {
			return errors.New("next3 original precreation projection changed")
		}
	}
	return nil
}

func TestNext3FlowProofRejectsLostFailureOrBatchFacts(t *testing.T) {
	for _, change := range []string{"valid", "batch count", "missing batch count", "duplicate index", "missing failed attempts", "wrong failed hash", "missing prewrite fact", "creation evidence on failure", "failure after success"} {
		t.Run(change, func(t *testing.T) {
			var install, native, read map[string]any
			loadPublicReceipt(t, "skills-3080-next3-installation-evidence175645.json", &install)
			loadPublicReceipt(t, "skills-3080-next3-create-evidence175645.json", &native)
			loadPublicReceipt(t, "skills-3080-next3-consume-evidence175645.json", &read)
			switch change {
			case "batch count":
				install["normal_read_batches"] = float64(1)
			case "missing batch count":
				delete(read, "bounded_normal_batches")
			case "duplicate index":
				rows := received27Array(t, read["batches"])
				received27Object(t, rows[1])["indices"] = []any{float64(0), float64(8), float64(9), float64(10), float64(11), float64(12), float64(13)}
			case "missing failed attempts":
				delete(native, "prior_failed_exact_invocation_attempts")
			case "wrong failed hash":
				received27Object(t, received27Array(t, native["prior_failed_exact_invocation_attempts"])[0])["CLI_stdout_sha256"] = "changed"
			case "missing prewrite fact":
				delete(received27Object(t, received27Array(t, native["prior_failed_exact_invocation_attempts"])[0]), "source_or_review_reason_branches_precede_skillsCreate_in_reviewed_deb430_code")
			case "creation evidence on failure":
				received27Object(t, received27Array(t, native["prior_failed_exact_invocation_attempts"])[0])["created_evidence_returned"] = true
			case "failure after success":
				received27Object(t, received27Array(t, native["prior_failed_exact_invocation_attempts"])[0])["timestamp"] = "2026-10-28T00:00:00Z"
			}
			i, err := json.Marshal(install)
			if err != nil {
				t.Fatal(err)
			}
			n, err := json.Marshal(native)
			if err != nil {
				t.Fatal(err)
			}
			r, err := json.Marshal(read)
			if err != nil {
				t.Fatal(err)
			}
			err = validateNext3FlowProof(i, n, r)
			if (change == "valid") != (err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
