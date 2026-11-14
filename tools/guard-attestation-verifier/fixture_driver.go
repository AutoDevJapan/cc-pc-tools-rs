package main

import (
    "context"
    _ "embed"
    "encoding/json"
    "io"
)

// The fixture is an untrusted public bundle. It is input to a diagnostic,
// never a trust root or an authorization to use an installed executable.
//go:embed actual-attestation.sigstore.json
var publicBundleFixture []byte

type fixtureReport struct {
    Domain string `json:"domain"`
    SignedClaimsVerified bool `json:"signed_claims_verified"`
    PayloadBytesVerified bool `json:"payload_bytes_verified"`
    SourceProofValidated bool `json:"source_proof_validated"`
    RunMetadataValidated bool `json:"run_metadata_validated"`
    UpdateAuthorized bool `json:"update_authorized"`
    Reason string `json:"reason"`
}

func runPublicFixtureDiagnostic(ctx context.Context,out io.Writer) (exit int) {
    if out==nil{return 1}
    report:=fixtureReport{Domain:"guard-attestation-fixture-diagnostic-v1",Reason:"guard_attestation_unavailable"}
    exit=1
    defer func(){if json.NewEncoder(out).Encode(report)!=nil {exit=1}}()
    if ctx==nil||ctx.Err()!=nil||len(publicBundleFixture)!=actualBundleBytes||subjectSHA(publicBundleFixture)!=actualBundleSHA{return}
    trusted,err:=acquirePublicGoodRoot(ctx)
    if err!=nil||ctx.Err()!=nil{return}
    if verifySignedDigest(ctx,trusted,publicBundleFixture,actualPESHA,actualPolicy())!=nil||
        verifySignedDigest(ctx,trusted,publicBundleFixture,actualProofSHA,actualPolicy())!=nil{return}
    report.SignedClaimsVerified=true
    report.Reason="signed_claims_verified_without_payload_or_update_authority"
    exit=0
    return
}
