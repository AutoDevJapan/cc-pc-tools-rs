//go:build windows

package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestWatchdogNormalProductUnavailableBeforeAnyTargetOrResident(t *testing.T){
	r,e:=openNormalWatchdogUpdate(context.Background())
	if r!=nil||e==nil||!strings.Contains(e.Error(),"formal_artifact_not_issued"){
		t.Fatalf("normal availability changed: %v %v",r,e)
	}
}
func TestWatchdogProductMoveInvalidatesPreviousAlias(t *testing.T){
	l,a:=watchdogBuildTestFixture()
	p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e!=nil{t.Fatal(e)}
	moved,e:=transferWatchdogProduct(p)
	if e!=nil||moved==nil||moved.Artifact!=a||p.Artifact!=nil||p.lease!=nil||!p.closed{t.Fatal("move did not transfer")}
	if e=p.Close();e!=nil||a.closes!=0||l.closes!=0{t.Fatal("old alias closed new owner")}
	if again,e:=transferWatchdogProduct(p);again!=nil||e==nil{t.Fatal("reused moved product")}
	if e=moved.Close();e!=nil||a.closes!=1||l.closes!=1{t.Fatal("new owner close")}
}
func TestWatchdogCopiedSealDoesNotIssueOrCloseForeignProduct(t *testing.T){
	l,a:=watchdogBuildTestFixture()
	p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e!=nil{t.Fatal(e)}
	copied:=&watchdogBuildProduct{Source:p.Source,Artifact:p.Artifact,Identity:p.Identity,
		lease:p.lease,seal:p.seal,artifactPath:p.artifactPath}
	r,e:=openWatchdogUpdateFromProduct(context.Background(),copied)
	if e==nil{t.Fatal("copied product accepted")}
	if r!=nil{_ = r.close()}
	if a.closes!=0||l.closes!=0{t.Fatal("rejected copy closed foreign product")}
	_ = p.Close()
}
func TestWatchdogCancelledOwnedProductIsClosedOnlyByNewOwner(t *testing.T){
	l,a:=watchdogBuildTestFixture()
	p,e:=produceWatchdogBuild(context.Background(),func(context.Context)(watchdogBuildLease,error){return l,nil},"held-output.exe")
	if e!=nil{t.Fatal(e)}
	ctx,cancel:=context.WithCancel(context.Background());cancel()
	r,e:=openWatchdogUpdateFromProduct(ctx,p)
	if !errors.Is(e,context.Canceled)||r==nil{t.Fatal("cancel cause")}
	if e=r.close();e!=nil||a.closes!=1||l.closes!=1{t.Fatal("cancel owned cleanup")}
}
