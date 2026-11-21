//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const watchdogOriginalRoot = "D:/CC/Tools/ocx-health-watchdog/evidence-1936/continuation-20261010-2318"
const watchdogOldSHA = "4bc9ba7108fbd8a31fa4c56b692f5f9a73b5e884b9d1a0c425c727ea5fcfe08c"
const watchdogOldSource = "e488e31712d1190ed1cfc1ce42ee1e48909a4cd6"
const watchdogOldMerge = "983b4b99c9042b453eaba6bfd146146df7421656"

var watchdogOriginalRecords = []struct{
	name string
	bytes int
	hash string
}{
	{"manifest.json",6308,"ad802a28ed54752bba547fdeb898dfc8a00a84f0dfaa9c8dc55717ce09eb3f9d"},
	{"validation.json",1271,"32f0e3a2fe9c2694b3b6e802231fa183c5b9d0e6792de865e2e58ef4f2fcfce8"},
	{"merged-pr.json",4976,"085165399be23da1ad5edaa33b9198f1764999e7e518ec580b9cb71e4350b335"},
	{"todo-final-note.txt",3272,"b54090befd6d4b9e29a6d545c8c9577decdde1cd531de1ad93789ba7e38a15a7"},
}

func watchdogSHA(b []byte) string {
	s:=sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// A normal source guard precedes every selected record/body read. Its success
// is only classification; origin, software-update purpose and live generation
// are established separately by the normal maintenance issuer.
func watchdogQualify(ctx context.Context,path string) error {
	if ctx==nil||ctx.Err()!=nil{return errors.New("watchdog_guard_context")}
	c, cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
	cmd:=exec.CommandContext(c,"D:/CC/Tools/client-records/scrub391.exe","--guard-path",path)
	cmd.SysProcAttr=&syscall.SysProcAttr{HideWindow:true,CreationFlags:0x08000000}
	cmd.WaitDelay=time.Second
	var stdout,stderr watchdogLimitedOutput
	cmd.Stdout=&stdout;cmd.Stderr=&stderr
	err:=cmd.Run();out:=stdout.Bytes()
	if err!=nil||c.Err()!=nil||stdout.overflow||stderr.overflow||stderr.Len()!=0||len(out)>65536{return errors.New("watchdog_source_unqualified")}
	var v struct{
		OK *bool `json:"ok"`
		Access *bool `json:"access_allowed"`
		Client *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
		Status string `json:"protection_status"`
	}
	if json.Unmarshal(out,&v)!=nil||v.OK==nil||v.Access==nil||v.Client==nil||v.Unverified==nil||!(*v.OK)||!(*v.Access)||*v.Client||*v.Unverified||v.Status!="clear"{
		return errors.New("watchdog_source_unqualified")
	}
	return nil
}

type watchdogOriginEvidence struct {
	scope *watchdogPinScope
	pins []*pin
	buffers [][]byte
	closed bool
	closeErr error
}

func (o *watchdogOriginEvidence) Recheck(ctx context.Context) error{
	if o==nil||o.closed||ctx.Err()!=nil||len(o.pins)!=len(watchdogOriginalRecords){return errors.New("watchdog_origin_not_live")}
	for i,p:=range o.pins{
		if o.scope.verifyName(p,watchdogOriginalRoot+"/"+watchdogOriginalRecords[i].name)!=nil{return errors.New("watchdog_origin_name_changed")}
		if _,e:=p.f.Seek(0,0);e!=nil{return e}
		b,e:=io.ReadAll(io.LimitReader(p.f,int64(watchdogOriginalRecords[i].bytes)+1))
		if e!=nil||!bytes.Equal(b,o.buffers[i]){return errors.New("watchdog_origin_bytes_changed")}
	}
	return nil
}
func (o *watchdogOriginEvidence) Close()error{
	if o==nil{return nil}
	if o.closed{return o.closeErr};o.closed=true
	if o.scope!=nil{o.closeErr=o.scope.Close()}
	return o.closeErr
}

// Caller supplies no path, expected hash or installed digest. The four original
// retained buffers are hashed and decoded from their own held objects.
func openWatchdogOriginEvidence(ctx context.Context)(o *watchdogOriginEvidence,err error){
	o=&watchdogOriginEvidence{scope:newWatchdogPinScope()}
	for _,r:=range watchdogOriginalRecords{
		path:=watchdogOriginalRoot+"/"+r.name
		if err=watchdogQualify(ctx,path);err!=nil{return o,err}
		if _,err=o.scope.parents(path);err!=nil{return o,err}
		p,e:=o.scope.open(path,syscall.GENERIC_READ|0x20000,1,3)
		if e!=nil{return o,e}
		if p.info.FileAttributes&0x10!=0{return o,errors.New("watchdog_origin_not_file")}
		b,e:=io.ReadAll(io.LimitReader(p.f,int64(r.bytes)+1))
		if e!=nil||len(b)!=r.bytes||watchdogSHA(b)!=r.hash{return o,errors.New("watchdog_origin_record_changed")}
		o.pins=append(o.pins,p);o.buffers=append(o.buffers,b)
	}
	if err=watchdogOriginContent(o.buffers);err!=nil{return o,err}
	return o,o.Recheck(ctx)
}

func watchdogOriginContent(buffers [][]byte)error{
	if len(buffers)!=4{return errors.New("watchdog_origin_count")}
	var manifest []struct{File string;Bytes int;SHA string `json:"sha256"`}
	var validation struct{
		Todo int
		Source string `json:"source_head"`
		Release struct{SHA string `json:"sha256"`;Subsystem int;Installed bool}
	}
	var merge struct{State string;Head string `json:"headRefOid"`;Merge struct{OID string} `json:"mergeCommit"`}
	if json.Unmarshal(buffers[0],&manifest)!=nil||json.Unmarshal(buffers[1],&validation)!=nil||
		json.Unmarshal(buffers[2],&merge)!=nil{return errors.New("watchdog_origin_schema")}
	found:=map[string]int{}
	for _,r:=range manifest{
		if r.File=="deployed-watchdog.exe"{
			if r.Bytes!=595456||strings.ToLower(r.SHA)!=watchdogOldSHA{return errors.New("watchdog_origin_payload")}
			found[r.File]++
		}
		for i:=1;i<len(watchdogOriginalRecords);i++{
			e:=watchdogOriginalRecords[i]
			if r.File==e.name{
				if r.Bytes!=e.bytes||strings.ToLower(r.SHA)!=e.hash{return errors.New("watchdog_origin_member")}
				found[r.File]++
			}
		}
	}
	for _,name:=range []string{"deployed-watchdog.exe","validation.json","merged-pr.json","todo-final-note.txt"}{
		if found[name]!=1{return errors.New("watchdog_origin_member_count")}
	}
	if validation.Todo!=1936||validation.Source!=watchdogOldSource||
		strings.ToLower(validation.Release.SHA)!=watchdogOldSHA||validation.Release.Subsystem!=2||!validation.Release.Installed||
		merge.State!="MERGED"||merge.Head!=watchdogOldSource||merge.Merge.OID!=watchdogOldMerge{
		return errors.New("watchdog_origin_release_source_mismatch")
	}
	return nil
}

// Recognition is separate from issuance. Original build/event qualification
// and a current-purpose lease are still required before opening the installed
// target. The saved validation summary is not relabelled as a raw build receipt.
func watchdogRecognizedPredecessor(id watchdogFileIdentity)bool{
	return id.valid()&&id.Bytes==595456&&id.SHA256==watchdogOldSHA
}
