package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// These admissions identify public release bytes already independently
// obtained from the vendor archives. Registry display strings alone are not
// authority, and this table admits no directories, arguments or caller hashes.
type ownerInstalledToolPlan struct {
	name, path, version, publisher, registry, archiveURL, archiveSHA, entry, sha string
	size int64
	aliases []string
}

func ownerInstalledToolPlans() []ownerInstalledToolPlan {
	return []ownerInstalledToolPlan{
		{name:"git",path:"C:/Program Files/Git/cmd/git.exe",version:"2.54.0",publisher:"The Git Development Community",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\Git_is1`,archiveURL:"https://github.com/git-for-windows/git/releases/download/v2.54.0.windows.1/MinGit-2.54.0-64-bit.zip",archiveSHA:"04f937e1f0918b17b9be6f2294cb2bb66e96e1d9832d1c298e2de088a1d0e668",entry:"cmd/git.exe",sha:"81ef35ae005ca9318018d18e3327578ce939fb99feaad6b2d7c8ab15f3de8db5",size:46480,aliases:[]string{"C:/Program Files/Git/cmd/git-lfs.exe","C:/Program Files/Git/cmd/git.exe"}},
		{name:"gh",path:"C:/Program Files/GitHub CLI/gh.exe",version:"2.101.0",publisher:"GitHub, Inc.",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{05425DD6-E9FE-4AEE-B289-8B61429F042A}`,archiveURL:"https://github.com/cli/cli/releases/download/v2.101.0/gh_2.101.0_windows_amd64.zip",archiveSHA:"bc6c814367b193cd8e713611d61e36013c0ef843b8f516458fe3eda039192794",entry:"bin/gh.exe",sha:"df05089117a23889b3f6ea08d7644849afe7b6025647a09f33b45c71f1418f75",size:42755384,aliases:[]string{"C:/Program Files/GitHub CLI/gh.exe"}},
		{name:"go",path:"C:/Program Files/Go/bin/go.exe",version:"1.27.0",publisher:"https://go.dev",registry:`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{649B7E21-BFFC-4209-925C-5736BBF28E54}`,archiveURL:"https://go.dev/dl/go1.27.0.windows-amd64.zip",archiveSHA:"f0c0a0d33ba94f4d2c5dbc887334ce678b21813504ddb3aafcb06e60a5a667c4",entry:"go/bin/go.exe",sha:"7d828191ba32519a9c9361789ab647486236ed45c660889196c7770a8ff1985c",size:17508352,aliases:[]string{"C:/Program Files/Go/bin/go.exe"}},
	}
}

func ownerInstalledToolPlanFor(path string) (ownerInstalledToolPlan, bool) {
	for _, plan := range ownerInstalledToolPlans() {
		if same(path, plan.path) { return plan, true }
	}
	return ownerInstalledToolPlan{}, false
}

type ownerInstalledToolIdentity struct { volume, file, created uint64 }
type ownerInstalledToolState struct {
	id ownerInstalledToolIdentity
	size int64
	links uint32
	acl, resolved string
	aclAllowed bool
	parentProof string
	parentsAllowed bool
}

type ownerInstalledToolLease interface {
	State() (ownerInstalledToolState, error)
	Read(context.Context, int64) ([]byte, error)
	Close() error
}

// This is an internal source seam, never constructed from runtime JSON.
type ownerInstalledToolAccess interface {
	Origin(context.Context, ownerInstalledToolPlan) error
	Aliases(context.Context, string) ([]string, error)
	Open(context.Context, string) (ownerInstalledToolLease, error)
}

var errOwnerInstalledTool = errors.New("installed_tool_verification_failed")
var errOwnerInstalledToolCleanup = errors.New("installed_tool_cleanup_failed")

// Native primary errors remain available to the controller through errors.Is,
// while Error exposes only a fixed cleanup identifier, never native text.
type ownerInstalledResourceFailure struct { primary error }
func (e *ownerInstalledResourceFailure) Error() string { return errOwnerInstalledToolCleanup.Error() }
func (e *ownerInstalledResourceFailure) Unwrap() []error { return []error{e.primary,errOwnerInstalledToolCleanup} }

func ownerInstalledCloseResult(failure *error, closeErr error) {
	if closeErr==nil { return }
	if *failure==nil { *failure=errOwnerInstalledToolCleanup;return }
	if !errors.Is(*failure,errOwnerInstalledToolCleanup) { *failure=&ownerInstalledResourceFailure{primary:*failure} }
}

func ownerInstalledFailureVerdict(primary string,cancelled,cleanup bool) ownerPathVerdict {
	// Only controller-owned fixed identifiers can be combined. The primary
	// refusal, cancellation and cleanup are independent facts, not raw strings.
	switch primary {
	case "verified_installed_development_tool": primary="installed_tool"
	case "installed_tool_cancelled": primary="installed_tool";cancelled=true
	case "installed_tool_cleanup_failed": primary="installed_tool";cleanup=true
	case "installed_tool_origin_unavailable","installed_tool_alias_mismatch","installed_tool_identity_unavailable","installed_tool_parent_untrusted","installed_tool_identity_mismatch","installed_tool_bytes_mismatch","installed_tool_identity_changed","installed_tool_alias_changed","installed_tool_origin_changed","installed_tool_read_failed":
	default: primary="installed_tool_origin_unavailable"
	}
	if cancelled { primary+="_cancelled" }
	if cleanup { primary+="_cleanup_failed" }
	return ownerRuleUnknown(primary)
}

func ownerInstalledToolClassification(path string) (verdict ownerPathVerdict) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return ownerInstalledToolWith(ctx, path, ownerInstalledToolNative{}, ownerLegacyProtection)
}

func ownerInstalledToolWith(ctx context.Context, path string, access ownerInstalledToolAccess, legacy func(string) ownerLegacyState) (verdict ownerPathVerdict) {
	plan, fixed := ownerInstalledToolPlanFor(path)
	if !fixed { return ownerRuleUnknown("installed_tool_not_declared") }
	return ownerInstalledToolVerify(ctx,plan,access,legacy)
}

// The normal caller always obtains plan from the closed table above. A test
// may supply a synthetic plan to exercise this same controller without reading
// a real installation; no public operation decodes a plan or digest.
func ownerInstalledToolVerify(ctx context.Context, plan ownerInstalledToolPlan, access ownerInstalledToolAccess, legacy func(string) ownerLegacyState) (verdict ownerPathVerdict) {
	verdict = ownerRuleUnknown("installed_tool_origin_unavailable")
	if ctx == nil || access == nil || legacy == nil || len(plan.aliases)==0 || len(plan.aliases)>2 { return }
	cleanupFailed,cancelledFailure:=false,false
	observeFailure:=func(err error) {
		cleanupFailed=cleanupFailed || errors.Is(err,errOwnerInstalledToolCleanup)
		cancelledFailure=cancelledFailure || errors.Is(err,context.Canceled) || errors.Is(err,context.DeadlineExceeded)
	}
	defer func() {
		if verdict.Status=="protected" { return }
		if cleanupFailed || cancelledFailure || ctx.Err()!=nil { verdict=ownerInstalledFailureVerdict(verdict.Basis,cancelledFailure || ctx.Err()!=nil,cleanupFailed) }
	}()
	if ctx.Err() != nil { return ownerRuleUnknown("installed_tool_cancelled") }
	for _, alias := range plan.aliases {
		if ownerCardPath(alias) || ownerWindowsAlias(alias) { return }
		switch legacy(alias) {
		case ownerLegacyClient: return ownerRuleProtected("legacy_exclusion")
		case ownerLegacyUnavailable: return ownerRuleUnknown("existing_exclusion_unavailable")
		}
	}
	if err:=access.Origin(ctx, plan);err!=nil {
		observeFailure(err)
		if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
		return
	}
	if ctx.Err()!=nil { return }
	aliases, err := access.Aliases(ctx, plan.path)
	observeFailure(err)
	if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
	if err != nil || !ownerInstalledToolAliases(aliases, plan.aliases) { return ownerRuleUnknown("installed_tool_alias_mismatch") }
	var leases []ownerInstalledToolLease
	defer func() {
		for i:=len(leases)-1;i>=0;i-- { if leases[i].Close()!=nil { cleanupFailed=true } }
	}()
	var states []ownerInstalledToolState
	for _, alias := range plan.aliases {
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		lease, openErr := access.Open(ctx, alias)
		if lease!=nil { leases=append(leases,lease) }
		observeFailure(openErr)
		if openErr==context.Canceled || openErr==context.DeadlineExceeded { return ownerRuleUnknown("installed_tool_cancelled") }
		if openErr!=nil || lease==nil { return ownerRuleUnknown("installed_tool_identity_unavailable") }
		if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
		state, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr==nil && (!state.parentsAllowed || state.parentProof=="") { return ownerRuleUnknown("installed_tool_parent_untrusted") }
		if stateErr!=nil || state.id.volume==0 || state.id.file==0 || state.id.created==0 || state.size!=plan.size || state.links!=uint32(len(plan.aliases)) || state.acl=="" || !state.aclAllowed || !same(state.resolved,alias) {
			return ownerRuleUnknown("installed_tool_identity_mismatch")
		}
		if len(states)>0 && (states[0].id!=state.id || states[0].acl!=state.acl || states[0].parentProof!=state.parentProof) { return ownerRuleUnknown("installed_tool_identity_mismatch") }
		states=append(states,state)
	}
	// All names are held with read sharing only before any bytes are read.
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	raw, readErr := leases[0].Read(ctx,plan.size)
	defer clear(raw)
	observeFailure(readErr)
	if readErr==context.Canceled || readErr==context.DeadlineExceeded { return ownerRuleUnknown("installed_tool_cancelled") }
	if readErr!=nil { return ownerRuleUnknown("installed_tool_read_failed") }
	if int64(len(raw))!=plan.size || hashBytes(raw)!=plan.sha { return ownerRuleUnknown("installed_tool_bytes_mismatch") }
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	for i,lease := range leases {
		now, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr!=nil || now!=states[i] { return ownerRuleUnknown("installed_tool_identity_changed") }
	}
	after, err := access.Aliases(ctx,plan.path)
	observeFailure(err)
	if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
	if err!=nil || !ownerInstalledToolAliases(after,plan.aliases) { return ownerRuleUnknown("installed_tool_alias_changed") }
	if err:=access.Origin(ctx,plan);err!=nil {
		observeFailure(err)
		if err==errOwnerInstalledToolCleanup { return ownerRuleUnknown("installed_tool_cleanup_failed") }
		return ownerRuleUnknown("installed_tool_origin_changed")
	}
	// Registry/alias observations are not an atomic filesystem transaction.
	// Recheck the actual held objects after those observations as well.
	for i,lease := range leases {
		now, stateErr := lease.State()
		observeFailure(stateErr)
		if stateErr!=nil || now!=states[i] { return ownerRuleUnknown("installed_tool_identity_changed") }
	}
	if ctx.Err()!=nil { return ownerRuleUnknown("installed_tool_cancelled") }
	return ownerRuleClear("verified_installed_development_tool")
}

func ownerInstalledToolAliases(actual, expected []string) bool {
	if len(actual)!=len(expected) { return false }
	a,b:=append([]string(nil),actual...),append([]string(nil),expected...)
	for i:=range a {
		if !filepath.IsAbs(a[i]) || ownerWindowsAlias(a[i]) { return false }
		a[i]=strings.ToLower(filepath.Clean(a[i]))
	}
	for i:=range b { b[i]=strings.ToLower(filepath.Clean(b[i])) }
	sort.Strings(a);sort.Strings(b)
	for i:=range a { if a[i]!=b[i] || i>0 && a[i]==a[i-1] { return false } }
	return true
}

type ownerInstalledToolNative struct{}

func (ownerInstalledToolNative) Origin(ctx context.Context, plan ownerInstalledToolPlan) (failure error) {
	if ctx.Err()!=nil { return ctx.Err() }
	name,err:=syscall.UTF16PtrFromString(plan.registry)
	if err!=nil { return errOwnerInstalledTool }
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE,name,0,syscall.KEY_QUERY_VALUE|0x0100,&key)!=nil { return errOwnerInstalledTool }
	defer func(){ownerInstalledCloseResult(&failure,syscall.RegCloseKey(key))}()
	for field,want:=range map[string]string{"DisplayVersion":plan.version,"Publisher":plan.publisher} {
		if ctx.Err()!=nil { return ctx.Err() }
		p,_:=syscall.UTF16PtrFromString(field)
		var kind uint32
		var data [4096]byte
		n:=uint32(len(data))
		if syscall.RegQueryValueEx(key,p,nil,&kind,&data[0],&n)!=nil || kind!=syscall.REG_SZ || n<2 || n>uint32(len(data)) || n%2!=0 { return errOwnerInstalledTool }
		words:=unsafe.Slice((*uint16)(unsafe.Pointer(&data[0])),int(n)/2)
		if words[len(words)-1]!=0 || syscall.UTF16ToString(words)!=want { return errOwnerInstalledTool }
	}
	if ctx.Err()!=nil { return ctx.Err() }
	return nil
}

func (ownerInstalledToolNative) Aliases(ctx context.Context, path string) (names []string,failure error) {
	if ctx.Err()!=nil { return nil,ctx.Err() }
	p,err:=syscall.UTF16PtrFromString(path)
	if err!=nil { return nil,errOwnerInstalledTool }
	var buffer [32768]uint16
	n:=uint32(len(buffer))
	k:=syscall.NewLazyDLL("kernel32.dll")
	h,_,_:=k.NewProc("FindFirstFileNameW").Call(uintptr(unsafe.Pointer(p)),0,uintptr(unsafe.Pointer(&n)),uintptr(unsafe.Pointer(&buffer[0])))
	if h==^uintptr(0) { return nil,errOwnerInstalledTool }
	defer func(){
		ok,_,_:=k.NewProc("FindClose").Call(h)
		if ok==0 { ownerInstalledCloseResult(&failure,errOwnerInstalledTool);names=nil }
	}()
	for len(names)<8 && ctx.Err()==nil {
		if n==0 || n>=uint32(len(buffer)) { return nil,errOwnerInstalledTool }
		names=append(names,filepath.VolumeName(path)+syscall.UTF16ToString(buffer[:n]))
		n=uint32(len(buffer))
		ok,_,callErr:=k.NewProc("FindNextFileNameW").Call(h,uintptr(unsafe.Pointer(&n)),uintptr(unsafe.Pointer(&buffer[0])))
		if ok==0 { if callErr==syscall.Errno(38) { return names,nil };return nil,errOwnerInstalledTool }
	}
	if ctx.Err()!=nil { return nil,ctx.Err() }
	return nil,errOwnerInstalledTool
}

type ownerInstalledToolFile struct {
	f *os.File
	parents []ownerInstalledToolParent
}

type ownerInstalledToolParent struct { file *os.File; path string }

type ownerInstalledToken interface {
	SID() (string,error)
	Close() error
}
type ownerInstalledNativeToken struct { token syscall.Token }
func (t ownerInstalledNativeToken) SID() (string,error) {
	user,err:=t.token.GetTokenUser()
	if err!=nil { return "",errOwnerInstalledTool }
	sid,err:=user.User.Sid.String()
	if err!=nil { return "",errOwnerInstalledTool }
	return sid,nil
}
func (t ownerInstalledNativeToken) Close() error { return t.token.Close() }

func ownerInstalledTokenSID(token ownerInstalledToken) (sid string,failure error) {
	if token==nil { return "",errOwnerInstalledTool }
	defer func(){
		ownerInstalledCloseResult(&failure,token.Close())
		if failure!=nil { sid="" }
	}()
	return token.SID()
}

func (ownerInstalledToolNative) Open(ctx context.Context,path string) (ownerInstalledToolLease,error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	lease:=&ownerInstalledToolFile{}
	var dirs []string
	for d:=filepath.Dir(path);;d=filepath.Dir(d) { dirs=append(dirs,d);if filepath.Dir(d)==d { break } }
	for i:=len(dirs)-1;i>=0;i-- {
		p,err:=syscall.UTF16PtrFromString(dirs[i]);if err!=nil { return lease,errOwnerInstalledTool }
		h,err:=syscall.CreateFile(p,0x20080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.OPEN_EXISTING,0x02200000,0)
		if err!=nil { return lease,errOwnerInstalledTool }
		parentFile:=os.NewFile(uintptr(h),dirs[i]);lease.parents=append(lease.parents,ownerInstalledToolParent{parentFile,dirs[i]})
		var info syscall.ByHandleFileInformation
		actual,pathErr:=ownerHandlePath(h)
		if syscall.GetFileInformationByHandle(h,&info)!=nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 || pathErr!=nil || !same(actual,dirs[i]) { return lease,errOwnerInstalledTool }
		if err:=ctx.Err();err!=nil { return lease,err }
	}
	if err:=ctx.Err();err!=nil { return lease,err }
	p,err:=syscall.UTF16PtrFromString(path);if err!=nil { return lease,errOwnerInstalledTool }
	h,err:=syscall.CreateFile(p,syscall.GENERIC_READ|0x20000,syscall.FILE_SHARE_READ,nil,syscall.OPEN_EXISTING,0x00200000,0)
	if err!=nil { return lease,errOwnerInstalledTool };lease.f=os.NewFile(uintptr(h),path)
	return lease,nil
}

func (f *ownerInstalledToolFile) State() (ownerInstalledToolState,error) {
	if f.f==nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	var i syscall.ByHandleFileInformation
	h:=syscall.Handle(f.f.Fd())
	if syscall.GetFileInformationByHandle(h,&i)!=nil || i.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT)!=0 { return ownerInstalledToolState{},errOwnerInstalledTool }
	resolved,err:=ownerHandlePath(h);if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	acl,err:=skillsUpdateSDDL(f.f);if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	token,err:=syscall.OpenCurrentProcessToken();if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	sid,err:=ownerInstalledTokenSID(ownerInstalledNativeToken{token})
	if err!=nil { return ownerInstalledToolState{},err }
	parentProof,parentsAllowed,err:=ownerInstalledToolParents(f.parents,sid)
	if err!=nil { return ownerInstalledToolState{},errOwnerInstalledTool }
	return ownerInstalledToolState{id:ownerInstalledToolIdentity{uint64(i.VolumeSerialNumber),uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow),uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime)},size:int64(uint64(i.FileSizeHigh)<<32|uint64(i.FileSizeLow)),links:i.NumberOfLinks,acl:hashBytes([]byte(acl)),resolved:resolved,aclAllowed:ownerInstalledToolACL(acl,sid),parentProof:parentProof,parentsAllowed:parentsAllowed},nil
}

func ownerInstalledToolParents(parents []ownerInstalledToolParent,user string) (string,bool,error) {
	if len(parents)==0 || len(parents)>64 { return "",false,errOwnerInstalledTool }
	var proof strings.Builder
	allowed:=true
	for _,parent:=range parents {
		if parent.file==nil { return "",false,errOwnerInstalledTool }
		h:=syscall.Handle(parent.file.Fd())
		var i syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h,&i)!=nil || i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 || i.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 { return "",false,errOwnerInstalledTool }
		actual,err:=ownerHandlePath(h);if err!=nil || !same(actual,parent.path) { return "",false,errOwnerInstalledTool }
		acl,err:=skillsUpdateSDDL(parent.file);if err!=nil { return "",false,errOwnerInstalledTool }
		if !ownerInstalledToolACLMode(acl,user,true) { allowed=false }
		// IDs, names and descriptors come from the retained parent objects.
		for _,v:=range []uint64{uint64(i.VolumeSerialNumber),uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow),uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime),uint64(i.FileAttributes)} { proof.WriteString(strconv.FormatUint(v,10));proof.WriteByte(':') }
		name:=strings.ToLower(filepath.Clean(actual));proof.WriteString(strconv.Itoa(len(name)));proof.WriteByte(':');proof.WriteString(name);proof.WriteByte(':');proof.WriteString(hashBytes([]byte(acl)));proof.WriteByte(';')
	}
	return hashBytes([]byte(proof.String())),allowed,nil
}

func (f *ownerInstalledToolFile) Read(ctx context.Context,size int64) ([]byte,error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	if f.f==nil { return nil,errOwnerInstalledTool }
	return ownerInstalledToolRead(ctx,f.f,size)
}

// The controller owns this bounded loop. The native receiver supplies its
// already-held file; only tests substitute a deterministic ReaderAt.
func ownerInstalledToolRead(ctx context.Context,reader io.ReaderAt,size int64) (raw []byte,failure error) {
	if err:=ctx.Err();err!=nil { return nil,err }
	if reader==nil || size<=0 || size>64<<20 { return nil,errOwnerInstalledTool }
	raw=make([]byte,int(size))
	defer func(){if failure!=nil { clear(raw);raw=nil }}()
	for offset:=0;offset<len(raw); {
		if err:=ctx.Err();err!=nil { return raw,err }
		end:=offset+65536;if end>len(raw) { end=len(raw) }
		n,err:=reader.ReadAt(raw[offset:end],int64(offset));offset+=n
		if err!=nil && !(err==io.EOF && offset==len(raw)) || n==0 { return raw,errOwnerInstalledTool }
	}
	if len(raw)<64 || !bytes.Equal(raw[:2],[]byte("MZ")) { return raw,errOwnerInstalledTool }
	// The entire buffer is hashed by the controller, never by a second open.
	return raw,nil
}

func ownerInstalledToolACL(sddl, user string) bool {
	return ownerInstalledToolACLMode(sddl,user,false)
}

func ownerInstalledToolACLMode(sddl, user string,directory bool) bool {
	trusted:=func(sid string)bool{return sid=="BA" || sid=="SY" || user!="" && sid==user || sid=="S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"}
	if !strings.HasPrefix(sddl,"O:") { return false }
	ownerEnd:=strings.Index(sddl,"G:");d:=strings.Index(sddl,"D:")
	if d<2 { return false };if ownerEnd<0 || ownerEnd>d { ownerEnd=d }
	if !trusted(sddl[2:ownerEnd]) { return false }
	dacl:=sddl[d+2:];if end:=strings.Index(dacl,"S:");end>=0 { dacl=dacl[:end] }
	start:=strings.IndexByte(dacl,'(');if start<0 { return false }
	flags:=dacl[:start]
	for _,flag:=range []string{"AI","AR","P"}{flags=strings.ReplaceAll(flags,flag,"")}
	if flags!="" { return false };dacl=dacl[start:]
	writes:=uint64(0x500d0116)
	if directory { writes|=0x40 } // FILE_DELETE_CHILD, in addition to add/write/ACL rights.
	rights:=map[string]uint64{"FA":0x1f01ff,"FR":0x120089,"FX":0x1200a0,"GA":0x10000000,"GW":0x40000000,"GR":0x80000000,"GX":0x20000000,"RC":0x20000,"SD":0x10000,"WD":0x40000,"WO":0x80000}
	aces:=0
	for dacl!="" {
		if dacl[0]!='(' { return false };end:=strings.IndexByte(dacl,')');if end<0 { return false }
		fields:=strings.Split(dacl[1:end],";");if len(fields)!=6 || fields[0]!="A" && fields[0]!="D" || fields[3]!="" || fields[4]!="" || fields[5]=="" { return false }
		var mask uint64
		if strings.HasPrefix(fields[2],"0x") { n,err:=strconv.ParseUint(fields[2][2:],16,32);if err!=nil { return false };mask=n } else {
			if len(fields[2])==0 || len(fields[2])%2!=0 { return false }
			for n:=0;n<len(fields[2]);n+=2 { bits,ok:=rights[fields[2][n:n+2]];if !ok { return false };mask|=bits }
		}
		aceFlags:=fields[1]
		for _,flag:=range []string{"OI","CI","NP","IO","ID"} { aceFlags=strings.ReplaceAll(aceFlags,flag,"") }
		if aceFlags!="" { return false }
		// INHERIT_ONLY is not effective on this already-existing object.
		// The actual child/leaf descriptor is independently checked as well.
		if fields[0]=="A" && !strings.Contains(fields[1],"IO") && mask&writes!=0 && !trusted(fields[5]) { return false }
		aces++;dacl=dacl[end+1:]
	}
	return aces>0
}

func (f *ownerInstalledToolFile) Close() error {
	var failed bool
	if f.f!=nil { if f.f.Close()!=nil { failed=true };f.f=nil }
	for i:=len(f.parents)-1;i>=0;i-- { if f.parents[i].file.Close()!=nil { failed=true } }
	f.parents=nil
	if failed { return errOwnerInstalledTool };return nil
}
