//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
)

var errSoftwareCleanupUnconfirmed = errors.New("software_resource_cleanup_unconfirmed")

type softwareCleanupReport struct { Attempted, Failed int }

type softwareCleanupError struct {
	primary error
	report softwareCleanupReport
}
func (e *softwareCleanupError) Error() string {
	if e.report.Failed>0 {return "software_resource_cleanup_unconfirmed"};return "software_object_unavailable"
}
func (e *softwareCleanupError) Unwrap() error { return e.primary }
func (e *softwareCleanupError) Is(target error) bool { return target == errSoftwareCleanupUnconfirmed && e.report.Failed>0 }

// Preserve the actual references whose Close result was not confirmed. They
// are never retried as arbitrary handles and do not grant permission to read.
var softwareUnconfirmedCleanup = struct {
	sync.Mutex
	owners [][]io.Closer
}{}

type softwareOwnedHandle struct { handle syscall.Handle }
func (h *softwareOwnedHandle) Close() error { return syscall.CloseHandle(h.handle) }

func softwareCleanupFacts(err error) softwareCleanupReport {
	var failure *softwareCleanupError
	if errors.As(err, &failure) { return failure.report }
	return softwareCleanupReport{}
}

func closeSoftwareOwnedResources(resources []io.Closer) softwareCleanupReport {
	report := softwareCleanupReport{}
	var unconfirmed []io.Closer
	for i := len(resources)-1; i >= 0; i-- {
		resource:=resources[i]
		if resource==nil {continue}
		err:=resource.Close()
		nested:=softwareCleanupFacts(err)
		hasOwnedReport:=false
		if owner,ok:=resource.(interface{cleanupReport() softwareCleanupReport});ok {nested=owner.cleanupReport();hasOwnedReport=true}
		if hasOwnedReport||nested.Attempted>0 {report.Attempted+=nested.Attempted}else{report.Attempted++}
		if err!=nil {
			if nested.Failed>0 {report.Failed+=nested.Failed}else{report.Failed++}
			unconfirmed=append(unconfirmed,resource)
		}
	}
	if len(unconfirmed)!=0 {
		softwareUnconfirmedCleanup.Lock()
		softwareUnconfirmedCleanup.owners=append(softwareUnconfirmedCleanup.owners,unconfirmed)
		softwareUnconfirmedCleanup.Unlock()
	}
	return report
}

func finishSoftwareAcquisition(primary error, resources ...io.Closer) error {
	prior := softwareCleanupFacts(primary)
	report := closeSoftwareOwnedResources(resources)
	report.Attempted += prior.Attempted
	report.Failed += prior.Failed
	if report.Attempted == 0 && report.Failed == 0 { return primary }
	return &softwareCleanupError{primary: primary, report: report}
}

// softwareOwnedFile records the actual handle while its os.File is still open.
// A failed Close can invalidate Go's File descriptor; the native owner record
// remains identifiable but never retries that raw handle automatically.
type softwareOwnedFile struct {
	mu sync.Mutex
	file *os.File
	handle syscall.Handle
	generation uint64
	identity softwareIdentity
	identityKnown bool
	closeAttempted bool
	closeCompleted bool
	closeDone chan struct{}
	closeErr error
	closeCalls int
}

var softwareFileGeneration atomic.Uint64

func newSoftwareOwnedFile(file *os.File) *softwareOwnedFile {
	owner:=&softwareOwnedFile{file:file,generation:softwareFileGeneration.Add(1)}
	if file!=nil {
		owner.handle=syscall.Handle(file.Fd())
		if identity,err:=fileSoftwareIdentity(file);err==nil {owner.identity=identity;owner.identityKnown=true}
	}
	return owner
}


func (owner *softwareOwnedFile) beginClose() (chan struct{},bool) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.closeAttempted {return owner.closeDone,false}
	owner.closeAttempted=true
	owner.closeCalls++
	owner.closeDone=make(chan struct{})
	return owner.closeDone,true
}

func (owner *softwareOwnedFile) completeClose() {
	err:=errUpdateObject
	if owner.file!=nil {err=owner.file.Close()}
	owner.mu.Lock()
	owner.closeErr=err
	owner.closeCompleted=true
	done:=owner.closeDone
	owner.mu.Unlock()
	close(done)
}

func (owner *softwareOwnedFile) closeAsync() <-chan struct{} {
	done,first:=owner.beginClose()
	if first {go owner.completeClose()}
	return done
}

func (owner *softwareOwnedFile) Close() error {
	_,first:=owner.beginClose()
	if first {owner.completeClose()}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if !owner.closeCompleted {return errSoftwareCleanupUnconfirmed}
	return owner.closeErr
}
