//go:build windows

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"syscall"
	"time"
)

// This owner is created from this attempt's os.Pipe, before Cmd.Start.
// Cmd receives only the read end. The parent owns and observes its writer.
type unslopHistoryStdin struct {
	reader, writer *softwareOwnedFile
	exchange *unslopHistoryExchange
	mu sync.Mutex
	started, finished, copyFinished, copyFailed bool
	done chan struct{}
	closeOnce sync.Once
	closeErr error
	copyErr error
}

type unslopHistoryStdinFacts struct {
	CopyStarted, CopyFinished, CopyFailed bool
	CloseAttempted, CloseCompleted, CloseFailed bool
	ReaderCloseFailed bool
}

// Package-private fault injection only changes a newly created owned pipe.
type unslopHistoryStdinFaultKey struct{}
type unslopHistoryStdinFaults struct {
	protectWriter bool
	owner *unslopHistoryStdin
}

func newUnslopHistoryStdin(exchange *unslopHistoryExchange) (*unslopHistoryStdin,error) {
	if exchange==nil {return nil,errors.New("history_protocol_unavailable")}
	reader,writer,err:=os.Pipe()
	if err!=nil {return nil,err}
	return &unslopHistoryStdin{reader:newSoftwareOwnedFile(reader),writer:newSoftwareOwnedFile(writer),exchange:exchange,done:make(chan struct{})},nil
}

func (owner *unslopHistoryStdin) start() {
	owner.mu.Lock()
	if owner.started {owner.mu.Unlock();return}
	owner.started=true
	owner.mu.Unlock()
	_ = owner.reader.Close()
	go func(){
		_,err:=owner.exchange.WriteTo(owner.writer.file)
		owner.mu.Lock()
		owner.copyFailed=err!=nil
		owner.copyErr=err
		owner.copyFinished=true
		owner.mu.Unlock()
		_ = owner.writer.Close()
		owner.mu.Lock()
		owner.finished=true
		owner.mu.Unlock()
		close(owner.done)
	}()
}

func (owner *unslopHistoryStdin) facts() unslopHistoryStdinFacts {
	owner.mu.Lock()
	facts:=unslopHistoryStdinFacts{CopyStarted:owner.started,CopyFinished:owner.copyFinished,CopyFailed:owner.copyFailed}
	owner.mu.Unlock()
	owner.writer.mu.Lock()
	facts.CloseAttempted=owner.writer.closeAttempted
	facts.CloseCompleted=owner.writer.closeCompleted
	facts.CloseFailed=owner.writer.closeCompleted&&owner.writer.closeErr!=nil
	owner.writer.mu.Unlock()
	owner.reader.mu.Lock()
	facts.ReaderCloseFailed=owner.reader.closeAttempted&&(!owner.reader.closeCompleted||owner.reader.closeErr!=nil)
	owner.reader.mu.Unlock()
	return facts
}

// Wait is already finished before this is called. Incomplete writes are
// interrupted and their original owner remains retained if cleanup fails.
func (owner *unslopHistoryStdin) finish() unslopHistoryStdinFacts {
	owner.mu.Lock()
	started:=owner.started
	owner.mu.Unlock()
	if started {
		select {
		case <-owner.done:
		case <-time.After(100*time.Millisecond):
			_ = owner.exchange.Close()
			_ = owner.writer.closeAsync()
			select {case <-owner.done:case <-time.After(2*time.Second):}
		}
	}
	_ = owner.Close()
	return owner.facts()
}


func (owner *unslopHistoryStdin) Close() error {
	owner.closeOnce.Do(func(){
		_ = owner.exchange.Close()
		writerDone:=owner.writer.closeAsync()
		readerDone:=owner.reader.closeAsync()
		timer:=time.NewTimer(2*time.Second)
		defer timer.Stop()
		for writerDone!=nil||readerDone!=nil {
			select {
			case <-writerDone:writerDone=nil
			case <-readerDone:readerDone=nil
			case <-timer.C:writerDone=nil;readerDone=nil
			}
		}
		for _,resource:=range []*softwareOwnedFile{owner.writer,owner.reader} {
			resource.mu.Lock()
			unconfirmed:=!resource.closeCompleted||resource.closeErr!=nil
			resource.mu.Unlock()
			if unconfirmed {
				softwareUnconfirmedCleanup.Lock()
				softwareUnconfirmedCleanup.owners=append(softwareUnconfirmedCleanup.owners,[]io.Closer{resource})
				softwareUnconfirmedCleanup.Unlock()
				owner.closeErr=errors.New("history_stdin_cleanup_unconfirmed")
			}
		}
		owner.mu.Lock()
		pending:=owner.started&&!owner.finished
		owner.mu.Unlock()
		if pending {
			softwareUnconfirmedCleanup.Lock()
			softwareUnconfirmedCleanup.owners=append(softwareUnconfirmedCleanup.owners,[]io.Closer{owner})
			softwareUnconfirmedCleanup.Unlock()
			owner.closeErr=errors.New("history_stdin_cleanup_unconfirmed")
		}
	})
	return owner.closeErr
}

func prepareUnslopHistoryStdin(ctx context.Context, exchange *unslopHistoryExchange) (*unslopHistoryStdin,error) {
	owner,err:=newUnslopHistoryStdin(exchange)
	if err!=nil {return nil,err}
	if fault,_:=ctx.Value(unslopHistoryStdinFaultKey{}).(*unslopHistoryStdinFaults);fault!=nil {
		fault.owner=owner
		if fault.protectWriter {
			if err=syscall.SetHandleInformation(owner.writer.handle,2,2);err!=nil {_ = owner.Close();return nil,err}
		}
	}
	return owner,nil
}
