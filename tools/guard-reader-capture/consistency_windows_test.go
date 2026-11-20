package main

import (
    "os"
    "path/filepath"
    "strings"
    "testing"
)

// The workflow repeats the fixed source in a few literals, and the README
// names it; none of them may drift from the constants this tool verifies.
// A hosted Windows checkout may write CRLF line endings, so both files are
// compared after normalizing them to LF.
func lfText(t *testing.T,name string) string {
    t.Helper()
    raw,err:=os.ReadFile(name)
    if err!=nil{t.Fatal(err)}
    return strings.ReplaceAll(string(raw),"\r\n","\n")
}

func TestWorkflowAndReadmeCarryTheFixedSource(t *testing.T) {
    workflow:=lfText(t,filepath.Join("..","..",".github","workflows","guard-reader-capture.yml"))
    for _,want:=range []string{
        "          ref: "+readerSource+"\n",
        "source = '"+readerSource+"'",
        "source_tree = '"+readerTree+"'",
        "source_module_tree = '"+readerModuleTree+"'",
        "READER_CAPTURE_CHECK_RUN_ID: ${{ job.check_run_id }}",
        "name: "+jobName+"\n",
        "  "+jobKey+":\n",
    } {
        if strings.Count(workflow,want)!=1 {t.Fatalf("workflow does not carry %q exactly once",want)}
    }
    readme:=lfText(t,"README.md")
    for _,want:=range []string{readerSource,readerTree,readerModuleTree} {
        if strings.Count(readme,want)!=1 {t.Fatalf("README does not carry %q exactly once",want)}
    }
}
