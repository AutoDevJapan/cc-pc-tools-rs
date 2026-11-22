//go:build windows

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func watchdogOriginRecordsFixture()[][]byte{
	rows:=[]map[string]any{{"file":"deployed-watchdog.exe","bytes":595456,"sha256":strings.ToUpper(watchdogOldSHA)}}
	for i:=1;i<len(watchdogOriginalRecords);i++{
		r:=watchdogOriginalRecords[i];rows=append(rows,map[string]any{"file":r.name,"bytes":r.bytes,"sha256":strings.ToUpper(r.hash)})
	}
	m,_:=json.Marshal(rows)
	v,_:=json.Marshal(map[string]any{"todo":1936,"source_head":watchdogOldSource,"release":map[string]any{"sha256":strings.ToUpper(watchdogOldSHA),"subsystem":2,"installed":true}})
	g,_:=json.Marshal(map[string]any{"state":"MERGED","headRefOid":watchdogOldSource,"mergeCommit":map[string]any{"oid":watchdogOldMerge}})
	return [][]byte{m,v,g,[]byte("fixture note")}
}
func TestWatchdogOriginalRecordContentBindsOnePriorRelease(t *testing.T){
	for _,kind:=range []string{"normal","missing","size","hash","source","merge","installed"}{
		t.Run(kind,func(t *testing.T){
			b:=watchdogOriginRecordsFixture()
			switch kind{
			case "missing":b[0]=[]byte("[]")
			case "size":b[0]=[]byte(strings.Replace(string(b[0]),"595456","595457",1))
			case "hash":b[0]=[]byte(strings.Replace(string(b[0]),strings.ToUpper(watchdogOldSHA),strings.Repeat("0",64),1))
			case "source":b[1]=[]byte(strings.Replace(string(b[1]),watchdogOldSource,watchdogOldMerge,1))
			case "merge":b[2]=[]byte(strings.Replace(string(b[2]),watchdogOldMerge,watchdogOldSource,1))
			case "installed":b[1]=[]byte(strings.Replace(string(b[1]),"true","false",1))
			}
			if (watchdogOriginContent(b)==nil)!=(kind=="normal"){t.Fatal("original record binding")}
		})
	}
}
func TestWatchdogCurrentRecognitionDoesNotAcceptUnknownBytes(t *testing.T){
	id:=watchdogTestID();id.Bytes=595456;id.SHA256=watchdogOldSHA
	if !watchdogRecognizedPredecessor(id){t.Fatal("original bytes unrecognized")}
	id.SHA256=strings.Repeat("0",64)
	if watchdogRecognizedPredecessor(id){t.Fatal("unknown current became pin")}
}
