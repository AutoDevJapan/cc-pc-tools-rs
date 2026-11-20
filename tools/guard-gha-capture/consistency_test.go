package main

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// The workflow repeats the fixed guard source and its tree in three literals,
// and this helper verifies the same two values; none of them may drift apart.
// A hosted Windows checkout may write CRLF line endings, so the workflow is
// compared after normalizing them to LF.
func workflowText(t *testing.T) string {
    t.Helper()
    raw,err:=os.ReadFile(filepath.Join("..","..",".github","workflows","unattended-readiness.yml"))
    if err!=nil{t.Fatal(err)}
    return strings.ReplaceAll(string(raw),"\r\n","\n")
}

// workflowPinProblem returns an error unless the workflow carries the source and
// the tree in exactly the places the producer reads them, and nowhere else.
func workflowPinProblem(workflow,source,tree string) error {
    for _,want:=range []string{
        "          ref: "+source+"\n",
        "source = '"+source+"'",
        "source_tree = '"+tree+"'",
        "  "+jobKey+":\n",
        "    name: Guard bootstrap artifact\n",
    } {
        if strings.Count(workflow,want)!=1 {return fmt.Errorf("workflow does not carry %q exactly once",want)}
    }
    if strings.Count(workflow,source)!=2||strings.Count(workflow,tree)!=1 {return errors.New("the fixed source or tree appears outside its literals")}
    return nil
}

func TestWorkflowCarriesTheFixedGuardSource(t *testing.T) {
    if err:=workflowPinProblem(workflowText(t),guardSource,guardTree);err!=nil {t.Fatal(err)}
}

// Every kind of drift must be refused, or the check above could pass without
// comparing anything.
func TestWorkflowPinCheckRefusesDrift(t *testing.T) {
    workflow:=workflowText(t)
    if err:=workflowPinProblem(workflow,guardSource,guardTree);err!=nil {t.Fatal("the control workflow was refused",err)}
    other:=strings.Repeat("0",40)
    checkout:="          ref: "+guardSource+"\n"
    for name,changed:=range map[string]string{
        "another checkout ref":strings.Replace(workflow,checkout,"          ref: "+other+"\n",1),
        "another record source":strings.Replace(workflow,"source = '"+guardSource+"'","source = '"+other+"'",1),
        "another record tree":strings.Replace(workflow,"source_tree = '"+guardTree+"'","source_tree = '"+other+"'",1),
        "repeated checkout ref":strings.Replace(workflow,checkout,checkout+checkout,1),
        "source named elsewhere":workflow+"# "+guardSource+"\n",
        "tree named elsewhere":workflow+"# "+guardTree+"\n",
        "renamed job key":strings.Replace(workflow,"  "+jobKey+":\n","  other-job:\n",1),
        "renamed job":strings.Replace(workflow,"    name: Guard bootstrap artifact\n","    name: Other\n",1),
    } {
        if changed==workflow {t.Fatalf("%s: the change did nothing",name)}
        if workflowPinProblem(changed,guardSource,guardTree)==nil {t.Fatalf("%s: drift accepted",name)}
    }
    if workflowPinProblem(workflow,other,guardTree)==nil||workflowPinProblem(workflow,guardSource,other)==nil {t.Fatal("pins that differ from the workflow were accepted")}
}

func TestPinsAreWellFormed(t *testing.T) {
    for name,value:=range map[string]string{"source":guardSource,"tree":guardTree,"repair-guard tree":guardRepairTree,"contract tree":guardContractTree} {
        if !lowerHex(value,40) {t.Fatalf("the %s pin is not a lowercase 40-digit object id",name)}
    }
    count:=guardInputCount
    if count<1||count>4096 {t.Fatal("the input count is outside the capture limit")}
}

func TestModuleTreePinsAcceptOnlyTheReviewedTrees(t *testing.T) {
    reviewed:=map[string]string{"repair-guard":guardRepairTree,"tools/civ6-ci-contract":guardContractTree}
    var asked []string
    record:=func(dir string)(string,error){asked=append(asked,dir);return reviewed[dir],nil}
    if !moduleTreesMatch(record) {t.Fatal("the reviewed module trees were refused")}
    if strings.Join(asked,",")!="repair-guard,tools/civ6-ci-contract" {t.Fatal("unexpected module directories",asked)}
    other:=strings.Repeat("0",40)
    for _,fault:=range []struct{name,dir,tree string;fail bool}{
        {"another repair-guard tree","repair-guard",other,false},
        {"another contract tree","tools/civ6-ci-contract",other,false},
        {"padded tree","repair-guard",guardRepairTree+"\n",false},
        {"uppercase tree","tools/civ6-ci-contract",strings.ToUpper(guardContractTree),false},
        {"empty tree","repair-guard","",false},
        {"unreadable repair-guard","repair-guard","",true},
        {"unreadable contract","tools/civ6-ci-contract","",true},
    } {
        read:=func(dir string)(string,error){
            if dir==fault.dir {if fault.fail {return reviewed[dir],errCapture};return fault.tree,nil}
            return reviewed[dir],nil
        }
        if moduleTreesMatch(read) {t.Fatalf("%s: accepted",fault.name)}
    }
}
