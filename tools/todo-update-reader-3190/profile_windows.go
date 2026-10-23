package main

import "path/filepath"

// The qualification this reader issues. Field names are the public contract
// returned to the ToDo3190 installer; failures print only {"qualified":false}.
type qualification struct {
	Qualified                   bool     `json:"qualified"`
	Domain                      string   `json:"domain"`
	TodoID                      int      `json:"todo_id"`
	VerifierTodoID              int      `json:"verifier_todo_id"`
	TargetKind                  string   `json:"target_kind"`
	Target                      string   `json:"target"`
	CurrentSHA256               string   `json:"current_sha256"`
	NewSHA256                   string   `json:"new_sha256"`
	NewBytes                    int64    `json:"new_bytes"`
	SourceRepository            string   `json:"source_repository"`
	SourceRoot                  string   `json:"source_root"`
	SourceHead                  string   `json:"source_head"`
	SourceTree                  string   `json:"source_tree"`
	BuildReceiptSHA256          string   `json:"build_receipt_sha256"`
	BuildJob                    string   `json:"build_job"`
	BuildCommand                []string `json:"build_command"`
	BuildLogSHA256              string   `json:"build_log_sha256"`
	DepInfoSHA256               string   `json:"dep_info_sha256"`
	InputsVerified              int      `json:"inputs_verified"`
	DepInfoEntries              int      `json:"dep_info_entries"`
	LocalCrates                 int      `json:"local_crates"`
	BaselineQualifiedBy         string   `json:"baseline_qualified_by"`
	BaselineSourceReceiptSHA256 string   `json:"baseline_source_receipt_sha256"`
	DataAccessGranted           bool     `json:"data_access_granted"`
	OriginalRegistryRecovered   bool     `json:"original_registry_recovered"`
}

const (
	qualificationDomain = "todo-runtime-update-3190-v1"
	qualificationTodo   = 3190
	verifierTodo        = 3463
	qualificationKind   = "todo"
)

// The installed baseline must first be qualified by the deployed guard's
// existing ToDo3085 capacity purpose. That old qualification only proves the
// current software; it is not renamed into this update's qualification.
type capacityExpectation struct {
	domain     string
	todoID     int64
	targetKind string
	oldSHA     string
	receiptSHA string
}

// The independent build receipt written by ToDo3190's normal Cargo run.
type receiptExpectation struct {
	domain              string
	todoID              int64
	job                 string
	repository          string
	command             []string
	baselineQualifiedBy string
}

// profile holds every compiled pin. Only tests construct other profiles; the
// CLI always uses productionProfile and accepts no caller-supplied values.
type profile struct {
	target         string
	root           string
	head           string
	tree           string
	origins        []string
	mainRef        string
	receiptName    string
	receiptSHA     string
	logName        string
	logSHA         string
	depInfoName    string
	depInfoSHA     string
	candidateName  string
	candidateSHA   string
	candidateBytes int64
	baselineSHA    string
	capacity       capacityExpectation
	receipt        receiptExpectation
	inputs         int
	depEntries     int
	localCrates    int
}

var repositoryOrigins = []string{
	"https://github.com/autodevjapan/cc-pc-tools-rs.git",
	"https://github.com/autodevjapan/cc-pc-tools-rs",
	"git@github.com:autodevjapan/cc-pc-tools-rs.git",
}

// One leaf, one baseline, one candidate, one source, one receipt.
var productionProfile = profile{
	target:         filepath.Clean("D:/CC/Tools/todo/todo.exe"),
	root:           filepath.Clean("D:/CC/Work/unattended-operations-20261013-wt/todo-3190-3110"),
	head:           "0e16e81798ff4463c6ae6bcf6c4c57a695d9d13c",
	tree:           "31d45a745d27271420e764f6dff493e3d58b3731",
	origins:        repositoryOrigins,
	mainRef:        "refs/remotes/origin/main",
	receiptName:    "todo3190-build-0e16e817.private.json",
	receiptSHA:     "20345f443566c16553dbd959d5cd07b53b8aa0e1579a55bff1a614ab0f75087c",
	logName:        "todo3190-release-0e16e817.private.log",
	logSHA:         "1fe224b19ba4d5265711e0119e554a035aa15b7cc7f4b0da327478bafb9fe1ef",
	depInfoName:    "target/release/todo.d",
	depInfoSHA:     "da3ce6ae3fcebe72fb5cecb701fc23efa70571086a5cd73a7d7642aca1e96941",
	candidateName:  "todo3190-todo-0e16e817.private.exe",
	candidateSHA:   "d4392e6326f1868e2ffddc31a80e83d3170f93d639b1a04656066be387180718",
	candidateBytes: 8017920,
	baselineSHA:    "f8710b516d3ecdd37564d7a1ab978c8689ab1bdd2ffaec4150d0e4bed4c0b983",
	capacity: capacityExpectation{
		domain:     "capacity-runtime-update-3085",
		todoID:     3085,
		targetKind: "todo",
		oldSHA:     "4b351db686ae315b884d75be952453c20204d25ab81eb1643df106e335b3364f",
		receiptSHA: "a3bc15ae1a7b8eb9cc1f06dbf379b587eecea9f379e1eab48768372c91e46786",
	},
	receipt: receiptExpectation{
		domain:              "todo3190-production-build-v1",
		todoID:              3190,
		job:                 "20261024-001451-todo-3190",
		repository:          "AutoDevJapan/cc-pc-tools-rs",
		command:             []string{"cargo", "build", "--release", "--locked", "-p", "cc-todo"},
		baselineQualifiedBy: "scrub391 --verify-capacity-runtime-update D:/CC/Tools/todo/todo.exe",
	},
	inputs:      126,
	depEntries:  116,
	localCrates: 8,
}
