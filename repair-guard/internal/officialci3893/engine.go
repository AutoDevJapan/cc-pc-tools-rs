// Package officialci3893 is the purpose engine received from the finite check.
// Live adapters are owned by ordinary implementation 3894. This package never
// changes general classification or qualifies arbitrary existing data.
package officialci3893

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
)

const Domain = "todo3387-official-public-ci-3894-v1"

// A time bound only limits reuse; current source/actor/CI authority is checked
// on every operation. The existing provider may wait for a scheduler resource.
const ReceiptLifetime = 24 * time.Hour

var ErrDenied = errors.New("official CI purpose denied")

type validationStage uint8

const (
	validationIdentity validationStage = iota + 1
	validationLoad
	validationBinding
	validationAsset
	validationState
	validationFuture
	validationExpiry
	validationLifetime
	validationManifest
	validationParent
	validationPhysical
	validationEvidence
)

func (s validationStage) reason() string {
	switch s {
	case validationIdentity:
		return "receipt_identity_invalid"
	case validationLoad:
		return "receipt_load_failed"
	case validationBinding:
		return "receipt_binding_changed"
	case validationAsset:
		return "receipt_asset_changed"
	case validationState:
		return "receipt_not_complete"
	case validationFuture:
		return "receipt_created_in_future"
	case validationExpiry:
		return "receipt_expired"
	case validationLifetime:
		return "receipt_lifetime_changed"
	case validationManifest:
		return "receipt_manifest_missing"
	case validationParent:
		return "receipt_parent_changed"
	case validationPhysical:
		return "physical_tree_not_verified"
	case validationEvidence:
		return "authority_evidence_changed"
	default:
		return "validation_not_verified"
	}
}

// Keep the original error relation, but never format an underlying OS path,
// journal detail or authority response into the diagnostic.
type validationFailure struct {
	stage validationStage
	cause error
}

func (e validationFailure) Error() string   { return "official CI: " + e.stage.reason() }
func (e validationFailure) Unwrap() error   { return e.cause }
func invalidAt(stage validationStage) error { return validationFailure{stage, ErrDenied} }

// Only a closed stage emitted by this engine crosses the metadata boundary.
func ValidationReason(err error) string {
	var failure validationFailure
	if errors.As(err, &failure) {
		return failure.stage.reason()
	}
	return ""
}

// Binding is supplied by trusted wiring, never decoded from a caller request.
// SourceRegistration is the digest of the independent normal self-source proof.
type Binding struct {
	SourceHead, SourceRegistration, AnchorReceipt, CreatorJob string
	SourceRepoID, RepoID, RunID, JobID                        int64
	SourceOwner, SourceRepo, Owner                            string
	ParentTodo, CreatorTodo, ProviderTodo, PR, Attempt        int
	ParentJob, OriginalProviderJob                            string
	Head, Tree, Labels                                        string
	Private                                                   bool
}

func FixedBinding(sourceHead, registration, anchorReceipt, creatorJob string) Binding {
	return Binding{
		SourceHead: sourceHead, SourceRegistration: registration, AnchorReceipt: anchorReceipt, CreatorJob: creatorJob,
		SourceRepoID: 1406818713, SourceOwner: "AutoDevJapan", SourceRepo: "cc-pc-tools-rs",
		RepoID: 1429803672, Owner: "AutoDevJapan", Private: true, PR: 17,
		Head: "97e157d8b12373bf4c209ce7a0324b4d432ff315", Tree: "2d724295a1a90122b0c69fa948bffc0464e4179a",
		RunID: 40228177555, Attempt: 1, JobID: 121273999986,
		Labels: "self-hosted,Windows,X64,todo3339", ParentTodo: 3387, CreatorTodo: 3894, ProviderTodo: 3888,
		ParentJob: "20261026-040912-todo-3387", OriginalProviderJob: "20261026-054854-todo-3888",
	}
}

type Asset struct {
	Repository, Tag, Name, URL, SHA256 string
	ID, Size                           int64
}

func OfficialAsset() Asset {
	return Asset{
		Repository: "actions/runner", Tag: "v2.338.0", ID: 615677454, Size: 104063355,
		Name:   "actions-runner-win-x64-2.338.0.zip",
		URL:    "https://github.com/actions/runner/releases/download/v2.338.0/actions-runner-win-x64-2.338.0.zip",
		SHA256: "f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f",
	}
}

// Authority must obtain these facts independently from registered self-source,
// normal ToDo/job lineage and GitHub metadata. A caller's JSON is not evidence.
// The ordinary implementation supplies the live adapter; tests inject fixtures.
type Evidence struct {
	Parent                                                   Identity
	FreshOwnedAnchor                                         bool
	Binding                                                  Binding
	Asset                                                    Asset
	Observed                                                 time.Time
	SourceVerified, JobVerified, Active, OfficialPublicAsset bool
}

type Authority interface {
	Observe(context.Context) (Evidence, error)
}
type ArchiveSource interface {
	Open(context.Context, Asset) (io.ReadCloser, error)
}

type Identity struct {
	Volume uint32
	FileID uint64
	Birth  uint64
}
type FileRecord struct {
	Name, SHA256 string
	Size         int64
	Identity     Identity
	Directory    bool
}
type Payload struct {
	Name  string
	Bytes []byte
}
type State string

const (
	Pending   State = "pending"
	Complete  State = "complete"
	Consuming State = "consuming"
	Consumed  State = "consumed"
	Failed    State = "failed"
)

// Receipt has no externally selected root. Its 128-bit ID is generated by the
// constructor; the backend creates that one new child of its verified anchor.
type Receipt struct {
	Domain, ID       string
	Binding          Binding
	Asset            Asset
	Created, Expires time.Time
	State            State
	Parent, Root     Identity
	Files            []FileRecord
}

// Journal is the independent, authenticated creation-receipt boundary, not a
// general registry. Reserve must be CreateNew+durable. Load must authenticate
// the same raw bytes it decodes. Replace must compare the entire previous
// authenticated record atomically, durably, without accepting caller snapshots.
// Interrupted Pending/Consuming records never become Complete on restart.
type Journal interface {
	Reserve(Receipt) error
	Load(string) (Receipt, error)
	Replace(Receipt, Receipt) error
}

type CreatedFS interface {
	ParentIdentity() Identity
	CreateRoot(string) (Identity, error)
	Install(string, Identity, []Payload) ([]FileRecord, error)
	WithVerified(Receipt, func([]Payload) error) error
}

type Engine struct {
	binding   Binding
	asset     Asset
	authority Authority
	archive   ArchiveSource
	journal   Journal
	fs        CreatedFS
	now       func() time.Time
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func hexN(s string, n int) bool {
	if len(s) != n {
		return false
	}
	b, e := hex.DecodeString(s)
	return e == nil && hex.EncodeToString(b) == s
}

// New has no default authority, downloader or receipt store.
// The only production asset is the fixed official pin. Tests substitute a tiny
// archive through the unexported fixture constructor.
func New(b Binding, a Authority, src ArchiveSource, j Journal, fs CreatedFS) (*Engine, error) {
	return newEngine(b, OfficialAsset(), a, src, j, fs)
}

func newEngine(b Binding, pin Asset, a Authority, src ArchiveSource, j Journal, fs CreatedFS) (*Engine, error) {
	if b != FixedBinding(b.SourceHead, b.SourceRegistration, b.AnchorReceipt, b.CreatorJob) ||
		!hexN(b.SourceHead, 40) || !hexN(b.SourceRegistration, 64) || !hexN(b.AnchorReceipt, 64) || b.CreatorJob == "" ||
		a == nil || src == nil || j == nil || fs == nil || pin.Size <= 0 || pin.Size > 128<<20 || !hexN(pin.SHA256, 64) {
		return nil, ErrDenied
	}
	return &Engine{b, pin, a, src, j, fs, time.Now}, nil
}

func (e *Engine) authorize(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	o, err := e.authority.Observe(ctx)
	if err != nil {
		return err
	}
	age := e.now().Sub(o.Observed)
	if o.Binding != e.binding || o.Asset != e.asset || age < 0 || age > time.Minute ||
		!o.SourceVerified || !o.JobVerified || !o.Active || !o.OfficialPublicAsset ||
		!o.FreshOwnedAnchor || o.Parent != e.fs.ParentIdentity() {
		return invalidAt(validationEvidence)
	}
	return ctx.Err()
}

func (e *Engine) Construct(ctx context.Context) (Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var r Receipt
	if err := e.authorize(ctx); err != nil {
		return r, err
	}
	input, err := e.archive.Open(ctx, e.asset)
	if err != nil {
		return r, err
	}
	stopClose := context.AfterFunc(ctx, func() { _ = input.Close() })
	archive, readErr := io.ReadAll(io.LimitReader(input, e.asset.Size+1))
	stopClose()
	closeErr := input.Close()
	if ctx.Err() != nil {
		return r, ctx.Err()
	}
	if readErr != nil || closeErr != nil || int64(len(archive)) != e.asset.Size || digest(archive) != e.asset.SHA256 {
		return r, ErrDenied
	}
	// Parse exactly the bytes whose hash was checked; never reopen a pathname.
	files, err := unpack(archive)
	if err != nil {
		return r, err
	}
	if err = e.authorize(ctx); err != nil {
		return r, err
	}
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return r, err
	}
	created := e.now().UTC()
	r = Receipt{Domain: Domain, ID: hex.EncodeToString(random[:]), Binding: e.binding, Asset: e.asset,
		Created: created, Expires: created.Add(ReceiptLifetime), State: Pending, Parent: e.fs.ParentIdentity()}
	if err = e.journal.Reserve(r); err != nil {
		return r, err
	}
	root, err := e.fs.CreateRoot(r.ID)
	if err != nil {
		return e.fail(r, err)
	}
	next := r
	next.Root = root
	if err = e.journal.Replace(r, next); err != nil {
		return r, err
	}
	r = next
	manifest, err := e.fs.Install(r.ID, r.Root, files)
	if err != nil {
		return e.fail(r, err)
	}
	if err = e.authorize(ctx); err != nil {
		return e.fail(r, err)
	}
	next = r
	next.Files = manifest
	if !e.now().Before(r.Expires) {
		return e.fail(r, ErrDenied)
	}
	next.State = Complete
	if err = e.journal.Replace(r, next); err != nil {
		return r, err
	}
	return next, nil
}

func (e *Engine) fail(r Receipt, why error) (Receipt, error) {
	next := r
	next.State = Failed
	if err := e.journal.Replace(r, next); err != nil {
		return r, errors.Join(why, err)
	}
	return next, why
}

func (e *Engine) load(ctx context.Context, id string) (Receipt, error) {
	var r Receipt
	if !hexN(id, 32) {
		return r, invalidAt(validationIdentity)
	}
	if err := e.authorize(ctx); err != nil {
		return r, err
	}
	r, err := e.journal.Load(id)
	now := e.now()
	if err != nil {
		return Receipt{}, invalidAt(validationLoad)
	}
	if r.Domain != Domain || r.ID != id {
		return Receipt{}, invalidAt(validationIdentity)
	}
	if r.Binding != e.binding {
		return Receipt{}, invalidAt(validationBinding)
	}
	if r.Asset != e.asset {
		return Receipt{}, invalidAt(validationAsset)
	}
	if r.State != Complete {
		return Receipt{}, invalidAt(validationState)
	}
	if r.Created.After(now) {
		return Receipt{}, invalidAt(validationFuture)
	}
	if !now.Before(r.Expires) {
		return Receipt{}, invalidAt(validationExpiry)
	}
	if r.Expires.Sub(r.Created) != ReceiptLifetime {
		return Receipt{}, invalidAt(validationLifetime)
	}
	if len(r.Files) == 0 {
		return Receipt{}, invalidAt(validationManifest)
	}
	if r.Parent != e.fs.ParentIdentity() {
		return Receipt{}, invalidAt(validationParent)
	}
	return r, nil
}

// Resume does not extend a deadline, adopt a pending root, or grant path access.
// Consumption always performs its own fresh authorization and physical checks.
func (e *Engine) Resume(ctx context.Context, id string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	r, err := e.load(ctx, id)
	if err != nil {
		return err
	}
	var callbackError error
	err = e.fs.WithVerified(r, func([]Payload) error {
		if !e.now().Before(r.Expires) {
			callbackError = invalidAt(validationExpiry)
		} else {
			callbackError = e.authorize(ctx)
		}
		return callbackError
	})
	if err != nil && callbackError == nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return validationFailure{validationPhysical, err}
	}
	return err
}

// Consume is one-shot. The callback receives only verified public byte snapshots
// while the physical tree is pinned. It receives no root/path authorization and
// must not reopen paths. It is intentionally not a Runner.Listener launcher.
func (e *Engine) Consume(ctx context.Context, id string, use func([]Payload) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if use == nil {
		return ErrDenied
	}
	r, err := e.load(ctx, id)
	if err != nil {
		return err
	}
	next := r
	next.State = Consuming
	if err = e.journal.Replace(r, next); err != nil {
		return err
	}
	r = next
	err = e.fs.WithVerified(r, func(p []Payload) error {
		if err := e.authorize(ctx); err != nil {
			return err
		}
		if !e.now().Before(r.Expires) {
			return ErrDenied
		}
		return use(p)
	})
	if err != nil {
		_, failed := e.fail(r, err)
		return failed
	}
	if err = ctx.Err(); err != nil {
		_, failed := e.fail(r, err)
		return failed
	}
	if !e.now().Before(r.Expires) {
		_, failed := e.fail(r, ErrDenied)
		return failed
	}
	next = r
	next.State = Consumed
	if err = e.journal.Replace(r, next); err != nil {
		return fmt.Errorf("consumer finished; receipt commit failed: %w", err)
	}
	return nil
}
