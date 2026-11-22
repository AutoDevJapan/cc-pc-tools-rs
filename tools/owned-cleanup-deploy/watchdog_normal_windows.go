//go:build windows

package main

import (
	"context"
	"errors"
	"syscall"
	"unsafe"
)

type watchdogProductFiles struct{
	product *watchdogBuildProduct
	originEvidence *watchdogOriginEvidence
	deferred *watchdogDeferredNative
	pins *watchdogPinScope
	targetBaseline *pin
	origin watchdogOrigin
	purpose watchdogTargetPurpose
	retainedDescriptors []unsafe.Pointer
	closed bool
	closeErr error
}

// There is no installed product-cache reader or caller receipt fallback.
// The normal producer must issue this same live object from its one build.
func openNormalWatchdogArtifact(context.Context)(*watchdogBuildProduct,error){
	return nil,errors.New("watchdog_formal_artifact_not_issued")
}

// Admission is private, generation-bound and one-shot. Source classification
// or original-record agreement alone cannot construct the product seal.
func openWatchdogUpdateFromProduct(ctx context.Context,product *watchdogBuildProduct)(resources *watchdogUpdateResources,err error){
	files:=&watchdogProductFiles{pins:newWatchdogPinScope()}
	resources=&watchdogUpdateResources{Files:files}
	if ctx==nil{
		return resources,errors.New("watchdog_formal_product_not_live")
	}
	product,err=transferWatchdogProduct(product)
	if err!=nil{return resources,err}
	files.product=product
	if err=ctx.Err();err!=nil{return resources,err}
	// There is no fallback if the admitted normal artifact path is not source
	// qualified. Opening target metadata is later than every producer check.
	if !same(product.artifactPath,watchdogFormalArtifactPath){
		return resources,errors.New("watchdog_formal_artifact_path_unbound")
	}
	if err=watchdogQualify(ctx,product.artifactPath);err!=nil{return resources,err}
	if err=product.lease.Recheck(ctx);err!=nil{return resources,err}
	actual,err:=product.Artifact.Identity(ctx)
	if err!=nil||actual!=product.Identity{return resources,errors.New("watchdog_formal_artifact_changed")}
	files.originEvidence,err=openWatchdogOriginEvidence(ctx)
	if err!=nil{return resources,err}
	// The historical validation record is not the raw build/event provenance.
	// A normal target-purpose issuer must establish that independent link. It
	// currently remains unavailable, before any installed target is opened.
	files.purpose,err=openNormalWatchdogPurpose(ctx,files.originEvidence,product)
	if err!=nil||files.purpose==nil{return resources,errors.Join(errors.New("watchdog_original_build_purpose_unavailable"),err)}
	if err=files.purpose.Recheck(ctx,product);err!=nil{return resources,err}
	if err=watchdogQualify(ctx,watchdogTarget);err!=nil{return resources,err}
	if _,err=files.pins.parents(watchdogTarget);err!=nil{return resources,err}
	files.targetBaseline,err=files.pins.open(watchdogTarget,syscall.GENERIC_READ|0x20000,5,3)
	if err!=nil{return resources,err}
	id,err:=watchdogIdentityHeld(files.targetBaseline,&files.retainedDescriptors)
	if err!=nil||!watchdogRecognizedPredecessor(id){return resources,errors.New("watchdog_current_origin_mismatch")}
	files.origin,err=files.purpose.BindCurrent(ctx,id)
	if err!=nil||!files.origin.valid()||files.origin.Installed!=id||files.origin.Target!=watchdogTarget||
		files.origin.SourceCommit!=watchdogOldSource{return resources,errors.New("watchdog_current_purpose_unbound")}
	files.deferred,err=newWatchdogDeferredNative(files.origin,product.Identity,product.artifactPath)
	if err!=nil{return resources,err}
	resident,err:=openWatchdogResident(ctx,files.Candidate)
	resources.Resident=resident
	if err!=nil{return resources,err}
	resources.Origin=files.origin
	resources.Artifact=product.Identity
	resources.Recheck=func(c context.Context)error{
		if c.Err()!=nil||product.closed||product.seal==nil||product.seal.owner!=product{
			return errors.New("watchdog_issued_product_expired")
		}
		if e:=product.lease.Recheck(c);e!=nil{return e}
		if e:=files.originEvidence.Recheck(c);e!=nil{return e}
		current,e:=product.Artifact.Identity(c)
		if e!=nil||current!=product.Identity{return errors.New("watchdog_issued_artifact_changed")}
		return files.purpose.Recheck(c,product)
	}
	return resources,nil
}

const watchdogFormalArtifactPath = "D:/CC/Build/ocx-watchdog-formal-pr773/target/release/ocx_health_watchdog.exe"

func transferWatchdogProduct(p *watchdogBuildProduct)(*watchdogBuildProduct,error){
	if p==nil{return nil,errors.New("watchdog_formal_product_not_live")}
	p.claimMu.Lock();defer p.claimMu.Unlock()
	if p.closed||p.claimed||p.seal==nil||p.seal.owner!=p||p.lease==nil||p.Artifact==nil||
		!p.Identity.valid()||!p.Source.valid()||p.artifactPath==""{
		return nil,errors.New("watchdog_formal_product_not_live")
	}
	owned:=&watchdogBuildProduct{Source:p.Source,Artifact:p.Artifact,Identity:p.Identity,
		CargoJSONSHA256:p.CargoJSONSHA256,lease:p.lease,artifactPath:p.artifactPath,claimed:true}
	owned.seal=&watchdogBuildSeal{owner:owned}
	// Invalidate the former producer alias before handing ownership out.
	p.Artifact=nil;p.lease=nil;p.seal=nil;p.claimed=true;p.closed=true
	return owned,nil
}

// No guessed current hash, saved summary true or another tool's Guard purpose
// can stand in for this original independent build-to-install generation.
type watchdogTargetPurpose interface{
	BindCurrent(context.Context,watchdogFileIdentity)(watchdogOrigin,error)
	Recheck(context.Context,*watchdogBuildProduct)error
	Close()error
}
func openNormalWatchdogPurpose(context.Context,*watchdogOriginEvidence,*watchdogBuildProduct)(watchdogTargetPurpose,error){
	return nil,errors.New("watchdog_original_build_purpose_unavailable")
}

func (f *watchdogProductFiles) Check(ctx context.Context)error{
	if f.deferred==nil{return errors.New("watchdog_files_not_issued")}
	return f.deferred.Check(ctx)
}
func (f *watchdogProductFiles) Stage(ctx context.Context)error{
	if f.deferred==nil{return errors.New("watchdog_files_not_issued")}
	return f.deferred.Stage(ctx)
}
func (f *watchdogProductFiles) Publish(ctx context.Context)error{
	if f.deferred==nil{return errors.New("watchdog_files_not_issued")}
	return f.deferred.Publish(ctx)
}
func (f *watchdogProductFiles) Rollback(ctx context.Context)error{
	if f.deferred==nil{return errors.New("watchdog_files_not_issued")}
	return f.deferred.Rollback(ctx)
}
func (f *watchdogProductFiles) Candidate()watchdogFileIdentity{
	if f.deferred==nil{return watchdogFileIdentity{}}
	return f.deferred.Candidate()
}
func (f *watchdogProductFiles) Steps()watchdogSteps{
	if f.deferred==nil{return watchdogSteps{}}
	return f.deferred.Steps()
}
func (f *watchdogProductFiles) Close()error{
	if f.closed{return f.closeErr};f.closed=true
	var es []error
	if f.deferred!=nil{es=append(es,f.deferred.Close())}
	if f.pins!=nil{es=append(es,f.pins.Close())}
	if len(f.retainedDescriptors)!=0{es=append(es,errors.New("watchdog_security_release_failed"))}
	if f.purpose!=nil{es=append(es,f.purpose.Close())}
	if f.originEvidence!=nil{es=append(es,f.originEvidence.Close())}
	if f.product!=nil{es=append(es,f.product.Close())}
	f.closeErr=errors.Join(es...)
	return f.closeErr
}
