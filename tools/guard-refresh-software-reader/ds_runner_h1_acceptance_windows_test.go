//go:build windows && ds_runner_h1_acceptance

package main

import (
    "bytes"
    "context"
    "encoding/base64"
    "encoding/json"
    "io"
    "net/http"
    "os"
    "testing"
    "time"

    "github.com/sigstore/sigstore-go/pkg/root"
    "github.com/sigstore/sigstore-go/pkg/tuf"
)

// The tagged acceptance is an explicit real-material test. Missing inputs fail;
// ordinary unit runs do not silently turn an unexecuted real test into a pass.
func dsRunnerH1Material(t *testing.T,name string,limit int64,expected string)[]byte{
    t.Helper();path:=os.Getenv(name);if path==""{t.Fatalf("required actual H1 material input missing: %s",name)}
    f,err:=os.Open(path);if err!=nil{t.Fatal(err)};raw,readErr:=io.ReadAll(io.LimitReader(f,limit+1));closeErr:=f.Close();if readErr!=nil||closeErr!=nil||len(raw)==0||int64(len(raw))>limit||digest(raw)!=expected{clear(raw);t.Fatal("actual H1 input bytes did not match the fixed tuple")};t.Cleanup(func(){clear(raw)});return raw
}
func TestDSRunnerH1ActualBytesAndSigstore(t *testing.T){
    p:=dsRunnerAdmittedCapture
    if p.RunID!=43778902807||p.ArtifactID!=14187089175||p.Producer!="1d5c06f36e63f723ed0ad099efbbeff8f27b2b28"{t.Fatal("test not bound to the original H1")}
    archive,signed:=dsRunnerH1ActualMaterials(t,p)
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel()
    held,err:=dsRunnerReadArchive(ctx,archive,p);if err!=nil{t.Fatalf("actual ZIP/proof/PE not accepted: %v",err)};defer held.erase()
    if len(held.payload)!=14213632||len(held.capture)!=1012||len(held.proof)!=257159{t.Fatal("actual H1 file lengths changed")}
    transport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second};defer transport.CloseIdleConnections();client:=&http.Client{Transport:transport,Timeout:30*time.Second}
    trusted,err:=root.FetchTrustedRootWithOptions(tuf.DefaultOptions().WithDisableLocalCache().WithContext(ctx).WithFetcher(ghaGuardNewTUFRequestFetcher(ctx,client)));if err!=nil{t.Fatalf("actual TUF trust material: %v",err)}
    if err=dsRunnerVerifySignedSubjects(ctx,trusted,signed,p);err!=nil{t.Fatalf("actual signature/identity/SCT/tlog/subjects: %v",err)}
    t.Run("proof-compiler-exit",func(t *testing.T){var proof dsRunnerSourceProof;if json.Unmarshal(held.proof,&proof)!=nil{t.Fatal("proof decode")};one:=1;proof.CompilerExit=&one;changed,err:=json.Marshal(proof);if err!=nil||bytes.Equal(changed,held.proof){t.Fatal("proof mutation not made")};m:=held.manifest;m.SourceManifestSHA=digest(changed);if dsRunnerProofMatches(ctx,changed,m)==nil{t.Fatal("nonzero compiler exit accepted after the outer digest was matched")}})
    t.Run("proof-after-differs",func(t *testing.T){var proof dsRunnerSourceProof;if json.Unmarshal(held.proof,&proof)!=nil{t.Fatal("proof decode")};var after dsRunnerBefore;if json.Unmarshal(proof.After,&after)!=nil{t.Fatal("after decode")};after.RunID++;proof.After,_=json.Marshal(after);changed,err:=json.Marshal(proof);if err!=nil||bytes.Equal(changed,held.proof){t.Fatal("after mutation not made")};m:=held.manifest;m.SourceManifestSHA=digest(changed);if dsRunnerProofMatches(ctx,changed,m)==nil{t.Fatal("changed after snapshot accepted with a matched outer digest")}})
    t.Run("payload-subject-mismatch",func(t *testing.T){changed:=append([]byte(nil),held.payload...);defer clear(changed);changed[len(changed)-1]^=1;wrong:=p;wrong.OutputSHA=digest(changed);if wrong.OutputSHA==p.OutputSHA{t.Fatal("payload mutation was vacuous")};if dsRunnerVerifySignedSubjects(ctx,trusted,signed,wrong)==nil{t.Fatal("real signature accepted another payload digest")}})
    t.Run("signature-byte-mutated",func(t *testing.T){var material map[string]json.RawMessage;if json.Unmarshal(signed,&material)!=nil{t.Fatal("bundle decode")};var envelope struct{Payload string `json:"payload"`;PayloadType string `json:"payloadType"`;Signatures []struct{Sig string `json:"sig"`;KeyID string `json:"keyid,omitempty"`} `json:"signatures"`};if json.Unmarshal(material["dsseEnvelope"],&envelope)!=nil||len(envelope.Signatures)!=1{t.Fatal("actual signature count")};signature,err:=base64.StdEncoding.DecodeString(envelope.Signatures[0].Sig);if err!=nil||len(signature)==0{t.Fatal("signature decode")};signature[len(signature)-1]^=1;envelope.Signatures[0].Sig=base64.StdEncoding.EncodeToString(signature);material["dsseEnvelope"],_=json.Marshal(envelope);changed,err:=json.Marshal(material);if err!=nil||bytes.Equal(changed,signed){t.Fatal("signature mutation not made")};wrong:=p;wrong.BundleSHA=digest(changed);if dsRunnerVerifySignedSubjects(ctx,trusted,changed,wrong)==nil{t.Fatal("signature mutation accepted after the bundle hash was matched")}})
    t.Run("nil-root",func(t *testing.T){if dsRunnerVerifySignedSubjects(ctx,nil,signed,p)==nil{t.Fatal("actual bundle accepted with no trust material")}})
    t.Run("cancelled",func(t *testing.T){stopped,stop:=context.WithCancel(ctx);stop();if dsRunnerVerifySignedSubjects(stopped,trusted,signed,p)==nil{t.Fatal("cancelled verification accepted the unchanged real bundle")}})
    t.Log("DS_RUNNER_H1_ACTUAL=payload_proof_signature_root_SCT_tlog_verified; runtime_candidate=not_issued; installed=not_tested")
    t.Run("wrong-producer",func(t *testing.T){wrong:=p;wrong.Producer="0000000000000000000000000000000000000001";if dsRunnerVerifySignedSubjects(ctx,trusted,signed,wrong)==nil{t.Fatal("actual signature accepted another producer identity")}})
}

// Hosted acquisition is fixture data only. It does not call the normal
// constructor, verify the owner credential, or issue a runtime candidate.
func dsRunnerH1ActualMaterials(t *testing.T,p dsRunnerCapturePin)([]byte,[]byte){
    t.Helper()
    if os.Getenv("DS_RUNNER_H1_ZIP")!=""||os.Getenv("DS_RUNNER_H1_BUNDLE")!=""{return dsRunnerH1Material(t,"DS_RUNNER_H1_ZIP",64<<20,p.ArchiveSHA),dsRunnerH1Material(t,"DS_RUNNER_H1_BUNDLE",4<<20,p.BundleSHA)}
    if os.Getenv("GITHUB_ACTIONS")!="true"||os.Getenv("GITHUB_REPOSITORY")!="AutoDevJapan/cc-pc-tools-rs"||os.Getenv("GITHUB_SERVER_URL")!="https://github.com"||os.Getenv("GH_TOKEN")==""{t.Fatal("actual H1 inputs missing; hosted fixture acquisition is not available")}
    ctx,cancel:=context.WithTimeout(context.Background(),2*time.Minute);defer cancel();owned,stop:=context.WithCancel(context.Background())
    apiTransport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second};objectTransport:=&http.Transport{Proxy:nil,TLSHandshakeTimeout:10*time.Second,ResponseHeaderTimeout:15*time.Second}
    defer apiTransport.CloseIdleConnections();defer objectTransport.CloseIdleConnections()
    session:=&dsRunnerCredential{secret:[]byte(os.Getenv("GH_TOKEN")),base:apiTransport,pin:p,root:owned,cancel:stop,ready:true,done:make(chan struct{})}
    defer func(){cleanup,c:=context.WithTimeout(context.Background(),5*time.Second);defer c();if err:=session.Close(cleanup);err!=nil{t.Error("hosted fixture credential cleanup failed")}}()
    api:=&http.Client{Transport:session,Timeout:30*time.Second,CheckRedirect:func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}};object:=&http.Client{Transport:objectTransport,Timeout:30*time.Second}
    archive,err:=ghaGuardLiveRedirectedDetailed(ctx,api,object,dsRunnerAPI+"/actions/artifacts/14187089175/zip",64<<20);if err!=nil||len(archive)!=14472201||digest(archive)!=p.ArchiveSHA{clear(archive);t.Fatal("original H1 ZIP fixture acquisition failed")};t.Cleanup(func(){clear(archive)})
    document,err:=dsRunnerHTTPBytes(ctx,object,dsRunnerAPI+"/attestations/sha256:"+p.OutputSHA+"?per_page=100",8<<20);if err!=nil{t.Fatal("actual public H1 bundle response unavailable")};defer clear(document)
    signed,err:=ghaFreshBundleFromJSON(document,ghaGuardPublicationRecord{OutputSHA:p.OutputSHA,AttestationID:p.AttestationID});if err!=nil||len(signed)!=11814||digest(signed)!=p.BundleSHA{clear(signed);t.Fatal("actual public H1 bundle bytes changed")};t.Cleanup(func(){clear(signed)});return archive,signed
}
