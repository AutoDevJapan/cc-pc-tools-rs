//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"strings"
	"syscall"
	"time"
)

// This command only reads the fixed registered task or stops one previously
// observed instance GUID. It never registers, edits, enables or disables tasks.
// Stop is implemented for a later reviewed apply; no caller CLI chooses a task.
const watchdogTaskScript = `
$ErrorActionPreference='Stop'
[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false)
$s=New-Object -ComObject 'Schedule.Service';$s.Connect()
$t=$s.GetFolder('\').GetTask('CC-OcxHealthWatchdog')
$xml=[string]$t.Xml
[xml]$d=$xml
$n=New-Object System.Xml.XmlNamespaceManager($d.NameTable)
$n.AddNamespace('t','http://schemas.microsoft.com/windows/2004/02/mit/task')
function V($p){$x=$d.SelectSingleNode($p,$n);if($null -eq $x){return ''};return [string]$x.InnerText}
$groups=$d.SelectNodes('/t:Task/t:Actions',$n)
$principals=$d.SelectNodes('/t:Task/t:Principals/t:Principal',$n)
$actions=$d.SelectNodes('/t:Task/t:Actions/*',$n)
if($groups.Count -ne 1 -or $principals.Count -ne 1 -or $actions.Count -ne 1){throw 'task_actions_unverified'}
$principalId=[string]$principals[0].GetAttribute('id')
if($actions[0].LocalName -cne 'Exec' -or $actions[0].NamespaceURI -cne 'http://schemas.microsoft.com/windows/2004/02/mit/task' -or
[string]::IsNullOrEmpty($principalId) -or $groups[0].GetAttribute('Context') -cne $principalId){throw 'task_context_unverified'}
$principal=V '/t:Task/t:Principals/t:Principal/t:UserId'
if(-not $t.Enabled -or
(V '/t:Task/t:Principals/t:Principal/t:LogonType') -ne 'S4U' -or
(V '/t:Task/t:Principals/t:Principal/t:RunLevel') -ne 'LeastPrivilege' -or
(V '/t:Task/t:Settings/t:MultipleInstancesPolicy') -ne 'IgnoreNew' -or
(V '/t:Task/t:Settings/t:ExecutionTimeLimit') -ne 'PT0S'){throw 'task_definition_unverified'}
$hash=[Security.Cryptography.SHA256]::Create()
try{$digest=[BitConverter]::ToString($hash.ComputeHash([Text.Encoding]::UTF8.GetBytes($xml))).Replace('-','').ToLowerInvariant()}finally{$hash.Dispose()}
$rows=@()
foreach($r in $t.GetInstances(0)){
 $rows+= [ordered]@{guid=[string]$r.InstanceGuid;pid=[uint32]$r.EnginePID;path=[string]$r.Path;action=[string]$r.CurrentAction;state=[int]$r.State}
}
$mode=$env:WATCHDOG_FIXED_TASK_MODE
if($mode -eq 'stop'){
 $guid=$env:WATCHDOG_FIXED_INSTANCE
 if($guid -notmatch '^\{[0-9A-Fa-f-]{36}\}$'){throw 'instance_invalid'}
 $found=@($t.GetInstances(0)|Where-Object{[string]$_.InstanceGuid -eq $guid})
 if($found.Count -ne 1){throw 'instance_changed'}
 if($digest -ne $env:WATCHDOG_FIXED_XML -or [string]$found[0].EnginePID -ne $env:WATCHDOG_FIXED_ENGINE -or
    [string]$found[0].CurrentAction -ne 'D:\CC\Tools\resident-guard\resident-guard.exe'){throw 'instance_generation_changed'}
# Re-fetch the definition on the exact registered task immediately before Stop.
 if([string]$t.Xml -cne $xml){throw 'task_definition_changed_before_stop'}
 $found[0].Stop()
}elseif($mode -ne 'read'){throw 'mode_invalid'}
[ordered]@{name='CC-OcxHealthWatchdog';xml_sha256=$digest;definition_xml=$xml;principal=$principal;
s4u=$true;limited=$true;executable=(V '/t:Task/t:Actions/t:Exec/t:Command');
arguments=(V '/t:Task/t:Actions/t:Exec/t:Arguments');working=(V '/t:Task/t:Actions/t:Exec/t:WorkingDirectory');
resident_disabled=(-not $s.GetFolder('\').GetTask('CC-ResidentGuard').Enabled);
credit_disabled=(-not $s.GetFolder('\').GetTask('OcxResetCreditWatchdog').Enabled);instances=$rows}|ConvertTo-Json -Depth 5 -Compress
`

type watchdogTaskInstance struct{
	GUID string `json:"guid"`
	PID uint32 `json:"pid"`
	Path string `json:"path"`
	Action string `json:"action"`
	State int `json:"state"`
}
type watchdogTaskSnapshot struct{
	View watchdogTaskView
	Principal string
	Instances []watchdogTaskInstance
}

type watchdogLimitedOutput struct{
	bytes.Buffer
	mu sync.Mutex
	overflow bool
}
func (b *watchdogLimitedOutput) Write(p []byte)(int,error){
	b.mu.Lock();defer b.mu.Unlock()
	n:=len(p)
	if b.Len()+n>128<<10{b.overflow=true;return n,nil}
	_,_ = b.Buffer.Write(p);return n,nil
}

func readWatchdogTask(ctx context.Context,mode,guid,expectedXML string,engine uint32)(watchdogTaskSnapshot,error){
	if ctx==nil||ctx.Err()!=nil||(mode!="read"&&mode!="stop"){
		return watchdogTaskSnapshot{},errors.New("watchdog_task_context")
	}
	if mode=="stop"&&(!watchdogInstanceGUID(guid)||!watchdogHex(expectedXML,64)||engine==0){return watchdogTaskSnapshot{},errors.New("watchdog_instance_unbound")}
	powershell:=filepath.Join(os.Getenv("SystemRoot"),"System32/WindowsPowerShell/v1.0/powershell.exe")
	if !filepath.IsAbs(powershell){return watchdogTaskSnapshot{},errors.New("watchdog_systemroot_unavailable")}
	if err:=watchdogQualify(ctx,powershell);err!=nil{return watchdogTaskSnapshot{},err}
	c,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
	words:=syscall.StringToUTF16(watchdogTaskScript)
	raw:=make([]byte,(len(words)-1)*2)
	for i,w:=range words[:len(words)-1]{raw[i*2]=byte(w);raw[i*2+1]=byte(w>>8)}
	cmd:=exec.CommandContext(c,powershell,"-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(raw))
	cmd.SysProcAttr=&syscall.SysProcAttr{HideWindow:true,CreationFlags:0x08000000};cmd.WaitDelay=time.Second
	env:=[]string{}
	for _,v:=range os.Environ(){key:=strings.ToUpper(strings.SplitN(v,"=",2)[0]);if !strings.HasPrefix(key,"WATCHDOG_FIXED_"){env=append(env,v)}}
	cmd.Env=append(env,"WATCHDOG_FIXED_TASK_MODE="+mode,"WATCHDOG_FIXED_INSTANCE="+guid,"WATCHDOG_FIXED_XML="+expectedXML,"WATCHDOG_FIXED_ENGINE="+strconv.FormatUint(uint64(engine),10))
	var stdout,stderr watchdogLimitedOutput;cmd.Stdout=&stdout;cmd.Stderr=&stderr
	err:=cmd.Run()
	if err!=nil||c.Err()!=nil||stdout.overflow||stderr.overflow||stderr.Len()!=0{
		// A timed-out stop has an unknown effect; do not retry or claim no-change.
		return watchdogTaskSnapshot{},errors.New("watchdog_task_command_unconfirmed")
	}
	return decodeWatchdogTask(stdout.Bytes())
}
func watchdogInstanceGUID(s string)bool{
	if len(s)!=38||s[0]!='{'||s[37]!='}'{return false}
	for i,c:=range s[1:37]{if i==8||i==13||i==18||i==23{if c!='-'{return false};continue};if !(c>='0'&&c<='9'||c>='a'&&c<='f'||c>='A'&&c<='F'){return false}}
	return true
}
func decodeWatchdogTask(raw []byte)(watchdogTaskSnapshot,error){
	var v struct{
		Name string `json:"name"`
		XML string `json:"xml_sha256"`
		Definition string `json:"definition_xml"`
		Principal string `json:"principal"`
		S4U bool `json:"s4u"`
		Limited bool `json:"limited"`
		Executable string `json:"executable"`
		Arguments string `json:"arguments"`
		Working string `json:"working"`
		Resident bool `json:"resident_disabled"`
		Credit bool `json:"credit_disabled"`
		Instances []watchdogTaskInstance `json:"instances"`
	}
	if json.Unmarshal(raw,&v)!=nil||v.Principal==""{return watchdogTaskSnapshot{},errors.New("watchdog_task_schema")}
	view:=watchdogTaskView{Name:v.Name,XMLSHA256:v.XML,S4U:v.S4U,Limited:v.Limited,Executable:v.Executable,
		Arguments:v.Arguments,WorkingDirectory:v.Working,ResidentGuardDisabled:v.Resident,CreditWatchdogDisabled:v.Credit}
	if !view.valid(){return watchdogTaskSnapshot{},errors.New("watchdog_task_definition_changed")}
	definition,e:=decodeWatchdogTaskDefinition(v.Definition)
	digest:=sha256.Sum256([]byte(v.Definition))
	if e!=nil||hex.EncodeToString(digest[:])!=v.XML||definition.Principal!=v.Principal||
		definition.Executable!=v.Executable||definition.Arguments!=v.Arguments||definition.Working!=v.Working||
		!definition.S4U||!definition.Limited{return watchdogTaskSnapshot{},errors.New("watchdog_task_actions_or_context_unverified")}
	seen:=map[string]bool{}
	for _,i:=range v.Instances{
		if !watchdogInstanceGUID(i.GUID)||i.PID==0||i.Path!="\\CC-OcxHealthWatchdog"||!same(i.Action,watchdogLauncher)||i.State!=4{
			return watchdogTaskSnapshot{},errors.New("watchdog_running_instance_unverified")
		}
		if seen[i.GUID]{return watchdogTaskSnapshot{},errors.New("watchdog_duplicate_instance")}
		seen[i.GUID]=true
	}
	return watchdogTaskSnapshot{View:view,Principal:v.Principal,Instances:v.Instances},nil
}


const watchdogTaskXMLNamespace = "http://schemas.microsoft.com/windows/2004/02/mit/task"
type watchdogTaskDefinition struct {
	Principal string
	Executable string
	Arguments string
	Working string
	S4U bool
	Limited bool
}
func decodeWatchdogTaskDefinition(raw string) (watchdogTaskDefinition, error) {
	var d struct {
		XMLName xml.Name
		Principals []struct {
			XMLName xml.Name
			Entries []struct {
				XMLName xml.Name
				ID string `xml:"id,attr"`
				User string `xml:"UserId"`
				Logon string `xml:"LogonType"`
				RunLevel string `xml:"RunLevel"`
			} `xml:",any"`
		} `xml:"Principals"`
		Actions []struct {
			XMLName xml.Name
			Context string `xml:"Context,attr"`
			Entries []struct {
				XMLName xml.Name
				Command string `xml:"Command"`
				Arguments string `xml:"Arguments"`
				Working string `xml:"WorkingDirectory"`
			} `xml:",any"`
		} `xml:"Actions"`
	}
	fail := func() (watchdogTaskDefinition,error) { return watchdogTaskDefinition{},errors.New("watchdog_task_definition_xml_unverified") }
	// Task.Xml is already a decoded Unicode string carried in UTF-8 JSON.
	// Windows may leave encoding="UTF-16" in its declaration; no byte decoding
	// or original-buffer hash changes are made for that declaration.
	decoder:=xml.NewDecoder(strings.NewReader(raw))
	decoder.CharsetReader=func(name string,input io.Reader)(io.Reader,error){
		if strings.EqualFold(name,"utf-16"){return input,nil}
		return nil,errors.New("watchdog_task_xml_encoding")
	}
	if len(raw)==0||len(raw)>96<<10||decoder.Decode(&d)!=nil||
		d.XMLName!=(xml.Name{Space:watchdogTaskXMLNamespace,Local:"Task"})||len(d.Principals)!=1||len(d.Actions)!=1 { return fail() }
	principals,actions:=d.Principals[0],d.Actions[0]
	if principals.XMLName.Space!=watchdogTaskXMLNamespace||actions.XMLName.Space!=watchdogTaskXMLNamespace||
		len(principals.Entries)!=1||len(actions.Entries)!=1 { return fail() }
	p,a:=principals.Entries[0],actions.Entries[0]
	if p.XMLName!=(xml.Name{Space:watchdogTaskXMLNamespace,Local:"Principal"})||
		a.XMLName!=(xml.Name{Space:watchdogTaskXMLNamespace,Local:"Exec"})||p.ID==""||actions.Context!=p.ID||p.User=="" { return fail() }
	return watchdogTaskDefinition{Principal:p.User,Executable:a.Command,Arguments:a.Arguments,Working:a.Working,
		S4U:p.Logon=="S4U",Limited:p.RunLevel=="LeastPrivilege"},nil
}
