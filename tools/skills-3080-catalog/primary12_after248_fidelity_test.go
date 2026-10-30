package main

import "testing"

func TestNextTwelveAfter248KeepsOriginalPacketsAndNullParent(t *testing.T) {
	const file = "skills-3080-primaryflow-next12-after248-evaluation095653.json"
	const sha = "248091e116ccc6aadbc32cf63283aea6a8d6229a3855ab51fd70e7c28e8c58f2"
	requireAssessmentInputRegistered(t, file, sha, 12)
	var v map[string]any
	if travel3Digest(loadPublicReceipt(t, file, &v)) != sha {
		t.Fatal("independent12 source changed")
	}
	if err := validateNextTwelveAfter248(v); err != nil {
		t.Fatal(err)
	}
}

func validateNextTwelveAfter248(v map[string]any) error {
	actors := map[string][5]string{}
	for _, hash := range []string{"06c5bf148ae735bb698723ceb7dd7fe8d7f9d3624043f7ac193c573a26f06d39", "acd485712541b8678fb65e0c55976d552f13f90f34e85ae78004f1c26e3116cc"} {
		actors[hash] = [5]string{"20261030-033405-todo-4134", "20261030-095653-todo-3080", "gpt-6.1-sol", "b0bfc631a8d355a1c406003da7c417b668dabd49dccf48b1d11ffa18a3132b72", ""}
	}
	return validateResumedVisibleSnapshot(v, 12, 0, 12, 2,
		map[string]any{"records": float64(247), "sha256": "5f80f085429c44f0c29a60b2bbc0a97bbed35bf2efff6a54af225d3608efaec2", "all_records_and70_original_contexts_and_held1_exactly_retained": true},
		map[string]any{"records": float64(259), "held_groups": float64(1), "acknowledged_groups": float64(260), "original_packets": float64(72), "sha256": "94f4fddf51c98eafc14466d9e9a64235ade779922131bdc7014288b981bc85ba"}, actors)
}

func TestPrimaryReceiptBindsExactPacketThroughAndActualNativeTurns(t *testing.T) {
	for _, change := range []string{"valid", "swapped ACK", "ACK id", "ACK sha", "ACK through", "native consumer", "native contexts missing", "native contexts empty", "native turn model", "native turn effort"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			loadPublicReceipt(t, "skills-3080-primaryflow-next12-after248-evaluation095653.json", &v)
			contexts := next4Object(v["shared_original_contexts"])
			c := next4Object(contexts["acd485712541b8678fb65e0c55976d552f13f90f34e85ae78004f1c26e3116cc"])
			ack := next4Object(c["independent_parent_ack"])
			native := next4Object(c["independent_native_metadata"])
			switch change {
			case "swapped ACK":
				c["independent_parent_ack"] = next4Object(contexts["06c5bf148ae735bb698723ceb7dd7fe8d7f9d3624043f7ac193c573a26f06d39"])["independent_parent_ack"]
			case "ACK id":
				ack["packet_id"] = "another-packet"
			case "ACK sha":
				ack["packet_sha256"] = "wrong"
			case "ACK through":
				ack["acknowledged_through"] = float64(134)
			case "native consumer":
				native["actual_consumer_job"] = "20261029-170905-todo-3080"
			case "native contexts missing":
				delete(native, "native_turn_contexts")
			case "native contexts empty":
				native["native_turn_contexts"] = []any{}
			case "native turn model":
				next4Object(native["native_turn_contexts"].([]any)[0])["model"] = "gpt-6-luna"
			case "native turn effort":
				next4Object(native["native_turn_contexts"].([]any)[0])["effort"] = "low"
			}
			err := validateNextTwelveAfter248(v)
			if (err == nil) != (change == "valid") {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
