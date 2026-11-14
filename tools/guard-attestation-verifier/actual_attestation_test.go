package main

import (
    "bytes"
    "context"
    "encoding/base64"
    "encoding/binary"
    "encoding/json"
    "encoding/hex"
    "errors"
    "io"
    "os"
    "os/exec"
    "strings"
    "testing"
    "time"

    "github.com/sigstore/sigstore-go/pkg/bundle"
    "github.com/sigstore/sigstore-go/pkg/fulcio/certificate"
    "github.com/sigstore/sigstore-go/pkg/verify"
)

// These public bytes are an untrusted input fixture, not a root of trust.
func actualBundleFixture(t *testing.T) []byte {
    t.Helper()
    raw,err:=os.ReadFile("actual-attestation.sigstore.json")
    if err!=nil||len(raw)!=actualBundleBytes||subjectSHA(raw)!=actualBundleSHA {
        t.Fatal("actual public bundle fixture changed",err)
    }
    return raw
}

func TestActualBundleCryptographicVerificationAndControls(t *testing.T) {
    raw:=actualBundleFixture(t)
    ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second)
    defer cancel()
    trusted,err:=acquirePublicGoodRoot(ctx)
    if err!=nil {t.Fatal("public-good TUF trusted root unavailable",err)}

    // Both subjects are in one DSSE statement. This is signature and identity
    // verification of digest claims only; payload bytes are tested separately.
    for _,digest:=range []string{actualPESHA,actualProofSHA} {
        if err:=verifySignedDigest(ctx,trusted,raw,digest,actualPolicy());err!=nil {
            t.Fatalf("actual bundle/digest verification failed: %s: %v",digest,err)
        }
    }

    cancelled,cancelNow:=context.WithCancel(context.Background());cancelNow()
    if verifySignedDigest(cancelled,trusted,raw,actualPESHA,actualPolicy())==nil {t.Fatal("cancelled verifier accepted")}

    policy:=actualPolicy()
    policy.WorkflowRevision=strings.Repeat("b",40)
    if verifySignedDigest(ctx,trusted,raw,actualPESHA,policy)==nil {t.Fatal("wrong workflow revision accepted")}
    policy=actualPolicy()
    policy.RunID++
    policy.RunURI=fixedRunURI(policy.RunID,policy.RunAttempt)
    if verifySignedDigest(ctx,trusted,raw,actualPESHA,policy)==nil {t.Fatal("wrong run accepted")}
    san,err:=verify.NewSANMatcher(policy.SAN,"");if err!=nil{t.Fatal(err)}
    issuer,err:=verify.NewIssuerMatcher(policy.Issuer,"");if err!=nil{t.Fatal(err)}
    wrongRepo:=certificate.Extensions{SourceRepositoryIdentifier:"2"}
    identity,err:=verify.NewCertificateIdentity(san,issuer,wrongRepo);if err!=nil{t.Fatal(err)}
    verifier,err:=verify.NewVerifier(trusted,verify.WithSignedCertificateTimestamps(1),verify.WithTransparencyLog(1),verify.WithObserverTimestamps(1))
    if err!=nil{t.Fatal(err)}
    var signed bundle.Bundle
    if err=signed.UnmarshalJSON(raw);err!=nil{t.Fatal(err)}
    digest,err:=hex.DecodeString(actualPESHA);if err!=nil{t.Fatal(err)}
    if _,err=verifier.Verify(&signed,verify.NewPolicy(verify.WithArtifactDigest("sha256",digest),verify.WithCertificateIdentity(identity)));err==nil {t.Fatal("wrong repository id accepted")}
    if verifySignedDigest(ctx,trusted,raw,strings.Repeat("0",64),actualPolicy())==nil {t.Fatal("different subject accepted")}

    var envelope struct {
        Payload string `json:"payload"`
        PayloadType string `json:"payloadType"`
        Signatures []struct { Signature string `json:"sig"` } `json:"signatures"`
    }
    var parsed map[string]json.RawMessage
    if json.Unmarshal(raw,&parsed)!=nil||json.Unmarshal(parsed["dsseEnvelope"],&envelope)!=nil||len(envelope.Signatures)!=1 {t.Fatal("unexpected fixture envelope")}
    signature,err:=base64.StdEncoding.DecodeString(envelope.Signatures[0].Signature)
    if err!=nil||len(signature)==0 {t.Fatal("unexpected fixture signature",err)}
    signature[len(signature)-1]^=1
    envelope.Signatures[0].Signature=base64.StdEncoding.EncodeToString(signature)
    changed,err:=json.Marshal(envelope);if err!=nil{t.Fatal(err)}
    parsed["dsseEnvelope"]=changed
    mutated,err:=json.Marshal(parsed);if err!=nil{t.Fatal(err)}
    if verifySignedDigest(ctx,trusted,mutated,actualPESHA,actualPolicy())==nil {t.Fatal("tampered signature accepted")}
}

func TestExactFrameAndDiagnosticFailure(t *testing.T) {
    var wire bytes.Buffer
    _=binary.Write(&wire,binary.BigEndian,uint32(3))
    wire.WriteString("abc")
    raw,err:=readExactFrame(&wire,3)
    if err!=nil||string(raw)!="abc" {t.Fatal("valid frame rejected",err)}
    if _,err=readExactFrame(bytes.NewReader([]byte{0,0,0,4}),3);err==nil {t.Fatal("wrong size accepted")}
    if _,err=readExactFrame(bytes.NewReader([]byte{0,0,0,3,1}),3);err==nil {t.Fatal("truncated frame accepted")}

    calls:=0
    load:=func(context.Context)(trustedMaterial,error){calls++;return nil,errors.New("unexpected root acquisition")}
    var out bytes.Buffer
    if code:=runDiagnostic(context.Background(),bytes.NewReader(nil),&out,load);code!=1||calls!=0 {t.Fatal("invalid input reached root loader or succeeded",code,calls)}
    var report verificationReport
    if json.Unmarshal(out.Bytes(),&report)!=nil||report.CryptographicVerified||report.UpdateAuthorized||report.SourceProofValidated||report.RunMetadataValidated||report.Reason!="guard_attestation_unavailable" {t.Fatal("invalid failure projection")}
}

type observedDiagnosticWriter struct { writes chan []byte }
func (w *observedDiagnosticWriter) Write(b []byte) (int,error) {
    w.writes<-append([]byte(nil),b...)
    return len(b),nil
}

func TestBoundedDiagnosticSuppressesLateSuccess(t *testing.T) {
    ctx,cancel:=context.WithCancel(context.Background())
    started:=make(chan struct{})
    release:=make(chan struct{})
    finished:=make(chan struct{})
    output:=&observedDiagnosticWriter{writes:make(chan []byte,2)}
    code:=make(chan int,1)
    go func(){code<-runBoundedDiagnostic(ctx,output,func(w io.Writer) int {
        close(started)
        <-release
        _,_=io.WriteString(w,"late success")
        close(finished)
        return 0
    })}()
    select {
    case <-started:
    case <-time.After(3*time.Second):
        close(release);cancel();t.Fatal("worker did not start")
    }
    cancel()
    select {
    case got:=<-code:
        if got!=1 {close(release);t.Fatal("cancelled diagnostic succeeded",got)}
    case <-time.After(3*time.Second):
        close(release);t.Fatal("cancelled diagnostic did not return")
    }
    close(release)
    select {
    case <-finished:
    case <-time.After(3*time.Second):
        t.Fatal("owned worker fixture did not terminate")
    }
    // Observe a post-completion window: reading output before the worker
    // completed would miss a delayed success write.
    select {
    case <-output.writes: t.Fatal("late success escaped after cancellation")
    case <-time.After(100*time.Millisecond):
    }
}

type shortDiagnosticWriter struct { calls int; content bytes.Buffer }
func (w *shortDiagnosticWriter) Write(b []byte) (int,error) {
    w.calls++
    if len(b)==0{return 0,nil}
    n:=len(b)-1
    _,_=w.content.Write(b[:n])
    return n,nil
}

func TestBoundedDiagnosticRejectsShortSuccessOutput(t *testing.T) {
    output:=&shortDiagnosticWriter{}
    report:="complete success report"
    got:=runBoundedDiagnostic(context.Background(),output,func(w io.Writer) int {
        _,_=io.WriteString(w,report)
        return 0
    })
    if got==0||output.calls!=1||output.content.String()!=report[:len(report)-1] {t.Fatal("short report was retried or treated as successful")}
}

type blockedDiagnosticWriter struct { started chan struct{}; release chan struct{}; finished chan struct{} }
func (w *blockedDiagnosticWriter) Write(b []byte) (int,error) {
    close(w.started)
    <-w.release
    close(w.finished)
    return len(b),nil
}

func TestBoundedDiagnosticReturnsWhenOutputBlocks(t *testing.T) {
    ctx,cancel:=context.WithCancel(context.Background())
    output:=&blockedDiagnosticWriter{started:make(chan struct{}),release:make(chan struct{}),finished:make(chan struct{})}
    result:=make(chan int,1)
    go func(){result<-runBoundedDiagnostic(ctx,output,func(w io.Writer) int {_,_=io.WriteString(w,"report");return 0})}()
    select {
    case <-output.started:
    case <-time.After(3*time.Second):
        close(output.release)
        cancel()
        t.Fatal("writer did not start")
    }
    cancel()
    select {
    case got:=<-result:
        if got!=1 {close(output.release);t.Fatal("blocked writer was accepted",got)}
    case <-time.After(3*time.Second):
        close(output.release)
        t.Fatal("cancellation did not bound blocked output")
    }
    close(output.release)
    select {
    case <-output.finished:
    case <-time.After(3*time.Second):
        t.Fatal("owned writer fixture did not terminate")
    }
}

type foreverBlockedWriter struct {}
func (foreverBlockedWriter) Write([]byte) (int,error) { <-make(chan struct{});return 0,nil }

// The child reproduces main's exit-on-return contract. The parent owns and
// waits for the child with a separate deadline, including the failure path.
func TestBoundedDiagnosticProcessExitsWithBlockedOutput(t *testing.T) {
    const child="GUARD_ATTEST_BLOCKED_TEST_CHILD"
    if os.Getenv(child)=="1" {
        ctx,cancel:=context.WithTimeout(context.Background(),100*time.Millisecond)
        code:=runBoundedDiagnostic(ctx,foreverBlockedWriter{},func(w io.Writer) int {_,_=io.WriteString(w,"report");return 0})
        cancel()
        os.Exit(code)
    }
    ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
    defer cancel()
    cmd:=exec.CommandContext(ctx,os.Args[0],"-test.run=^TestBoundedDiagnosticProcessExitsWithBlockedOutput$")
    cmd.Env=append(os.Environ(),child+"=1")
    err:=cmd.Run()
    if ctx.Err()!=nil {t.Fatal("owned child exceeded deadline",ctx.Err())}
    var exit *exec.ExitError
    if !errors.As(err,&exit)||exit.ExitCode()!=1 {t.Fatal("blocked output child did not exit with failure",err)}
}
