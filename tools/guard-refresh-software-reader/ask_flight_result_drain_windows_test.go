//go:build windows

package main

import (
    "bytes"
    "errors"
    "io"
    "os"
    "testing"
)

type askResultFaultReader struct{}
func (askResultFaultReader) Read([]byte)(int,error){return 0,errors.New("synthetic pipe failure")}

func TestAskResultStreamRequiresEOFWithoutReadFailureOrOverflow(t *testing.T){
    clean:=newAskResultStream();if _,err:=clean.ReadFrom(bytes.NewReader([]byte("ok")));err!=nil||!clean.complete()||string(clean.output.Bytes())!="ok"{t.Fatal("normal EOF was not recorded")}
    failing:=newAskResultStream();if _,err:=failing.ReadFrom(askResultFaultReader{});err==nil||failing.complete()||!failing.drained||!failing.readFailed||failing.eof{t.Fatal("read failure was accepted")}
    overflow:=newAskResultStream();if _,err:=overflow.ReadFrom(bytes.NewReader(bytes.Repeat([]byte("x"),65537)));err==nil||overflow.complete()||!overflow.overflow||overflow.eof{t.Fatal("overflow was accepted")}
    unstarted:=newAskResultStream();if unstarted.complete(){t.Fatal("missing drain was accepted")}
    reader,writer,err:=os.Pipe();if err!=nil{t.Fatal("pipe unavailable")};defer reader.Close()
    if _,err=writer.Write([]byte("ok"));err!=nil{writer.Close();t.Fatal("pipe write failed")}
    if err=writer.Close();err!=nil{t.Fatal("pipe close failed")}
    actual:=newAskResultStream();if _,err=io.Copy(actual,reader);err!=nil||!actual.complete()||string(actual.output.Bytes())!="ok"{t.Fatal("os/exec copy path missed EOF")}
}
