// This narrow repair guard is not a reconstruction of the lost private registry.
// Unknown resources remain protected; only verified repair source/explicit receipts qualify.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const base = "D:/CC/Work/runtime-recovery-20261012"

// Explicit 2026-10-13 unattended-operation repair scope; every existing root
// still needs the same independently checked Git origin and physical path.
var roots = []string{base, "D:/CC/Work/operations-owner-replies-2474", "D:/CC/Work/operations-original-records-2467", "D:/CC/Work/unattended-operations-20261013", "C:/Users/makhmeto/Desktop/dev/ADJ/cc-pc-tools-rs"}
var receipts = []string{
	"D:/CC/Work/unattended-week-readiness-20261013.md",
	"D:/CC/Work/operations-organization-request-20261012.md",
	"D:/CC/Work/operations-owner-reply-followup-2474.md",
	"D:/CC/Work/operations-owner-replies-parent-steering-2474.md",
	"D:/CC/Work/operations-parent-2472.active",
	"D:/CC/Work/operations-verified-status-receipt-2472.json",
	"D:/CC/Work/operations-unlisted-question-receipt-2473.json",
	"D:/CC/Work/operations-unlisted-question-2472.txt",
	"D:/CC/Work/operations-unlisted-inventory-2472.json",
	"D:/CC/Work/operations-unlisted-decision-2473.json",
	"D:/CC/Work/operations-owner-replies-2473.json",
	"D:/CC/Work/operations-membership-choice-2473.json",
}

func physical(path string) (string, error) {
	path, err := portable(path)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}
func portable(path string) (string, error) {
	if strings.HasPrefix(path, `\\?\UNC\`) {
		return `\\` + path[8:], nil
	}
	if strings.HasPrefix(path, `\\?\`) {
		rest := path[4:]
		if len(rest) >= 3 && ((rest[0] >= 'A' && rest[0] <= 'Z') || (rest[0] >= 'a' && rest[0] <= 'z')) && rest[1] == ':' && rest[2] == '\\' {
			return rest, nil
		}
		return "", os.ErrInvalid
	}
	if strings.HasPrefix(path, `\\.\`) {
		return "", os.ErrInvalid
	}
	return path, nil
}
func same(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func within(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
func sourceVerified(root string) bool {
	return sourceOriginVerified(root, "autodevjapan/cc-pc-tools-rs")
}
func sourceOriginVerified(root, repository string) bool {
	output, ok := currentGit(root, "remote", "get-url", "origin")
	if !ok {
		return false
	}
	origin := strings.ToLower(output)
	return origin == "https://github.com/"+repository+".git" || origin == "https://github.com/"+repository || origin == "git@github.com:"+repository+".git"
}
func boundedSchedulerTree(path string) bool {
	// Only standard scheduler children of these already verified PC-tool sources.
	// This is workspace creation scope, not a substitute private exclusions file.
	namePattern := regexp.MustCompile(`^todo-([1-9][0-9]*)-[A-Za-z0-9_-]+$`)
	for _, root := range roots {
		container := filepath.Clean(root + "-wt")
		rel, err := filepath.Rel(container, path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			continue
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		match := namePattern.FindStringSubmatch(parts[0])
		if match == nil {
			continue
		}
		// Git is an external process. Check path membership first so one
		// unrelated root cannot consume every scheduled repair's deadline.
		// The matching root still needs the same origin and physical checks.
		if !sourceVerified(root) {
			continue
		}
		if _, err := os.Stat(container); err == nil {
			resolved, e := physical(container)
			if e != nil || !same(resolved, container) {
				continue
			}
		} else if !os.IsNotExist(err) {
			continue
		}
		parent, e := physical(filepath.Dir(container))
		rootParent, e2 := physical(filepath.Dir(root))
		if e != nil || e2 != nil || !same(parent, rootParent) {
			continue
		}
		workspace := filepath.Join(container, parts[0])
		if _, err := os.Stat(workspace); os.IsNotExist(err) {
			return len(parts) == 1
		}
		resolved, e := physical(workspace)
		if e != nil || !same(resolved, workspace) || !sourceVerified(workspace) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		command := exec.CommandContext(ctx, "git", "-C", workspace, "branch", "--show-current")
		command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		command.WaitDelay = time.Second
		branch, e := command.Output()
		cancel()
		if e != nil || !strings.HasPrefix(strings.TrimSpace(string(branch)), "scheduler/todo-"+match[1]+"-") {
			continue
		}
		if target, e := physical(path); e == nil && within(resolved, target) {
			return true
		}
	}
	return false
}
func legacyMatch(path string) bool {
	state := ownerLegacyProtection(path)
	return state == ownerLegacyClient || state == ownerLegacyUnavailable
}
func allowed(path string) bool {
	return classifyOwnerPath(path).AccessAllowed
}

// Existing, purpose-checked operational receipts remain supported. They do not
// define the owner's protection categories or replace normal source identity.
func repairPathAllowed(path string) bool {
	path, e := portable(path)
	if e != nil {
		return false
	}
	absolute, err := filepath.Abs(path)
	if err != nil || legacyMatch(absolute) {
		return false
	}
	if owned2736PathAllowed(absolute) {
		return true
	}
	if requestedResumePathAllowed(absolute) {
		return true
	}
	if consoleImport3264Allowed(absolute) {
		return true
	}
	if ombCaptureAllowed(absolute) {
		return true
	}
	// The exact launcher now has its own complete permanent-source proof.
	// It must not depend on the old linked repair checkout's Git directory.
	if modelLauncherRuntimeAllowed(absolute) {
		return true
	}
	if owned3107PathAllowed(absolute) {
		return true
	}
	if conversationPathAllowed(absolute) {
		return true
	}
	if productPathAllowed(absolute) {
		return true
	}
	if currentScopePathAllowed(absolute) {
		return true
	}
	if currentDailyVideoPathAllowed(absolute) {
		return true
	}
	if audio2906PathAllowed(absolute) {
		return true
	}
	if boundedSchedulerTree(absolute) {
		return true
	}
	if consoleRuntimeAllowed(absolute) && sourceVerified(base) {
		return true
	}
	if civ6Runtime(absolute) {
		return true
	}
	// A not-yet-created leaf has no physical path, so this precedes physical().
	if productCIRuntime(absolute, productCIDirectory, legacyMatch) && sourceVerified(base) {
		return true
	}
	if discordAskRuntime(absolute, legacyMatch) && sourceVerified(base) {
		return true
	}
	target, err := physical(absolute)
	if err != nil {
		// The one explicitly authorized new worktree may be created empty.
		planned := same(absolute, roots[1]) || same(absolute, roots[2])
		if !os.IsNotExist(err) || !planned || !sourceVerified(base) {
			return false
		}
		parent, e := physical(filepath.Dir(absolute))
		expected, e2 := physical("D:/CC/Work")
		return e == nil && e2 == nil && same(parent, expected)
	}
	if legacyMatch(target) {
		return false
	}
	if (runtimeExecutable(absolute, target, publicationGateRuntime) || runtimeExecutable(absolute, target, schedulerRuntime) || finiteRuntimeExecutable(absolute, target, jobsRuntime, time.Now().Unix()) || progressTUIRuntime(absolute, target, progressTUIDirectory)) && sourceVerified(base) {
		return true
	}
	for _, root := range roots {
		resolved, e := physical(root)
		if e == nil && within(resolved, target) && sourceVerified(resolved) {
			return true
		}
	}
	for _, receipt := range receipts {
		resolved, e := physical(receipt)
		if e == nil && same(absolute, receipt) && same(target, resolved) && sourceVerified(base) {
			return true
		}
	}
	return false
}

type guardArgs struct {
	path, succession  string
	runtimeUpdate     string
	runtimeUpdateSet  bool
	refresh           string
	refreshSet        bool
	refreshRequest    string
	refreshRequestSet bool
	product           int
	pathSet           bool
	productSet        bool
	successionSet     bool
	remainingArgs     bool
}

func parseGuardArgs(args []string) (guardArgs, error) {
	flags := flag.NewFlagSet("scrub391", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("guard-path", "", "resource absolute path")
	product := flags.Int("verify-product-resume", 0, "verify original ToDo 1626, 2407 or 2475; no launch or mutation")
	succession := flags.String("verify-current-succession", "", "verify one declared current product successor; no launch or mutation")
	refresh := flags.String("refresh-owner-source", "", "refresh one positively registered self-source identity; no source content read")
	refreshRequest := flags.String("refresh-request", "", "owned finite refresh request identity")
	runtimeUpdate := flags.String("verify-capacity-runtime-update", "", "qualify one exact ToDo3085 software update; grants no data access")
	err := flags.Parse(args)
	parsed := guardArgs{path: *path, succession: *succession, product: *product, refresh: *refresh, refreshRequest: *refreshRequest, remainingArgs: flags.NArg() != 0}
	parsed.runtimeUpdate = *runtimeUpdate
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "verify-capacity-runtime-update":
			parsed.runtimeUpdateSet = true
		case "guard-path":
			parsed.pathSet = true
		case "verify-product-resume":
			parsed.productSet = true
		case "verify-current-succession":
			parsed.successionSet = true
		case "refresh-owner-source":
			parsed.refreshSet = true
		case "refresh-request":
			parsed.refreshRequestSet = true
		}
	})
	return parsed, err
}

func currentSuccessionFlagPresent(args []string) bool {
	for _, arg := range args {
		arg = strings.TrimLeft(arg, "-")
		if arg == "verify-current-succession" || strings.HasPrefix(arg, "verify-current-succession=") {
			return true
		}
	}
	return false
}

func parseCurrentSuccessionTask(value string) (int, bool) {
	task, err := strconv.Atoi(value)
	return task, err == nil && task > 0 && strconv.Itoa(task) == value
}

func printGuardUsage(output io.Writer) {
	fmt.Fprintln(output, "Usage of scrub391:")
	fmt.Fprintln(output, "  --prepare-official-ci3387\n    \tprepare the one fixed official public Windows SDK using normal new ownership")
	fmt.Fprintln(output, "  --verify-official-ci3387 <receipt-id>\n    \tverify original purpose, identities and public bytes; metadata only")
	fmt.Fprintln(output, "  --consume-official-ci3387 <receipt-id>\n    \treturn verified public software byte snapshots once to the existing CI provider; no launch")
	fmt.Fprintln(output, "  --verify-official-ci3387-update\n    \tcompare only the previously published guard software for this merged repair; no update or data access")
	fmt.Fprintln(output, "  -guard-path string")
	fmt.Fprintln(output, "    \tresource absolute path")
	fmt.Fprintln(output, "  -verify-product-resume int")
	fmt.Fprintln(output, "    \tverify original ToDo 1626, 2407 or 2475; no launch or mutation")
	fmt.Fprintln(output, "  -verify-current-succession int")
	fmt.Fprintln(output, "    \tverify one declared current product successor; no launch or mutation")
	fmt.Fprintln(output, "  -refresh-owner-source string\n    \trefresh one positively registered self-source identity (bounded metadata only)")
	fmt.Fprintln(output, "  -verify-capacity-runtime-update string")
	fmt.Fprintln(output, "    \tqualify one exact ToDo3085 software update; grants no data access")
	fmt.Fprintln(output, "  -verify-model-runtime-update string")
	fmt.Fprintln(output, "    \tqualify one exact ToDo3081 software update; grants no data access")
	fmt.Fprintln(output, "  -export-omb-runtime-capture")
	fmt.Fprintln(output, "    \tmaterialize the one independently verified new OMB report capture; no UI approval")
	fmt.Fprintln(output, "  -qualify-skills3080 string\n    \tcheck one manifest-bound CreateNew request; no general classification change")
	fmt.Fprintln(output, "  -create-skills3080 string\n    \tconsume the same reviewed bytes through exclusive new creation")
	fmt.Fprintln(output, "  -create-skills3080-forward string\n    \tCreateNew and issue current-handle evidence for one canonical native invocation")
	fmt.Fprintln(output, "  -consume-skills3080 string\n    \treturn exact public bytes from pinned current handles after native creation verification")
	fmt.Fprintln(output, "  --consume-skills3080-admitted\n    \tconsume only the actual registered readonly caller's immutable public Skill scope")
	fmt.Fprintln(output, "  -receive-skills3080-result string\n    \treceive one registered public final response and bounded native model projection")
	fmt.Fprintln(output, "  -verify-skills3080-update string\n    \tqualify the reviewed merged Windows guard update only; no data access")
	fmt.Fprintln(output, "  --verify-omb-producer-3255")
	fmt.Fprintln(output, "    \tcompare four original OMB software components; no source or data access grant")
}

func verifyCurrentSuccession(taskID int) object {
	db, err := readObject(productStore, 64<<20)
	if err != nil {
		return object{"verified": false}
	}
	if _, ok := requested3650Product(taskID); ok {
		return requestedResumeReceipt(db, taskID)
	}
	if declared, ok := currentProductByTask(taskID); ok {
		return currentSuccessionReceipt(db, declared)
	}
	receipt, _, ok := currentSubworkReceiptWith(db, taskID, currentProducts, currentTodoSpecsRoot, time.Now(), legacyMatch, currentPlain, sourceOriginVerified, currentGitHubIdentityVerified, currentGitCommonDir, currentProductPath)
	if !ok {
		return object{"verified": false}
	}
	return receipt
}

func runGuardCLI(args []string, stdout, stderr io.Writer, currentVerifier func(int) object) int {
	if official3894Flag(args) {
		return runOfficial3894CLI(args, stdout, official3894Operation)
	}
	if omb3255FlagPresent(args) {
		return runOMB3255CLI(args, stdout, verifyOMB3255Software)
	}
	if skillsFlag(args) {
		return runSkillsCLI(args, stdout, skillsOperation)
	}
	if ombCaptureFlagPresent(args) {
		return runOMBCaptureCLI(args, stdout, exportOMBCapture)
	}
	if model3081FlagPresent(args) {
		return runModel3081CLI(args, stdout, model3081RuntimeReceipt)
	}
	return runGuardCLIWithRuntime(args, stdout, stderr, currentVerifier, capacity3085RuntimeReceipt)
}

func runGuardCLIWithRuntime(args []string, stdout, stderr io.Writer, currentVerifier func(int) object, runtimeVerifier func(string) object) int {
	return runGuardCLIWithVerifiers(args, stdout, stderr, currentVerifier, runtimeVerifier, classifyOwnerPath)
}

func runGuardCLIWithPath(args []string, stdout, stderr io.Writer, currentVerifier func(int) object, pathVerifier func(string) ownerPathVerdict) int {
	return runGuardCLIWithVerifiers(args, stdout, stderr, currentVerifier, capacity3085RuntimeReceipt, pathVerifier)
}

func runGuardCLIWithVerifiers(args []string, stdout, stderr io.Writer, currentVerifier func(int) object, runtimeVerifier func(string) object, pathVerifier func(string) ownerPathVerdict) int {
	parsed, err := parseGuardArgs(args)
	if err != nil {
		if capacity3085FlagPresent(args) {
			_ = json.NewEncoder(stdout).Encode(object{"qualified": false})
			return 1
		}
		if currentSuccessionFlagPresent(args) {
			_ = json.NewEncoder(stdout).Encode(object{"verified": false})
			return 1
		}
		if err == flag.ErrHelp {
			printGuardUsage(stderr)
			return 0
		}
		fmt.Fprintln(stderr, err)
		printGuardUsage(stderr)
		return 2
	}
	if capacity3085FlagPresent(args) {
		if !parsed.runtimeUpdateSet || parsed.pathSet || parsed.productSet || parsed.successionSet || parsed.refreshSet || parsed.refreshRequestSet || parsed.remainingArgs || !filepath.IsAbs(parsed.runtimeUpdate) {
			_ = json.NewEncoder(stdout).Encode(object{"qualified": false})
			return 1
		}
		receipt := runtimeVerifier(parsed.runtimeUpdate)
		if receipt["qualified"] != true {
			receipt = object{"qualified": false}
		}
		if json.NewEncoder(stdout).Encode(receipt) != nil || receipt["qualified"] != true {
			return 1
		}
		return 0
	}
	if currentSuccessionFlagPresent(args) && !parsed.successionSet {
		_ = json.NewEncoder(stdout).Encode(object{"verified": false})
		return 1
	}
	if parsed.refreshSet {
		if parsed.pathSet || parsed.productSet || parsed.successionSet || parsed.remainingArgs || !filepath.IsAbs(parsed.refresh) {
			_ = json.NewEncoder(stdout).Encode(object{"refreshed": false})
			return 2
		}
		ok := ownerRefreshIdentityRequest(parsed.refresh, parsed.refreshRequest)
		_ = json.NewEncoder(stdout).Encode(object{"refreshed": ok, "original_registry_recovered": false})
		if !ok {
			return 1
		}
		return 0
	}
	if parsed.refreshRequestSet {
		_ = json.NewEncoder(stdout).Encode(object{"refreshed": false})
		return 2
	}
	if parsed.successionSet {
		if parsed.pathSet || parsed.productSet || parsed.remainingArgs {
			_ = json.NewEncoder(stdout).Encode(object{"verified": false})
			return 1
		}
		taskID, validTask := parseCurrentSuccessionTask(parsed.succession)
		if !validTask {
			_ = json.NewEncoder(stdout).Encode(object{"verified": false})
			return 1
		}
		receipt := currentVerifier(taskID)
		if receipt["verified"] != true {
			receipt = object{"verified": false}
		}
		if json.NewEncoder(stdout).Encode(receipt) != nil || receipt["verified"] != true {
			return 1
		}
		return 0
	}
	if parsed.product != 0 {
		db, err := readObject(productStore, 64<<20)
		if err != nil {
			_ = json.NewEncoder(stdout).Encode(object{"verified": false, "reason": "store_unavailable"})
			return 1
		}
		r := verifyProduct(db, parsed.product)
		_ = json.NewEncoder(stdout).Encode(r)
		if r["verified"] != true {
			return 1
		}
		return 0
	}
	ok := parsed.path != "" && filepath.IsAbs(parsed.path)
	verdict := ownerUnverified()
	if ok {
		verdict = pathVerifier(parsed.path)
	}
	retirement := ok && (retirement3250PathAllowed(parsed.path) || ci3574RootAllowed(parsed.path))
	if retirement && verdict.Status == "unverified" && !ownerCardPath(parsed.path) {
		verdict = ownerPathVerdict{AccessAllowed: true, Status: "clear", Categories: []string{}}
	}
	result := map[string]any{"ok": ok, "client_resource": !verdict.AccessAllowed, "classification_unverified": verdict.Status == "unverified", "ci_retirement_qualified": retirement, "original_registry_recovered": false, "scope": "owner protection policy: client projects and owner card information; verified sources and purpose-checked receipts", "access_allowed": verdict.AccessAllowed, "protection_status": verdict.Status, "protection_categories": verdict.Categories}
	_ = json.NewEncoder(stdout).Encode(result)
	if !ok {
		return 2
	}
	return 0
}

func main() {
	os.Exit(runGuardCLI(os.Args[1:], os.Stdout, os.Stderr, verifyCurrentSuccession))
}
