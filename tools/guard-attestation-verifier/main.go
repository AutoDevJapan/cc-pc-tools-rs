package main

import (
    "bytes"
    "context"
    "reflect"
    "crypto/sha256"
    "encoding/hex"
    "encoding/binary"
    "encoding/json"
    "errors"
    "io"
    "strconv"
    "net/url"
    "os"
    "time"

    "github.com/sigstore/sigstore-go/pkg/bundle"
    "github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
    "github.com/sigstore/sigstore-go/pkg/verify"
)

var errVerification = errors.New("guard_attestation_unavailable")

// Candidate validation only; no caller-supplied identity is a grant.
type identityPolicy struct {
    Issuer string `json:"issuer"`
    SAN string `json:"san"`
    RepositoryID string `json:"repository_id"`
    RepositoryURI string `json:"repository_uri"`
    WorkflowRevision string `json:"workflow_revision"`
    Ref string `json:"ref"`
    RunURI string `json:"run_uri"`
    RunID int64 `json:"run_id"`
    RunAttempt int `json:"run_attempt"`
}
type verificationRequest struct {
    Domain string `json:"domain"`
    Source string `json:"source"`
    SourceTree string `json:"source_tree"`
    Identity identityPolicy `json:"identity"`
    PEBytes int `json:"pe_bytes"`
    PESHA string `json:"pe_sha256"`
    ProofBytes int `json:"proof_bytes"`
    ProofSHA string `json:"proof_sha256"`
}

func lowerHex(s string,n int) bool {
    if len(s)!=n{return false}
    for _,r:=range s {if !((r>='0'&&r<='9')||(r>='a'&&r<='f')){return false}}
    return true
}

func decodeRequest(raw []byte) (verificationRequest,error) {
    var req verificationRequest
    if len(raw)==0||len(raw)>64<<10{return req,errVerification}
    dec:=json.NewDecoder(bytes.NewReader(raw));dec.DisallowUnknownFields()
    if dec.Decode(&req)!=nil||dec.Decode(new(any))!=io.EOF{return req,errVerification}
    canonical,err:=json.Marshal(req);if err!=nil||!bytes.Equal(canonical,raw){return verificationRequest{},errVerification}
    if req.Domain!="guard-attestation-verifier-v1"||req.Source!="80c4ed5cbd864585f4894c6286d3d5c92194c1f5"||req.SourceTree!="84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7"||req.PEBytes<1||req.PEBytes>32<<20||req.ProofBytes<1||req.ProofBytes>8<<20||!lowerHex(req.PESHA,64)||!lowerHex(req.ProofSHA,64)||!validPolicy(req.Identity){return verificationRequest{},errVerification}
    return req,nil
}

func validPolicy(p identityPolicy) bool {
    return p.Issuer=="https://token.actions.githubusercontent.com" &&
        p.SAN=="https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/unattended-readiness.yml@refs/heads/main" &&
        p.RepositoryID=="1406818713" &&
        p.RepositoryURI=="https://github.com/AutoDevJapan/cc-pc-tools-rs" &&
        lowerHex(p.WorkflowRevision,40) && p.Ref=="refs/heads/main" &&
        p.RunID>0 && p.RunAttempt>0 && p.RunURI==fixedRunURI(p.RunID,p.RunAttempt) && validRunURI(p.RunURI)
}
func matchesVerifiedIdentity(expected,actual identityPolicy) bool {
    return validPolicy(expected)&&actual==expected
}

// This function validates a single Sigstore bundle cryptographically against
// an already obtained public trusted root and a fixed identity. A caller must
// separately obtain both subject bundles, check the GitHub run/job/step and
// bind this same verified result to the held artifact before any update.
type trustedMaterial = root.TrustedMaterial

func verifySubject(ctx context.Context, trusted trustedMaterial, rawBundle,subject []byte, digestHex string, policy identityPolicy) error {
    if len(subject)==0||len(subject)>32<<20||!lowerHex(digestHex,64){return errVerification}
    subjectHash:=sha256.Sum256(subject)
    if hex.EncodeToString(subjectHash[:])!=digestHex{return errVerification}
    return verifySignedDigest(ctx,trusted,rawBundle,digestHex,policy)
}

// verifySignedDigest validates the bundle against an expected digest. The
// caller must bind that digest to the held subject bytes separately.
func verifySignedDigest(ctx context.Context, trusted trustedMaterial, rawBundle []byte, digestHex string, policy identityPolicy) error {
    if ctx==nil||ctx.Err()!=nil||trusted==nil||reflect.ValueOf(trusted).Kind()==reflect.Pointer&&reflect.ValueOf(trusted).IsNil()||len(rawBundle)==0||len(rawBundle)>4<<20||!lowerHex(digestHex,64)||!validPolicy(policy){return errVerification}
    digest,err:=hex.DecodeString(digestHex);if err!=nil{return errVerification}
    var signed bundle.Bundle
    if err=signed.UnmarshalJSON(rawBundle);err!=nil||signed.Bundle==nil||signed.Bundle.GetDsseEnvelope()==nil{return errVerification}
    san,err:=verify.NewSANMatcher(policy.SAN,"");if err!=nil{return errVerification}
    issuer,err:=verify.NewIssuerMatcher(policy.Issuer,"");if err!=nil{return errVerification}
    expected:=certificate.Extensions{
        BuildSignerURI:policy.SAN,
        BuildSignerDigest:policy.WorkflowRevision,
        BuildConfigURI:policy.SAN,
        BuildConfigDigest:policy.WorkflowRevision,
        RunnerEnvironment:"github-hosted",
        SourceRepositoryURI:policy.RepositoryURI,
        SourceRepositoryIdentifier:policy.RepositoryID,
        SourceRepositoryDigest:policy.WorkflowRevision,
        SourceRepositoryRef:policy.Ref,
        RunInvocationURI:policy.RunURI,
        BuildTrigger:"workflow_dispatch",
        SourceRepositoryVisibilityAtSigning:"public",
    }
    identity,err:=verify.NewCertificateIdentity(san,issuer,expected);if err!=nil{return errVerification}
    verifier,err:=verify.NewVerifier(trusted,verify.WithSignedCertificateTimestamps(1),verify.WithTransparencyLog(1),verify.WithObserverTimestamps(1))
    if err!=nil{return errVerification}
    result,err:=verifier.Verify(&signed,verify.NewPolicy(verify.WithArtifactDigest("sha256",digest),verify.WithCertificateIdentity(identity)))
    if err!=nil||result==nil||result.Statement==nil||result.Statement.GetType()!="https://in-toto.io/Statement/v1"||result.Statement.GetPredicateType()!="https://slsa.dev/provenance/v1"||result.Signature==nil||result.Signature.Certificate==nil||ctx.Err()!=nil{return errVerification}
    subjects:=result.Statement.GetSubject()
    if len(subjects)!=2||subjects[0]==nil||subjects[1]==nil||
        subjects[0].GetName()!="guard.exe"||subjects[0].GetDigest()["sha256"]!=actualPESHA||
        subjects[1].GetName()!="source-proof.json"||subjects[1].GetDigest()["sha256"]!=actualProofSHA||
        (digestHex!=actualPESHA&&digestHex!=actualProofSHA){return errVerification}
    return nil
}

func fixedRunURI(run int64,attempt int) string {
    if run<1||attempt<1{return ""}
    return "https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/"+strconv.FormatInt(run,10)+"/attempts/"+strconv.Itoa(attempt)
}

func validRunURI(raw string) bool {
    u,err:=url.Parse(raw);return err==nil&&u.Scheme=="https"&&u.Host=="github.com"&&u.User==nil&&u.RawQuery==""&&u.Fragment==""
}

// No installed verifier or trusted root is yet admitted for normal runtime.
func enabledAttestationVerifier() any {return nil}

// The command is a diagnostic verifier only. Input is three big-endian
// length-prefixed frames: PE, source proof, Sigstore bundle. Neither input nor
// this report supplies authorization to open or replace an installed target.
type verificationReport struct {
    Domain string `json:"domain"`
    CryptographicVerified bool `json:"cryptographic_verified"`
    SourceProofValidated bool `json:"source_proof_validated"`
    RunMetadataValidated bool `json:"run_metadata_validated"`
    UpdateAuthorized bool `json:"update_authorized"`
    Reason string `json:"reason"`
}
const actualPEBytes = 14299136
const actualProofBytes = 296705
const actualBundleBytes = 11619
const actualPESHA = "a61e25065ae35d51f1ac8133959176b21e2c3740c1516b76f799371f1c27f9a2"
const actualProofSHA = "aeeefe291c6a10a0f8d1bf1ee1a0952daf29dba87046596980c7c509c690f8c8"
const actualBundleSHA = "c7c63a9adc5d342674e61116f732f4fd6dd1b82a8a039228ce80e9ff5fee85e1"

func actualPolicy() identityPolicy {
    return identityPolicy{
        Issuer:"https://token.actions.githubusercontent.com",
        SAN:"https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/unattended-readiness.yml@refs/heads/main",
        RepositoryID:"1406818713",
        RepositoryURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs",
        WorkflowRevision:"f2f9e4c4cddba70240004e7084443c27c51baee9",
        Ref:"refs/heads/main",
        RunURI:fixedRunURI(43574333045,1),
        RunID:43574333045,RunAttempt:1,
    }
}

func readExactFrame(in io.Reader, expected uint32) ([]byte,error) {
    var header [4]byte
    if _,err:=io.ReadFull(in,header[:]);err!=nil||binary.BigEndian.Uint32(header[:])!=expected{return nil,errVerification}
    raw:=make([]byte,expected)
    if _,err:=io.ReadFull(in,raw);err!=nil{clear(raw);return nil,errVerification}
    return raw,nil
}

func subjectSHA(b []byte) string {
    h:=sha256.Sum256(b)
    return hex.EncodeToString(h[:])
}

func acquirePublicGoodRoot(ctx context.Context) (trustedMaterial,error) {
    if ctx==nil||ctx.Err()!=nil{return nil,errVerification}
    opts:=tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx)
    trusted,err:=root.FetchTrustedRootWithOptions(opts)
    if err!=nil||ctx.Err()!=nil||trusted==nil{return nil,errVerification}
    return trusted,nil
}

func runDiagnostic(ctx context.Context,in io.Reader,out io.Writer,load func(context.Context)(trustedMaterial,error)) (exit int) {
    if out==nil{return 1}
    report:=verificationReport{Domain:"guard-attestation-diagnostic-v1",Reason:"guard_attestation_unavailable"}
    exit=1
    defer func(){if json.NewEncoder(out).Encode(report)!=nil {exit=1}}()
    if ctx==nil||ctx.Err()!=nil||in==nil||load==nil{return}
    pe,err:=readExactFrame(in,actualPEBytes);if err!=nil{return};defer clear(pe)
    proof,err:=readExactFrame(in,actualProofBytes);if err!=nil{return};defer clear(proof)
    signed,err:=readExactFrame(in,actualBundleBytes);if err!=nil{return};defer clear(signed)
    var extra [1]byte
    if _,err=io.ReadFull(in,extra[:]);err!=io.EOF{return}
    if subjectSHA(pe)!=actualPESHA||subjectSHA(proof)!=actualProofSHA||subjectSHA(signed)!=actualBundleSHA{return}
    trusted,err:=load(ctx);if err!=nil||ctx.Err()!=nil||trusted==nil{return}
    req:=verificationRequest{Domain:"guard-attestation-verifier-v1",Source:"80c4ed5cbd864585f4894c6286d3d5c92194c1f5",SourceTree:"84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7",Identity:actualPolicy(),PEBytes:len(pe),PESHA:actualPESHA,ProofBytes:len(proof),ProofSHA:actualProofSHA}
    if verifySubjects(ctx,trusted,req,pe,proof,signed,signed)!=nil{return}
    report.CryptographicVerified=true
    report.Reason="guard_attestation_verified_no_update_authority"
    exit=0
    return
}

type diagnosticOutcome struct { exit int; report []byte }

// A bounded command may abandon a stuck read or write only because main exits
// the process immediately after this returns. It never appends a second report
// after any output attempt. The caller must accept output only with exit 0.
func runBoundedDiagnostic(ctx context.Context,out io.Writer,run func(io.Writer) int) int {
    if ctx==nil||out==nil||run==nil||ctx.Err()!=nil{return 1}
    done:=make(chan diagnosticOutcome,1)
    go func(){var buf bytes.Buffer;exit:=run(&buf);done<-diagnosticOutcome{exit,buf.Bytes()}}()
    var result diagnosticOutcome
    select {
    case result=<-done:
        if ctx.Err()!=nil{return 1}
    case <-ctx.Done():
        return 1
    }
    written:=make(chan bool,1)
    go func(){n,err:=out.Write(result.report);written<-err==nil&&n==len(result.report)}()
    select {
    case ok:=<-written:
        if ok&&ctx.Err()==nil{return result.exit}
        return 1
    case <-ctx.Done():
        return 1
    }
}

func main() {
    if len(os.Args)!=1{os.Exit(2)}
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute)
    exit:=runBoundedDiagnostic(ctx,os.Stdout,func(out io.Writer) int {
        if os.Getenv("GUARD_ATTESTATION_DIAGNOSTIC_FIXTURE")=="1" {return runPublicFixtureDiagnostic(ctx,out)}
        return runDiagnostic(ctx,os.Stdin,out,acquirePublicGoodRoot)
    })
    cancel()
    os.Exit(exit)
}

// Verify both attested subjects from their exact in-memory bytes. This does not
// bind them to GitHub run metadata or authorize installation.
func verifySubjects(ctx context.Context,trusted trustedMaterial,request verificationRequest,pe,proof,peBundle,proofBundle []byte) error {
    if !validPolicy(request.Identity)||request.Domain!="guard-attestation-verifier-v1"||request.Source!="80c4ed5cbd864585f4894c6286d3d5c92194c1f5"||request.SourceTree!="84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7"||len(pe)!=request.PEBytes||len(proof)!=request.ProofBytes {return errVerification}
    // Signature binds the exact proof bytes, but a future source-proof
    // validator must independently parse and check its before/after records.
    if err:=verifySubject(ctx,trusted,peBundle,pe,request.PESHA,request.Identity);err!=nil{return errVerification}
    if err:=verifySubject(ctx,trusted,proofBundle,proof,request.ProofSHA,request.Identity);err!=nil{return errVerification}
    return nil
}

func subjectDigestMatches(subjects []string,expected string) bool {
    if !lowerHex(expected,64)||len(subjects)==0||len(subjects)>1024{return false};count:=0;for _,value:=range subjects {if value==expected {count++}};return count==1
}
