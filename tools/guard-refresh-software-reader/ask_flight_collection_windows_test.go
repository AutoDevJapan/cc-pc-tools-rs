//go:build windows

package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "strings"
    "syscall"
    "testing"
    "time"
)

// This helper runs only as an owned child of the same test executable.
// The descendant is either waited by the root or terminated by its Job.
func TestAskStrictJobOwnedHelper(t *testing.T) {
    if os.Getenv("ASK_STRICT_JOB_HELPER")!="1" {return}
    role:=os.Getenv("ASK_STRICT_JOB_ROLE")
    if role=="short" {os.Exit(0)}
    if role=="long" {time.Sleep(20*time.Second);os.Exit(0)}
    if !askWorkerParent(){os.Exit(41)}
    mode:=os.Getenv("ASK_STRICT_JOB_MODE")
    childPID:=0
    shortExited:=false
    if mode=="long"||mode=="short" {
        child:=exec.Command(os.Args[0],"-test.run=^TestAskStrictJobOwnedHelper$")
        child.SysProcAttr=&syscall.SysProcAttr{HideWindow:true,CreationFlags:0x08000000}
        env:=make([]string,0,len(os.Environ())+1)
        for _,item:=range os.Environ(){if !strings.HasPrefix(strings.ToUpper(item),"ASK_STRICT_JOB_ROLE="){env=append(env,item)}}
        child.Env=append(env,"ASK_STRICT_JOB_ROLE="+mode)
        if child.Start()!=nil {os.Exit(42)}
        childPID=child.Process.Pid
        if mode=="short" {
            if child.Wait()!=nil {os.Exit(43)}
            shortExited=true
        }
    }
    if json.NewEncoder(os.Stdout).Encode(struct{PID int `json:"pid"`;ShortExited bool `json:"short_exited"`}{childPID,shortExited})!=nil {os.Exit(44)}
    line,err:=bufio.NewReader(os.Stdin).ReadString('\n')
    if err!=nil||line!="ACCEPT\n"{os.Exit(45)}
    // Keep the root alive long enough for a failed first list query to be
    // followed by successful native list queries.
    time.Sleep(500*time.Millisecond)
    fmt.Fprintln(os.Stdout,"owned")
    os.Exit(0)
}

func TestAskStrictOwnedJobCollection(t *testing.T) {
    cases:=[]struct{
        name,mode string
        fault askStrictJobFaults
        wantReaped bool
    }{
        {"normal","normal",askStrictJobFaults{captureDiagnostics:true},true},
        {"known-child","long",askStrictJobFaults{captureDiagnostics:true},true},
        {"query-failure-then-zero","normal",askStrictJobFaults{failListOnce:true},false},
        {"partial-list-then-zero","normal",askStrictJobFaults{partialListOnce:true},false},
        {"member-open-failure","long",askStrictJobFaults{failMemberOpenOnce:true},false},
        {"member-identity-failure","long",askStrictJobFaults{failMembershipOnce:true},false},
        {"short-unobserved-total","short",askStrictJobFaults{omitNonRoot:true},false},
        {"signal-not-confirmed","normal",askStrictJobFaults{failWaitOnce:true},false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        t.Setenv("ASK_STRICT_JOB_HELPER","1")
        t.Setenv("ASK_STRICT_JOB_ROLE","root")
        t.Setenv("ASK_STRICT_JOB_MODE",tc.mode)
        ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
        defer cancel()
        ctx=context.WithValue(ctx,askStrictJobKey{},true)
        faults:=tc.fault
        ctx=context.WithValue(ctx,askStrictJobFaultKey{},&faults)
        var ready struct{PID int `json:"pid"`;ShortExited bool `json:"short_exited"`}
        var childHandle syscall.Handle
        var childConfirmed bool
        var readyErr error
        result:=askOwnedChildWithHandshake(ctx,os.Args[0],func(raw []byte)bool{
            if json.Unmarshal(raw,&ready)!=nil {readyErr=fmt.Errorf("invalid owned ready");return false}
            if tc.mode=="short"&&!ready.ShortExited {readyErr=fmt.Errorf("short child did not exit");return false}
            if tc.mode=="long" {
                if ready.PID<=0 {readyErr=fmt.Errorf("missing owned child");return false}
                var err error
                childHandle,err=syscall.OpenProcess(syscall.SYNCHRONIZE,false,uint32(ready.PID))
                if err!=nil {readyErr=fmt.Errorf("owned child handle unavailable");return false}
            }
            return true
        },"-test.run=^TestAskStrictJobOwnedHelper$")
        defer clear(result.Raw)
        if tc.name=="normal" {
            t.Logf("root generation diagnostic: total=%d held=%d signalled=%d root_creation_nonzero=%t active=%d zero=%t final_query=%t list_calls=%d shown=%d held_omitted=%d omitted_overflow=%t limit=%d members=%+v",faults.finalTotal,faults.finalHeld,faults.finalSignalled,faults.finalRootCreation!=0,faults.finalActive,faults.zeroObserved,faults.finalQueryOK,faults.listCalls,len(faults.finalMembers),faults.finalOmitted,faults.diagnosticOmittedOverflow,askStrictJobDiagnosticLimit,faults.finalMembers)
        }
        if childHandle!=0 {
            state,err:=syscall.WaitForSingleObject(childHandle,2000)
            closeErr:=syscall.CloseHandle(childHandle)
            if err!=nil||state!=syscall.WAIT_OBJECT_0||closeErr!=nil {t.Fatal("owned descendant not confirmed terminated")}
            childConfirmed=true
        }
        if readyErr!=nil||!result.Started||!result.Assigned||!result.WaitVerified||result.TimedOut||result.Exit!=0 {
            t.Fatalf("owned root setup/wait failed: %v %+v",readyErr,result)
        }
        if result.Reaped!=tc.wantReaped {t.Fatalf("unexpected strict collection: %+v",result)}
        if !tc.wantReaped {
            fake:=result
            fake.Raw,_=json.Marshal(ghaFreshWorkerResult{guardTargetMetadataResult:ghaFreshTestInspection(),CredentialCleanupState:"completed"})
            if _,ok:=ghaFreshDecodeInspection(fake);ok {t.Fatal("unproved native collection accepted by decoder")}
            clear(fake.Raw)
        }
        if !faults.zeroObserved||!faults.finalQueryOK||faults.finalActive!=0 {t.Fatal("native final active-zero was not observed")}
        switch tc.name {
        case "normal":
            if !askStrictNormalFixtureEvidence(faults) {t.Fatal("root generation and complete collection not confirmed")}
        case "known-child":
            if ready.PID<=0||!childConfirmed||!askStrictNormalFixtureEvidence(faults) {t.Fatal("known owned child was not fully collected")}
            pinned:=false
            for _,member:=range faults.finalMembers {
                if member.PID==uint32(ready.PID)&&!member.Root&&member.CreationNonZero {pinned=true;break}
            }
            if !pinned {t.Fatal("known child generation not present in bounded job evidence")}
        case "query-failure-then-zero":
            if !faults.listFaultSeen||faults.listCalls<2||faults.finalTotal<1 {t.Fatal("sticky query fault not exercised")}
        case "partial-list-then-zero":
            if !faults.partialSeen||faults.listCalls<2||faults.finalTotal<1 {t.Fatal("partial list fault not exercised")}
        case "member-open-failure":
            if !faults.openFaultSeen||ready.PID<=0 {t.Fatal("member open fault not exercised")}
        case "member-identity-failure":
            if !faults.membershipFaultSeen||ready.PID<=0 {t.Fatal("membership fault not exercised")}
        case "short-unobserved-total":
            if !ready.ShortExited||faults.finalTotal<2||faults.finalHeld!=1||faults.finalSignalled!=1 {t.Fatal("unobserved lifetime count not exercised")}
        case "signal-not-confirmed":
            if !faults.waitFaultSeen||faults.finalTotal<1||faults.finalSignalled>=faults.finalHeld {t.Fatal("unsignalled member not exercised")}
        }
    })}
}

func TestAskStrictJobRejectsUnboundRootBeforeResume(t *testing.T) {
    for _,tc:=range []struct{name string;fault askStrictJobFaults}{
        {"creation",askStrictJobFaults{failRootIdentityOnce:true}},
        {"membership",askStrictJobFaults{failRootMembershipOnce:true}},
    } {t.Run(tc.name,func(t *testing.T){
        t.Setenv("ASK_STRICT_JOB_HELPER","1")
        t.Setenv("ASK_STRICT_JOB_ROLE","root")
        t.Setenv("ASK_STRICT_JOB_MODE","normal")
        ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second)
        defer cancel()
        ctx=context.WithValue(ctx,askStrictJobKey{},true)
        faults:=tc.fault
        ctx=context.WithValue(ctx,askStrictJobFaultKey{},&faults)
        readyCalls:=0
        result:=askOwnedChildWithHandshake(ctx,os.Args[0],func([]byte)bool{readyCalls++;return true},"-test.run=^TestAskStrictJobOwnedHelper$")
        defer clear(result.Raw)
        if !result.Started||result.Assigned||!result.WaitVerified||result.Reaped||result.TimedOut||readyCalls!=0 {
            t.Fatalf("unbound root resumed or falsely collected: %+v ready=%d",result,readyCalls)
        }
        if tc.name=="creation"&&!faults.rootIdentityFaultSeen||tc.name=="membership"&&!faults.rootMembershipFaultSeen {
            t.Fatal("root identity negative did not execute")
        }
    })}
}

func TestAskStrictJobDiagnosticsAreBounded(t *testing.T) {
    faults:=&askStrictJobFaults{captureDiagnostics:true}
    collector:=&askStrictJobCollector{root:1,faults:faults}
    for i:=0;i<askStrictJobDiagnosticLimit+7;i++ {
        collector.recordDiagnostic(nil,uint32(i+1),askStrictJobMember{created:1})
    }
    if len(faults.finalMembers)!=askStrictJobDiagnosticLimit||faults.finalOmitted!=7||
        faults.diagnosticOmittedOverflow||!faults.finalMembers[0].Root||
        faults.finalMembers[0].ImagePathClass!="unavailable"||collector.unknown||len(collector.members)!=0 {
        t.Fatal("diagnostic bound or separation failed")
    }
    faults.finalOmitted=int(^uint(0)>>1)
    collector.recordDiagnostic(nil,999,askStrictJobMember{created:1})
    if !faults.diagnosticOmittedOverflow||faults.finalOmitted!=int(^uint(0)>>1){t.Fatal("omitted count overflow was not recorded")}
    disabled:=&askStrictJobFaults{}
    collector.faults=disabled
    collector.recordDiagnostic(nil,1,askStrictJobMember{created:1})
    if len(disabled.finalMembers)!=0||disabled.finalOmitted!=0||disabled.diagnosticOmittedOverflow{t.Fatal("disabled diagnostic recorded data")}
}

// An owned Job can contain multiple processes; each must be pinned and signalled.
func askStrictNormalFixtureEvidence(f askStrictJobFaults) bool {
    return f.finalTotal>0&&uint64(f.finalTotal)==uint64(f.finalHeld)&&f.finalHeld==f.finalSignalled&&f.finalRootCreation!=0
}

func TestAskStrictNormalFixtureEvidence(t *testing.T) {
    cases:=[]struct{name string;total uint32;held,signalled int;root uint64;want bool}{
        {"one",1,1,1,1,true},
        {"two",2,2,2,1,true},
        {"zero-total",0,0,0,1,false},
        {"missing-held",2,1,1,1,false},
        {"missing-signal",2,2,1,1,false},
        {"extra-held",1,2,2,1,false},
        {"extra-signal",1,1,2,1,false},
        {"missing-root",2,2,2,0,false},
        {"negative-held",1,-1,-1,1,false},
    }
    for _,tc:=range cases {t.Run(tc.name,func(t *testing.T){
        got:=askStrictNormalFixtureEvidence(askStrictJobFaults{finalTotal:tc.total,finalHeld:tc.held,finalSignalled:tc.signalled,finalRootCreation:tc.root})
        if got!=tc.want {t.Fatalf("fixture evidence: got %t want %t",got,tc.want)}
    })}
}
