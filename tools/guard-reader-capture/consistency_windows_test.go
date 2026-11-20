package main

import (
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "testing"
)

// The workflow repeats the fixed source and the source pin in a few literals,
// and the README names them; none of them may drift from the constants this
// tool verifies.
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
        "source_pin = "+strconv.Itoa(sourcePin)+"\n",
        "source_pin_rehearsal = $"+strconv.FormatBool(sourcePinRehearsal)+"\n",
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
    for _,want:=range []string{"`source_pin`","`source_pin_rehearsal` is false","Source pin "+strconv.Itoa(sourcePin)} {
        if strings.Count(readme,want)<1 {t.Fatalf("README does not name %q",want)}
    }
    if strings.Contains(readme,"Generation 0")||strings.Contains(readme,"A later generation") {t.Fatal("README still calls the source pin a generation")}
}
