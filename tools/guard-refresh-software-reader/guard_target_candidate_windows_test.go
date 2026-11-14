//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
	"testing"
)

// Synthetic package-private issuer fixtures exercise adapter behavior only.
// They neither authenticate GitHub nor mint a normal runtime capability.
func guardCandidateOwnedFixture(t *testing.T,ctx context.Context) (*guardTargetCandidate,*ghaGuardVerifiedCandidate) {
	t.Helper()
	raw:=ghaGuardTestPE()
	generation:=ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:4,ArtifactID:5,OutputSHA:digest(raw),ProofSHA:strings.Repeat("c",64)}
	source:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},generation:generation,payload:raw}
	source.self=source
	value,err:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,nil})
	if err!=nil {t.Fatal("synthetic candidate admission",err)}
	artifact,ok:=value.(*guardTargetCandidate)
	if !ok {t.Fatal("wrong artifact adapter")}
	t.Cleanup(func(){_ = artifact.Close()})
	return artifact,source
}

func guardCandidatePurposeFixture(t *testing.T,ctx context.Context,artifact *guardTargetCandidate,mode string) (*guardTargetOwnerPurpose,*guardTargetTestFixture) {
	t.Helper()
	value,err:=guardTargetNormalPurposeForArtifact(ctx,artifact)
	if err!=nil {t.Fatal("fixed purpose",err)}
	purpose:=value.(*guardTargetOwnerPurpose)
	metadata:=newGuardTargetFixture(mode,func(){})
	purpose.open=func(context.Context)(guardTargetMetadataLease,error){metadata.opens++;return metadata.lease,nil}
	t.Cleanup(func(){_ = purpose.Close()})
	return purpose,metadata
}

func TestGuardTargetCandidateNormalFactoryCannotMintAuthority(t *testing.T) {
	artifact,err:=guardTargetNormalArtifactFromServer(context.Background())
	if artifact!=nil||!errors.Is(err,errGuardTargetArtifactUnavailable) {t.Fatal("missing server issuer became an artifact")}
	purpose,err:=guardTargetNormalPurposeForArtifact(context.Background(),nil)
	if purpose!=nil||!errors.Is(err,errGuardTargetPurposeUnavailable){t.Fatal("missing artifact became target purpose")}
	calls:=0
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	artifact,err=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){calls++;return nil,nil})
	if artifact!=nil||!errors.Is(err,context.Canceled)||calls!=0{t.Fatal("cancelled caller reached issuer")}
}

func TestGuardTargetCandidateRejectsInvalidBeforePurposeOrBorrow(t *testing.T) {
	for _,fault:=range []string{"missing","partial","partial-cleanup","closed","source","tree","generation","digest","size","pe","cancelled-return"}{
		t.Run(fault,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			raw:=ghaGuardTestPE()
			generation:=ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:4,ArtifactID:5,OutputSHA:digest(raw),ProofSHA:strings.Repeat("c",64)}
			source:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},generation:generation,payload:raw}
			source.self=source
			switch fault {
			case "closed":_ = source.Close()
			case "source":source.generation.Source=strings.Repeat("1",40)
			case "tree":source.generation.SourceTree=strings.Repeat("1",40)
			case "generation":source.generation.ArtifactID=0
			case "digest":source.payload[0]^=1
			case "size":source.payload=nil
			case "pe":source.payload=[]byte("not an executable");source.generation.OutputSHA=digest(source.payload)
			}
			value,err:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){
				if fault=="missing"{return nil,nil}
				if fault=="cancelled-return"{cancel()}
				if fault=="partial"{return source,errors.New("untrusted private source failure")}
				if fault=="partial-cleanup"{return source,errors.Join(context.Canceled,errGuardTargetCleanup)}
				return source,nil
			})
			if err==nil {t.Fatal("invalid candidate accepted",fault)}
			if fault=="partial-cleanup"&&(!errors.Is(err,context.Canceled)||!errors.Is(err,errGuardTargetCleanup)){t.Fatal("partial compound facts lost")}
			if value!=nil {
				if value.VerifySourceGeneration(){t.Fatal("partial artifact usable",fault)}
				if purpose,e:=guardTargetNormalPurposeForArtifact(ctx,value);purpose!=nil||e==nil{t.Fatal("partial resource obtained purpose",fault)}
				if value.Close()!=nil{t.Fatal("partial close failed")}
				guardCandidateOwnedClosed(t,source)
			}
			if fault!="missing"&&source.VerifySourceGeneration(){t.Fatal("partial candidate not closed")}
		})
	}
}

func TestGuardTargetCandidateMetadataUsesSameBorrowedGeneration(t *testing.T) {
	for _,fault:=range []string{"normal","identity","acl","cancel-open","partial-open","close"}{
		t.Run(fault,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			artifact,source:=guardCandidateOwnedFixture(t,ctx)
			mode:="normal"
			if fault=="identity"{mode="changed-id"};if fault=="acl"{mode="changed-acl"}
			purpose,fixture:=guardCandidatePurposeFixture(t,ctx,artifact,mode)
			original:=purpose.open
			purpose.open=func(ctx context.Context)(guardTargetMetadataLease,error){
				lease,err:=original(ctx)
				if fault=="cancel-open"{cancel()}
				if fault=="partial-open"{return lease,errors.New("private opening failure")}
				if fault=="close"{fixture.mode="metadata-close"}
				return lease,err
			}
			result:=inspectGuardTargetMetadata(ctx,artifact,purpose)
			if result.Qualified!=(fault=="normal")||fixture.opens!=1||fixture.leafCloses!=1{t.Fatal("wrong metadata outcome",fault,result.Reason)}
			if result.DataAccessGranted||result.SoftwareReplaceAllowed||result.UpdatePerformed{t.Fatal("metadata operation granted writes")}
			if source.VerifySourceGeneration()||artifact.VerifySourceGeneration(){t.Fatal("controller did not close artifact")}
			guardCandidateOwnedClosed(t,source)
			if fault=="close"&&!result.CleanupFailed{t.Fatal("cleanup failure lost")}
			if fault=="cancel-open"&&!result.Cancelled{t.Fatal("cancellation lost")}
		})
	}
}

func TestGuardTargetCandidatePurposeRejectsDifferentOwnerAndGeneration(t *testing.T) {
	for _,fault:=range []string{"wrapper","same-source-wrapper","candidate","generation","purpose-copy","artifact-copy","domain","target","closed-purpose","cancel","without-borrow","repeat-open"}{
		t.Run(fault,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			artifact,source:=guardCandidateOwnedFixture(t,ctx)
			purpose,fixture:=guardCandidatePurposeFixture(t,ctx,artifact,"normal")
			var actual guardTargetArtifact=artifact
			switch fault {
			case "wrapper":actual,_=guardCandidateOwnedFixture(t,ctx)
			case "same-source-wrapper":copy:=&guardTargetCandidate{candidate:artifact.candidate,generation:artifact.generation,ctx:ctx,payloadBytes:artifact.payloadBytes,admitted:true};copy.self=copy;actual=copy
			case "candidate":other,_:=guardCandidateOwnedFixture(t,ctx);oldOwner:=artifact.candidate;artifact.candidate=other.candidate;defer func(){artifact.candidate=oldOwner}()
			case "generation":source.mu.Lock();source.generation.JobID++;source.mu.Unlock()
			case "purpose-copy":purpose=&guardTargetOwnerPurpose{self:purpose,artifact:purpose.artifact,candidate:purpose.candidate,generation:purpose.generation,ctx:ctx,domain:purpose.domain,target:purpose.target,open:purpose.open}
			case "artifact-copy":actual=&guardTargetCandidate{self:artifact,candidate:artifact.candidate,generation:artifact.generation,ctx:ctx,payloadBytes:artifact.payloadBytes,admitted:true}
			case "domain":purpose.domain="other"
			case "target":purpose.target="D:/unrelated"
			case "closed-purpose":_ = purpose.Close()
			case "cancel":cancel()
			}
			if fault=="without-borrow" {
				lease,err:=purpose.OpenMetadata(ctx);if lease!=nil||err==nil||fixture.opens!=0{t.Fatal("metadata opened outside held payload")};return
			}
			if fault=="repeat-open" {
				err:=artifact.WithPinnedPayload(func([]byte)error{first,e:=purpose.OpenMetadata(ctx);if e!=nil||first==nil{return errors.New("first open rejected")};defer first.Close();second,e:=purpose.OpenMetadata(ctx);if second!=nil||e==nil{return errors.New("second open accepted")};return nil})
				if err!=nil||fixture.opens!=1{t.Fatal("single open contract failed",err)};return
			}
			if purpose.Recheck(ctx,actual)==nil||fixture.opens!=0{t.Fatal("different capability accepted",fault)}
		})
	}
}

func TestGuardTargetCandidateBorrowAndCloseKeepOwnershipSeparate(t *testing.T) {
	for _,fault:=range []string{"purpose-close","mutate-before","mutate-during","cancel-during","old-alias-close","owner-close","artifact-close","panic","nested"}{
		t.Run(fault,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			artifact,source:=guardCandidateOwnedFixture(t,ctx)
			purpose,_:=guardCandidatePurposeFixture(t,ctx,artifact,"normal")
			if fault=="purpose-close"{if purpose.Close()!=nil||!artifact.VerifySourceGeneration(){t.Fatal("purpose closed artifact")};if !artifact.VerifySourceGeneration(){t.Fatal("owned source released by purpose")};return}
			if fault=="mutate-before"{source.mu.Lock();source.payload[0]^=1;source.mu.Unlock()}
			calls:=0
			panicked:=false
			var result error
			func(){defer func(){if recover()!=nil{panicked=true}}();result=artifact.WithPinnedPayload(func(raw []byte)error{
				calls++
				switch fault {
				case "mutate-during":raw[0]^=1
				case "cancel-during":cancel()
				case "old-alias-close":if source.Close()==nil{t.Fatal("old alias close accepted after move")}
				case "owner-close":_ = artifact.candidate.Close()
				case "artifact-close":if !errors.Is(artifact.Close(),errGuardTargetCleanup){t.Fatal("active close reported completed cleanup")}
				case "panic":panic("owned fixture panic")
				case "nested":if artifact.WithPinnedPayload(func([]byte)error{return nil})==nil{t.Fatal("nested borrow accepted")}
				}
				return nil
			})}()
			if fault=="panic"{if !panicked||artifact.VerifySourceGeneration(){t.Fatal("panic did not invalidate")};return}
			if fault=="nested"||fault=="old-alias-close"{if result!=nil||calls!=1||!artifact.VerifySourceGeneration(){t.Fatal("rejected old alias/nested borrow damaged current owner")};return}
			if result==nil||artifact.VerifySourceGeneration(){t.Fatal("borrow failure qualified",fault)}
			if fault=="mutate-before"&&calls!=0{t.Fatal("changed input reached callback")}
		})
	}
}


func guardCandidateUnclaimedFixture() *ghaGuardVerifiedCandidate {
	raw:=ghaGuardTestPE()
	source:=&ghaGuardVerifiedCandidate{issuer:&ghaGuardIssuer{token:1},payload:raw,
		generation:ghaGuardGeneration{Source:ghaGuardSource,SourceTree:ghaGuardSourceTree,ProducerRevision:strings.Repeat("b",40),RunID:2,RunAttempt:1,JobID:3,CheckRunID:4,ArtifactID:5,OutputSHA:digest(raw),ProofSHA:strings.Repeat("c",64)}}
	source.self=source
	return source
}

func guardCandidateOwnedClosed(t *testing.T,source *ghaGuardVerifiedCandidate) {
	t.Helper()
	source.mu.Lock()
	defer source.mu.Unlock()
	if !source.closed||source.issuer!=nil||source.payload!=nil||source.borrowers!=0 {t.Fatal("exclusive owner did not finish cleanup")}
}

func TestGuardTargetCandidateClaimsExactlyOneAdapter(t *testing.T) {
	for _,mode:=range []string{"sequential","concurrent"}{
		t.Run(mode,func(t *testing.T){
			ctx:=context.Background()
			source:=guardCandidateUnclaimedFixture()
			load:=func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,nil}
			type acquired struct { artifact guardTargetArtifact; err error }
			var results [2]acquired
			if mode=="sequential" {
				results[0].artifact,results[0].err=guardTargetArtifactFromServer(ctx,load)
				results[1].artifact,results[1].err=guardTargetArtifactFromServer(ctx,load)
			} else {
				start:=make(chan struct{})
				var done sync.WaitGroup
				for i:=range results {done.Add(1);go func(index int){defer done.Done();<-start;results[index].artifact,results[index].err=guardTargetArtifactFromServer(ctx,load)}(i)}
				close(start);done.Wait()
			}
			var winner *guardTargetCandidate
			losers:=0
			for _,result:=range results {
				if result.err==nil&&result.artifact!=nil {if winner!=nil{t.Fatal("same source produced two live artifact owners")};winner=result.artifact.(*guardTargetCandidate)} else {if result.artifact!=nil{t.Fatal("loser received an owning wrapper")};losers++}
			}
			if winner==nil||losers!=1||!winner.VerifySourceGeneration(){t.Fatal("duplicate claim damaged the winner")}
			defer winner.Close()
			purpose,metadata:=guardCandidatePurposeFixture(t,ctx,winner,"normal")
			if result:=inspectGuardTargetMetadata(ctx,winner,purpose);!result.Qualified||metadata.opens!=1||metadata.leafCloses!=1{t.Fatal("one retained owner lost metadata admission",result.Reason)}
			guardCandidateOwnedClosed(t,source)
			if third,err:=guardTargetArtifactFromServer(ctx,load);third!=nil||err==nil{t.Fatal("closed claimed source was issued again")}
		})
	}
}

func TestGuardTargetCandidateClaimFailureNeverClosesExistingOwner(t *testing.T) {
	for _,mode:=range []string{"normal-duplicate","partial-duplicate","borrow-in-progress","closed-source"}{
		t.Run(mode,func(t *testing.T){
			ctx:=context.Background()
			source:=guardCandidateUnclaimedFixture()
			load:=func(context.Context)(*ghaGuardVerifiedCandidate,error){if mode=="partial-duplicate"{return source,errors.New("private loader failed after ownership move")};return source,nil}
			if mode=="closed-source" {
				_ = source.Close()
				if artifact,err:=guardTargetArtifactFromServer(ctx,load);artifact!=nil||err==nil{t.Fatal("closed source moved")}
				return
			}
			if mode=="borrow-in-progress" {
				calls:=0
				if err:=source.WithPinnedPayload(func([]byte)error{artifact,err:=guardTargetArtifactFromServer(ctx,load);if artifact!=nil||err==nil{t.Fatal("active raw borrower was moved")};calls++;return nil});err!=nil||calls!=1||!source.VerifySourceGeneration(){t.Fatal("failed claim closed the raw borrower")}
				defer source.Close()
				artifact,err:=guardTargetArtifactFromServer(ctx,load);if err!=nil||artifact==nil{t.Fatal("finished raw borrow was not claimable")};defer artifact.Close();return
			}
			first,err:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,nil})
			if err!=nil||first==nil{t.Fatal("first owner failed")};defer first.Close()
			second,err:=guardTargetArtifactFromServer(ctx,load)
			if second!=nil||err==nil||!first.VerifySourceGeneration(){t.Fatal("failed duplicate closed another owner")}
			uses:=0
			if first.WithPinnedPayload(func([]byte)error{uses++;return nil})!=nil||uses!=1{t.Fatal("failed duplicate altered the held payload")}
		})
	}
}

func TestGuardTargetCandidatePartialErrorHasOneCleanupOwner(t *testing.T) {
	for _,mode:=range []string{"primary","cancelled","cleanup","cancel-cleanup","invalid-generation"}{
		t.Run(mode,func(t *testing.T){
			ctx:=context.Background()
			source:=guardCandidateUnclaimedFixture()
			if mode=="invalid-generation"{source.generation.ArtifactID=0}
			primary:=error(errors.New("private upstream acquisition error"))
			if mode=="cancelled"||mode=="cancel-cleanup"{primary=errors.Join(primary,context.Canceled)}
			if mode=="cleanup"||mode=="cancel-cleanup"{primary=errors.Join(primary,errGuardTargetCleanup)}
			value,err:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,primary})
			if value==nil||err==nil||value.VerifySourceGeneration(){t.Fatal("partial result lost cleanup ownership or became usable")}
			if errors.Is(primary,context.Canceled)!=errors.Is(err,context.Canceled)||errors.Is(primary,errGuardTargetCleanup)!=errors.Is(err,errGuardTargetCleanup){t.Fatal("partial compound facts lost")}
			if other,e:=guardTargetArtifactFromServer(ctx,func(context.Context)(*ghaGuardVerifiedCandidate,error){return source,nil});other!=nil||e==nil{t.Fatal("partial resource acquired twice")}
			artifact:=value.(*guardTargetCandidate)
			owned:=artifact.candidate
			if purpose,e:=guardTargetNormalPurposeForArtifact(ctx,value);purpose!=nil||e==nil{t.Fatal("partial owner issued a target purpose")}
			if artifact.Close()!=nil||artifact.Close()!=nil{t.Fatal("idempotent wrapper cleanup failed")}
			if owned.WaitClosed(ctx)!=nil||source.Close()==nil{t.Fatal("old raw alias regained cleanup authority")}
			guardCandidateOwnedClosed(t,source)
		})
	}
}

func TestGuardTargetCandidateClaimDisablesOldAliasesAndOwnedCopies(t *testing.T) {
	ctx:=context.Background()
	artifact,source:=guardCandidateOwnedFixture(t,ctx)
	owned:=artifact.candidate
	if source.VerifySourceGeneration(){t.Fatal("old alias verification succeeded")}
	if _,ok:=source.Generation();ok{t.Fatal("old alias exposed generation")}
	calls:=0
	if source.WithPinnedPayload(func([]byte)error{calls++;return nil})==nil||calls!=0{t.Fatal("old alias borrowed after move")}
	if source.Close()==nil||source.WaitClosed(ctx)==nil||!artifact.VerifySourceGeneration(){t.Fatal("old alias invalidated or impersonated owner")}
	copy:=*owned
	if copy.VerifySourceGeneration(){t.Fatal("copied owned wrapper verified")}
	if _,ok:=copy.Generation();ok{t.Fatal("copied owned wrapper exposed generation")}
	if copy.WithPinnedPayload(func([]byte)error{calls++;return nil})==nil||copy.Close()==nil||copy.WaitClosed(ctx)==nil||calls!=0{t.Fatal("copied owned wrapper operated on source")}
	if !artifact.VerifySourceGeneration(){t.Fatal("rejected copy damaged actual owner")}
	if artifact.Close()!=nil||owned.WaitClosed(ctx)!=nil{t.Fatal("actual owner did not close")}
	guardCandidateOwnedClosed(t,source)
}

func TestGuardTargetCandidateActiveCloseWaitsForSameOwnedBorrow(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
	artifact,source:=guardCandidateOwnedFixture(t,ctx)
	owned:=artifact.candidate
	started,release:=make(chan struct{}),make(chan struct{})
	result:=make(chan error,1)
	var releaseOnce sync.Once
	releaseBorrow:=func(){releaseOnce.Do(func(){close(release)})}
	defer releaseBorrow()
	go func(){result<-artifact.WithPinnedPayload(func([]byte)error{close(started);select{case <-release:return nil;case <-ctx.Done():return ctx.Err()}})}()
	select{case <-started:case <-ctx.Done():t.Fatal("owned borrow did not start")}
	if !errors.Is(artifact.Close(),errGuardTargetCleanup){t.Fatal("active Close falsely completed cleanup")}
	waitCtx,stop:=context.WithCancel(context.Background());stop()
	if owned.WaitClosed(waitCtx)==nil{t.Fatal("owner drain completed while borrower retained bytes")}
	if source.Close()==nil{t.Fatal("old alias acquired Close authority")}
	releaseBorrow()
	select{case err:=<-result:if err==nil{t.Fatal("closed artifact borrow succeeded")};case <-ctx.Done():t.Fatal("owned borrow did not finish")}
	if owned.WaitClosed(ctx)!=nil{t.Fatal("same owned borrower did not drain")}
	if !errors.Is(artifact.Close(),errGuardTargetCleanup){t.Fatal("second Close erased incomplete-cleanup fact")}
	guardCandidateOwnedClosed(t,source)
}
