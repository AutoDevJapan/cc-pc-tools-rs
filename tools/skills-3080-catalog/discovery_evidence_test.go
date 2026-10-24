package main

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

type discoveryEvidence struct {
	Source struct {
		Depth       int `json:"depth"`
		Directories int `json:"directories"`
		Semantics   struct {
			Binary bool `json:"source_binary_equivalence_proven"`
		} `json:"directory_count_semantics"`
	} `json:"source_limits"`
	NonDot      float64 `json:"non_dot_directory_count"`
	Measurement struct {
		ByDepth    map[string]float64 `json:"non_dot_directories_by_depth"`
		Comparable float64            `json:"directory_count_comparable_to_source"`
		Former     float64            `json:"previously_reported_count_excluding_max_depth"`
		NonDot     float64            `json:"non_dot_directories"`
	} `json:"metadata_remeasurement"`
	Resolved struct {
		Supported     bool   `json:"explanation_of_scan_limit_supported"`
		Scope         string `json:"explanation_support_scope"`
		WarningLinked bool   `json:"warning_cause_linked"`
	} `json:"resolved"`
}

func validateDiscoveryProjection(e discoveryEvidence) error {
	if e.Source.Depth != 6 || e.Source.Directories != 2000 || e.Measurement.ByDepth["0"] != 1 {
		return errors.New("fixed source limit or counted root was lost")
	}
	sum := 0.0
	for depth, count := range e.Measurement.ByDepth {
		d, err := strconv.Atoi(depth)
		if err != nil || d < 0 || d > e.Source.Depth || count < 0 || math.IsNaN(count) || math.IsInf(count, 0) || count != math.Trunc(count) {
			return errors.New("invalid measured source depth/count")
		}
		sum += count
	}
	boundary := e.Measurement.ByDepth[strconv.Itoa(e.Source.Depth)]
	if boundary <= 0 || e.Measurement.Comparable != sum || e.Measurement.NonDot != sum || e.NonDot != sum || e.Measurement.Former+boundary != sum || sum <= float64(e.Source.Directories) {
		return errors.New("parent depth boundary was excluded from the counted child directories")
	}
	if !e.Resolved.Supported || e.Resolved.Scope == "" || e.Resolved.WarningLinked || e.Source.Semantics.Binary {
		return errors.New("source/count consistency was promoted into unproven warning or binary causation")
	}
	return nil
}

func TestDiscoveryDepthProjectionKeepsBoundaryAndUnprovenCauseSeparate(t *testing.T) {
	for _, kind := range []string{"valid", "exclude_boundary", "drop_root", "binary_equivalence", "warning_cause"} {
		t.Run(kind, func(t *testing.T) {
			var e discoveryEvidence
			loadPublicReceipt(t, "skills-3080-discovery-limit-evidence.json", &e)
			switch kind {
			case "exclude_boundary":
				e.Measurement.Comparable = e.Measurement.Former
			case "drop_root":
				e.Measurement.ByDepth["0"] = 0
			case "binary_equivalence":
				e.Source.Semantics.Binary = true
			case "warning_cause":
				e.Resolved.WarningLinked = true
			}
			if err := validateDiscoveryProjection(e); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
