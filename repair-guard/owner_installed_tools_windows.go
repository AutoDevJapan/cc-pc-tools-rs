package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// These admissions identify public release bytes already independently
// obtained from the vendor archives, or published in a vendor-signed manifest
// or checksum list. Registry display strings alone are not authority, and this
// table admits no directories, arguments or caller hashes.
type ownerInstalledToolPlan struct {
	name, path, version, publisher, registry, archiveURL, archiveSHA, entry, sha string
	size int64
	aliases []string
	// Optional names are an inventory of the canonical object, not extra
	// classified paths. Only the compiled rustup row has optional names.
	optionalAliases []string
	// Per-user installers such as rustup record themselves under the current
	// user hive with their own value names. Both choices are compiled into the
	// plan; nothing is read from input.
	userRegistry bool
	originFields map[string]string
	// A registration-free plan belongs to an executable whose installer writes no
	// uninstall registration (the Claude Code native installer, an fnm-extracted
	// Node archive). It is decided by the compiled public pin and by every
	// filesystem control; only the two registry observations are skipped. It names
	// no registry value, publisher or user hive, and it declares a budget.
	noRegistration bool
	// budget bounds one classification of the plan's path. Zero selects the short
	// default of the registered rows; large pinned executables declare their own.
	budget time.Duration
}

// rustup 1.29.3 may create any of these proxies as hardlinks instead of
// symlinks. The names come from that release's TOOLS and DUP_TOOLS lists:
// https://github.com/rust-lang/rustup/blob/1.29.3/src/lib.rs
// They are admitted only as names of the canonical rustup object.
const ownerInstalledToolMaxAliases = 14

// Every expected size is compiled in and never read from input. The largest
// pinned release (Claude Code, about 384 MB) must fit; ownerInstalledToolRead
// refuses any size above this single bound before it allocates.
const (
	ownerInstalledToolMaxRead = 512 << 20
	ownerInstalledToolDefaultBudget = 2 * time.Second
	ownerInstalledToolMaxBudget = time.Minute
)

func ownerInstalledRustupHardlinkAliases() []string {
	const directory = "C:/Users/makhmeto/.cargo/bin/"
	names := []string{"rustc.exe", "rustdoc.exe", "cargo.exe", "rust-lldb.exe", "rust-gdb.exe", "rust-gdbgui.exe", "rls.exe", "cargo-clippy.exe", "clippy-driver.exe", "cargo-miri.exe", "rust-analyzer.exe", "rustfmt.exe", "cargo-fmt.exe"}
	for i := range names { names[i] = directory + names[i] }
	return names
}

func ownerInstalledToolPlans() []ownerInstalledToolPlan {
	plans := []ownerInstalledToolPlan{
		{name:"git",path:"C:/Program Files/Git/cmd/git.exe",version:"2.54.0",publisher:"The Git Development Community",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Git_is1`,archiveURL:"https://github.com/git-for-windows/git/releases/download/v2.54.0.windows.1/MinGit-2.54.0-64-bit.zip",archiveSHA:"04f937e1f0918b17b9be6f2294cb2bb66e96e1d9832d1c298e2de088a1d0e668",entry:"cmd/git.exe",sha:"81ef35ae005ca9318018d18e3327578ce939fb99feaad6b2d7c8ab15f3de8db5",size:46480,aliases:[]string{"C:/Program Files/Git/cmd/git-lfs.exe","C:/Program Files/Git/cmd/git.exe"}},
		{name:"gh",path:"C:/Program Files/GitHub CLI/gh.exe",version:"2.101.0",publisher:"GitHub, Inc.",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{05425DD6-E9FE-4AEE-B289-8B61429F042A}`,archiveURL:"https://github.com/cli/cli/releases/download/v2.101.0/gh_2.101.0_windows_amd64.zip",archiveSHA:"bc6c814367b193cd8e713611d61e36013c0ef843b8f516458fe3eda039192794",entry:"bin/gh.exe",sha:"df05089117a23889b3f6ea08d7644849afe7b6025647a09f33b45c71f1418f75",size:42755384,aliases:[]string{"C:/Program Files/GitHub CLI/gh.exe"}},
		{name:"go",path:"C:/Program Files/Go/bin/go.exe",version:"1.27.0",publisher:"https://go.dev",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{649B7E21-BFFC-4209-925C-5736BBF28E54}`,archiveURL:"https://go.dev/dl/go1.27.0.windows-amd64.zip",archiveSHA:"f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4",entry:"go/bin/go.exe",sha:"7d828191ba32519a9c9361789ab647486236ed45c660889196c7770a8ff1985c",size:17508352,aliases:[]string{"C:/Program Files/Go/bin/go.exe"}},
		{name:"gofmt",path:"C:/Program Files/Go/bin/gofmt.exe",version:"1.27.0",publisher:"https://go.dev",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{649B7E21-BFFC-4209-925C-5736BBF28E54}`,archiveURL:"https://go.dev/dl/go1.27.0.windows-amd64.zip",archiveSHA:"f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4",entry:"go/bin/gofmt.exe",sha:"e7252077f308fe8344091a168b24cb56d1366e7d8f5c64c2086ce17d262e3eef",size:3392000,aliases:[]string{"C:/Program Files/Go/bin/gofmt.exe"}},
		{name:"rustup",path:"C:/Users/makhmeto/.cargo/bin/rustup.exe",version:"1.29.3",userRegistry:true,originFields:map[string]string{"DisplayName":"Rustup: the Rust toolchain installer"},registry:`Software\Microsoft\Windows\CurrentVersion\Uninstall\Rustup`,archiveURL:"https://static.rust-lang.org/rustup/archive/1.29.3/x86_64-pc-windows-msvc/rustup-init.exe",archiveSHA:"38027d51d69a5d05493627a0634dbea52d6cd793c8f072d2373d3f4163eec7db",entry:"rustup-init.exe",sha:"38027d51d69a5d05493627a0634dbea52d6cd793c8f072d2373d3f4163eec7db",size:12839424,aliases:[]string{"C:/Users/makhmeto/.cargo/bin/rustup.exe"},optionalAliases:ownerInstalledRustupHardlinkAliases()},
		{name:"node",path:"C:/Users/makhmeto/AppData/Roaming/fnm/node-versions/v22.21.0/installation/node.exe",version:"22.21.0",noRegistration:true,budget:20*time.Second,archiveURL:"https://nodejs.org/dist/v22.21.0/node-v22.21.0-win-x64.zip",archiveSHA:"84d31df6571c3c7156707265bf51e2a021656fa1584bf6f4486b792dcde54d7d",entry:"node-v22.21.0-win-x64/node.exe",sha:"47cd67453da180a39cdf5daedb89862667b8e627e60c116607874633450cd30d",size:85766656,aliases:[]string{"C:/Users/makhmeto/AppData/Roaming/fnm/node-versions/v22.21.0/installation/node.exe"}},
	}
	return append(plans, ownerInstalledClaudePlans()...)
}

// Claude Code's native installer places one self-contained executable below the
// user's .local directory and writes no uninstall registration. Every release
// has a manifest signed with Anthropic's release key; the compiled digest and
// size are that manifest's win32-x64 entry, and archiveSHA is the digest of the
// signed manifest bytes. The installed file is admitted only while it equals one
// of these released files, so a newer release stays unknown until a reviewed
// change pins it.
func ownerInstalledClaudePlan(version, manifest, sha string, size int64) ownerInstalledToolPlan {
	const path = "C:/Users/makhmeto/.local/bin/claude.exe"
	return ownerInstalledToolPlan{name:"claude",path:path,version:version,noRegistration:true,budget:30*time.Second,archiveURL:"https://downloads.claude.ai/claude-code-releases/"+version+"/manifest.json",archiveSHA:manifest,entry:"platforms.win32-x64",sha:sha,size:size,aliases:[]string{path}}
}

func ownerInstalledClaudePlans() []ownerInstalledToolPlan {
	return []ownerInstalledToolPlan{
		ownerInstalledClaudePlan("2.1.310","895e85ce24c3df3fdb77ac1006bdb2b66f1c42dd557000e65ced1e4aad847e74","f9a3920634ae3b961ee92c2eb58d0051f5f8852b120983d0a4f57b6a5efeba61",296791712),
		ownerInstalledClaudePlan("2.1.311","d456e7293d621e6e09dd73b1d2a112bef62dae08d1ee44c1065356e64da65c0d","966f4b2ce962298f5dee6a942e8c47c8f97392e69e728e622319a98b259c0680",298870432),
		ownerInstalledClaudePlan("2.1.312","f9bc2d099665e498cbc4123f81f2220852c3a75ee118c0081ef83f42f165aec9","6099672ebe5dcb518b67d1d32dde1e3360bc6bee1dae7f8d28a69c49acc7c865",298690208),
		ownerInstalledClaudePlan("2.1.314","ded1c9a0a47ae65dc03165b0fab7b0bbd4aadd55de20b53c9f994308552d623d","66e00df32e6981f686a91f67287fcc86e6e1b80ba4973a7b7c4aa43c59986cfa",301388448),
		ownerInstalledClaudePlan("2.1.315","7abee26292714236414ec4a4296686cd65951594ccec8cbd20dcfd8960ff6794","df57be6478a5bd6d5f5a0aa352729804aabff655339a3fa991b1ad8aae00b1e1",335468192),
		ownerInstalledClaudePlan("2.1.316","ac9d8bb6a65febbccf9e65bde2e3e4298b8d6974e96f559fbf1d740891a747db","e896a9d8da519353a49bf69ca4a695ba7e09067752687c675b3969041e632d52",335469216),
		ownerInstalledClaudePlan("2.1.317","2337b6f8d99d893d2c9f12761b124bf64e80823a566464d44767e4ca595ee2b7","26b01ec22df2c17ac7f5703aa547d36706e8d58412136ced0344a8ced4d86038",338187936),
		ownerInstalledClaudePlan("2.1.318","608425e4ad6b6513a43c281c170d0bdd71b2d30bbf0df09df9b1a2e8c448df07","b9aa5b08dc2cc46ea77d334dce792d1b8fdbb3a43bb290efdebdc905b1181082",342408352),
		ownerInstalledClaudePlan("2.1.319","e5ddfc17fd4745e7b5a6ccff7ccc1e119f323726c4b585c544cd9b5a6b1752e5","2e055732c9d03d22700d9046251a8370b673607bfcc9e4d16a1b0e8fb1345cbd",344807072),
		ownerInstalledClaudePlan("2.1.321","d073d63928081951b871b35780d7abc83c26fa9b8b97f1ace64fc90d4591d325","e6dae5fc45902b15751f2591a02f51760351c4788c5fd8291b3f1443d40c13a4",346487456),
		ownerInstalledClaudePlan("2.1.322","05d2eff648bb528c1479c719bb8eca9a759657631e285d03aaf37fe779173f1c","345996b4bdbae21d817f750fae2f34cbf8d81224a5444c78136091d121beb298",346496160),
		ownerInstalledClaudePlan("2.1.323","6bc7a4857751e4a2d8257bcbd3ff9e817ca13f5defcee4b9c51d7d12741f820d","5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13",367634080),
		ownerInstalledClaudePlan("2.1.324","7e4d3f343603f341fd3fffb45d9c762afd35a5966adc622d8eeb80aa1e00b2d5","06465fda1b2df163eb14f7d279dc93c504c183706709d395d826868724daf816",373058208),
		ownerInstalledClaudePlan("2.1.325","919cc577ed98b12e00154f8bd925df23bbf2eb2b56ec29a09c78ee85f65725f3","87e0cb6ae0f4775da388cd3043f4d8097ef961dcddbf55fe90bc61ffc3f57a30",372461728),
		ownerInstalledClaudePlan("2.1.326","883e153b05d4a34e4225cdc90d692e4415ed4e46b26434b6430f128b6b8beb1e","44e85ac6d3e36b924a0d631d3f69c13a6907491115d71e97c8eb6160c82b2aed",378067616),
		ownerInstalledClaudePlan("2.1.327","9d2eb8df972ef777fde7e580266169ba8d7c8d4336cd5bcecaee4432e2e92f58","d6c8075bfb3b87f1c1254e64b6c35dadc628d21e75930ae51e47047b3f05a4a2",379536544),
		ownerInstalledClaudePlan("2.1.328","b94760a7ad96de4998334fd15149d6d9df5fa964c94b6990edf402f8a7562ca8","551d573567b46ce78aba1b20beb20ac2494f5f8adffd1b41dfd269a96e1d9839",379651744),
		ownerInstalledClaudePlan("2.1.329","7d2293210d31ab8af6ee0c3f7bbef503ea96ab2115f7fbcfc9382f84a6240742","527162bb4578e3dc36319fa913a530d5dc1f752115a6a4fd139981702bf282d0",384179872),
	}
}

// Several releases of one executable may be pinned for the same path, in table
// order. The policy only asks whether a path is declared, and the first plan of
// a path answers that, so routing does not depend on how many releases are pinned.
func ownerInstalledToolPlansFor(path string) []ownerInstalledToolPlan {
	var plans []ownerInstalledToolPlan
	for _, plan := range ownerInstalledToolPlans() {
		if same(path, plan.path) { plans = append(plans, plan) }
	}
	return plans
}

func ownerInstalledToolPlanFor(path string) (ownerInstalledToolPlan, bool) {
	if plans := ownerInstalledToolPlansFor(path); len(plans) > 0 { return plans[0], true }
	return ownerInstalledToolPlan{}, false
}

type ownerInstalledToolIdentity struct { volume, file, created uint64 }
type ownerInstalledToolState struct {
	id ownerInstalledToolIdentity
	size int64
	links uint32
	acl, resolved string
	aclAllowed bool
	parentProof string
	parentsAllowed bool
}

type ownerInstalledToolLease interface {
	State() (ownerInstalledToolState, error)
	Read(context.Context, int64) ([]byte, error)
	Close() error
}

// This is an internal source seam, never constructed from runtime JSON.
type ownerInstalledToolAccess interface {
	Origin(context.Context, ownerInstalledToolPlan) error
	Aliases(context.Context, string) ([]string, error)
	Open(context.Context, string) (ownerInstalledToolLease, error)
}

var errOwnerInstalledTool = errors.New("installed_tool_verification_failed")
var errOwnerInstalledToolCleanup = errors.New("installed_tool_cleanup_failed")

// Native primary errors remain available to the controller through errors.Is,
// while Error exposes only a fixed cleanup identifier, never native text.
type ownerInstalledResourceFailure struct { primary error }
func (e *ownerInstalledResourceFailure) Error() string { return errOwnerInstalledToolCleanup.Error() }
func (e *ownerInstalledResourceFailure) Unwrap() []error { return []error{e.primary,errOwnerInstalledToolCleanup} }

func ownerInstalledCloseResult(failure *error, closeErr error) {
	if closeErr==nil { return }
	if *failure==nil { *failure=errOwnerInstalledToolCleanup;return }
	if !errors.Is(*failure,errOwnerInstalledToolCleanup) { *failure=&ownerInstalledResourceFailure{primary:*failure} }
}

func ownerInstalledFailureVerdict(primary string,cancelled,cleanup bool) ownerPathVerdict {
	// Only controller-owned fixed identifiers can be combined. The primary
	// refusal, cancellation and cleanup are independent facts, not raw strings.
	switch primary {
	case "verified_installed_development_tool": primary="installed_tool"
	case "installed_tool_cancelled": primary="installed_tool";cancelled=true
	case "installed_tool_cleanup_failed": primary="installed_tool";cleanup=true
	case "installed_tool_origin_unavailable","installed_tool_alias_mismatch","installed_tool_identity_unavailable","installed_tool_parent_untrusted","installed_tool_identity_mismatch","installed_tool_bytes_mismatch","installed_tool_identity_changed","installed_tool_alias_changed","installed_tool_origin_changed","installed_tool_read_failed","existing_exclusion_unavailable":
	default: primary="installed_tool_origin_unavailable"
	}
	if cancelled { primary+="_cancelled" }
	if cleanup { primary+="_cleanup_failed" }
	return ownerRuleUnknown(primary)
}

// One deadline bounds every pinned release of the path: the short default, or
// the largest budget those plans declare.
func ownerInstalledToolBudget(plans []ownerInstalledToolPlan) time.Duration {
	budget := ownerInstalledToolDefaultBudget
	for _, plan := range plans {
		if plan.budget > budget && plan.budget <= ownerInstalledToolMaxBudget { budget = plan.budget }
	}
	return budget
}

func ownerInstalledToolClassification(path string) (verdict ownerPathVerdict) {
	ctx, cancel := context.WithTimeout(context.Background(), ownerInstalledToolBudget(ownerInstalledToolPlansFor(path)))
	defer cancel()
	return ownerInstalledToolWith(ctx, path, ownerInstalledToolNative{}, ownerLegacyProtection)
}

func ownerInstalledToolWith(ctx context.Context, path string, access ownerInstalledToolAccess, legacy func(string) ownerLegacyState) (verdict ownerPathVerdict) {
	plans := ownerInstalledToolPlansFor(path)
	if len(plans) == 0 { return ownerRuleUnknown("installed_tool_not_declared") }
	return ownerInstalledToolVerifyAny(ctx,plans,access,legacy)
}

// The refusals of the controller below, in the order a held object can first
// reach them. A later position means the held object matched more of that plan.
var ownerInstalledToolRefusalOrder = []string{
	"installed_tool_origin_unavailable",
	"existing_exclusion_unavailable",
	"installed_tool_alias_mismatch",
	"installed_tool_identity_unavailable",
	"installed_tool_parent_untrusted",
	"installed_tool_identity_mismatch",
	"installed_tool_read_failed",
	"installed_tool_bytes_mismatch",
	"installed_tool_identity_changed",
	"installed_tool_alias_changed",
	"installed_tool_origin_changed",
}

func ownerInstalledToolRefusalRank(basis string) int {
	for i, known := range ownerInstalledToolRefusalOrder {
		if basis == known { return i + 1 }
	}
	return 0
}

// Several releases of one executable may be pinned for the same path. Each pin
// is judged by the complete controller; the first clear verdict wins. A
// protected exclusion, a cancellation and a cleanup failure do not depend on the
// pin, so they end the search at once and can never be replaced by a later pin.
// Any other refusal is ranked by how far the held object got through the
// controller, so a release whose size matched and whose bytes did not is
// reported ahead of the early size mismatches of the other releases.
func ownerInstalledToolVerifyAny(ctx context.Context, plans []ownerInstalledToolPlan, access ownerInstalledToolAccess, legacy func(string) ownerLegacyState) (verdict ownerPathVerdict) {
	verdict = ownerRuleUnknown("installed_tool_not_declared")
	best := -1
	for _, plan := range plans {
		got := ownerInstalledToolVerify(ctx,plan,access,legacy)
		if got.AccessAllowed || got.Status == "protected" || strings.Contains(got.Basis,"_cancelled") || strings.Contains(got.Basis,"_cleanup_failed") { return got }
		if rank := ownerInstalledToolRefusalRank(got.Basis); rank > best { verdict, best = got, rank }
	}
	return verdict
}

// A plan is either registered or registration-free. It is refused before any
// object is opened when the two shapes are mixed, when its content size is not
// positive and within the single compiled read bound, or when its budget is
// negative, above the bound, set on a registered plan or missing from a
// registration-free one.
func ownerInstalledToolPlanValid(plan ownerInstalledToolPlan) bool {
	if _, valid := ownerInstalledToolInventory(plan, plan.aliases); !valid { return false }
	if plan.size <= 0 || plan.size > ownerInstalledToolMaxRead || plan.budget < 0 || plan.budget > ownerInstalledToolMaxBudget { return false }
	if !plan.noRegistration { return plan.budget == 0 }
	return plan.registry == "" && plan.publisher == "" && !plan.userRegistry && plan.originFields == nil && plan.budget > 0
}

// The normal caller always obtains plan from the closed table above. A test
// may supply a synthetic plan to exercise this same controller without reading
// a real installation; no public operation decodes a plan or digest.
func ownerInstalledToolVerify(ctx context.Context, plan ownerInstalledToolPlan, access ownerInstalledToolAccess, legacy func(string) ownerLegacyState) (verdict ownerPathVerdict) {
	verdict = ownerRuleUnknown("installed_tool_origin_unavailable")
	if ctx == nil || access == nil || legacy == nil { return }
	if !ownerInstalledToolPlanValid(plan) { return }
	cleanupFailed,cancelledFailure:=false,false
	observeFailure:=func(err error) {
		cleanupFailed=cleanupFailed || errors.Is(err,errOwnerInstalledToolCleanup)
		cancelledFailure=cancelledFailure || errors.Is(err,context.Canceled) || errors.Is(err,context.DeadlineExceeded)
	}
	defer func() {
		if verdict.Status=="protected" { return }
		if cleanupFailed || cancelledFailure || ctx.Err()!=nil { verdict=ownerInstalledFailureVerdict(verdict.Basis,cancelledFailure || ctx.Err()!=nil,cleanupFailed) }
	}()
	if ctx.Err() != nil { return ownerRuleUnknown("installed_tool_cancelled") }
	for _, alias := range plan.aliases {
		if ownerCardPath(alias) || ownerWindowsAlias(alias) { return }
		switch legacy(alias) {
		case ownerLegacyClient: return ownerRuleProtected("legacy_exclusion")
		case ownerLegacyUnavailable: return ownerRuleUnknown("existing_exclusion_unavailable")
		}
	}
	if !plan.noRegistration {
		if err:=access.Origin(ctx, plan);err!=nil {
			observeFailure(err)
			if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
			return
		}
	}
	if ctx.Err()!=nil { return }
	observed, err := access.Aliases(ctx, plan.path)
	observeFailure(err)
	if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
	aliases, inventoryOK := ownerInstalledToolInventory(plan, observed)
	if err != nil || !inventoryOK { return ownerRuleUnknown("installed_tool_alias_mismatch") }
	// Optional hardlink names must pass the same exclusions as the required
	// names before any object or executable content is opened.
	for _, alias := range aliases {
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		if ownerCardPath(alias) || ownerWindowsAlias(alias) { return }
		switch legacy(alias) {
		case ownerLegacyClient: return ownerRuleProtected("legacy_exclusion")
		case ownerLegacyUnavailable: return ownerRuleUnknown("existing_exclusion_unavailable")
		}
	}
	var leases []ownerInstalledToolLease
	defer func() {
		for i:=len(leases)-1;i>=0;i-- { if leases[i].Close()!=nil { cleanupFailed=true } }
	}()
	var states []ownerInstalledToolState
	for _, alias := range aliases {
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		lease, openErr := access.Open(ctx, alias)
		if lease!=nil { leases=append(leases,lease) }
		observeFailure(openErr)
		if openErr==context.Canceled || openErr==context.DeadlineExceeded { return ownerRuleUnknown("installed_tool_cancelled") }
		if openErr!=nil || lease==nil { return ownerRuleUnknown("installed_tool_identity_unavailable") }
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		state, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr==nil && (!state.parentsAllowed || state.parentProof=="") { return ownerRuleUnknown("installed_tool_parent_untrusted") }
		if stateErr!=nil || state.id.volume==0 || state.id.file==0 || state.id.created==0 || state.size!=plan.size || state.links!=uint32(len(aliases)) || state.acl=="" || !state.aclAllowed || !same(state.resolved,alias) {
			return ownerRuleUnknown("installed_tool_identity_mismatch")
		}
		if len(states)>0 && (states[0].id!=state.id || states[0].acl!=state.acl || states[0].parentProof!=state.parentProof) { return ownerRuleUnknown("installed_tool_identity_mismatch") }
		states=append(states,state)
	}
	// All names are held with read sharing only before any bytes are read.
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	raw, readErr := leases[0].Read(ctx,plan.size)
	defer clear(raw)
	observeFailure(readErr)
	if readErr==context.Canceled || readErr==context.DeadlineExceeded { return ownerRuleUnknown("installed_tool_cancelled") }
	if readErr!=nil { return ownerRuleUnknown("installed_tool_read_failed") }
	if int64(len(raw))!=plan.size || hashBytes(raw)!=plan.sha { return ownerRuleUnknown("installed_tool_bytes_mismatch") }
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	for i,lease := range leases {
		now, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr!=nil || now!=states[i] { return ownerRuleUnknown("installed_tool_identity_changed") }
	}
	after, err := access.Aliases(ctx,plan.path)
	observeFailure(err)
	if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
	if err!=nil || !ownerInstalledToolAliases(after,aliases) { return ownerRuleUnknown("installed_tool_alias_changed") }
	for _, alias := range aliases {
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		if ownerCardPath(alias) || ownerWindowsAlias(alias) { return }
		switch legacy(alias) {
		case ownerLegacyClient: return ownerRuleProtected("legacy_exclusion")
		case ownerLegacyUnavailable: return ownerRuleUnknown("existing_exclusion_unavailable")
		}
	}
	if !plan.noRegistration {
		if err:=access.Origin(ctx,plan);err!=nil {
			observeFailure(err)
			if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
			return ownerRuleUnknown("installed_tool_origin_changed")
		}
	}
	// Registry/alias observations are not an atomic filesystem transaction.
	// Recheck the actual held objects after those observations as well.
	for i,lease := range leases {
		now, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr!=nil || now!=states[i] { return ownerRuleUnknown("installed_tool_identity_changed") }
	}
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	return ownerRuleClear("verified_installed_development_tool")
}

// Select only observed names from the compiled inventory. Required names must
// all exist; optional names may be symlinks or absent and are not assumed to be
// hardlinks. The returned order is fixed by the plan, with the required name
// first. The final observation must equal this selected set, not a new subset.
func ownerInstalledToolInventory(plan ownerInstalledToolPlan, actual []string) ([]string, bool) {
	if len(plan.aliases)==0 || len(plan.aliases)>2 || len(plan.aliases)+len(plan.optionalAliases)>ownerInstalledToolMaxAliases { return nil,false }
	allowed:=append(append([]string(nil),plan.aliases...),plan.optionalAliases...)
	if len(actual)<len(plan.aliases) || len(actual)>len(allowed) || !ownerInstalledToolAliases(allowed,allowed) || !ownerInstalledToolAliases(actual,actual) { return nil,false }
	names:=make(map[string]bool,len(actual))
	for _, name:=range actual { names[strings.ToLower(filepath.Clean(name))]=true }
	for _, name:=range plan.aliases { if !names[strings.ToLower(filepath.Clean(name))] { return nil,false } }
	var selected []string
	for _, name:=range allowed { if names[strings.ToLower(filepath.Clean(name))] { selected=append(selected,name) } }
	if len(selected)!=len(actual) { return nil,false }
	return selected,true
}

func ownerInstalledToolAliases(actual, expected []string) bool {
	if len(actual)!=len(expected) { return false }
	a,b:=append([]string(nil),actual...),append([]string(nil),expected...)
	for i:=range a {
		if !filepath.IsAbs(a[i]) || ownerWindowsAlias(a[i]) { return false }
		a[i]=strings.ToLower(filepath.Clean(a[i]))
	}
	for i:=range b { b[i]=strings.ToLower(filepath.Clean(b[i])) }
	sort.Strings(a);sort.Strings(b)
	for i:=range a { if a[i]!=b[i] || i>0 && a[i]==a[i-1] { return false } }
	return true
}

type ownerInstalledToolNative struct{}

// System installers are identified by version and publisher in the machine
// hive. A plan may instead name the current user hive and its own value names.
func ownerInstalledToolOriginSpec(plan ownerInstalledToolPlan) (syscall.Handle,map[string]string) {
	hive:=syscall.Handle(syscall.HKEY_LOCAL_MACHINE)
	if plan.userRegistry { hive=syscall.Handle(syscall.HKEY_CURRENT_USER) }
	fields:=map[string]string{"DisplayVersion":plan.version,"Publisher":plan.publisher}
	if plan.originFields!=nil { fields=plan.originFields }
	return hive,fields
}

// Compare every UTF-16 code unit returned by the registry, including one
// terminator. UTF16ToString alone would ignore data after an embedded NUL.
func ownerInstalledToolRegistrySZEqual(kind uint32, data []byte, n uint32, want string) bool {
	if kind!=syscall.REG_SZ || n<2 || uint64(n)>uint64(len(data)) || n%2!=0 { return false }
	expected,err:=syscall.UTF16FromString(want)
	if err!=nil || len(expected)!=int(n)/2 { return false }
	for i,word:=range expected {
		if word!=(uint16(data[2*i]) | uint16(data[2*i+1])<<8) { return false }
	}
	return true
}

func (ownerInstalledToolNative) Origin(ctx context.Context, plan ownerInstalledToolPlan) (failure error) {
	if ctx.Err()!=nil { return ctx.Err() }
	name,err:=syscall.UTF16PtrFromString(plan.registry)
	if err!=nil { return errOwnerInstalledTool }
	var key syscall.Handle
	hive,fields:=ownerInstalledToolOriginSpec(plan)
	if syscall.RegOpenKeyEx(hive,name,0,syscall.KEY_QUERY_VALUE|0x0100,&key)!=nil { return errOwnerInstalledTool }
	defer func(){ownerInstalledCloseResult(&failure,syscall.RegCloseKey(key))}()
	for field,want:=range fields {
		if ctx.Err()!=nil { return ctx.Err() }
		p,_:=syscall.UTF16PtrFromString(field)
		var kind uint32
		var data [4096]byte
		n:=uint32(len(data))
		if syscall.RegQueryValueEx(key,p,nil,&kind,&data[0],&n)!=nil || !ownerInstalledToolRegistrySZEqual(kind,data[:],n,want) { return errOwnerInstalledTool }
	}
	if ctx.Err()!=nil { return ctx.Err() }
	return nil
}

func (ownerInstalledToolNative) Aliases(ctx context.Context, path string) (names []string,failure error) {
	if ctx.Err()!=nil { return nil,ctx.Err() }
	p,err:=syscall.UTF16PtrFromString(path)
	if err!=nil { return nil,errOwnerInstalledTool }
	var buffer [32768]uint16
	n:=uint32(len(buffer))
	k:=syscall.NewLazyDLL("kernel32.dll")
	h,_,_:=k.NewProc("FindFirstFileNameW").Call(uintptr(unsafe.Pointer(p)),0,uintptr(unsafe.Pointer(&n)),uintptr(unsafe.Pointer(&buffer[0])))
	if h==^uintptr(0) { return nil,errOwnerInstalledTool }
	defer func(){
		ok,_,_:=k.NewProc("FindClose").Call(h)
		if ok==0 { ownerInstalledCloseResult(&failure,errOwnerInstalledTool);names=nil }
	}()
	for len(names)<ownerInstalledToolMaxAliases && ctx.Err()==nil {
		if n==0 || n>=uint32(len(buffer)) { return nil,errOwnerInstalledTool }
		names=append(names,filepath.VolumeName(path)+syscall.UTF16ToString(buffer[:n]))
		n=uint32(len(buffer))
		ok,_,callErr:=k.NewProc("FindNextFileNameW").Call(h,uintptr(unsafe.Pointer(&n)),uintptr(unsafe.Pointer(&buffer[0])))
		if ok==0 { if callErr==syscall.Errno(38) { return names,nil };return nil,errOwnerInstalledTool }
	}
	if ctx.Err()!=nil { return nil,ctx.Err() }
	return nil,errOwnerInstalledTool
}

type ownerInstalledToolFile struct {
	f *os.File
	parents []ownerInstalledToolParent
}

type ownerInstalledToolParent struct { file *os.File; path string }

type ownerInstalledToken interface {
	SID() (string,error)
	Close() error
}
type ownerInstalledNativeToken struct { token syscall.Token }
func (t ownerInstalledNativeToken) SID() (string,error) {
	user,err:=t.token.GetTokenUser()
	if err!=nil { return "",errOwnerInstalledTool }
	sid,err:=user.User.Sid.String()
	if err!=nil { return "",errOwnerInstalledTool }
	return sid,nil
}
func (t ownerInstalledNativeToken) Close() error { return t.token.Close() }

func ownerInstalledTokenSID(token ownerInstalledToken) (sid string,failure error) {
	if token==nil { return "",errOwnerInstalledTool }
	defer func(){
		ownerInstalledCloseResult(&failure,token.Close())
		if failure!=nil { sid="" }
	}()
	return token.SID()
}

func (ownerInstalledToolNative) Open(ctx context.Context,path string) (ownerInstalledToolLease,error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	lease:=&ownerInstalledToolFile{}
	var dirs []string
	for d:=filepath.Dir(path);;d=filepath.Dir(d) { dirs=append(dirs,d);if filepath.Dir(d)==d { break } }
	for i:=len(dirs)-1;i>=0;i-- {
		p,err:=syscall.UTF16PtrFromString(dirs[i]);if err!=nil { return lease,errOwnerInstalledTool }
		h,err:=syscall.CreateFile(p,0x20080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.OPEN_EXISTING,0x02200000,0)
		if err!=nil { return lease,errOwnerInstalledTool }
		parentFile:=os.NewFile(uintptr(h),dirs[i]);lease.parents=append(lease.parents,ownerInstalledToolParent{parentFile,dirs[i]})
		var info syscall.ByHandleFileInformation
		actual,pathErr:=ownerHandlePath(h)
		if syscall.GetFileInformationByHandle(h,&info)!=nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 || pathErr!=nil || !same(actual,dirs[i]) { return lease,errOwnerInstalledTool }
		if err:=ctx.Err();err!=nil { return lease,err }
	}
	if err:=ctx.Err();err!=nil { return lease,err }
	p,err:=syscall.UTF16PtrFromString(path);if err!=nil { return lease,errOwnerInstalledTool }
	h,err:=syscall.CreateFile(p,syscall.GENERIC_READ|0x20000,syscall.FILE_SHARE_READ,nil,syscall.OPEN_EXISTING,0x00200000,0)
	if err!=nil { return lease,errOwnerInstalledTool };lease.f=os.NewFile(uintptr(h),path)
	return lease,nil
}

func (f *ownerInstalledToolFile) State() (ownerInstalledToolState,error) {
	if f.f==nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	var i syscall.ByHandleFileInformation
	h:=syscall.Handle(f.f.Fd())
	if syscall.GetFileInformationByHandle(h,&i)!=nil || i.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT)!=0 { return ownerInstalledToolState{},errOwnerInstalledTool }
	resolved,err:=ownerHandlePath(h);if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	acl,err:=skillsUpdateSDDL(f.f);if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	token,err:=syscall.OpenCurrentProcessToken();if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	sid,err:=ownerInstalledTokenSID(ownerInstalledNativeToken{token})
	if err!=nil { return ownerInstalledToolState{},err }
	parentProof,parentsAllowed,err:=ownerInstalledToolParents(f.parents,sid)
	if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	return ownerInstalledToolState{id:ownerInstalledToolIdentity{uint64(i.VolumeSerialNumber),uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow),uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime)},size:int64(uint64(i.FileSizeHigh)<<32|uint64(i.FileSizeLow)),links:i.NumberOfLinks,acl:hashBytes([]byte(acl)),resolved:resolved,aclAllowed:ownerInstalledToolACL(acl,sid),parentProof:parentProof,parentsAllowed:parentsAllowed},nil
}

func ownerInstalledToolParents(parents []ownerInstalledToolParent,user string) (string,bool,error) {
	if len(parents)==0 || len(parents)>64 { return "",false,errOwnerInstalledTool }
	var proof strings.Builder
	allowed:=true
	for position,parent:=range parents {
		if parent.file==nil { return "",false,errOwnerInstalledTool }
		h:=syscall.Handle(parent.file.Fd())
		var i syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h,&i)!=nil || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 || i.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 { return "",false,errOwnerInstalledTool }
		actual,err:=ownerHandlePath(h);if err!=nil || !same(actual,parent.path) { return "",false,errOwnerInstalledTool }
		acl,err:=skillsUpdateSDDL(parent.file);if err!=nil { return "",false,errOwnerInstalledTool }
		if !ownerInstalledToolParentAllowed(acl,user,position==len(parents)-1,filepath.Dir(parent.path)==parent.path) { allowed=false }
		// IDs, names and descriptors come from the retained parent objects.
		for _,v:=range []uint64{uint64(i.VolumeSerialNumber),uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow),uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime),uint64(i.FileAttributes)} { proof.WriteString(strconv.FormatUint(v,10));proof.WriteByte(':') }
		name:=strings.ToLower(filepath.Clean(actual));proof.WriteString(strconv.Itoa(len(name)));proof.WriteByte(':');proof.WriteString(name);proof.WriteByte(':');proof.WriteString(hashBytes([]byte(acl)));proof.WriteByte(';')
	}
	return hashBytes([]byte(proof.String())),allowed,nil
}

func (f *ownerInstalledToolFile) Read(ctx context.Context,size int64) ([]byte,error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	if f.f==nil { return nil,errOwnerInstalledTool }
	return ownerInstalledToolRead(ctx,f.f,size)
}

// The controller owns this bounded loop. The native receiver supplies its
// already-held file; only tests substitute a deterministic ReaderAt.
func ownerInstalledToolRead(ctx context.Context,reader io.ReaderAt,size int64) (raw []byte,failure error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	if reader==nil || size<=0 || size>ownerInstalledToolMaxRead { return nil,errOwnerInstalledTool }
	raw=make([]byte,int(size))
	defer func(){if failure!=nil { clear(raw);raw=nil }}()
	for offset:=0;offset<len(raw); {
		if err:=ctx.Err();err!=nil { return raw,err }
		end:=offset+65536;if end>len(raw) { end=len(raw) }
		n,err:=reader.ReadAt(raw[offset:end],int64(offset));offset+=n
		if err!=nil && !(err==io.EOF && offset==len(raw)) || n==0 { return raw,errOwnerInstalledTool }
	}
	if len(raw)<64 || !bytes.Equal(raw[:2],[]byte("MZ")) { return raw,errOwnerInstalledTool }
	// The entire buffer is hashed by the controller, never by a second open.
	return raw,nil
}

func ownerInstalledToolACL(sddl, user string) bool {
	return ownerInstalledToolACLMode(sddl,user,false)
}

func ownerInstalledToolACLMode(sddl, user string,directory bool) bool {
	writes:=uint64(0x500d0116)
	if directory { writes|=0x40 } // FILE_DELETE_CHILD, in addition to add/write/ACL rights.
	return ownerInstalledToolACLWrites(sddl,user,writes)
}

// The directory that holds the executable keeps the strict rule above, because
// a neighboring file could be loaded together with the executable. A higher
// ancestor matters only for rights that could remove, rename or re-own an
// existing path component: delete-child, delete, change-ACL, change-owner and
// the generic write/all bits. Add-only entries, such as the Authenticated Users
// add-subdirectory entry of a stock system volume root, cannot replace one.
// Delete on a volume root cannot remove the root and is not counted there.
func ownerInstalledToolParentAllowed(sddl, user string,installDirectory,volumeRoot bool) bool {
	switch {
	case installDirectory: return ownerInstalledToolACLMode(sddl,user,true)
	case volumeRoot: return ownerInstalledToolACLWrites(sddl,user,0x500c0040)
	}
	return ownerInstalledToolACLWrites(sddl,user,0x500d0040)
}

func ownerInstalledToolACLWrites(sddl, user string,writes uint64) bool {
	trusted:=func(sid string)bool{return sid=="BA" || sid=="SY" || user!="" && sid==user || sid=="S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"}
	if !strings.HasPrefix(sddl,"O:") { return false }
	ownerEnd:=strings.Index(sddl,"G:");d:=strings.Index(sddl,"D:")
	if d<2 { return false };if ownerEnd<0 || ownerEnd>d { ownerEnd=d }
	if !trusted(sddl[2:ownerEnd]) { return false }
	dacl:=sddl[d+2:];if end:=strings.Index(dacl,"S:");end>=0 { dacl=dacl[:end] }
	start:=strings.IndexByte(dacl,'(');if start<0 { return false }
	flags:=dacl[:start]
	for _,flag:=range []string{"AI","AR","P"}{flags=strings.ReplaceAll(flags,flag,"")}
	if flags!="" { return false };dacl=dacl[start:]
	// SDDL spells a mask with a two-letter name only when the bits match exactly,
	// and it uses the Directory Service names for single file-system bits (CC is
	// read-data, LC add-subdirectory, DT delete-child). The bits keep their
	// file-system meaning here; any other name leaves the descriptor unknown.
	rights:=map[string]uint64{"FA":0x1f01ff,"FR":0x120089,"FX":0x1200a0,"GA":0x10000000,"GW":0x40000000,"GR":0x80000000,"GX":0x20000000,"RC":0x20000,"SD":0x10000,"WD":0x40000,"WO":0x80000,"FW":0x120116,"CC":0x1,"DC":0x2,"LC":0x4,"SW":0x8,"RP":0x10,"WP":0x20,"DT":0x40,"LO":0x80,"CR":0x100}
	aces:=0
	for dacl!="" {
		if dacl[0]!='(' { return false };end:=strings.IndexByte(dacl,')');if end<0 { return false }
		fields:=strings.Split(dacl[1:end],";");if len(fields)!=6 || fields[0]!="A" && fields[0]!="D" || fields[3]!="" || fields[4]!="" || fields[5]=="" { return false }
		var mask uint64
		if strings.HasPrefix(fields[2],"0x") { n,err:=strconv.ParseUint(fields[2][2:],16,32);if err!=nil { return false };mask=n } else {
			if len(fields[2])==0 || len(fields[2])%2!=0 { return false }
			for n:=0;n<len(fields[2]);n+=2 { bits,ok:=rights[fields[2][n:n+2]];if !ok { return false };mask|=bits }
		}
		aceFlags:=fields[1]
		for _,flag:=range []string{"OI","CI","NP","IO","ID"} { aceFlags=strings.ReplaceAll(aceFlags,flag,"") }
		if aceFlags!="" { return false }
		// INHERIT_ONLY is not effective on this already-existing object.
		// The actual child/leaf descriptor is independently checked as well.
		if fields[0]=="A" && !strings.Contains(fields[1],"IO") && mask&writes!=0 && !trusted(fields[5]) { return false }
		aces++;dacl=dacl[end+1:]
	}
	return aces>0
}

func (f *ownerInstalledToolFile) Close() error {
	var failed bool
	if f.f!=nil { if f.f.Close()!=nil { failed=true };f.f=nil }
	for i:=len(f.parents)-1;i>=0;i-- { if f.parents[i].file.Close()!=nil { failed=true } }
	f.parents=nil
	if failed { return errOwnerInstalledTool };return nil
}
