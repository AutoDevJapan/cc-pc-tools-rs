package main

import (
	"fmt"
	"testing"
	"time"
)

// Validate a limited observation report, not historical authentication. The
// account/login snapshot is current; producer model identity is independent.
// Neither proves which credentials served the earlier requests. Those requests
// therefore remain explicitly unknown, including when the current snapshot is
// entirely OAuth. No credential or historical billing material is opened here.
func validateSubscriptionObservation(v map[string]any, expectedJobs []string) error {
	if e := next4Fields(v, map[string]any{"mode": "unknown_historical_execution", "current_observed_mode": "oauth_subscription_only", "historical_execution_authentication_status": "unknown", "historical_execution_authentication_proven": false, "normal_login_status_chatgpt": true, "normal_login_status_api_key": false, "all_types_codex_or_oauth": true, "all_additional_credits_disabled": true, "raw_credentials_account_ids_labels_or_balances_returned_or_saved": false, "historical_per_request_billing_proven": false}); e != nil {
		return e
	}
	if reason, ok := v["historical_execution_authentication_limitation"].(string); !ok || reason == "" {
		return fmt.Errorf("unknown historical authentication limitation absent")
	}
	at, ok := v["observed_at"].(string)
	if !ok {
		return fmt.Errorf("subscription observation time absent")
	}
	if _, e := time.Parse(time.RFC3339Nano, at); e != nil {
		return fmt.Errorf("subscription observation time invalid")
	}
	if source, ok := v["source"].(string); !ok || source == "" {
		return fmt.Errorf("subscription observation source absent")
	}
	rows, ok := v["original_producer_conditions"].([]any)
	if !ok || len(rows) != len(expectedJobs) || len(expectedJobs) == 0 {
		return fmt.Errorf("exact producer execution conditions absent")
	}
	wanted := map[string]bool{}
	for _, id := range expectedJobs {
		if id == "" || wanted[id] {
			return fmt.Errorf("producer expectation missing/duplicated")
		}
		wanted[id] = true
	}
	for _, row := range rows {
		r := next4Object(row)
		id, _ := r["producer_job"].(string)
		if !wanted[id] {
			return fmt.Errorf("producer identity absent/repeated/outside result")
		}
		delete(wanted, id)
		if e := next4Fields(r, map[string]any{"model": "gpt-6-luna", "effort": "max", "model_origin": "owner_request", "effort_origin": "owner_request", "metadata_equals_independently_received_native_identity": true}); e != nil {
			return e
		}
	}
	if len(wanted) != 0 {
		return fmt.Errorf("producer conditions incomplete")
	}
	return nil
}

func TestSubscriptionExecutionRejectsMissingKeyModeOrUnboundProducer(t *testing.T) {
	jobs := []string{"20261029-000437-todo-4193", "20261029-033138-todo-4203", "20261029-035105-todo-4204"}
	for _, mutation := range []string{"valid", "missing", "api key mode", "current api key mode", "login missing", "credits enabled", "producer missing", "producer identity", "producer origin", "historical billing invented", "historical authentication invented", "unknown limitation missing"} {
		t.Run(mutation, func(t *testing.T) {
			var h map[string]any
			loadPublicReceipt(t, "skills-3080-next-four-installation-evidence175645.json", &h)
			v := next4Object(h["subscription_execution"])
			switch mutation {
			case "missing":
				v = nil
			case "api key mode":
				v["mode"] = "api_key"
			case "current api key mode":
				v["current_observed_mode"] = "api_key"
			case "login missing":
				delete(v, "normal_login_status_chatgpt")
			case "credits enabled":
				v["all_additional_credits_disabled"] = false
			case "producer missing":
				v["original_producer_conditions"] = []any{}
			case "producer identity":
				next4Object(v["original_producer_conditions"].([]any)[0])["producer_job"] = "other"
			case "producer origin":
				next4Object(v["original_producer_conditions"].([]any)[0])["model_origin"] = "policy"
			case "historical billing invented":
				v["historical_per_request_billing_proven"] = true
			case "historical authentication invented":
				v["historical_execution_authentication_proven"] = true
			case "unknown limitation missing":
				delete(v, "historical_execution_authentication_limitation")
			}
			e := validateSubscriptionObservation(v, jobs)
			if mutation == "valid" && e != nil || mutation != "valid" && e == nil {
				t.Fatalf("subscription mutation %s: %v", mutation, e)
			}
		})
	}
}

func TestSubscriptionObservationRejectsUnprovenHistoricalAuthentication(t *testing.T) {
	for _, claim := range []string{"verified_oauth_subscription", "verified_api_key", ""} {
		t.Run(claim, func(t *testing.T) {
			var h map[string]any
			loadPublicReceipt(t, "skills-3080-rental-ledger-installation-evidence175645.json", &h)
			v := next4Object(h["subscription_execution"])
			v["historical_execution_authentication_status"] = claim
			if err := validateSubscriptionObservation(v, []string{"20261029-061701-todo-4208"}); err == nil {
				t.Fatal("current login/account metadata accepted as historical authentication proof")
			}
		})
	}
}
