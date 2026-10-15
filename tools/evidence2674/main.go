// evidence2674 reads a finite set of operational receipts as data. It never
// executes recorded commands or prints source strings, paths, or errors.
package main

import (
 "bufio"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "fmt"
 "os"
 "path/filepath"
 "regexp"
 "sort"
 "strings"
)

func hash(b []byte) string { h:=sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func emit(v any) { b,_:=json.Marshal(v); fmt.Println(string(b)) }
func fail() { emit(map[string]bool{"success":false}); os.Exit(1) }
var refs=regexp.MustCompile(`(?i)(?:D:[/\\]CC[/\\]Work[/\\])(?:[a-z0-9._-]+[/\\])*[a-z0-9._-]+`)
var ids=map[int]bool{2467:true,2488:true,2648:true,2665:true,2669:true,2671:true,2674:true}

func todo() {
 b,e:=os.ReadFile("D:/CC/Tools/todo/todo.json"); if e!=nil {fail()}
 var root map[string]json.RawMessage; if json.Unmarshal(b,&root)!=nil {fail()}
 var items []map[string]any
 if json.Unmarshal(root["items"],&items)!=nil { fail() }
 for _,t:= range items {
  n,_:=t["id"].(float64); if !ids[int(n)] {continue}
  out:=map[string]any{"record":int(n)}
  for _,k:=range []string{"status","progress","waiting_on"} {if v,ok:=t[k];ok {out[k]=v}}
  // References stay inside the private index. They are not evidence of authority.
  raw,_:=json.Marshal(t); out["sha256"]=hash(raw)
  out["references"]=len(refs.FindAll(raw,-1))
  keys:=[]string{}; for k:=range t {keys=append(keys,k)};sort.Strings(keys);out["keys"]=keys
  emit(out)
 }
}

func survey(path string) {
 f,e:=os.Open(path);if e!=nil {fail()};defer f.Close()
 s:=bufio.NewScanner(f);s.Buffer(make([]byte,65536),32*1024*1024)
 i:=0; shapes:=map[string]int{}; hits:=0
 for s.Scan(){i++;b:=s.Bytes();var m map[string]json.RawMessage
  if json.Unmarshal(b,&m)!=nil {shapes["invalid"]++;continue}
  keys:=[]string{};for k:=range m {keys=append(keys,k)};sort.Strings(keys);shapes[strings.Join(keys,",")]++
  low:=strings.ToLower(string(b));if strings.Contains(low,"client-projects.txt")||strings.Contains(low,"scrub391") {hits++;if hits<=8 {emit(map[string]any{"record":i,"sha256":hash(b),"keys":keys})}}
 }
 emit(map[string]any{"records":i,"matches":hits,"shapes":shapes,"success":s.Err()==nil})
}

func inventory(root string) {
 entries,e:=os.ReadDir(root);if e!=nil {fail()};i:=0
 for _,d:=range entries {if d.IsDir(){continue};i++;fi,e:=d.Info();if e!=nil{fail()};emit(map[string]any{"record":i,"name_sha256":hash([]byte(d.Name())),"bytes":fi.Size(),"jsonl":filepath.Ext(d.Name())==".jsonl"})}
}
func main(){if len(os.Args)<2{fail()};switch os.Args[1]{case "todo":todo();case "survey":if len(os.Args)!=3{fail()};survey(os.Args[2]);case "inventory":if len(os.Args)!=3{fail()};inventory(os.Args[2]);default:fail()}}
