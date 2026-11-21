//go:build windows

package main

import (
	"context"
	"errors"
	"encoding/json"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func watchdogTaskFixtureJSON()map[string]any{
	definition:=watchdogTaskFixtureXML()
	digest:=sha256.Sum256([]byte(definition))
	return map[string]any{
		"name":watchdogTask,"xml_sha256":hex.EncodeToString(digest[:]),"definition_xml":definition,"principal":"S-1-5-21-1",
		"s4u":true,"limited":true,"executable":watchdogLauncher,
		"arguments":"--launch-service opencodex_watchdog","working":"D:/CC/Tools/resident-guard",
		"resident_disabled":true,"credit_disabled":true,
		"instances":[]any{map[string]any{"guid":"{4AB9BD54-AC1C-494C-9B08-9EE212A74F3E}","pid":5312,"path":"\\CC-OcxHealthWatchdog","action":watchdogLauncher,"state":4}},
	}
}
func TestWatchdogFixedTaskDecoderRejectsChangedDefinitionsAndInstances(t *testing.T){
	for _,kind:=range []string{"normal","s4u","action","principal","xml","disabled","instance-pid","instance-guid","instance-path"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogTaskFixtureJSON()
			switch kind{
			case "s4u":v["s4u"]=false
			case "action":v["arguments"]="install"
			case "principal":v["principal"]=""
			case "xml":v["xml_sha256"]=""
			case "disabled":v["resident_disabled"]=false
			default:
				if strings.HasPrefix(kind,"instance-"){
					entry:=v["instances"].([]any)[0].(map[string]any)
					if kind=="instance-pid"{entry["pid"]=0}
					if kind=="instance-guid"{entry["guid"]="foreign"}
					if kind=="instance-path"{entry["path"]="\\other"}
				}
			}
			raw,_:=json.Marshal(v)
			if _,e:=decodeWatchdogTask(raw);(e==nil)!=(kind=="normal"){t.Fatal("task decoder")}
		})
	}
}
func TestWatchdogInstanceGUIDIsStrictlyFiniteData(t *testing.T){
	for _,s:=range []string{"","'; Stop-Process","{4AB9BD54-AC1C-494C-9B08-9EE212A74F3Z}","{4AB9BD54AC1C494C9B089EE212A74F3E}"}{
		if watchdogInstanceGUID(s){t.Fatal("unbound selector")}
	}
	if !watchdogInstanceGUID("{4AB9BD54-AC1C-494C-9B08-9EE212A74F3E}"){t.Fatal("valid GUID")}
}


func watchdogTaskFixtureXML()string{
	return `<Task xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task"><Principals><Principal id="Author"><UserId>S-1-5-21-1</UserId><LogonType>S4U</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals><Actions Context="Author"><Exec><Command>`+watchdogLauncher+`</Command><Arguments>--launch-service opencodex_watchdog</Arguments><WorkingDirectory>D:/CC/Tools/resident-guard</WorkingDirectory></Exec></Actions></Task>`
}
func TestWatchdogTaskDefinitionBindsExactActionAndPrincipalContext(t *testing.T){
	for _,kind:=range []string{"normal","utf16-declaration","extra-comhandler","extra-exec","missing-action","wrong-context","missing-context","extra-principal","missing-id","foreign-namespace","digest","projection"}{
		t.Run(kind,func(t *testing.T){
			v:=watchdogTaskFixtureJSON();x:=v["definition_xml"].(string)
			switch kind{
			case "utf16-declaration":x=`<?xml version="1.0" encoding="UTF-16"?>`+x
			case "extra-comhandler":x=strings.Replace(x,"</Actions>","<ComHandler><ClassId>foreign</ClassId></ComHandler></Actions>",1)
			case "extra-exec":x=strings.Replace(x,"</Actions>","<Exec><Command>other</Command></Exec></Actions>",1)
			case "missing-action":i:=strings.Index(x,"<Exec>");j:=strings.Index(x,"</Exec>")+len("</Exec>");x=x[:i]+x[j:]
			case "wrong-context":x=strings.Replace(x,`Context="Author"`,`Context="Other"`,1)
			case "missing-context":x=strings.Replace(x,` Context="Author"`,"",1)
			case "extra-principal":x=strings.Replace(x,"</Principals>",`<Principal id="Other"><UserId>S-1-5-21-2</UserId></Principal></Principals>`,1)
			case "missing-id":x=strings.Replace(x,` id="Author"`,"",1)
			case "foreign-namespace":x=strings.Replace(x,"<Exec>",`<Exec xmlns="urn:foreign">`,1)
			case "projection":v["principal"]="foreign"
			}
			digest:=sha256.Sum256([]byte(x));v["definition_xml"]=x;v["xml_sha256"]=hex.EncodeToString(digest[:])
			if kind=="digest"{v["xml_sha256"]=strings.Repeat("b",64)}
			raw,_:=json.Marshal(v)
			if _,e:=decodeWatchdogTask(raw);(e==nil)!=(kind=="normal"||kind=="utf16-declaration"){t.Fatalf("action/context verification: %v",e)}
		})
	}
}
func TestWatchdogTaskStopRechecksSameXMLImmediatelyBeforeStop(t *testing.T){
	i:=strings.Index(watchdogTaskScript,"if([string]$t.Xml -cne $xml)")
	stop:=strings.Index(watchdogTaskScript,"$found[0].Stop()")
	if i<0||stop<i||strings.Contains(watchdogTaskScript[i:stop],"GetTask("){t.Fatal("missing same-definition stop recheck")}
}

// The observations below exercise the same closed topology builder and
// pre-stop snapshot predicate as the native adapter, without opening a process.
func watchdogChainFixture(count int)(watchdogTaskInstance,[]watchdogProcessEntry,map[uint32]*watchdogProcess){
	instance:=watchdogTaskInstance{PID:5312}
	rows:=make([]watchdogProcessEntry,0,count)
	processes:=map[uint32]*watchdogProcess{}
	for depth:=0;depth<count;depth++{
		pid:=uint32(5312+depth);parent:=uint32(4300);if depth>0{parent=pid-1}
		image:=watchdogLauncher;if depth==count-1{image=watchdogTarget}
		rows=append(rows,watchdogProcessEntry{PID:pid,Parent:parent})
		processes[pid]=&watchdogProcess{pid:pid,parent:parent,created:uint64(100+depth),image:image}
	}
	return instance,rows,processes
}
func TestWatchdogNormalDirectAndReducedChainsRemainClosed(t *testing.T){
	for _,count:=range []int{2,3}{
		for _,kind:=range []string{"normal","foreign-image","foreign-sid","parent","birth","zero-birth","not-live","extra-child","leaf-child","missing","partial-open"}{
			t.Run(string(rune('0'+count))+"/"+kind,func(t *testing.T){
				instance,rows,processes:=watchdogChainFixture(count)
				leaf:=uint32(5312+count-1)
				switch kind{
				case "foreign-image":processes[leaf].image="D:/foreign.exe"
				case "parent":processes[leaf].parent=42
				case "birth":processes[leaf].created=99
				case "zero-birth":processes[leaf].created=0
				case "extra-child":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:instance.PID})
				case "leaf-child":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:leaf})
				case "missing":rows=rows[:len(rows)-1]
				}
				chain,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){
					p:=processes[pid];sid:="same-sid";live:=true
					if pid==leaf&&kind=="foreign-sid"{sid="foreign"}
					if pid==leaf&&kind=="not-live"{live=false}
					if pid==leaf&&kind=="partial-open"{return p,sid,live,errors.New("partial")}
					return p,sid,live,nil
				})
				good:=kind=="normal"
				if (e==nil)!=good{t.Fatalf("normal topology/refusal: count=%d kind=%s chain=%d err=%v",count,kind,len(chain),e)}
				if good&&(!watchdogChainShape(chain)||len(chain)!=count||watchdogChainRows(chain,rows)!=nil){t.Fatal("pre-stop shape disagrees")}
				if kind=="partial-open"&&(len(chain)==0||chain[len(chain)-1]!=processes[leaf]){t.Fatal("partial held process reference lost")}
			})
		}
	}
	for _,count:=range []int{1,4}{
		instance,rows,processes:=watchdogChainFixture(count)
		if _,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){return processes[pid],"same-sid",true,nil});e==nil{t.Fatalf("unsupported topology accepted: %d",count)}
	}
}
func TestWatchdogBothNormalChainsRecheckRowsBeforeStop(t *testing.T){
	for _,count:=range []int{2,3}{
		for _,kind:=range []string{"normal","changed-parent","extra-descendant","extra-sibling","wrong-shape"}{
			t.Run(string(rune('0'+count))+"/"+kind,func(t *testing.T){
				instance,rows,processes:=watchdogChainFixture(count)
				chain,e:=watchdogChainFromSnapshot(instance,"same-sid",rows,func(pid,parent uint32)(*watchdogProcess,string,bool,error){return processes[pid],"same-sid",true,nil})
				if e!=nil{t.Fatal(e)}
				switch kind{
				case "changed-parent":rows[len(rows)-1].Parent=42
				case "extra-descendant":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:chain[len(chain)-1].pid})
				case "extra-sibling":rows=append(rows,watchdogProcessEntry{PID:7000,Parent:chain[0].pid})
				case "wrong-shape":chain[len(chain)-1].image=watchdogLauncher
				}
				if (watchdogChainRows(chain,rows)==nil)!=(kind=="normal"){t.Fatal("changed rows/shape reached Stop")}
			})
		}
	}
}

type watchdogCheckFinalTaskFixture struct{
	*watchdogTestResident
	cancelAt int
	cancel context.CancelFunc
}
func(f *watchdogCheckFinalTaskFixture) Task(ctx context.Context)(watchdogTaskView,error){
	view,e:=f.watchdogTestResident.Task(ctx)
	if f.taskCalls==f.cancelAt{f.cancel()}
	return view,e
}
func TestWatchdogCheckOnlyFinalTaskCancellationCannotQualify(t *testing.T){
	for _,kind:=range []string{"normal","first-task-cancel","final-task-cancel","cancel-and-close-failure"}{
		t.Run(kind,func(t *testing.T){
			ctx,cancel:=context.WithCancel(context.Background());defer cancel()
			all,files,resident:=watchdogTestResources();at:=0
			if kind=="first-task-cancel"{at=1}else if kind!="normal"{at=2}
			if kind=="cancel-and-close-failure"{resident.fail="close"}
			all.Resident=&watchdogCheckFinalTaskFixture{watchdogTestResident:resident,cancelAt:at,cancel:cancel}
			result:=runWatchdogUpdate(ctx,false,func(context.Context)(*watchdogUpdateResources,error){return all,nil})
			good:=kind=="normal"
			if result.Qualified!=good||result.Updated||result.ObservationAccepted||result.DataAccessGranted||result.GeneralGuardChanged||len(resident.calls)!=0||strings.Join(files.calls,",")!="check"{
				t.Fatalf("check-only cancellation/mutation: %+v files=%v resident=%v",result,files.calls,resident.calls)
			}
			if !good&&(result.Reason!="watchdog_check_final_recheck_failed"||ctx.Err()!=context.Canceled||result.TaskUnchanged){t.Fatal("final cancellation became success",result)}
			if files.closeCount!=1||resident.closeCount!=1{t.Fatal("acquired resources not closed once")}
			if result.CleanupFailed!=(kind=="cancel-and-close-failure"){t.Fatal("primary/cleanup facts mixed",result)}
		})
	}
}
