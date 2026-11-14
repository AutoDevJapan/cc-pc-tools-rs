package main

import (
    "encoding/json"
    "context"
    "strings"
    "testing"
)

func validIdentityFixture() identityPolicy {
    return identityPolicy{
        Issuer:"https://token.actions.githubusercontent.com",
        SAN:"https://github.com/AutoDevJapan/cc-pc-tools-rs/.github/workflows/unattended-readiness.yml@refs/heads/main",
        RepositoryID:"1406818713",
        RepositoryURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs",
        WorkflowRevision:strings.Repeat("a",40),
        Ref:"refs/heads/main",
        RunURI:"https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/17/attempts/1",
        RunID:17,RunAttempt:1,
    }
}

func TestFixedIdentityPolicyRejectsWrongClaims(t *testing.T) {
    good:=validIdentityFixture()
    for _,fault:=range []string{"none","issuer","san","repo","revision","ref","run"} {
        t.Run(fault,func(t *testing.T){
            got:=good
            switch fault {
            case "issuer":got.Issuer="https://other"
            case "san":got.SAN+=".other"
            case "repo":got.RepositoryID="2"
            case "revision":got.WorkflowRevision=strings.Repeat("b",40)
            case "ref":got.Ref="refs/heads/other"
            case "run":got.RunURI="https://github.com/AutoDevJapan/cc-pc-tools-rs/actions/runs/18/attempts/1"
            }
            if matchesVerifiedIdentity(good,got)!=(fault=="none") {t.Fatal("identity policy mismatch",fault)}
        })
    }
}

func TestAttestationInputRejectsUnknownFields(t *testing.T) {
    req:=verificationRequest{Domain:"guard-attestation-verifier-v1",Source:"80c4ed5cbd864585f4894c6286d3d5c92194c1f5",SourceTree:"84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7",Identity:validIdentityFixture(),PEBytes:1,PESHA:strings.Repeat("a",64),ProofBytes:1,ProofSHA:strings.Repeat("b",64)}
    raw,err:=json.Marshal(req);if err!=nil {t.Fatal(err)}
    if _,err=decodeRequest(raw);err!=nil {t.Fatal(err)}
    raw=append(raw[:len(raw)-1],[]byte(`,"unknown":true}`)...)
    if _,err=decodeRequest(raw);err==nil {t.Fatal("unknown request field accepted")}
}

func TestUnconnectedVerifierNeverIssuesAuthority(t *testing.T) {
    if enabledAttestationVerifier()!=nil {t.Fatal("unreviewed verifier became runtime authority")}
}

func TestVerifySubjectRejectsUnavailableTrustAndBundle(t *testing.T) {
    policy:=validIdentityFixture()
    raw:=actualBundleFixture(t)
    // The matching digest and public bundle avoid an early hash mismatch.
    if verifySignedDigest(context.Background(),nil,raw,actualPESHA,policy)==nil {t.Fatal("missing trusted root accepted")}
}

func TestVerifySubjectsRequiresBothAndBoundSizes(t *testing.T) {
    req:=verificationRequest{Domain:"guard-attestation-verifier-v1",Source:"80c4ed5cbd864585f4894c6286d3d5c92194c1f5",SourceTree:"84437789dfbe1bf1d56e4b96698b5c8da4a3dfb7",Identity:validIdentityFixture(),PEBytes:1,PESHA:strings.Repeat("a",64),ProofBytes:1,ProofSHA:strings.Repeat("b",64)}
    if verifySubjects(context.Background(),nil,req,[]byte("x"),[]byte("y"),nil,nil)==nil {t.Fatal("unsigned subjects accepted")}
    if verifySubjects(context.Background(),nil,req,[]byte("xx"),[]byte("y"),nil,nil)==nil {t.Fatal("wrong subject length accepted")}
}

func TestSignedStatementSubjectDigestSelection(t *testing.T) {
    if !subjectDigestMatches([]string{strings.Repeat("a",64)},strings.Repeat("a",64)) {t.Fatal("valid subject missing")}
    if subjectDigestMatches([]string{strings.Repeat("a",64),strings.Repeat("a",64)},strings.Repeat("a",64)) {t.Fatal("duplicate subject accepted")}
    if subjectDigestMatches([]string{strings.Repeat("b",64)},strings.Repeat("a",64)) {t.Fatal("different subject accepted")}
}
