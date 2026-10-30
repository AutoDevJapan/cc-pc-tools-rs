package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func travel3ObservationBodies(t *testing.T, operation string) map[string][]byte {
	t.Helper()
	var value struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	loadPublicReceipt(t, operation, &value)
	result := map[string][]byte{}
	for _, file := range value.Files {
		body, err := base64.StdEncoding.DecodeString(file.Body)
		if err != nil || travel3Digest(body) != file.SHA || result[file.Target] != nil {
			t.Fatal("original observation scenario inputs differ")
		}
		result[file.Target] = body
	}
	return result
}

func TestTravel3AndNext3RequireObservedZeros(t *testing.T) {
	for _, variant := range []struct {
		name, prefix, suffix, operation string
	}{
		{"travel3", "skills-3080-travel3-", "214717.json", "skills-3080-operations/create-travel3-214717.json"},
		{"next3", "skills-3080-next3-", "175645.json", "skills-3080-operations/create-travel-next3-175645.json"},
	} {
		bodies := travel3ObservationBodies(t, variant.operation)
		for _, observation := range []struct {
			artifact string
			parent   string
			field    string
		}{
			{"installation-evidence", "", "new_global_source_raw_credit"},
			{"installation-evidence", "frozen_full_content_review", "Must"},
			{"application", "", "new_service_or_booking_payment_monitoring_or_notification_operations"},
			{"discovery", "", "parse_error_count"},
			{"discovery", "", "same_names_other_paths"},
			{"actual-structure", "", "exit"},
		} {
			for _, change := range []string{"valid", "missing", "null"} {
				t.Run(variant.name+"/"+observation.field+"/"+change, func(t *testing.T) {
					var value map[string]any
					loadPublicReceipt(t, variant.prefix+observation.artifact+variant.suffix, &value)
					object := value
					if observation.parent != "" {
						object = received27Object(t, value[observation.parent])
					}
					if change == "missing" {
						delete(object, observation.field)
					} else if change == "null" {
						object[observation.field] = nil
					}
					raw, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					switch observation.artifact {
					case "installation-evidence":
						var install travel3Install
						var native travel3Native
						var read travel3Read
						if err = json.Unmarshal(raw, &install); err != nil {
							t.Fatal(err)
						}
						loadPublicReceipt(t, variant.prefix+"create-evidence"+variant.suffix, &native)
						loadPublicReceipt(t, variant.prefix+"consume-evidence"+variant.suffix, &read)
						if variant.name == "next3" {
							err = validateNext3Header(install, native, read)
						} else {
							err = validateTravel3Header(install, native, read)
						}
					case "application":
						var application travel3Application
						if err = json.Unmarshal(raw, &application); err != nil {
							t.Fatal(err)
						}
						if variant.name == "next3" {
							err = validateNext3Application(application, bodies)
						} else {
							err = validateTravel3Application(application, bodies)
						}
					case "discovery":
						var discovery travel3Discovery
						if err = json.Unmarshal(raw, &discovery); err != nil {
							t.Fatal(err)
						}
						err = validateTravel3DiscoveryBoundary(discovery)
					case "actual-structure":
						err = validateTravel3Structure(raw)
					}
					if (change == "valid") != (err == nil) {
						t.Fatalf("%s observation: %s: %v", observation.field, change, err)
					}
				})
			}
		}
	}
}
