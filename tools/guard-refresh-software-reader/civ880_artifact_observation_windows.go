//go:build windows

package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"strconv"
	"syscall"
	"sync"
	"time"
	"unicode/utf8"
	"unicode"
	"unsafe"
	"golang.org/x/sys/windows"
)

// Parsing and comparison establish observations, not authority. Acquisition
// requires a separate authenticated owner; stage operations retain their own
// file handles. None of these operations executes software or issues a purpose.
// Caller projections and nil errors are not grants. The Civ-specific normal
// factory and dispatcher are not connected by this observation slice.
const (
	civ880RepositoryID = int64(1392152644)
	civ880SourceCommit = "6a77a4ae7b9368a45a17ba0a5ac2ae72529355a1"
	civ880SourceTree = "c52c78ebce0b84891bca5df78cbe7fe279f87282"
	civ880MaxCargoStream = 32 << 20
	civ880MaxCargoLine = 2 << 20
	civ880MaxArchive = 512 << 20
	civ880MaxPE = 128 << 20
	civ880MaxInventoryRows = 200000
	civ880MaxInventoryBytes = 64 << 20
)

var errCiv880Observation = errors.New("civ880_observation_unqualified")
var errCiv880StagePreserved = errors.New("civ880_stage_preserved_reconciliation_required")

type civ880CargoPackageIDs struct {
	Autotest string
	Bootstrap string
	Bridge string
}

type civ880RoleSpec struct {
	role string
	packageName string
	target string
	filename string
	dll bool
}

func civ880RoleSpecs() [5]civ880RoleSpec {
	return [5]civ880RoleSpec{
		{"autotest", "civ6-autotest-rs", "civ6-autotest", "civ6-autotest.exe", false},
		{"diagnostics", "civ6-autotest-rs", "civ6-startup-diagnostics", "civ6-startup-diagnostics.exe", false},
		{"scene", "civ6-autotest-rs", "civ6-bootstrap-scene", "civ6-bootstrap-scene.exe", false},
		{"bridge", "civ6-native-bridge-rs", "civ6_native_bridge_rs", "civ6_native_bridge_rs.dll", true},
		{"bootstrap", "civ6-bootstrap-rs", "civ6_bootstrap_rs", "civ6_bootstrap_rs.dll", true},
	}
}

type civ880CargoProfile struct {
	OptLevel string
	DebugAssertions bool
	OverflowChecks bool
	DebugInfo string
}

type civ880CargoObservation struct {
	Role string
	PackageID string
	Target string
	Kind []string
	CrateTypes []string
	Profile civ880CargoProfile
	Fresh bool
	Candidates []string
	Executable string
}

func civ880Object(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') { return nil, errCiv880Observation }
	m := make(map[string]json.RawMessage)
	for dec.More() {
		token, err = dec.Token()
		key, ok := token.(string)
		if err != nil || !ok { return nil, errCiv880Observation }
		if _, exists := m[key]; exists { return nil, errCiv880Observation }
		var value json.RawMessage
		if dec.Decode(&value) != nil { return nil, errCiv880Observation }
		m[key] = value
	}
	token, err = dec.Token()
	if err != nil || token != json.Delim('}') || dec.Decode(new(any)) != io.EOF { return nil, errCiv880Observation }
	return m, nil
}

// Duplicate object keys make a server or manifest record ambiguous even when
// a particular decoder would keep the last value. Bound nesting as well as
// byte length before using typed projections.
func civ880UniqueJSON(raw []byte) bool {
	decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.UseNumber()
	var value func(int)bool
	value=func(depth int)bool{
		if depth>64{return false}
		token,err:=decoder.Token();if err!=nil{return false}
		delim,ok:=token.(json.Delim);if !ok{return true}
		switch delim{
		case '{':
			keys:=map[string]bool{}
			for decoder.More(){keyToken,err:=decoder.Token();key,ok:=keyToken.(string);if err!=nil||!ok||keys[key]{return false};keys[key]=true;if !value(depth+1){return false}}
			end,err:=decoder.Token();return err==nil&&end==json.Delim('}')
		case '[':
			for decoder.More(){if !value(depth+1){return false}}
			end,err:=decoder.Token();return err==nil&&end==json.Delim(']')
		default:return false
		}
	}
	return value(0)&&decoder.Decode(new(any))==io.EOF
}

func civ880String(raw json.RawMessage) (string, error) {
	var s string
	if len(raw) == 0 || json.Unmarshal(raw, &s) != nil || s == "" || len(s) > 32768 || strings.IndexByte(s, 0) >= 0 { return "", errCiv880Observation }
	return s, nil
}

func civ880Bool(raw json.RawMessage) (bool, error) {
	var b bool
	if !bytes.Equal(raw, []byte("true")) && !bytes.Equal(raw, []byte("false")) { return false, errCiv880Observation }
	if json.Unmarshal(raw, &b) != nil { return false, errCiv880Observation }
	return b, nil
}

func civ880Strings(raw json.RawMessage, maximum int) ([]string, error) {
	var list []string
	if len(raw) == 0 || json.Unmarshal(raw, &list) != nil || len(list) == 0 || len(list) > maximum { return nil, errCiv880Observation }
	seen := make(map[string]bool)
	for _, s := range list {
		if s == "" || len(s) > 32768 || strings.IndexByte(s, 0) >= 0 || seen[s] { return nil, errCiv880Observation }
		seen[s] = true
	}
	return list, nil
}

func civ880SameSet(actual []string, expected ...string) bool {
	if len(actual) != len(expected) { return false }
	for _, want := range expected {
		found := false
		for _, s := range actual { if s == want { found = true; break } }
		if !found { return false }
	}
	return true
}

func civ880PackageID(ids civ880CargoPackageIDs, name string) string {
	switch name {
	case "civ6-autotest-rs": return ids.Autotest
	case "civ6-bootstrap-rs": return ids.Bootstrap
	case "civ6-native-bridge-rs": return ids.Bridge
	default: return ""
	}
}

func civ880InRoot(root, candidate string) bool {
	if !filepath.IsAbs(root) || !filepath.IsAbs(candidate) || filepath.Clean(root) != root || filepath.Clean(candidate) != candidate { return false }
	rel, err := filepath.Rel(root, candidate)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Selects only observations for the fixed five roles. Package IDs and root
// must later be derived from a separately authenticated source/producer owner;
// passing arbitrary strings here never issues a capability. No path is opened.
func civ880ObserveCargoPhase(raw []byte, ids civ880CargoPackageIDs, root string, dllPhase bool) ([5]civ880CargoObservation, error) {
	var out [5]civ880CargoObservation
	if len(raw) == 0 || len(raw) > civ880MaxCargoStream || !filepath.IsAbs(root) || filepath.Clean(root) != root { return out, errCiv880Observation }
	if ids.Autotest == "" || ids.Bootstrap == "" || ids.Bridge == "" || ids.Autotest == ids.Bootstrap || ids.Autotest == ids.Bridge || ids.Bootstrap == ids.Bridge { return out, errCiv880Observation }
	specs := civ880RoleSpecs()
	seen := [5]bool{}
	finished := false
	scan := bufio.NewScanner(bytes.NewReader(raw))
	scan.Buffer(make([]byte, 4096), civ880MaxCargoLine)
	for scan.Scan() {
		line := bytes.TrimSpace(scan.Bytes())
		if len(line) == 0 { continue }
		if len(line) > civ880MaxCargoLine { return [5]civ880CargoObservation{}, errCiv880Observation }
		// Cargo only controls its own JSON output; non-JSON tool output is not
		// a Cargo event and cannot supply one of the selected artifact records.
		if line[0] != '{' { continue }
		if finished { return [5]civ880CargoObservation{}, errCiv880Observation }
		if !civ880UniqueJSON(line) { return [5]civ880CargoObservation{}, errCiv880Observation }
		object, err := civ880Object(line)
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		reason, err := civ880String(object["reason"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		if reason == "build-finished" {
			success, err := civ880Bool(object["success"])
			if err != nil || !success { return [5]civ880CargoObservation{}, errCiv880Observation }
			finished = true
			continue
		}
		if reason != "compiler-artifact" { continue }
		target, err := civ880Object(object["target"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		name, err := civ880String(target["name"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		index := -1
		for i, spec := range specs { if spec.target == name { index = i; break } }
		if index < 0 { continue }
		spec := specs[index]
		// A binary build may emit library dependencies. Only the designated
		// phase can supply an observation for a selected role.
		pkg, err := civ880String(object["package_id"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		// Another package may use the same target name. It cannot provide the
		// required role and must not shadow the exact source-bound package.
		if pkg != civ880PackageID(ids, spec.packageName) { continue }
		if spec.dll != dllPhase { continue }
		if seen[index] { return [5]civ880CargoObservation{}, errCiv880Observation }
		fresh, err := civ880Bool(object["fresh"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		kind, err := civ880Strings(target["kind"], 8)
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		crateTypes, err := civ880Strings(target["crate_types"], 8)
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		if !civ880ValidKinds(target["kind"],spec.dll) || !civ880ValidKinds(target["crate_types"],spec.dll) { return [5]civ880CargoObservation{}, errCiv880Observation }
		profile, err := civ880Object(object["profile"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		test, err := civ880Bool(profile["test"])
		if err != nil || test { return [5]civ880CargoObservation{}, errCiv880Observation }
		opt, err := civ880String(profile["opt_level"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		debug, err := civ880Bool(profile["debug_assertions"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		overflow, err := civ880Bool(profile["overflow_checks"])
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		debugInfo, exists := profile["debuginfo"]
		if !exists || len(debugInfo) > 128 || !civ880ObservedProfile(profile,object) { return [5]civ880CargoObservation{}, errCiv880Observation }
		files, err := civ880Strings(object["filenames"], 64)
		if err != nil { return [5]civ880CargoObservation{}, errCiv880Observation }
		var candidates []string
		for _, file := range files {
			if !strings.EqualFold(filepath.Base(file), spec.filename) { continue }
			if !civ880InRoot(root, file) { return [5]civ880CargoObservation{}, errCiv880Observation }
			for _, previous := range candidates { if strings.EqualFold(previous, file) { return [5]civ880CargoObservation{}, errCiv880Observation } }
			candidates = append(candidates, file)
		}
		if len(candidates) == 0 || len(candidates) > 8 { return [5]civ880CargoObservation{}, errCiv880Observation }
		executable := ""
		if spec.dll {
			if value, ok := object["executable"]; ok && !bytes.Equal(bytes.TrimSpace(value), []byte("null")) { return [5]civ880CargoObservation{}, errCiv880Observation }
		} else {
			executable, err = civ880String(object["executable"])
			if err != nil || !civ880InRoot(root, executable) { return [5]civ880CargoObservation{}, errCiv880Observation }
			found := false
			for _, candidate := range candidates { if strings.EqualFold(candidate, executable) { found = true } }
			if !found { return [5]civ880CargoObservation{}, errCiv880Observation }
		}
		out[index] = civ880CargoObservation{Role:spec.role, PackageID:pkg, Target:name, Kind:kind, CrateTypes:crateTypes, Profile:civ880CargoProfile{OptLevel:opt, DebugAssertions:debug, OverflowChecks:overflow, DebugInfo:string(debugInfo)}, Fresh:fresh, Candidates:candidates, Executable:executable}
		seen[index] = true
	}
	if scan.Err() != nil || !finished { return [5]civ880CargoObservation{}, errCiv880Observation }
	for i, spec := range specs { if spec.dll == dllPhase && !seen[i] { return [5]civ880CargoObservation{}, errCiv880Observation } }
	return out, nil
}

// Each Cargo phase must independently end successfully. This does not
// authenticate the producer or authorize any current output object.
// In-memory Cargo stream fixture comparison. Actual acquisition currently
// verifies publisher-recorded stream digests, not absent raw Cargo JSON.
func civ880ObserveCargoStreams(bins, dlls []byte, ids civ880CargoPackageIDs, root string) ([5]civ880CargoObservation, error) {
	var out [5]civ880CargoObservation
	binRows, err := civ880ObserveCargoPhase(bins, ids, root, false)
	if err != nil { return out, err }
	dllRows, err := civ880ObserveCargoPhase(dlls, ids, root, true)
	if err != nil { return out, err }
	for i, spec := range civ880RoleSpecs() { if spec.dll { out[i] = dllRows[i] } else { out[i] = binRows[i] } }
	return out, nil
}

// Checks a bounded DOS/PE+ header for the fixed role, without loading a DLL.
// This is not an Authenticode, export, ABI, origin, file-identity, ACL, or full
// PE validation. It accepts bytes only; it never opens a caller-supplied path.
func civ880ObservePEHeader(raw []byte, role string) error {
	if len(raw) < 64 || len(raw) > civ880MaxPE || raw[0] != 'M' || raw[1] != 'Z' { return errCiv880Observation }
	isDLL, found := false, false
	for _, spec := range civ880RoleSpecs() { if spec.role == role { isDLL, found = spec.dll, true; break } }
	if !found { return errCiv880Observation }
	off := uint64(binary.LittleEndian.Uint32(raw[0x3c:0x40]))
	if off > uint64(len(raw)) || uint64(len(raw))-off < 26 { return errCiv880Observation }
	p := raw[off:]
	if !bytes.Equal(p[:4], []byte{'P','E',0,0}) || binary.LittleEndian.Uint16(p[4:6]) != 0x8664 { return errCiv880Observation }
	sections := uint64(binary.LittleEndian.Uint16(p[6:8]))
	optional := uint64(binary.LittleEndian.Uint16(p[20:22]))
	flags := binary.LittleEndian.Uint16(p[22:24])
	if sections == 0 || sections > 96 || optional < 112 || flags&0x0002 == 0 || (flags&0x2000 != 0) != isDLL { return errCiv880Observation }
	if 24+optional+sections*40 > uint64(len(p)) || binary.LittleEndian.Uint16(p[24:26]) != 0x20b { return errCiv880Observation }
	return nil
}

type civ880RequiredExport struct { name string; function bool }

func civ880RequiredExports(role string) []civ880RequiredExport {
	switch role {
	case "bridge":return []civ880RequiredExport{{"BridgeRuntimeInstall",true},{"BridgeRuntimeClose",true},{"BridgeRuntimeRebuild",true},{"BridgeRuntimeStopObservation",true},{"BRIDGE_RUNTIME_PROPERTY_LOG",false},{"BRIDGE_RUNTIME_NATIVE_FRAME_TRACE",false},{"BRIDGE_RUNTIME_NATIVE_CENSUS",false},{"BRIDGE_RUNTIME_NATIVE_CENSUS_SIZE",false}}
	case "bootstrap":return []civ880RequiredExport{{"RustBridgeBootstrapPrepare",true},{"RustBridgeBootstrapStopObservation",true},{"BOOTSTRAP_TRACE_POINTER",false},{"BOOTSTRAP_PREPARE_DIAGNOSTIC_POINTER",false},{"BOOTSTRAP_PREPARE_DIAGNOSTIC_SIZE",false}}
	case "autotest","diagnostics","scene":return []civ880RequiredExport{}
	default:return nil
	}
}

type civ880PESection struct { start,virtualSize,rawSize,rawOffset,flags uint64 }

// Independently inspect the fixed export names and code/data placement. This
// checks bytes only; it neither loads a library nor proves runtime ABI safety.
func civ880ObservePEExports(raw []byte, role string) (map[string]uint32,error) {
	if civ880ObservePEHeader(raw,role)!=nil{return nil,errCiv880Observation}
	required:=civ880RequiredExports(role);if required==nil{return nil,errCiv880Observation}
	read16:=func(offset uint64)(uint16,bool){if offset>uint64(len(raw))||uint64(len(raw))-offset<2{return 0,false};return binary.LittleEndian.Uint16(raw[offset:offset+2]),true}
	read32:=func(offset uint64)(uint32,bool){if offset>uint64(len(raw))||uint64(len(raw))-offset<4{return 0,false};return binary.LittleEndian.Uint32(raw[offset:offset+4]),true}
	pe32,ok:=read32(0x3c);if !ok{return nil,errCiv880Observation};pe:=uint64(pe32)
	count,ok:=read16(pe+6);if !ok{return nil,errCiv880Observation};optionalSize,ok:=read16(pe+20);if !ok||optionalSize<120{return nil,errCiv880Observation}
	optional:=pe+24;table:=optional+uint64(optionalSize);sections:=make([]civ880PESection,0,count)
	for i:=uint64(0);i<uint64(count);i++ {
		entry:=table+i*40
		virtualSize,a:=read32(entry+8);start,b:=read32(entry+12);rawSize,c:=read32(entry+16);rawOffset,d:=read32(entry+20);flags,e:=read32(entry+36)
		if !a||!b||!c||!d||!e||uint64(rawOffset)>uint64(len(raw))||uint64(rawSize)>uint64(len(raw))-uint64(rawOffset){return nil,errCiv880Observation}
		span:=uint64(virtualSize);if uint64(rawSize)>span{span=uint64(rawSize)}
		if uint64(start)+span>1<<32{return nil,errCiv880Observation}
		sections=append(sections,civ880PESection{uint64(start),uint64(virtualSize),uint64(rawSize),uint64(rawOffset),uint64(flags)})
	}
	mapRVA:=func(rva,length uint64)(uint64,bool,bool){
		var offset uint64;var executable,found bool
		for _,s:=range sections {if rva<s.start{continue};relative:=rva-s.start;virtual:=s.virtualSize;if virtual==0{virtual=s.rawSize};if relative>s.rawSize||length>s.rawSize-relative||relative>virtual||length>virtual-relative{continue};if found{return 0,false,false};offset=s.rawOffset+relative;executable=s.flags&0x20000000!=0;found=true}
		return offset,executable,found
	}
	if len(required)==0{return map[string]uint32{},nil}
	directories,ok:=read32(optional+108);if !ok||directories==0{return nil,errCiv880Observation}
	exportRVA,ok:=read32(optional+112);if !ok{return nil,errCiv880Observation};exportSize,ok:=read32(optional+116);if !ok||exportSize<40||uint64(exportRVA)+uint64(exportSize)>1<<32{return nil,errCiv880Observation}
	directory,_,ok:=mapRVA(uint64(exportRVA),40);if !ok{return nil,errCiv880Observation}
	functions,a:=read32(directory+20);names,b:=read32(directory+24);funcRVA,c:=read32(directory+28);nameRVA,d:=read32(directory+32);ordinalRVA,e:=read32(directory+36)
	if !a||!b||!c||!d||!e||functions==0||functions>65536||names>functions{return nil,errCiv880Observation}
	functionsAt,_,a:=mapRVA(uint64(funcRVA),uint64(functions)*4);namesAt,_,b:=mapRVA(uint64(nameRVA),uint64(names)*4);ordinalsAt,_,c:=mapRVA(uint64(ordinalRVA),uint64(names)*2)
	if !a||!b||!c{return nil,errCiv880Observation}
	found:=map[string]uint32{}
	for i:=uint64(0);i<uint64(names);i++ {
		nameRVA,ok:=read32(namesAt+i*4);if !ok{return nil,errCiv880Observation};nameAt,_,ok:=mapRVA(uint64(nameRVA),1);if !ok{return nil,errCiv880Observation}
		end:=nameAt;for end<uint64(len(raw))&&end-nameAt<256&&raw[end]!=0{end++}
		if end==uint64(len(raw))||end-nameAt>=256{return nil,errCiv880Observation}
		_,_,ok=mapRVA(uint64(nameRVA),end-nameAt+1);if !ok{return nil,errCiv880Observation}
		if !utf8.Valid(raw[nameAt:end]){return nil,errCiv880Observation}
		name:=string(raw[nameAt:end]);ordinal,ok:=read16(ordinalsAt+i*2);if !ok||uint32(ordinal)>=functions{return nil,errCiv880Observation}
		rva,ok:=read32(functionsAt+uint64(ordinal)*4);if !ok{return nil,errCiv880Observation}
		if _,exists:=found[name];exists{return nil,errCiv880Observation};found[name]=rva
	}
	selected:=map[string]uint32{}
	for _,want:=range required {
		rva,ok:=found[want.name];if !ok||rva==0||uint64(rva)>=uint64(exportRVA)&&uint64(rva)<uint64(exportRVA)+uint64(exportSize){return nil,errCiv880Observation}
		var executable bool;ok=false
		if want.function {_,executable,ok=mapRVA(uint64(rva),1)} else {
			for _,s:=range sections {if uint64(rva)<s.start{continue};relative:=uint64(rva)-s.start;size:=s.virtualSize;if s.rawSize>size{size=s.rawSize};if relative<=size&&8<=size-relative{if ok{return nil,errCiv880Observation};executable=s.flags&0x20000000!=0;ok=true}}
		}
		if !ok||executable!=want.function{return nil,errCiv880Observation};selected[want.name]=rva
	}
	return selected,nil
}

func civ880ManifestExports(raw json.RawMessage, role string, actual map[string]uint32) bool {
	required:=civ880RequiredExports(role);if required==nil||len(actual)!=len(required){return false}
	rows,err:=civ880RawArray(raw,len(required));if err!=nil{return false}
	seen:=map[string]bool{}
	for _,value:=range rows {
		row,err:=civ880Object(value);if err!=nil{return false}
		name,e1:=civ880String(row["name"]);kind,e2:=civ880String(row["kind"]);rva,e3:=civ880PositiveInt(row["rva"])
		if e1!=nil||e2!=nil||e3!=nil||rva>int64(^uint32(0))||seen[name]||actual[name]!=uint32(rva){return false}
		found:=false;for _,want:=range required{if want.name==name{expected:="data";if want.function{expected="function"};if kind!=expected{return false};found=true;break}}
		if !found{return false};seen[name]=true
	}
	return len(seen)==len(required)
}

func civ880LowerHex(s string, length int) bool {
	if len(s) != length || strings.ToLower(s) != s { return false }
	b, err := hex.DecodeString(s)
	if err != nil || len(b)*2 != length { return false }
	for _, value := range b { if value != 0 { return true } }
	return false
}

type civ880RunProjection struct {
	RepositoryID int64
	HeadRepositoryID int64
	HeadSHA, Event, HeadBranch string
	WorkflowID int64
	WorkflowPath string
	RunID int64
	Attempt int
	Status string
	Conclusion string
}

type civ880JobProjection struct {
	RunID int64
	Attempt int
	JobID int64
	CheckRunID int64
	Status string
	Conclusion string
}

type civ880ArtifactProjection struct {
	RepositoryID int64
	HeadRepositoryID int64
	RunID int64
	ID int64
	Name string
	Bytes int64
	Digest string
	Expired bool
}

type civ880PublicationProjection struct {
	RepositoryID int64
	SourceCommit string
	SourceTree string
	ProducerRevision string
	WorkflowID int64
	WorkflowPath string
	RunID int64
	Attempt int
	JobID int64
	CheckRunID int64
	ArtifactID int64
	ArtifactName string
	SourceInputsSHA256 string
	OutputManifestSHA256 string
	ArchiveSHA256 string
}

// A consistency check on projections, not an issuer. Even mutually matching
// caller-supplied JSON can be forged. A normal authenticated owner must still
// obtain all server responses, establish the reviewed workflow/producer H1,
// validate source inputs, and hold the same archive before any purpose is issued.
// Historical fixture comparator only; the production acquisition validates
// its observed source/server/archive tuple through civ880OriginMatches.
func civ880CompareUntrustedProjections(run civ880RunProjection, job civ880JobProjection, artifact civ880ArtifactProjection, pub civ880PublicationProjection, sourceInputs, outputManifest, archive []byte) error {
	if run.RepositoryID != civ880RepositoryID || run.HeadRepositoryID != civ880RepositoryID || pub.RepositoryID != civ880RepositoryID || artifact.RepositoryID != civ880RepositoryID || artifact.HeadRepositoryID != civ880RepositoryID { return errCiv880Observation }
	if pub.SourceCommit != civ880SourceCommit || pub.SourceTree != civ880SourceTree || !civ880LowerHex(pub.ProducerRevision, 40) || run.HeadSHA != pub.ProducerRevision { return errCiv880Observation }
	if run.WorkflowID <= 0 || run.WorkflowID != pub.WorkflowID || run.WorkflowPath == "" || run.WorkflowPath != pub.WorkflowPath || !strings.HasPrefix(run.WorkflowPath, ".github/workflows/") { return errCiv880Observation }
	if run.RunID <= 0 || run.Attempt <= 0 || job.JobID <= 0 || job.CheckRunID <= 0 || artifact.ID <= 0 || artifact.Name == "" { return errCiv880Observation }
	if run.RunID != job.RunID || run.RunID != artifact.RunID || run.RunID != pub.RunID || run.Attempt != job.Attempt || run.Attempt != pub.Attempt || job.JobID != pub.JobID || job.CheckRunID != pub.CheckRunID || artifact.ID != pub.ArtifactID || artifact.Name != pub.ArtifactName { return errCiv880Observation }
	if run.Status != "completed" || run.Conclusion != "success" || job.Status != "completed" || job.Conclusion != "success" || artifact.Expired { return errCiv880Observation }
	if !civ880LowerHex(pub.SourceInputsSHA256, 64) || !civ880LowerHex(pub.OutputManifestSHA256, 64) || !civ880LowerHex(pub.ArchiveSHA256, 64) { return errCiv880Observation }
	if len(sourceInputs) == 0 || len(sourceInputs) > 16<<20 || len(outputManifest) == 0 || len(outputManifest) > 16<<20 { return errCiv880Observation }
	sourceHash := sha256.Sum256(sourceInputs)
	outputHash := sha256.Sum256(outputManifest)
	if hex.EncodeToString(sourceHash[:]) != pub.SourceInputsSHA256 || hex.EncodeToString(outputHash[:]) != pub.OutputManifestSHA256 { return errCiv880Observation }
	if len(archive) == 0 || len(archive) > civ880MaxArchive || artifact.Bytes != int64(len(archive)) || artifact.Digest != "sha256:"+pub.ArchiveSHA256 { return errCiv880Observation }
	h := sha256.Sum256(archive)
	if hex.EncodeToString(h[:]) != pub.ArchiveSHA256 { return errCiv880Observation }
	return nil
}

const civ880ActionsURL = "https://api.github.com/repos/AutoDevJapan/civ6-banseiki/actions"
const civ880GitURL = "https://api.github.com/repos/AutoDevJapan/civ6-banseiki"

type civ880GitEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	SHA string `json:"sha"`
	Size *int64 `json:"size"`
}

func civ880GitJSON(ctx context.Context,client *http.Client,endpoint string,out any) error {
	if ctx==nil||ctx.Err()!=nil||client==nil||out==nil||(endpoint!=civ880GitURL&&!strings.HasPrefix(endpoint,civ880GitURL+"/git/")){return errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return errCiv880Observation}
	req.Header.Set("Accept","application/vnd.github+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
	clientCopy:=*client;clientCopy.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err:=clientCopy.Do(req);if err!=nil||response==nil||response.Body==nil{return errCiv880Observation}
	if response.StatusCode!=http.StatusOK||response.Header.Get("Link")!=""{_ = response.Body.Close();return errCiv880Observation}
	raw,readErr:=io.ReadAll(io.LimitReader(civ880ContextReader{ctx,response.Body},(16<<20)+1));closeErr:=response.Body.Close()
	if readErr!=nil||closeErr!=nil||ctx.Err()!=nil||len(raw)==0||len(raw)>16<<20{clear(raw);return errCiv880Observation};defer clear(raw)
	if !civ880UniqueJSON(raw){return errCiv880Observation}
	decoder:=json.NewDecoder(bytes.NewReader(raw));if decoder.Decode(out)!=nil||decoder.Decode(new(any))!=io.EOF{return errCiv880Observation}
	return nil
}

func civ880GitObjectID(kind string,raw []byte) string {
	h:=sha1.New();_,_ = fmt.Fprintf(h,"%s %d%c",kind,len(raw),byte(0));_,_ = h.Write(raw);return hex.EncodeToString(h.Sum(nil))
}

// Fetch a nonrecursive tree and independently hash its exact Git wire format.
func civ880GitTree(ctx context.Context,client *http.Client,id string) ([]civ880GitEntry,error) {
	if !civ880LowerHex(id,40){return nil,errCiv880Observation}
	var reply struct{SHA string `json:"sha"`;Truncated *bool `json:"truncated"`;Tree *[]civ880GitEntry `json:"tree"`}
	if civ880GitJSON(ctx,client,civ880GitURL+"/git/trees/"+id,&reply)!=nil||reply.SHA!=id||reply.Truncated==nil||*reply.Truncated||reply.Tree==nil||len(*reply.Tree)>civ880MaxInventoryRows{return nil,errCiv880Observation}
	entries:=append([]civ880GitEntry(nil),(*reply.Tree)...);seen:=map[string]bool{}
	for _,entry:=range entries {
		if ctx.Err()!=nil||entry.Path==""||entry.Path=="."||entry.Path==".."||!utf8.ValidString(entry.Path)||strings.ContainsAny(entry.Path,"/\\\x00")||seen[entry.Path]||!civ880LowerHex(entry.SHA,40){return nil,errCiv880Observation};seen[entry.Path]=true
		valid:=entry.Mode=="040000"&&entry.Type=="tree" || (entry.Mode=="100644"||entry.Mode=="100755"||entry.Mode=="120000")&&entry.Type=="blob"
		if !valid || entry.Type=="blob"&&(entry.Size==nil||*entry.Size<0||*entry.Size>512<<20){return nil,errCiv880Observation}
	}
	sort.Slice(entries,func(i,j int)bool{a,b:=entries[i].Path,entries[j].Path;if entries[i].Type=="tree"{a+="/"};if entries[j].Type=="tree"{b+="/"};return a<b})
	var wire bytes.Buffer
	for _,entry:=range entries{mode:=entry.Mode;if mode=="040000"{mode="40000"};object,err:=hex.DecodeString(entry.SHA);if err!=nil||len(object)!=sha1.Size{return nil,errCiv880Observation};_,_ = fmt.Fprintf(&wire,"%s %s%c",mode,entry.Path,byte(0));_,_ = wire.Write(object)}
	if ctx.Err()!=nil||civ880GitObjectID("tree",wire.Bytes())!=id{return nil,errCiv880Observation}
	return entries,nil
}

func civ880VerifyGitBlob(ctx context.Context,client *http.Client,row civ880SourceRecord) error {
	if ctx==nil||ctx.Err()!=nil||client==nil||!civ880LowerHex(row.GitBlobSHA1,40)||!civ880HexAllowZero(row.SHA256,64)||row.Bytes>512<<20{return errCiv880Observation}
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,civ880GitURL+"/git/blobs/"+row.GitBlobSHA1,nil);if err!=nil{return errCiv880Observation}
	req.Header.Set("Accept","application/vnd.github.raw+json");req.Header.Set("X-GitHub-Api-Version","2022-11-28")
	clientCopy:=*client;clientCopy.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err:=clientCopy.Do(req);if err!=nil||response==nil||response.Body==nil{return errCiv880Observation}
	if response.StatusCode!=http.StatusOK||response.Header.Get("Link")!=""{_ = response.Body.Close();return errCiv880Observation}
	sha1Hash,sha256Hash:=sha1.New(),sha256.New();_,_ = fmt.Fprintf(sha1Hash,"blob %d%c",row.Bytes,byte(0))
	var scratch [64<<10]byte;defer clear(scratch[:])
	n,readErr:=io.CopyBuffer(io.MultiWriter(sha1Hash,sha256Hash),io.LimitReader(civ880ContextReader{ctx,response.Body},int64(row.Bytes)+1),scratch[:]);closeErr:=response.Body.Close()
	if readErr!=nil||closeErr!=nil||ctx.Err()!=nil||n!=int64(row.Bytes)||hex.EncodeToString(sha1Hash.Sum(nil))!=row.GitBlobSHA1||hex.EncodeToString(sha256Hash.Sum(nil))!=row.SHA256{return errCiv880Observation}
	return nil
}

// Full H1 namespace: the commit/tree, every directory object, exact tracked
// path/mode/blob/size and each actual blob's SHA-1/SHA-256 are checked. This
// still does not establish that H1 was reviewed, admit toolchain inputs, or
// grant a target purpose. Checkout filters/normalization mismatches fail closed.
func civ880VerifyH1GitNamespace(ctx context.Context,client *http.Client,product *civ880ObservedProduct) error {
	if ctx==nil||ctx.Err()!=nil||client==nil||product==nil{return errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	product.mu.Lock();defer product.mu.Unlock()
	if product.closed||!civ880LowerHex(product.Source,40)||!civ880LowerHex(product.Tree,40)||len(product.SourceFiles)==0||len(product.SourceFiles)>civ880MaxInventoryRows{return errCiv880Observation}
	var repo struct{ID int64 `json:"id"`};if civ880GitJSON(ctx,client,civ880GitURL,&repo)!=nil||repo.ID!=civ880RepositoryID{return errCiv880Observation}
	var commit struct{SHA string `json:"sha"`;Tree struct{SHA string `json:"sha"`} `json:"tree"`}
	if civ880GitJSON(ctx,client,civ880GitURL+"/git/commits/"+product.Source,&commit)!=nil||commit.SHA!=product.Source||commit.Tree.SHA!=product.Tree{return errCiv880Observation}
	expected:=make(map[string]civ880SourceRecord,len(product.SourceFiles));folded:=map[string]bool{};var total uint64
	for _,row:=range product.SourceFiles{
		if !civ880InventoryName(row.Name)||!civ880LowerHex(row.GitBlobSHA1,40)||!civ880HexAllowZero(row.SHA256,64)||expected[row.Name].Name!=""||folded[strings.ToLower(row.Name)]||row.Bytes>512<<20{return errCiv880Observation}
		if total>(2<<30)-row.Bytes{return errCiv880Observation};total+=row.Bytes;expected[row.Name]=row;folded[strings.ToLower(row.Name)]=true
	}
	found:=map[string]bool{};trees:=0
	workflowFound:=false
	var walk func(string,string,int)error
	walk=func(treeID,prefix string,depth int)error{
		trees++;if ctx.Err()!=nil||depth>64||trees>civ880MaxInventoryRows{return errCiv880Observation}
		entries,err:=civ880GitTree(ctx,client,treeID);if err!=nil{return err}
		for _,entry:=range entries{
			name:=entry.Path;if prefix!=""{name=path.Join(prefix,entry.Path)}
			if !civ880InventoryName(name){return errCiv880Observation}
			if entry.Type=="tree"{if walk(entry.SHA,name,depth+1)!=nil{return errCiv880Observation};continue}
			row,ok:=expected[name];if !ok||found[name]||entry.Size==nil||uint64(*entry.Size)!=row.Bytes||entry.SHA!=row.GitBlobSHA1{return errCiv880Observation}
			if name==".github/workflows/rust.yml"{if entry.Mode!="100644"||entry.SHA!=product.WorkflowBlob{return errCiv880Observation};workflowFound=true}
			found[name]=true
		}
		return nil
	}
	if walk(product.Tree,"",0)!=nil||len(found)!=len(expected)||!workflowFound{return errCiv880Observation}
	// The same Git blob can appear at multiple paths; verify its actual bytes
	// once only if every occurrence also claims the same size and SHA-256.
	unique:=map[string]civ880SourceRecord{}
	for _,row:=range product.SourceFiles{if old,ok:=unique[row.GitBlobSHA1];ok&&(old.Bytes!=row.Bytes||old.SHA256!=row.SHA256){return errCiv880Observation};unique[row.GitBlobSHA1]=row}
	local,cancel:=context.WithCancel(ctx);defer cancel();jobs:=make(chan civ880SourceRecord);var wg sync.WaitGroup;var once sync.Once;failed:=false
	for i:=0;i<4;i++{wg.Add(1);go func(){defer wg.Done();for row:=range jobs{if local.Err()!=nil{return};if civ880VerifyGitBlob(local,client,row)!=nil{once.Do(func(){failed=true;cancel()});return}}}()}
	sent:=true
	for _,row:=range unique{select{case jobs<-row:case <-local.Done():sent=false};if !sent{break}}
	close(jobs);wg.Wait()
	if failed||!sent||ctx.Err()!=nil{return errCiv880Observation}
	return nil
}

// This reader obtains observations from the supplied transport. It does not
// construct that transport, authenticate it, establish the reviewed H1, or
// turn the resulting JSON into a purpose lease.
func civ880ServerJSON(ctx context.Context, client *http.Client, endpoint string, result any) error {
	if ctx==nil||ctx.Err()!=nil||client==nil||result==nil||!strings.HasPrefix(endpoint,civ880ActionsURL+"/"){return errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	request,err:=http.NewRequestWithContext(ctx,http.MethodGet,endpoint,nil);if err!=nil{return errCiv880Observation}
	request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
	once:=*client;once.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err:=once.Do(request);if err!=nil||response==nil||response.Body==nil{return errCiv880Observation}
	if response.StatusCode!=http.StatusOK||response.Header.Get("Link")!=""{_ = response.Body.Close();return errCiv880Observation}
	raw,readErr:=io.ReadAll(io.LimitReader(civ880ContextReader{ctx,response.Body},(2<<20)+1));closeErr:=response.Body.Close()
	if readErr!=nil||closeErr!=nil||len(raw)==0||len(raw)>2<<20||ctx.Err()!=nil{clear(raw);return errCiv880Observation}
	defer clear(raw)
	if !civ880UniqueJSON(raw){return errCiv880Observation}
	decoder:=json.NewDecoder(bytes.NewReader(raw));if decoder.Decode(result)!=nil||decoder.Decode(new(any))!=io.EOF{return errCiv880Observation}
	return nil
}

type civ880ServerObservation struct {
	Run civ880RunProjection
	Job civ880JobProjection
	Artifact civ880ArtifactProjection
}

type civ880OriginObservation struct {
	RepositoryID int64 `json:"repository_id"`
	Schema string `json:"schema"`
	Source string `json:"source_sha"`
	Tree string `json:"source_tree"`
	WorkflowBlob string `json:"workflow_blob"`
	RunID int64 `json:"run_id"`
	Attempt int `json:"run_attempt"`
	JobKey string `json:"job_key"`
	RunnerOS string `json:"runner_os"`
	ArtifactID int64 `json:"artifact_id"`
	ArtifactName string `json:"artifact_name"`
	UploadSHA string `json:"upload_reported_sha256"`
	ManifestSHA string `json:"manifest_sha256"`
	CargoSHA string `json:"cargo_metadata_sha256"`
}

// A capture event is an advisory historical snapshot, never a live receipt.
// Source, completed producer, origin, current archive and held-object checks
// remain independent requirements in the acquisition and readonly owners.
type civ880CaptureEvent struct {
	Schema                string          `json:"schema"`
	EvidenceKind          string          `json:"evidence_kind"`
	Phase                 string          `json:"phase"`
	ExitCode              *int            `json:"native_exit_code"`
	Complete              *bool           `json:"collection_complete"`
	StopReason            string          `json:"stop_reason"`
	CaptureStopReason     string          `json:"capture_stop_reason"`
	StartReturned         *bool           `json:"start_returned"`
	RootExitObserved      *bool           `json:"root_exit_observed"`
	FailureStage          json.RawMessage `json:"failure_stage"`
	ExceptionKind         json.RawMessage `json:"exception_kind"`
	ExceptionHRESULT      json.RawMessage `json:"exception_hresult"`
	NativeErrorCode       json.RawMessage `json:"native_error_code"`
	ResultSaveState       string          `json:"result_save_state"`
	ResultBytes           *int64          `json:"result_bytes"`
	ResultSHA             string          `json:"result_sha256"`
	SecondaryFailureCount *int64          `json:"secondary_failure_count"`
	StdoutState           string          `json:"stdout_state"`
	StderrState           string          `json:"stderr_state"`
	StdoutBytes           *int64          `json:"stdout_bytes"`
	StdoutSHA             string          `json:"stdout_sha256"`
	StderrBytes           *int64          `json:"stderr_bytes"`
	StderrSHA             string          `json:"stderr_sha256"`
}

func civ880CaptureEvents(raw []byte) ([2]civ880CaptureEvent, error) {
	var result [2]civ880CaptureEvent
	seen := [2]bool{}
	if len(raw) == 0 || len(raw) > 8<<20 {
		return [2]civ880CaptureEvent{}, errCiv880Observation
	}
	marker := []byte("CIV_PRODUCT_NATIVE_CAPTURE ")
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		line = bytes.TrimSuffix(line, []byte{'\r'})
		split := bytes.IndexByte(line, ' ')
		if split <= 0 {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, string(line[:split])); err != nil {
			continue
		}
		body := line[split+1:]
		if !bytes.HasPrefix(body, marker) {
			continue
		}
		body = body[len(marker):]
		if len(body) == 0 || len(body) > 8192 || bytes.Contains(body, marker) || !civ880UniqueJSON(body) {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		fields, err := civ880Object(body)
		if err != nil || len(fields) != 23 {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		for _, key := range []string{
			"schema", "evidence_kind", "phase", "native_exit_code", "collection_complete",
			"stop_reason", "capture_stop_reason", "start_returned", "root_exit_observed",
			"failure_stage", "exception_kind", "exception_hresult", "native_error_code",
			"result_save_state", "result_bytes", "result_sha256", "secondary_failure_count",
			"stdout_state", "stderr_state", "stdout_bytes", "stdout_sha256", "stderr_bytes", "stderr_sha256",
		} {
			if _, ok := fields[key]; !ok {
				return [2]civ880CaptureEvent{}, errCiv880Observation
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		var event civ880CaptureEvent
		if decoder.Decode(&event) != nil || decoder.Decode(new(any)) != io.EOF ||
			event.Schema != "civ6-native-capture/v3" || event.EvidenceKind != "historical_capture_snapshot" {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		phase, err := civ880String(fields["phase"])
		if err != nil || phase != event.Phase {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		index := -1
		if event.Phase == "bins" {
			index = 0
		} else if event.Phase == "dlls" {
			index = 1
		} else {
			continue
		}
		if seen[index] || event.ExitCode == nil || *event.ExitCode != 0 ||
			event.Complete == nil || !*event.Complete || event.StopReason != "completed" ||
			event.CaptureStopReason != "completed" || event.StartReturned == nil || !*event.StartReturned ||
			event.RootExitObserved == nil || !*event.RootExitObserved || event.ResultSaveState != "saved" ||
			event.ResultBytes == nil || *event.ResultBytes <= 0 || *event.ResultBytes > 512<<20 ||
			!civ880LowerHex(event.ResultSHA, 64) || event.SecondaryFailureCount == nil ||
			*event.SecondaryFailureCount != 0 || event.StdoutState != "owned_hashed" ||
			event.StderrState != "owned_hashed" || event.StdoutBytes == nil || *event.StdoutBytes <= 0 ||
			*event.StdoutBytes > 128<<20 || event.StderrBytes == nil || *event.StderrBytes < 0 ||
			*event.StderrBytes > 128<<20 || !civ880LowerHex(event.StdoutSHA, 64) ||
			!civ880LowerHex(event.StderrSHA, 64) {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		for _, diagnostic := range []json.RawMessage{
			event.FailureStage, event.ExceptionKind, event.ExceptionHRESULT, event.NativeErrorCode,
		} {
			if !bytes.Equal(bytes.TrimSpace(diagnostic), []byte("null")) {
				return [2]civ880CaptureEvent{}, errCiv880Observation
			}
		}
		if *event.StderrBytes == 0 && event.StderrSHA != digest(nil) {
			return [2]civ880CaptureEvent{}, errCiv880Observation
		}
		result[index] = event
		seen[index] = true
	}
	if !seen[0] || !seen[1] {
		return [2]civ880CaptureEvent{}, errCiv880Observation
	}
	return result, nil
}

func civ880CapturesMatch(events [2]civ880CaptureEvent, product *civ880ObservedProduct) bool {
	if product == nil {
		return false
	}
	for i, event := range events {
		if event.Schema != "civ6-native-capture/v3" ||
			event.EvidenceKind != "historical_capture_snapshot" ||
			event.StdoutSHA != product.Streams[i].StdoutSHA || event.StderrSHA != product.Streams[i].StderrSHA {
			return false
		}
	}
	return true
}

// The value comes from the exact observed job log; a matching log remains an
// observation until the code-owned authenticated transport and reviewed H1
// have independently been established.
func civ880OriginFromJobLog(raw []byte) (civ880OriginObservation,error) {
	var result civ880OriginObservation
	if len(raw)==0||len(raw)>8<<20{return result,errCiv880Observation}
	marker:=[]byte("CIV_PRODUCT_ARTIFACT_ORIGIN ");matches:=0
	for _,line:=range bytes.Split(raw,[]byte{'\n'}) {
		line=bytes.TrimSuffix(line,[]byte{'\r'});split:=bytes.IndexByte(line,' ');if split<=0{continue}
		if _,err:=time.Parse(time.RFC3339Nano,string(line[:split]));err!=nil{continue}
		body:=line[split+1:];if !bytes.HasPrefix(body,marker){continue};body=body[len(marker):]
		if len(body)==0||len(body)>8192||bytes.Contains(body,marker)||!civ880UniqueJSON(body){return civ880OriginObservation{},errCiv880Observation}
		fields,fieldErr:=civ880Object(body)
		if fieldErr!=nil||len(fields)!=14{return civ880OriginObservation{},errCiv880Observation}
		for _,key:=range []string{"repository_id","schema","source_sha","source_tree","workflow_blob","run_id","run_attempt","job_key","runner_os","artifact_id","artifact_name","upload_reported_sha256","manifest_sha256","cargo_metadata_sha256"}{
			value,exists:=fields[key]
			if !exists||bytes.Equal(bytes.TrimSpace(value),[]byte("null")){return civ880OriginObservation{},errCiv880Observation}
		}
		decoder:=json.NewDecoder(bytes.NewReader(body));decoder.DisallowUnknownFields()
		var parsed civ880OriginObservation
		if decoder.Decode(&parsed)!=nil||decoder.Decode(new(any))!=io.EOF{return civ880OriginObservation{},errCiv880Observation}
		if parsed.Schema!="civ6-product-artifact-origin/v1"||parsed.RepositoryID!=civ880RepositoryID||!civ880LowerHex(parsed.Source,40)||!civ880LowerHex(parsed.Tree,40)||!civ880LowerHex(parsed.WorkflowBlob,40)||parsed.RunID<=0||parsed.Attempt<=0||parsed.JobKey!="test"||parsed.RunnerOS!="Windows"||parsed.ArtifactID<=0||parsed.ArtifactName==""||!civ880LowerHex(parsed.UploadSHA,64)||!civ880LowerHex(parsed.ManifestSHA,64)||!civ880LowerHex(parsed.CargoSHA,64){return civ880OriginObservation{},errCiv880Observation}
		result=parsed;matches++
	}
	if matches!=1{return civ880OriginObservation{},errCiv880Observation}
	return result,nil
}

func civ880OriginMatches(origin civ880OriginObservation,server *civ880ServerObservation,product *civ880ObservedProduct) bool {
	return server!=nil&&product!=nil&&origin.RepositoryID==civ880RepositoryID&&origin.Source==server.Run.HeadSHA&&origin.Source==product.Source&&origin.Tree==product.Tree&&origin.WorkflowBlob==product.WorkflowBlob&&origin.RunID==server.Run.RunID&&origin.RunID==product.RunID&&origin.Attempt==server.Run.Attempt&&int64(origin.Attempt)==product.Attempt&&origin.ArtifactID==server.Artifact.ID&&origin.ArtifactName==server.Artifact.Name&&origin.UploadSHA==product.ArchiveSHA&&server.Artifact.Digest=="sha256:"+origin.UploadSHA&&server.Artifact.Bytes==product.ArchiveBytes&&origin.ManifestSHA==product.ManifestSHA&&origin.CargoSHA==product.CargoSHA
}

// The four IDs select metadata to inspect, not authority. A production caller
// must bind them to a reviewed code-owned plan and one authenticated owner;
// the selected source tree, upload identity and origin remain separate checks.
func civ880ObserveServer(ctx context.Context, client *http.Client, runID int64, attempt int, jobID, artifactID int64) (*civ880ServerObservation,error) {
	if ctx==nil||ctx.Err()!=nil||client==nil||runID<=0||attempt<=0||jobID<=0||artifactID<=0{return nil,errCiv880Observation}
	var run struct {
		ID int64 `json:"id"`; Attempt int `json:"run_attempt"`; HeadSHA string `json:"head_sha"`; HeadBranch string `json:"head_branch"`; Event string `json:"event"`; Path string `json:"path"`; WorkflowID int64 `json:"workflow_id"`; Status string `json:"status"`; Conclusion string `json:"conclusion"`
		Repository struct{ ID int64 `json:"id"` } `json:"repository"`; HeadRepository struct{ ID int64 `json:"id"` } `json:"head_repository"`
	}
	if civ880ServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d",civ880ActionsURL,runID),&run)!=nil || run.ID!=runID || run.Attempt!=attempt || run.Repository.ID!=civ880RepositoryID || run.HeadRepository.ID!=civ880RepositoryID || !civ880LowerHex(run.HeadSHA,40) || run.HeadBranch=="" || (run.Event!="pull_request"&&run.Event!="push"&&run.Event!="workflow_dispatch") || run.WorkflowID<=0 || run.Path!=".github/workflows/rust.yml" || run.Status!="completed" || run.Conclusion!="success" {return nil,errCiv880Observation}
	var jobs struct {
		Total int `json:"total_count"`; Jobs []struct {
			ID int64 `json:"id"`; RunID int64 `json:"run_id"`; Name string `json:"name"`; Status string `json:"status"`; Conclusion string `json:"conclusion"`; CheckRunURL string `json:"check_run_url"`
			Steps []struct{ Number int `json:"number"`; Name string `json:"name"`; Status string `json:"status"`; Conclusion string `json:"conclusion"` } `json:"steps"`
		} `json:"jobs"`
	}
	if civ880ServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d/attempts/%d/jobs?per_page=100",civ880ActionsURL,runID,attempt),&jobs)!=nil || jobs.Total!=len(jobs.Jobs) || len(jobs.Jobs)>100 {return nil,errCiv880Observation}
	var job civ880JobProjection;matches:=0
	for _,item:=range jobs.Jobs {
		if item.Name!="test (windows-latest)" {continue};matches++
		if item.ID!=jobID||item.RunID!=runID||item.Status!="completed"||item.Conclusion!="success"{return nil,errCiv880Observation}
		prefix:="https://api.github.com/repos/AutoDevJapan/civ6-banseiki/check-runs/"
		if !strings.HasPrefix(item.CheckRunURL,prefix){return nil,errCiv880Observation}
		checkID,err:=strconv.ParseInt(strings.TrimPrefix(item.CheckRunURL,prefix),10,64);if err!=nil||checkID<=0{return nil,errCiv880Observation}
		required:=[]string{"Capture Windows product build inputs","Build all tools","Upload source-bound Windows product software","Record Windows product artifact origin"};next,last:=0,0
		for _,step:=range item.Steps {
			known:=false;for _,name:=range required{if step.Name==name{known=true;break}}
			if !known{continue}
			if next>=len(required)||step.Name!=required[next]||step.Number<=last||step.Status!="completed"||step.Conclusion!="success"{return nil,errCiv880Observation};last=step.Number;next++
		}
		if next!=len(required){return nil,errCiv880Observation}
		job=civ880JobProjection{RunID:runID,Attempt:attempt,JobID:jobID,CheckRunID:checkID,Status:item.Status,Conclusion:item.Conclusion}
	}
	if matches!=1{return nil,errCiv880Observation}
	var artifacts struct {
		Total int `json:"total_count"`; Artifacts []struct {
			ID int64 `json:"id"`; Name string `json:"name"`; Digest string `json:"digest"`; Expired bool `json:"expired"`; ExpiresAt time.Time `json:"expires_at"`; Size int64 `json:"size_in_bytes"`
			WorkflowRun struct { ID int64 `json:"id"`; RepositoryID int64 `json:"repository_id"`; HeadRepositoryID int64 `json:"head_repository_id"`; HeadSHA string `json:"head_sha"` } `json:"workflow_run"`
		} `json:"artifacts"`
	}
	if civ880ServerJSON(ctx,client,fmt.Sprintf("%s/runs/%d/artifacts?per_page=100",civ880ActionsURL,runID),&artifacts)!=nil || artifacts.Total!=len(artifacts.Artifacts)||len(artifacts.Artifacts)>100{return nil,errCiv880Observation}
	var artifact civ880ArtifactProjection;matches=0
	expectedName:=fmt.Sprintf("civ6-windows-product-%s-%d-%d",run.HeadSHA,runID,attempt)
	for _,item:=range artifacts.Artifacts {
		if item.Name!=expectedName{continue};matches++
		if item.ID!=artifactID||item.WorkflowRun.ID!=runID||item.WorkflowRun.RepositoryID!=civ880RepositoryID||item.WorkflowRun.HeadRepositoryID!=civ880RepositoryID||item.WorkflowRun.HeadSHA!=run.HeadSHA||item.Expired||!item.ExpiresAt.After(time.Now().UTC())||item.Size<=0||item.Size>civ880MaxArchive||!strings.HasPrefix(item.Digest,"sha256:")||!civ880LowerHex(strings.TrimPrefix(item.Digest,"sha256:"),64){return nil,errCiv880Observation}
		artifact=civ880ArtifactProjection{RepositoryID:item.WorkflowRun.RepositoryID,HeadRepositoryID:item.WorkflowRun.HeadRepositoryID,RunID:item.WorkflowRun.ID,ID:item.ID,Name:item.Name,Bytes:item.Size,Digest:item.Digest,Expired:item.Expired}
	}
	if matches!=1||ctx.Err()!=nil{return nil,errCiv880Observation}
	return &civ880ServerObservation{Run:civ880RunProjection{RepositoryID:run.Repository.ID,HeadRepositoryID:run.HeadRepository.ID,HeadSHA:run.HeadSHA,Event:run.Event,HeadBranch:run.HeadBranch,WorkflowID:run.WorkflowID,WorkflowPath:run.Path,RunID:run.ID,Attempt:run.Attempt,Status:run.Status,Conclusion:run.Conclusion},Job:job,Artifact:artifact},nil
}

// The authenticated API request and the credential-free object download are
// separate connections. The redirect and expected digest are server
// observations, not independently established origin or purpose authority.
func civ880DownloadObserved(ctx context.Context, api, object *http.Client, observation *civ880ServerObservation) ([]byte,error) {
	if ctx==nil||ctx.Err()!=nil||api==nil||object==nil||observation==nil||observation.Artifact.ID<=0||observation.Artifact.Bytes<=0||observation.Artifact.Bytes>civ880MaxArchive{return nil,errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok{return nil,errCiv880Observation}
	want:=strings.TrimPrefix(observation.Artifact.Digest,"sha256:")
	if observation.Artifact.Digest!="sha256:"+want||!civ880LowerHex(want,64){return nil,errCiv880Observation}
	request,err:=http.NewRequestWithContext(ctx,http.MethodGet,fmt.Sprintf("%s/artifacts/%d/zip",civ880ActionsURL,observation.Artifact.ID),nil);if err!=nil{return nil,errCiv880Observation}
	request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
	once:=*api;once.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err:=once.Do(request);if err!=nil||response==nil||response.Body==nil{return nil,errCiv880Observation}
	location:=response.Header.Get("Location");closeErr:=response.Body.Close()
	if response.StatusCode!=http.StatusFound||closeErr!=nil||ctx.Err()!=nil{return nil,errCiv880Observation}
	target,err:=ghaGuardLiveRedirectURL(location);if err!=nil{return nil,errCiv880Observation}
	download,err:=http.NewRequestWithContext(ctx,http.MethodGet,target.String(),nil);if err!=nil{return nil,errCiv880Observation}
	withoutCredentials:=*object;withoutCredentials.Jar=nil;withoutCredentials.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err=withoutCredentials.Do(download);if err!=nil||response==nil||response.Body==nil{return nil,errCiv880Observation}
	if response.StatusCode!=http.StatusOK{_ = response.Body.Close();return nil,errCiv880Observation}
	raw,readErr:=io.ReadAll(io.LimitReader(civ880ContextReader{ctx,response.Body},observation.Artifact.Bytes+1));closeErr=response.Body.Close()
	if readErr!=nil||closeErr!=nil||ctx.Err()!=nil||int64(len(raw))!=observation.Artifact.Bytes||digest(raw)!=want{clear(raw);return nil,errCiv880Observation}
	return raw,nil
}

func civ880DownloadJobLog(ctx context.Context,api,object *http.Client,jobID int64) ([]byte,error) {
	if ctx==nil||ctx.Err()!=nil||api==nil||object==nil||jobID<=0{return nil,errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok{return nil,errCiv880Observation}
	request,err:=http.NewRequestWithContext(ctx,http.MethodGet,fmt.Sprintf("%s/jobs/%d/logs",civ880ActionsURL,jobID),nil);if err!=nil{return nil,errCiv880Observation}
	request.Header.Set("Accept","application/vnd.github+json");request.Header.Set("X-GitHub-Api-Version","2022-11-28")
	once:=*api;once.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err:=once.Do(request);if err!=nil||response==nil||response.Body==nil{return nil,errCiv880Observation}
	location:=response.Header.Get("Location");closeErr:=response.Body.Close()
	if response.StatusCode!=http.StatusFound||closeErr!=nil||ctx.Err()!=nil{return nil,errCiv880Observation}
	target,err:=ghaGuardLiveRedirectURL(location);if err!=nil{return nil,errCiv880Observation}
	download,err:=http.NewRequestWithContext(ctx,http.MethodGet,target.String(),nil);if err!=nil{return nil,errCiv880Observation}
	withoutCredentials:=*object;withoutCredentials.Jar=nil;withoutCredentials.CheckRedirect=func(*http.Request,[]*http.Request)error{return http.ErrUseLastResponse}
	response,err=withoutCredentials.Do(download);if err!=nil||response==nil||response.Body==nil{return nil,errCiv880Observation}
	if response.StatusCode!=http.StatusOK{_ = response.Body.Close();return nil,errCiv880Observation}
	raw,readErr:=io.ReadAll(io.LimitReader(civ880ContextReader{ctx,response.Body},(8<<20)+1));closeErr=response.Body.Close()
	if readErr!=nil||closeErr!=nil||ctx.Err()!=nil||len(raw)==0||len(raw)>8<<20{clear(raw);return nil,errCiv880Observation}
	return raw,nil
}

// This composes the before/download/after observations and H1-v6 archive
// parser. The fixed normal owner, producer-source proof, exact job publication,
// protected staging namespace and any lease issuance are deliberately outside
// this observation function and cannot be supplied as caller JSON or hashes.
func civ880ObserveAcquisition(ctx context.Context, authority *ghaGuardEvidenceAuthority, runID int64, attempt int, jobID, artifactID int64) (*civ880ObservedProduct,*civ880ServerObservation,error) {
	transport:=&http.Transport{Proxy:nil};defer transport.CloseIdleConnections()
	object:=&http.Client{Transport:transport,Timeout:30*time.Second}
	return civ880ObserveAcquisitionWith(ctx,authority,object,runID,attempt,jobID,artifactID)
}

// object is injectable here for tests only. The normal function above creates
// a fresh transport with no credentials or cookie jar for object storage.
func civ880ObserveAcquisitionWith(ctx context.Context, authority *ghaGuardEvidenceAuthority, object *http.Client, runID int64, attempt int, jobID, artifactID int64) (*civ880ObservedProduct,*civ880ServerObservation,error) {
	return civ880ObserveAcquisitionWithVerifier(ctx,authority,object,runID,attempt,jobID,artifactID,civ880VerifyH1GitNamespace)
}

// Tests may inject a source verifier to isolate transport ordering. Normal
// acquisition above always selects the actual full Git namespace verifier.
func civ880ObserveAcquisitionWithVerifier(ctx context.Context, authority *ghaGuardEvidenceAuthority, object *http.Client, runID int64, attempt int, jobID, artifactID int64, verifySource func(context.Context,*http.Client,*civ880ObservedProduct)error) (*civ880ObservedProduct,*civ880ServerObservation,error) {
	if ctx==nil||ctx.Err()!=nil||authority==nil||authority.client==nil||object==nil||verifySource==nil{return nil,nil,errCiv880Observation}
	client:=authority.client
	before,err:=civ880ObserveServer(ctx,client,runID,attempt,jobID,artifactID);if err!=nil{return nil,nil,err}
	log,err:=civ880DownloadJobLog(ctx,client,object,jobID);if err!=nil{return nil,nil,err}
	origin,originErr:=civ880OriginFromJobLog(log);events,eventErr:=civ880CaptureEvents(log);clear(log);if originErr!=nil||eventErr!=nil{return nil,nil,errCiv880Observation}
	archive,err:=civ880DownloadObserved(ctx,client,object,before);if err!=nil{return nil,nil,err};defer clear(archive)
	product,err:=civ880ObserveProductArchive(ctx,archive);if err!=nil{return nil,nil,err}
	completed:=false;defer func(){if !completed{product.clear()}}()
	if !civ880OriginMatches(origin,before,product)||!civ880CapturesMatch(events,product)||verifySource(ctx,client,product)!=nil{return nil,nil,errCiv880Observation}
	after,err:=civ880ObserveServer(ctx,client,runID,attempt,jobID,artifactID)
	if err!=nil||ctx.Err()!=nil||authority.client!=client||before==nil||after==nil||*before!=*after||!civ880OriginMatches(origin,before,product){return nil,nil,errCiv880Observation}
	completed=true
	return product,before,nil
}

// The publisher's v6 draft uploads these seven flat entries. This structure
// contains observations and owned buffers, not a verified issuer or a read
// capability. Its source revision must be checked against the eventual reviewed
// H1 generation and the independently authenticated server run.
type civ880ObservedProduct struct {
	mu sync.Mutex
	closed bool
	Source, Tree, WorkflowBlob string
	RunID, Attempt int64
	ManifestSHA, CargoSHA, ArchiveSHA string
	ArchiveBytes int64
	SourceInventorySHA, CargoLockSHA string
	ContextSHA,CompletionSHA string
	Streams [2]civ880StreamDigests
	PrepareVersion uint64 // source declaration, not a binary ABI proof
	SourceFiles []civ880SourceRecord
	files [5]civ880ObservedFile
}

type civ880StreamDigests struct { StdoutSHA,StderrSHA string }

var civ880ProfileDefaults=[14]string{
	"CARGO_PROFILE_RELEASE_PANIC","CARGO_PROFILE_RELEASE_LTO","CARGO_PROFILE_RELEASE_CODEGEN_UNITS","CARGO_PROFILE_RELEASE_OPT_LEVEL","CARGO_PROFILE_RELEASE_DEBUG","CARGO_PROFILE_RELEASE_DEBUG_ASSERTIONS","CARGO_PROFILE_RELEASE_OVERFLOW_CHECKS","CARGO_PROFILE_RELEASE_RPATH","CARGO_PROFILE_RELEASE_SPLIT_DEBUGINFO","CARGO_PROFILE_RELEASE_STRIP","CARGO_PROFILE_RELEASE_INCREMENTAL","CARGO_PROFILE_RELEASE_BUILD_OVERRIDE_CODEGEN_UNITS","CARGO_PROFILE_RELEASE_BUILD_OVERRIDE_OPT_LEVEL","CARGO_PROFILE_RELEASE_BUILD_OVERRIDE_DEBUG",
}

func civ880ProfileProjection(raw json.RawMessage) bool {
	var rows []json.RawMessage
	if len(raw)==0||json.Unmarshal(raw,&rows)!=nil||len(rows)>4096{return false}
	seen:=map[string]bool{}
	for _,row:=range rows{
		item,err:=civ880Object(row);if err!=nil{return false};name,err:=civ880String(item["name"]);if err!=nil||!strings.HasPrefix(name,"CARGO_PROFILE_")||seen[name]{return false}
		for _,r:=range name{if !((r>='A'&&r<='Z')||(r>='0'&&r<='9')||r=='_'){return false}}
		present,err:=civ880Bool(item["present"]);if err!=nil{return false};sha,exists:=item["sha256"];if !exists{return false}
		if present{value,err:=civ880String(sha);if err!=nil||!civ880HexAllowZero(value,64){return false}}else if !bytes.Equal(bytes.TrimSpace(sha),[]byte("null")){return false}
		seen[name]=true
	}
	for _,name:=range civ880ProfileDefaults{if !seen[name]{return false}}
	return true
}

func civ880ProfileValue(raw json.RawMessage, name string) (bool,string,bool) {
	var rows []json.RawMessage;if json.Unmarshal(raw,&rows)!=nil{return false,"",false}
	for _,row:=range rows{item,err:=civ880Object(row);if err!=nil{return false,"",false};observed,err:=civ880String(item["name"]);if err!=nil{return false,"",false};if observed!=name{continue};present,err:=civ880Bool(item["present"]);if err!=nil{return false,"",false};if !present{return false,"",true};sum,err:=civ880String(item["sha256"]);return present,sum,err==nil}
	return false,"",false
}

type civ880SourceRecord struct {
	Name string
	Bytes uint64
	SHA256 string
	GitBlobSHA1 string
}

func civ880HexAllowZero(value string, length int) bool {
	if len(value)!=length||strings.ToLower(value)!=value{return false}
	_,err:=hex.DecodeString(value);return err==nil
}

func civ880InventoryName(name string) bool {
	if name==""||len(name)>4096||!utf8.ValidString(name)||strings.ContainsAny(name,"\\:"){return false}
	for _,r:=range name{if unicode.IsControl(r){return false}}
	for _,part:=range strings.Split(name,"/"){if part==""||part=="."||part==".."{return false}}
	return true
}

// Input order is part of sha256-lines-v2; the receiver never sorts the rows.
// This proves consistency with the advertised inventory, not its provenance.
func civ880Inventory(ctx context.Context, raw json.RawMessage, requireGit bool) ([]civ880SourceRecord,string,error) {
	if ctx==nil||ctx.Err()!=nil||len(raw)==0||len(raw)>civ880MaxInventoryBytes{return nil,"",errCiv880Observation}
	var values []json.RawMessage
	if json.Unmarshal(raw,&values)!=nil||len(values)==0||len(values)>civ880MaxInventoryRows{return nil,"",errCiv880Observation}
	rows:=make([]civ880SourceRecord,0,len(values));seen:=make(map[string]bool,len(values));h:=sha256.New()
	for _,value:=range values {
		if ctx.Err()!=nil{return nil,"",errCiv880Observation}
		item,err:=civ880Object(value);if err!=nil{return nil,"",err}
		name,e1:=civ880String(item["name"]);sum,e2:=civ880String(item["sha256"])
		var size uint64
		if e1!=nil||e2!=nil||!civ880InventoryName(name)||!civ880HexAllowZero(sum,64)||len(item["bytes"])==0||bytes.Equal(bytes.TrimSpace(item["bytes"]),[]byte("null"))||json.Unmarshal(item["bytes"],&size)!=nil||size>512<<20||seen[name]{return nil,"",errCiv880Observation}
		seen[name]=true;git:="-"
		blob,ok:=item["git_blob_sha1"]
		if !ok{return nil,"",errCiv880Observation}
		if bytes.Equal(bytes.TrimSpace(blob),[]byte("null")){if requireGit{return nil,"",errCiv880Observation}} else {git,err=civ880String(blob);if err!=nil||!civ880HexAllowZero(git,40){return nil,"",errCiv880Observation}}
		if _,err:=fmt.Fprintf(h,"%s %s %d %s\n",sum,git,size,name);err!=nil{return nil,"",errCiv880Observation}
		rows=append(rows,civ880SourceRecord{Name:name,Bytes:size,SHA256:sum,GitBlobSHA1:git})
	}
	return rows,hex.EncodeToString(h.Sum(nil)),nil
}

func civ880JSONEquivalent(left,right json.RawMessage) bool {
	parse:=func(raw []byte)([]byte,bool){var value any;decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.UseNumber();if decoder.Decode(&value)!=nil||decoder.Decode(new(any))!=io.EOF{return nil,false};normalized,err:=json.Marshal(value);return normalized,err==nil}
	a,ok:=parse(left);if !ok{return false};b,ok:=parse(right);return ok&&bytes.Equal(a,b)
}

func civ880InputProjection(ctx context.Context, raw json.RawMessage) ([]civ880SourceRecord,string,string,error) {
	inputs,err:=civ880Object(raw);if err!=nil{return nil,"","",errCiv880Observation}
	inventory,e1:=civ880String(inputs["source_inventory_sha256"]);lock,e2:=civ880String(inputs["cargo_lock_sha256"]);count,e3:=civ880PositiveInt(inputs["source_file_count"]);format,e4:=civ880String(inputs["inventory_format"])
	if e1!=nil||e2!=nil||e3!=nil||e4!=nil||format!="sha256-lines-v2"||!civ880LowerHex(inventory,64)||!civ880LowerHex(lock,64){return nil,"","",errCiv880Observation}
	sources,actual,err:=civ880Inventory(ctx,inputs["source_files"],true)
	if err!=nil||int64(len(sources))!=count||actual!=inventory{return nil,"","",errCiv880Observation}
	lockFound:=false;for _,source:=range sources{if source.Name=="tools/Cargo.lock"&&source.SHA256==lock{lockFound=true}}
	if !lockFound||!civ880ProfileProjection(inputs["profile_environment"]){return nil,"","",errCiv880Observation}
	var configs []json.RawMessage
	if json.Unmarshal(inputs["configs"],&configs)!=nil||configs==nil||len(configs)>256{return nil,"","",errCiv880Observation}
	seen:=map[string]bool{}
	for _,raw:=range configs {
		item,err:=civ880Object(raw);if err!=nil{return nil,"","",errCiv880Observation}
		scope,e1:=civ880String(item["scope"]);name,e2:=civ880String(item["name"]);sum,e3:=civ880String(item["sha256"]);var size uint64
		validScope:=scope=="cargo-home"
		if strings.HasPrefix(scope,"ancestor-"){suffix:=strings.TrimPrefix(scope,"ancestor-");validScope=suffix!="";for _,r:=range suffix{if r<'0'||r>'9'{validScope=false}}}
		key:=scope+"/"+name
		if e1!=nil||e2!=nil||e3!=nil||!validScope||(name!="config"&&name!="config.toml")||!civ880HexAllowZero(sum,64)||len(item["bytes"])==0||bytes.Equal(bytes.TrimSpace(item["bytes"]),[]byte("null"))||json.Unmarshal(item["bytes"],&size)!=nil||size>128<<20||seen[key]{return nil,"","",errCiv880Observation};seen[key]=true
	}
	var vendors []json.RawMessage
	if json.Unmarshal(inputs["vendors"],&vendors)!=nil||len(vendors)==0||len(vendors)>200000{return nil,"","",errCiv880Observation}
	for _,raw:=range vendors {
		item,err:=civ880Object(raw);if err!=nil{return nil,"","",errCiv880Observation}
		name,e1:=civ880String(item["name"]);version,e2:=civ880String(item["version"]);kind,e3:=civ880String(item["source_kind"]);storage,e4:=civ880String(item["storage_kind"]);sum,e5:=civ880String(item["sha256"]);manifest,e6:=civ880String(item["manifest_sha256"])
		if e1!=nil||e2!=nil||e3!=nil||e4!=nil||e5!=nil||e6!=nil||kind!="crates.io"||(storage!="directory"&&storage!="registry-extracted")||!civ880HexAllowZero(sum,64)||!civ880HexAllowZero(manifest,64)||seen["vendor/"+name+"@"+version]{return nil,"","",errCiv880Observation}
		for _,r:=range name{if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||strings.ContainsRune("-_",r)){return nil,"","",errCiv880Observation}}
		for _,r:=range version{if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||strings.ContainsRune(".-+",r)){return nil,"","",errCiv880Observation}}
		seen["vendor/"+name+"@"+version]=true
		files,got,err:=civ880Inventory(ctx,item["files"],false);if err!=nil||got!=sum{return nil,"","",errCiv880Observation}
		checksumRaw,exists:=item["checksum_metadata_sha256"];if !exists{return nil,"","",errCiv880Observation}
		checksum:="";if !bytes.Equal(bytes.TrimSpace(checksumRaw),[]byte("null")){checksum,err=civ880String(checksumRaw);if err!=nil||!civ880HexAllowZero(checksum,64){return nil,"","",errCiv880Observation}}else if storage!="registry-extracted"{return nil,"","",errCiv880Observation}
		manifestFound,checksumFound:=false,false
		for _,file:=range files{if file.Name=="Cargo.toml"&&file.SHA256==manifest{manifestFound=true};if file.Name==".cargo-checksum.json"{if checksum==""||file.SHA256!=checksum{return nil,"","",errCiv880Observation};checksumFound=true}}
		if !manifestFound||(checksum!=""&&!checksumFound){return nil,"","",errCiv880Observation}
	}
	var environment []json.RawMessage
	if json.Unmarshal(inputs["environment"],&environment)!=nil||len(environment)!=3{return nil,"","",errCiv880Observation}
	for _,raw:=range environment {
		item,err:=civ880Object(raw);if err!=nil{return nil,"","",errCiv880Observation};name,e1:=civ880String(item["name"]);sum,e2:=civ880String(item["sha256"])
		if e1!=nil||e2!=nil||!civ880HexAllowZero(sum,64)||(name!="CARGO_INCREMENTAL"&&name!="CARGO_PROFILE_DEV_DEBUG"&&name!="CARGO_PROFILE_TEST_DEBUG")||seen["environment/"+name]{return nil,"","",errCiv880Observation};seen["environment/"+name]=true
		if name!="CARGO_INCREMENTAL"{present,profileSHA,found:=civ880ProfileValue(inputs["profile_environment"],name);if !found||!present||profileSHA!=sum{return nil,"","",errCiv880Observation}}
	}
	return sources,inventory,lock,nil
}

type civ880ObservedFile struct {
	Role, Name, SHA, CargoRecordSHA, PackageIDSHA string
	data []byte
}

func (p *civ880ObservedProduct) clear() {
	if p == nil { return }
	p.mu.Lock();defer p.mu.Unlock()
	if p.closed{return}
	p.closed=true
	for i := range p.files { clear(p.files[i].data); p.files[i].data = nil }
}

type civ880ContextReader struct { context context.Context; reader io.Reader }

func (r civ880ContextReader) Read(p []byte) (int,error) {
	if err := r.context.Err(); err != nil { return 0,err }
	return r.reader.Read(p)
}

func civ880ReadZipEntry(ctx context.Context, file *zip.File, limit int64) ([]byte,error) {
	if ctx == nil || ctx.Err() != nil || file == nil || file.Flags&1 != 0 || !file.Mode().IsRegular() || file.UncompressedSize64 == 0 || file.UncompressedSize64 > uint64(limit) { return nil,errCiv880Observation }
	reader,err := file.Open(); if err != nil { return nil,errCiv880Observation }
	raw,readErr := io.ReadAll(io.LimitReader(civ880ContextReader{ctx,reader},limit+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || ctx.Err() != nil || int64(len(raw)) > limit || uint64(len(raw)) != file.UncompressedSize64 { clear(raw); return nil,errCiv880Observation }
	return raw,nil
}

func civ880CheckedWrite(ctx context.Context,file *os.File,raw []byte) error {
	if ctx==nil||ctx.Err()!=nil||file==nil||len(raw)==0||len(raw)>civ880MaxPE{return errCiv880Observation}
	for offset:=0;offset<len(raw);{
		if ctx.Err()!=nil{return errCiv880Observation}
		end:=offset+(64<<10);if end>len(raw){end=len(raw)}
		n,err:=file.Write(raw[offset:end]);if err!=nil||n!=end-offset{return errCiv880Observation};offset=end
	}
	if ctx.Err()!=nil||file.Sync()!=nil||ctx.Err()!=nil{return errCiv880Observation}
	return nil
}

func civ880RawArray(raw json.RawMessage, count int) ([]json.RawMessage,error) {
	var result []json.RawMessage
	trimmed:=bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0]!='[' || json.Unmarshal(raw,&result) != nil || len(result) != count { return nil,errCiv880Observation }
	return result,nil
}

func civ880PositiveInt(raw json.RawMessage) (int64,error) {
	var value int64
	if len(raw) == 0 || json.Unmarshal(raw,&value) != nil || value <= 0 { return 0,errCiv880Observation }
	return value,nil
}

func civ880ExactArguments(raw json.RawMessage, expected []string) bool {
	var values []string
	if len(raw)==0 || json.Unmarshal(raw,&values)!=nil || len(values)!=len(expected) { return false }
	for i,value:=range values { if value!=expected[i] { return false } }
	return true
}

func civ880ObservedCommands(raw json.RawMessage) bool {
	commands,err:=civ880Object(raw);if err!=nil{return false}
	bins:=[]string{"cargo","+1.95.0","build","--locked","--release","--manifest-path","tools/Cargo.toml","--workspace","--bins","--message-format=json-render-diagnostics"}
	dlls:=[]string{"cargo","+1.95.0","build","--locked","--release","--manifest-path","tools/Cargo.toml","--package","civ6-native-bridge-rs","--package","civ6-bootstrap-rs","--lib","--target","x86_64-pc-windows-msvc","--message-format=json-render-diagnostics"}
	for phase,expected:=range map[string][]string{"bins":bins,"dlls":dlls} {
		item,err:=civ880Object(commands[phase]);if err!=nil{return false}
		if !bytes.Equal(bytes.TrimSpace(item["exit_code"]),[]byte("0")) || !civ880ExactArguments(item["argv"],expected) { return false }
		sha,err:=civ880String(item["cargo_json_sha256"]);if err!=nil||!civ880LowerHex(sha,64){return false}
		stderr,err:=civ880String(item["stderr_sha256"]);if err!=nil||!civ880LowerHex(stderr,64){return false}
	}
	return true
}

func civ880CommandStreams(raw json.RawMessage) ([2]civ880StreamDigests,error) {
	var result [2]civ880StreamDigests
	commands,err:=civ880Object(raw);if err!=nil||!civ880ObservedCommands(raw){return result,errCiv880Observation}
	for i,phase:=range []string{"bins","dlls"}{item,err:=civ880Object(commands[phase]);if err!=nil{return [2]civ880StreamDigests{},errCiv880Observation};stdout,e1:=civ880String(item["cargo_json_sha256"]);stderr,e2:=civ880String(item["stderr_sha256"]);if e1!=nil||e2!=nil{return [2]civ880StreamDigests{},errCiv880Observation};result[i]=civ880StreamDigests{StdoutSHA:stdout,StderrSHA:stderr}}
	return result,nil
}

func civ880ObservedProfile(profile map[string]json.RawMessage, record map[string]json.RawMessage) bool {
	test,e1:=civ880Bool(profile["test"]);_,e2:=civ880Bool(profile["debug_assertions"]);_,e3:=civ880Bool(profile["overflow_checks"]);_,e4:=civ880Bool(record["fresh"]);opt,e5:=civ880String(profile["opt_level"])
	if e1!=nil||e2!=nil||e3!=nil||e4!=nil||e5!=nil||test||(opt!="0"&&opt!="1"&&opt!="2"&&opt!="3"&&opt!="s"&&opt!="z") { return false }
	debug,ok:=profile["debuginfo"];if !ok{return false}
	if !bytes.Equal(bytes.TrimSpace(debug),[]byte("null")) && !bytes.Equal(bytes.TrimSpace(debug),[]byte("0")) && !bytes.Equal(bytes.TrimSpace(debug),[]byte("1")) && !bytes.Equal(bytes.TrimSpace(debug),[]byte("2")) && !bytes.Equal(bytes.TrimSpace(debug),[]byte(`"line-directives-only"`)) && !bytes.Equal(bytes.TrimSpace(debug),[]byte(`"line-tables-only"`)) { return false }
	var features []string
	if len(record["features"])==0 || json.Unmarshal(record["features"],&features)!=nil || features==nil || len(features)>256{return false}
	seen:=map[string]bool{}
	for _,feature:=range features { if feature==""||len(feature)>256||seen[feature]{return false};for _,r:=range feature{if !((r>='a'&&r<='z')||(r>='A'&&r<='Z')||(r>='0'&&r<='9')||strings.ContainsRune("-_.+",r)){return false}};seen[feature]=true }
	return true
}

func civ880ValidKinds(raw json.RawMessage, dll bool) bool {
	kinds,err := civ880Strings(raw,2); if err != nil { return false }
	if !dll { return civ880SameSet(kinds,"bin") }
	hasDLL := false
	for _,kind := range kinds {
		if kind == "cdylib" { hasDLL = true } else if kind != "rlib" { return false }
	}
	return hasDLL
}

func civ880ObservedRelative(spec civ880RoleSpec) string {
	if spec.dll { return "tools/target/x86_64-pc-windows-msvc/release/"+spec.filename }
	return "tools/target/release/"+spec.filename
}

// Parse only the H1-v6 candidate format currently supplied by the publisher.
// No path is opened and success is not origin verification. In particular the
// public source-inventory hash is not the complete before/after source proof.
func civ880ObserveProductArchive(ctx context.Context, archive []byte) (result *civ880ObservedProduct, err error) {
	if ctx == nil || ctx.Err() != nil || len(archive) == 0 || len(archive) > civ880MaxArchive { return nil,errCiv880Observation }
	if _,ok := ctx.Deadline(); !ok { return nil,errCiv880Observation }
	z,err := zip.NewReader(bytes.NewReader(archive),int64(len(archive)))
	if err != nil || len(z.File) != 7 { return nil,errCiv880Observation }
	allowed := map[string]bool{"manifest.json":true,"cargo-artifacts.json":true}
	for _,spec := range civ880RoleSpecs() { allowed[spec.filename] = true }
	entries := make(map[string]*zip.File,7)
	var total uint64
	for _,file := range z.File {
		if ctx.Err() != nil || file == nil || !allowed[file.Name] || entries[file.Name] != nil || file.Flags&1 != 0 || !file.Mode().IsRegular() || file.UncompressedSize64 == 0 { return nil,errCiv880Observation }
		limit := uint64(civ880MaxPE)
		if file.Name == "manifest.json" || file.Name == "cargo-artifacts.json" { limit = civ880MaxPE }
		if file.UncompressedSize64 > limit || file.CompressedSize64 > uint64(civ880MaxArchive) { return nil,errCiv880Observation }
		total += file.UncompressedSize64
		if total > uint64(civ880MaxArchive) { return nil,errCiv880Observation }
		entries[file.Name] = file
	}
	manifestRaw,err := civ880ReadZipEntry(ctx,entries["manifest.json"],civ880MaxPE); if err != nil { return nil,err }
	defer clear(manifestRaw)
	cargoRaw,err := civ880ReadZipEntry(ctx,entries["cargo-artifacts.json"],civ880MaxPE); if err != nil { return nil,err }
	defer clear(cargoRaw)
	if !civ880UniqueJSON(manifestRaw)||!civ880UniqueJSON(cargoRaw){return nil,errCiv880Observation}
	m,err := civ880Object(manifestRaw); if err != nil { return nil,err }
	c,err := civ880Object(cargoRaw); if err != nil { return nil,err }
	schema,e1 := civ880String(m["schema"]); repo,e2 := civ880String(m["repository"]); target,e3 := civ880String(m["target"])
	repoID,e4 := civ880PositiveInt(m["repository_id"]); cargoSchema,e5 := civ880String(c["schema"])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || schema != "civ6-windows-product-artifacts/v1" || repo != "AutoDevJapan/civ6-banseiki" || repoID != civ880RepositoryID || target != "x86_64-pc-windows-msvc" || cargoSchema != "civ6-selected-cargo-artifacts/v1" { return nil,errCiv880Observation }
	streams,err:=civ880CommandStreams(m["commands"]);if err!=nil{return nil,errCiv880Observation}
	compiler,err:=civ880Object(m["compiler"]);if err!=nil{return nil,err}
	toolchain,e1:=civ880String(compiler["toolchain"]);host,e2:=civ880String(compiler["host"]);rustc,e3:=civ880String(compiler["rustc_sha256"]);cargo,e4:=civ880String(compiler["cargo_sha256"]);toolManifest,e5:=civ880String(compiler["rustup_manifest_sha256"])
	cargoDispatcher,e6:=civ880String(compiler["cargo_dispatcher_sha256"]);rustupDispatcher,e7:=civ880String(compiler["rustup_dispatcher_sha256"])
	if e1!=nil||e2!=nil||e3!=nil||e4!=nil||e5!=nil||e6!=nil||e7!=nil||toolchain!="1.95.0"||host!="x86_64-pc-windows-msvc"||!civ880LowerHex(rustc,64)||!civ880LowerHex(cargo,64)||!civ880LowerHex(toolManifest,64)||!civ880LowerHex(cargoDispatcher,64)||!civ880LowerHex(rustupDispatcher,64){return nil,errCiv880Observation}
	source,e1 := civ880String(m["source_sha"]); tree,e2 := civ880String(m["source_tree"]); cargoSHA,e3 := civ880String(m["cargo_artifacts_sha256"])
	if e1 != nil || e2 != nil || e3 != nil || !civ880LowerHex(source,40) || !civ880LowerHex(tree,40) || !civ880LowerHex(cargoSHA,64) || digest(cargoRaw) != cargoSHA { return nil,errCiv880Observation }
	producer,err := civ880Object(m["producer"]); if err != nil { return nil,err }
	run,e1 := civ880PositiveInt(producer["run_id"]); attempt,e2 := civ880PositiveInt(producer["run_attempt"]); blob,e3 := civ880String(producer["workflow_blob"]); job,e4 := civ880String(producer["job"]); platform,e5 := civ880String(producer["runner_os"]); arch,e6 := civ880String(producer["runner_arch"])
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || !civ880LowerHex(blob,40) || job != "test" || platform != "Windows" || arch != "X64" { return nil,errCiv880Observation }
	if !civ880JSONEquivalent(m["inputs"],c["inputs_before"]) || !civ880JSONEquivalent(m["inputs"],c["inputs_after"]) { return nil,errCiv880Observation }
	sourceFiles,inventory,lock,err:=civ880InputProjection(ctx,m["inputs"])
	if err!=nil{return nil,errCiv880Observation}
	contextSHA,e1:=civ880String(m["context_sha256"]);completionSHA,e2:=civ880String(m["completion_sha256"])
	if e1!=nil||e2!=nil||!civ880LowerHex(contextSHA,64)||!civ880LowerHex(completionSHA,64){return nil,errCiv880Observation}
	var prepare uint64
	if json.Unmarshal(m["source_declared_prepare_version"],&prepare) != nil || prepare == 0 { return nil,errCiv880Observation }
	rows,err := civ880RawArray(m["files"],5); if err != nil { return nil,err }
	records,err := civ880RawArray(c["records"],5); if err != nil { return nil,err }
	rowByName := make(map[string]map[string]json.RawMessage,5)
	for _,raw := range rows {
		row,e := civ880Object(raw); if e != nil { return nil,e }
		name,e := civ880String(row["name"]); if e != nil || !allowed[name] || name == "manifest.json" || name == "cargo-artifacts.json" || rowByName[name] != nil { return nil,errCiv880Observation }
		rowByName[name] = row
	}
	recordByName := make(map[string]map[string]json.RawMessage,5)
	for _,raw := range records {
		record,e := civ880Object(raw); if e != nil { return nil,e }
		relative,e := civ880String(record["filename"]); if e != nil || recordByName[relative] != nil { return nil,errCiv880Observation }
		recordByName[relative] = record
	}
	owned := &civ880ObservedProduct{Source:source,Tree:tree,WorkflowBlob:blob,RunID:run,Attempt:attempt,ManifestSHA:digest(manifestRaw),CargoSHA:cargoSHA,ArchiveSHA:digest(archive),ArchiveBytes:int64(len(archive)),SourceInventorySHA:inventory,CargoLockSHA:lock,ContextSHA:contextSHA,CompletionSHA:completionSHA,Streams:streams,PrepareVersion:prepare,SourceFiles:sourceFiles}
	result = owned
	completed := false
	defer func(){ if !completed { owned.clear(); result = nil } }()
	packageIDs := make(map[string]string,3)
	for i,spec := range civ880RoleSpecs() {
		row,record := rowByName[spec.filename],recordByName[civ880ObservedRelative(spec)]
		if row == nil || record == nil { return nil,errCiv880Observation }
		name,e1 := civ880String(row["name"]); wantHash,e2 := civ880String(row["sha256"]); rowTarget,e3 := civ880String(row["target"]); rowKind,e4 := civ880String(row["kind"]); rowCargo,e5 := civ880String(row["cargo_record_sha256"]); size,e6 := civ880PositiveInt(row["bytes"])
		wantKind := "bin"; if spec.dll { wantKind = "cdylib" }
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || name != spec.filename || rowTarget != "x86_64-pc-windows-msvc" || rowKind != wantKind || size > civ880MaxPE || !civ880LowerHex(wantHash,64) || !civ880LowerHex(rowCargo,64) { return nil,errCiv880Observation }
		pkg,e1 := civ880Object(record["package"]); target,e2 := civ880Object(record["target"]); profile,e3 := civ880Object(record["profile"])
		if e1 != nil || e2 != nil || e3 != nil { return nil,errCiv880Observation }
		pkgName,e1 := civ880String(pkg["name"]); version,e2 := civ880String(pkg["version"]); manifest,e3 := civ880String(pkg["manifest"]); id,e4 := civ880String(pkg["id_sha256"]); targetName,e5 := civ880String(target["name"]); recordSHA,e6 := civ880String(record["cargo_record_sha256"])
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || !civ880ObservedProfile(profile,record) || version == "" || pkgName != spec.packageName || manifest != "tools/"+spec.packageName+"/Cargo.toml" || targetName != spec.target || !civ880LowerHex(id,64) || recordSHA != rowCargo || !civ880ValidKinds(target["kind"],spec.dll) || !civ880ValidKinds(target["crate_types"],spec.dll) { return nil,errCiv880Observation }
		if previous := packageIDs[pkgName]; previous != "" && previous != id { return nil,errCiv880Observation }; packageIDs[pkgName] = id
		for other,previous := range packageIDs { if other != pkgName && previous == id { return nil,errCiv880Observation } }
		payload,e := civ880ReadZipEntry(ctx,entries[spec.filename],civ880MaxPE); if e != nil { return nil,e }
		result.files[i] = civ880ObservedFile{Role:spec.role,Name:spec.filename,SHA:wantHash,CargoRecordSHA:rowCargo,PackageIDSHA:id,data:payload}
		if int64(len(payload)) != size || digest(payload) != wantHash { return nil,errCiv880Observation }
		exports,e:=civ880ObservePEExports(payload,spec.role);if e!=nil||!civ880ManifestExports(row["exports"],spec.role,exports){return nil,errCiv880Observation}
	}
	if ctx.Err() != nil { return nil,errCiv880Observation }
	completed = true
	return result,nil
}

// The handles below are observations of an already-created directory, not a
// qualified staging namespace or an artifact admission. The owner must create
// and protect that namespace, authenticate its producer, and retain the lease.
// On any partial open failure the returned lease remains owned by the caller.
type civ880HeldObservation struct {
	mu sync.Mutex
	root string
	parents []*os.File
	paths []string
	leaves [5]*os.File
	unwrapped []syscall.Handle
	closed bool
}

// These native structures are used only for a new, single-component child of
// an already-held directory. NtCreateFile returns the handle to the object it
// creates, avoiding a create-then-open pathname gap. No caller path or DACL is
// a purpose grant; the normal parent qualification is a separate prerequisite.
type civ880UnicodeString struct { Length,MaximumLength uint16; Buffer *uint16 }
type civ880ObjectAttributes struct { Length uint32; RootDirectory syscall.Handle; ObjectName *civ880UnicodeString; Attributes uint32; SecurityDescriptor uintptr; SecurityQualityOfService uintptr }
type civ880IOStatus struct { Status uintptr; Information uintptr }
type civ880GenericMapping struct { Read,Write,Execute,All uint32 }
var civ880DirectoryMapping=civ880GenericMapping{Read:0x00120089,Write:0x00120116,Execute:0x001200A0,All:0x001F01FF}

type civ880NtCreateFailure struct { status uint32 }
func (e *civ880NtCreateFailure) Error() string{return "civ880_nt_create_unqualified"}
func (e *civ880NtCreateFailure) Unwrap() error{return errCiv880Observation}

func civ880DefiniteNameCollision(handle syscall.Handle,err error) bool {
	var failure *civ880NtCreateFailure
	return errors.As(err,&failure)&&failure.status==0xc0000035&&(handle==0||handle==syscall.InvalidHandle)
}

func civ880NtCreateNew(parent syscall.Handle, name string, directory bool, security uintptr) (syscall.Handle,error) {
	if parent==0||name==""||strings.ContainsAny(name,"/\\:\x00")||name=="."||name==".."||unsafe.Sizeof(uintptr(0))!=8{return 0,errCiv880Observation}
	wide,err:=syscall.UTF16FromString(name);if err!=nil||len(wide)<2||len(wide)>32767{return 0,errCiv880Observation}
	nameValue:=civ880UnicodeString{Length:uint16((len(wide)-1)*2),MaximumLength:uint16(len(wide)*2),Buffer:&wide[0]}
	attrs:=civ880ObjectAttributes{Length:uint32(unsafe.Sizeof(civ880ObjectAttributes{})),RootDirectory:parent,ObjectName:&nameValue,Attributes:0x40,SecurityDescriptor:security}
	var handle syscall.Handle;var ioStatus civ880IOStatus
	access,attributes,options:=uint32(0xC0010000),uint32(0x80),uint32(0x60) // GENERIC_READ|WRITE|DELETE; regular synchronous file
	if directory{access=0x001700A3;attributes=0x10;options=0x21} // LIST|ADD_FILE|TRAVERSE|READ_ATTRIBUTES plus READ_CONTROL|WRITE_DAC|DELETE|SYNCHRONIZE
	call:=syscall.NewLazyDLL("ntdll.dll").NewProc("NtCreateFile")
	status,_,_:=call.Call(uintptr(unsafe.Pointer(&handle)),uintptr(access),uintptr(unsafe.Pointer(&attrs)),uintptr(unsafe.Pointer(&ioStatus)),0,uintptr(attributes),uintptr(syscall.FILE_SHARE_READ),2,uintptr(options),0,0)
	runtime.KeepAlive(wide);runtime.KeepAlive(nameValue);runtime.KeepAlive(attrs);runtime.KeepAlive(ioStatus)
	if uint32(status)!=0||uint32(ioStatus.Status)!=0||ioStatus.Information!=2||handle==0||handle==syscall.InvalidHandle{return handle,&civ880NtCreateFailure{status:uint32(status)}}
	return handle,nil
}

func civ880PersistentLocalACL(root string) bool {
	volume:=filepath.VolumeName(root)
	if len(volume)!=2||volume[1]!=':'||!((volume[0]>='A'&&volume[0]<='Z')||(volume[0]>='a'&&volume[0]<='z')){return false}
	name,err:=syscall.UTF16PtrFromString(volume+"\\");if err!=nil{return false}
	var flags uint32
	ok,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("GetVolumeInformationW").Call(uintptr(unsafe.Pointer(name)),0,0,0,0,uintptr(unsafe.Pointer(&flags)),0,0)
	runtime.KeepAlive(name)
	return ok!=0&&flags&0x8!=0 // FILE_PERSISTENT_ACLS
}

func civ880DACLBytes(descriptor uintptr) ([]byte,uint16,error) {
	if descriptor==0{return nil,0,errCiv880Observation}
	var present,defaulted int32;var acl *byte
	ok,_,_:=updateAdvapi.NewProc("GetSecurityDescriptorDacl").Call(descriptor,uintptr(unsafe.Pointer(&present)),uintptr(unsafe.Pointer(&acl)),uintptr(unsafe.Pointer(&defaulted)))
	if ok==0||present==0||acl==nil{return nil,0,errCiv880Observation}
	var control uint16;var revision uint32
	ok,_,_=updateSDControl.Call(descriptor,uintptr(unsafe.Pointer(&control)),uintptr(unsafe.Pointer(&revision)));if ok==0{return nil,0,errCiv880Observation}
	var info struct{AceCount,BytesInUse,BytesFree uint32}
	ok,_,_=updateAdvapi.NewProc("GetAclInformation").Call(uintptr(unsafe.Pointer(acl)),uintptr(unsafe.Pointer(&info)),unsafe.Sizeof(info),2)
	if ok==0||info.BytesInUse<8||info.BytesInUse>1<<20{return nil,0,errCiv880Observation}
	return append([]byte(nil),unsafe.Slice(acl,info.BytesInUse)...),control,nil
}

type civ880StageParent struct {
	mu sync.Mutex
	paths []string
	handles []*os.File
	stamps []string
	unwrapped []syscall.Handle
	transferred bool
	closed bool
}

// At is an internal/test seam. A normal factory must supply its own fixed,
// purpose-qualified parent; an arbitrary caller-selected path cannot do so.
// Partial handles are always returned to the owner for explicit closure.
func civ880OpenStageParentAt(ctx context.Context,root string) (*civ880StageParent,error) {
	parent:=&civ880StageParent{}
	if ctx==nil||ctx.Err()!=nil||!filepath.IsAbs(root)||filepath.Clean(root)!=root||!civ880PersistentLocalACL(root){return parent,errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return parent,errCiv880Observation}
	var paths []string;for p:=root;;p=filepath.Dir(p){paths=append(paths,p);if len(paths)>64{return parent,errCiv880Observation};if filepath.Dir(p)==p{break}}
	for i:=len(paths)-1;i>=0;i-- {
		ptr,err:=syscall.UTF16PtrFromString(paths[i]);if err!=nil{return parent,errCiv880Observation}
		access:=uint32(0x20080);if i==0{access|=0x0004} // FILE_ADD_SUBDIRECTORY only on the final parent
		h,err:=syscall.CreateFile(ptr,access,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.OPEN_EXISTING,0x02000000|0x00200000,0);if err!=nil{return parent,errCiv880Observation}
		file:=os.NewFile(uintptr(h),paths[i]);if file==nil{parent.unwrapped=append(parent.unwrapped,h);return parent,errCiv880Observation}
		parent.paths=append(parent.paths,paths[i]);parent.handles=append(parent.handles,file)
		stamp,err:=civ880ParentObservation(file,paths[i]);if err!=nil{return parent,errCiv880Observation};parent.stamps=append(parent.stamps,stamp)
		if ctx.Err()!=nil{return parent,errCiv880Observation}
	}
	return parent,nil
}

func (parent *civ880StageParent) current(ctx context.Context) bool {
	if parent==nil||parent.closed||ctx==nil||ctx.Err()!=nil||len(parent.handles)==0||len(parent.handles)!=len(parent.paths)||len(parent.stamps)!=len(parent.paths){return false}
	for i,file:=range parent.handles{stamp,err:=civ880ParentObservation(file,parent.paths[i]);if err!=nil||stamp!=parent.stamps[i]||ctx.Err()!=nil{return false}}
	return true
}

func (parent *civ880StageParent) closeOwned(transferred bool) error {
	if parent==nil{return nil};parent.mu.Lock();defer parent.mu.Unlock()
	if parent.closed{return nil};if parent.transferred!=transferred{return errCiv880Observation}
	for i:=len(parent.unwrapped)-1;i>=0;i--{if parent.unwrapped[i]!=0{if err:=syscall.CloseHandle(parent.unwrapped[i]);err!=nil{return errors.Join(errCiv880Observation,err)};parent.unwrapped[i]=0}}
	for i:=len(parent.handles)-1;i>=0;i--{if parent.handles[i]!=nil{if err:=parent.handles[i].Close();err!=nil{return errors.Join(errCiv880Observation,err)};parent.handles[i]=nil}}
	parent.closed=true;return nil
}

func (parent *civ880StageParent) Close() error{return parent.closeOwned(false)}

type civ880CreatedStage struct {
	mu sync.Mutex
	parent *civ880StageParent
	root *os.File
	rootPath,rootStamp string
	rootID softwareIdentity
	leaves [5]*os.File
	ids [5]softwareIdentity
	acls [5]string
	unwrapped []syscall.Handle
	security uintptr
	securityOther []uintptr
	generatedSecurity []uintptr
	parentOwned bool
	rootAttempted bool
	rootCreated bool
	rootCollision bool
	unknownCreate bool
	leafDeleteRequested [5]bool
	rootDeleteRequested bool
	cleanupStarted bool
	cleanupDone bool
	complete bool
	closed bool
}

func civ880StageDirectoryIdentity(file *os.File,expected string) (softwareIdentity,error) {
	var empty softwareIdentity
	if file==nil{return empty,errCiv880Observation}
	h:=syscall.Handle(file.Fd());name,ok:=handlePath(h);var info syscall.ByHandleFileInformation
	if !ok||!sameCleanPath(name,expected)||syscall.GetFileInformationByHandle(h,&info)!=nil||info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0||info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0||info.VolumeSerialNumber==0||(info.FileIndexHigh==0&&info.FileIndexLow==0){return empty,errCiv880Observation}
	return softwareIdentity{Volume:fmt.Sprint(info.VolumeSerialNumber),FileID:fmt.Sprint(uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow)),Created:fmt.Sprint(uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime))},nil
}

func civ880StageName(name string) bool {
	if len(name)!=len("civ880-")+32||!strings.HasPrefix(name,"civ880-"){return false}
	return civ880HexAllowZero(strings.TrimPrefix(name,"civ880-"),32)
}

func civ880CreateObservedStage(ctx context.Context,parent *civ880StageParent,product *civ880ObservedProduct) (*civ880CreatedStage,error) {
	var random [16]byte;if _,err:=io.ReadFull(rand.Reader,random[:]);err!=nil{return nil,errCiv880Observation}
	return civ880CreateObservedStageNamed(ctx,parent,product,"civ880-"+hex.EncodeToString(random[:]))
}

// Named is an internal deterministic collision fixture seam. A successful
// stage owns actual CREATE_NEW handles and remains non-authoritative. Any
// partial result remains owned and is never silently removed on failure.
func civ880CreateObservedStageNamed(ctx context.Context,parent *civ880StageParent,product *civ880ObservedProduct,name string) (*civ880CreatedStage,error) {
	return civ880CreateObservedStageWithHook(ctx,parent,product,name,nil)
}

// The hook is a fixture seam; the normal constructor above supplies nil.
func civ880CreateObservedStageWithHook(ctx context.Context,parent *civ880StageParent,product *civ880ObservedProduct,name string,hook func(int)error) (*civ880CreatedStage,error) {
	stage:=&civ880CreatedStage{parent:parent}
	stage.mu.Lock();defer stage.mu.Unlock()
	if ctx==nil||ctx.Err()!=nil||parent==nil||product==nil||!civ880StageName(name){return stage,errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return stage,errCiv880Observation}
	parent.mu.Lock()
	if parent.transferred||!parent.current(ctx){parent.mu.Unlock();return stage,errCiv880Observation}
	parent.transferred=true;stage.parentOwned=true;parent.mu.Unlock()
	product.mu.Lock();defer product.mu.Unlock();if product.closed{return stage,errCiv880Observation}
	token,err:=syscall.OpenCurrentProcessToken();if err!=nil{return stage,errCiv880Observation}
	stage.unwrapped=append(stage.unwrapped,syscall.Handle(token))
	user,err:=token.GetTokenUser();if err!=nil{return stage,errCiv880Observation}
	sid,err:=user.User.Sid.String();if err!=nil{return stage,errCiv880Observation}
	sddl,err:=syscall.UTF16PtrFromString("D:P(A;OICI;FA;;;"+sid+")");if err!=nil{return stage,errCiv880Observation}
	ok,_,_:=updateStringSD.Call(uintptr(unsafe.Pointer(sddl)),1,uintptr(unsafe.Pointer(&stage.security)),0);runtime.KeepAlive(sddl)
	if ok==0||stage.security==0{return stage,errCiv880Observation}
	parentFile:=parent.handles[len(parent.handles)-1];stage.rootPath=filepath.Join(parent.paths[len(parent.paths)-1],name)
	stage.rootAttempted=true
	h,err:=civ880NtCreateNew(syscall.Handle(parentFile.Fd()),name,true,stage.security)
	if err!=nil{stage.rootCollision=civ880DefiniteNameCollision(h,err);stage.unknownCreate=!stage.rootCollision;if h!=0&&h!=syscall.InvalidHandle{stage.unwrapped=append(stage.unwrapped,h)};return stage,errCiv880Observation}
	stage.rootCreated=true
	stage.root=os.NewFile(uintptr(h),stage.rootPath);if stage.root==nil{stage.unknownCreate=true;stage.unwrapped=append(stage.unwrapped,h);return stage,errCiv880Observation}
	stage.rootID,err=civ880StageDirectoryIdentity(stage.root,stage.rootPath);if err!=nil{return stage,errCiv880Observation}
	var actualDescriptor uintptr
	code,_,_:=updateGetSecurity.Call(stage.root.Fd(),1,4,0,0,0,0,uintptr(unsafe.Pointer(&actualDescriptor)))
	if actualDescriptor!=0{stage.securityOther=append(stage.securityOther,actualDescriptor)}
	if code!=0||actualDescriptor==0{return stage,errCiv880Observation}
	// Compare against the OS-generated effective DACL for a directory created
	// from this descriptor; Windows may add generic mappings/inheritance flags.
	var generated uintptr
	ok,_,_ = updateAdvapi.NewProc("CreatePrivateObjectSecurityEx").Call(0,stage.security,uintptr(unsafe.Pointer(&generated)),0,1,0,uintptr(token),uintptr(unsafe.Pointer(&civ880DirectoryMapping)))
	if generated!=0{stage.generatedSecurity=append(stage.generatedSecurity,generated)}
	if ok==0||generated==0{return stage,errCiv880Observation}
	expectedACL,_,err:=civ880DACLBytes(generated);if err!=nil{return stage,errCiv880Observation};defer clear(expectedACL)
	actualACL,actualControl,err:=civ880DACLBytes(actualDescriptor);if err!=nil{return stage,errCiv880Observation};defer clear(actualACL)
	if !bytes.Equal(expectedACL,actualACL)||actualControl&0x1000==0{return stage,errCiv880Observation}
	stage.rootStamp,err=civ880ParentObservation(stage.root,stage.rootPath);if err!=nil||!parent.current(ctx){return stage,errCiv880Observation}
	seen:=map[softwareIdentity]bool{}
	for i,spec:=range civ880RoleSpecs(){
		fileData:=product.files[i];if ctx.Err()!=nil||fileData.Role!=spec.role||fileData.Name!=spec.filename||len(fileData.data)==0||digest(fileData.data)!=fileData.SHA{return stage,errCiv880Observation}
		h,err:=civ880NtCreateNew(syscall.Handle(stage.root.Fd()),spec.filename,false,stage.security)
		if err!=nil{stage.unknownCreate=true;if h!=0&&h!=syscall.InvalidHandle{stage.unwrapped=append(stage.unwrapped,h)};return stage,errCiv880Observation}
		filePath:=filepath.Join(stage.rootPath,spec.filename);file:=os.NewFile(uintptr(h),filePath);if file==nil{stage.unknownCreate=true;stage.unwrapped=append(stage.unwrapped,h);return stage,errCiv880Observation};stage.leaves[i]=file
		actual,ok:=handlePath(h);id,identityErr:=fileSoftwareIdentity(file);acl,aclErr:=updateFileACL(file)
		if !ok||!sameCleanPath(actual,filePath)||identityErr!=nil||seen[id]||aclErr!=nil||acl==""{return stage,errCiv880Observation}
		seen[id]=true;stage.ids[i]=id;stage.acls[i]=acl
		if civ880CheckedWrite(ctx,file,fileData.data)!=nil||civ880CheckHeldBytes(ctx,file,fileData.data,fileData.SHA)!=nil{return stage,errCiv880Observation}
		if hook!=nil&&hook(i)!=nil{return stage,errCiv880Observation}
	}
	var fileTemplate uintptr
	ok,_,_=updateAdvapi.NewProc("CreatePrivateObjectSecurityEx").Call(0,stage.security,uintptr(unsafe.Pointer(&fileTemplate)),0,0,0,uintptr(token),uintptr(unsafe.Pointer(&civ880DirectoryMapping)))
	if fileTemplate!=0{stage.generatedSecurity=append(stage.generatedSecurity,fileTemplate)}
	if ok==0||fileTemplate==0{return stage,errCiv880Observation}
	fileExpected,_,err:=civ880DACLBytes(fileTemplate);if err!=nil{return stage,errCiv880Observation};defer clear(fileExpected)
	for i,file:=range stage.leaves {
		var descriptor uintptr;code,_,_:=updateGetSecurity.Call(file.Fd(),1,4,0,0,0,0,uintptr(unsafe.Pointer(&descriptor)))
		if descriptor!=0{stage.securityOther=append(stage.securityOther,descriptor)}
		if code!=0||descriptor==0{return stage,errCiv880Observation}
		descriptorACL,_,err:=civ880DACLBytes(descriptor);if err!=nil{return stage,errCiv880Observation};same:=bytes.Equal(descriptorACL,fileExpected);clear(descriptorACL);if !same{return stage,errCiv880Observation}
		id,err:=fileSoftwareIdentity(file);if err!=nil||id!=stage.ids[i]{return stage,errCiv880Observation};acl,err:=updateFileACL(file);if err!=nil||acl!=stage.acls[i]{return stage,errCiv880Observation}
	}
	stamp,err:=civ880ParentObservation(stage.root,stage.rootPath);if err!=nil||stamp!=stage.rootStamp||!parent.current(ctx)||!civ880StageInventory(ctx,stage.root){return stage,errCiv880Observation}
	stage.complete=true;return stage,nil
}

// Close releases handles only after exact-object Cleanup has completed.
// Partial, foreign or unknown namespaces stay held for owner reconciliation.
func (stage *civ880CreatedStage) Close() error {
	if stage==nil{return nil};stage.mu.Lock();defer stage.mu.Unlock();if stage.closed{return nil}
	if stage.rootAttempted&&!stage.rootCollision&&!stage.cleanupDone{return errCiv880StagePreserved}
	var failures []error
	for i:=len(stage.leaves)-1;i>=0;i--{if stage.leaves[i]!=nil{if err:=stage.leaves[i].Close();err!=nil{failures=append(failures,err)}else{stage.leaves[i]=nil}}}
	for i:=len(stage.unwrapped)-1;i>=0;i--{if stage.unwrapped[i]!=0{if err:=syscall.CloseHandle(stage.unwrapped[i]);err!=nil{failures=append(failures,err)}else{stage.unwrapped[i]=0}}}
	if len(failures)>0{return errors.Join(append([]error{errCiv880Observation},failures...)...)}
	if stage.root!=nil{if err:=stage.root.Close();err!=nil{return errors.Join(errCiv880Observation,err)};stage.root=nil}
	for i:=len(stage.generatedSecurity)-1;i>=0;i--{if stage.generatedSecurity[i]!=0{ok,_,_:=updateAdvapi.NewProc("DestroyPrivateObjectSecurity").Call(uintptr(unsafe.Pointer(&stage.generatedSecurity[i])));if ok==0{return errCiv880Observation};stage.generatedSecurity[i]=0}}
	for i:=len(stage.securityOther)-1;i>=0;i--{if stage.securityOther[i]!=0{if result,_,_:=updateLocalFree.Call(stage.securityOther[i]);result!=0{return errCiv880Observation};stage.securityOther[i]=0}}
	if stage.security!=0{if result,_,_:=updateLocalFree.Call(stage.security);result!=0{return errCiv880Observation};stage.security=0}
	if stage.parent!=nil&&stage.parentOwned{if err:=stage.parent.closeOwned(true);err!=nil{return err};stage.parent=nil;stage.parentOwned=false}
	stage.closed=true;return nil
}

func civ880Disposition(file *os.File) error {
	if file==nil{return errCiv880Observation}
	value:=uint32(1)
	ok,_,_:=syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle").Call(file.Fd(),4,uintptr(unsafe.Pointer(&value)),unsafe.Sizeof(value))
	runtime.KeepAlive(value)
	if ok==0{return errCiv880Observation};return nil
}

// Cleanup applies deletion only to the exact handles obtained at FILE_CREATE;
// it never reopens or unlinks a caller path. The stage and its parent remain
// held on any unconfirmed outcome. A positive check for path absence follows
// closing each delete-pending handle; another object's appearance is a refusal,
// not permission to delete it.
func (stage *civ880CreatedStage) Cleanup(ctx context.Context) error {
	return stage.cleanupWithHook(ctx,nil)
}

// The hook is a deterministic test seam. The normal cleanup passes nil.
func (stage *civ880CreatedStage) cleanupWithHook(ctx context.Context,hook func(string,int)error) error {
	if stage==nil||ctx==nil||ctx.Err()!=nil{return errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	stage.mu.Lock();defer stage.mu.Unlock()
	if stage.cleanupDone{return nil};if stage.closed{return errCiv880Observation}
	if !stage.rootAttempted||stage.rootCollision {
		if stage.rootCreated||stage.root!=nil||stage.unknownCreate{return errCiv880StagePreserved}
		for _,leaf:=range stage.leaves{if leaf!=nil{return errCiv880StagePreserved}}
		stage.cleanupDone=true;return nil
	}
	stage.cleanupStarted=true
	if stage.unknownCreate||!stage.rootCreated||stage.parent==nil||!stage.parentOwned||!stage.parent.current(ctx){return errCiv880StagePreserved}
	if stage.root==nil {
		if stage.rootDeleteRequested{if _,err:=os.Lstat(stage.rootPath);errors.Is(err,os.ErrNotExist){stage.cleanupDone=true;return nil}}
		return errCiv880StagePreserved
	}
	id,err:=civ880StageDirectoryIdentity(stage.root,stage.rootPath);if err!=nil||id!=stage.rootID||id.FileID==""||id.Volume==""{return errCiv880StagePreserved}
	if stage.rootStamp!=""{stamp,err:=civ880ParentObservation(stage.root,stage.rootPath);if err!=nil||stamp!=stage.rootStamp{return errCiv880StagePreserved}}
	if stage.rootDeleteRequested {
		for _,leaf:=range stage.leaves{if leaf!=nil{return errCiv880StagePreserved}}
		if err:=stage.root.Close();err!=nil{return errors.Join(errCiv880StagePreserved,err)};stage.root=nil
		if _,err:=os.Lstat(stage.rootPath);!errors.Is(err,os.ErrNotExist){return errCiv880StagePreserved}
		stage.cleanupDone=true;return nil
	}
	entries,err:=civ880StageEntries(ctx,stage.root);if err!=nil{return errCiv880StagePreserved}
	expected:=map[string]int{};for i,spec:=range civ880RoleSpecs(){if stage.leaves[i]!=nil{expected[spec.filename]=i}}
	seen:=map[string]bool{}
	for _,entry:=range entries{if _,ok:=expected[entry.name];!ok||entry.attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT)!=0{return errCiv880StagePreserved};seen[entry.name]=true}
	for name,i:=range expected{if !seen[name]&&!stage.leafDeleteRequested[i]{return errCiv880StagePreserved}}
	for i:=len(stage.leaves)-1;i>=0;i-- {
		file:=stage.leaves[i];if file==nil{continue};if ctx.Err()!=nil{return errCiv880StagePreserved}
		current,err:=fileSoftwareIdentity(file);if err!=nil||current!=stage.ids[i]||current.FileID==""||current.Volume==""{return errCiv880StagePreserved}
		if !stage.leafDeleteRequested[i]{if ctx.Err()!=nil{return errCiv880StagePreserved};if err:=civ880Disposition(file);err!=nil{return errCiv880StagePreserved};stage.leafDeleteRequested[i]=true;if hook!=nil&&hook("leaf_marked",i)!=nil{return errCiv880StagePreserved}}
		if err:=file.Close();err!=nil{return errors.Join(errCiv880StagePreserved,err)};stage.leaves[i]=nil
		if _,err:=os.Lstat(filepath.Join(stage.rootPath,civ880RoleSpecs()[i].filename));!errors.Is(err,os.ErrNotExist){return errCiv880StagePreserved}
	}
	entries,err=civ880StageEntries(ctx,stage.root);if err!=nil||len(entries)!=0||!stage.parent.current(ctx){return errCiv880StagePreserved}
	if !stage.rootDeleteRequested{if ctx.Err()!=nil{return errCiv880StagePreserved};if err:=civ880Disposition(stage.root);err!=nil{return errCiv880StagePreserved};stage.rootDeleteRequested=true;if hook!=nil&&hook("root_marked",-1)!=nil{return errCiv880StagePreserved}}
	if err:=stage.root.Close();err!=nil{return errors.Join(errCiv880StagePreserved,err)};stage.root=nil
	if _,err:=os.Lstat(stage.rootPath);!errors.Is(err,os.ErrNotExist){return errCiv880StagePreserved}
	stage.cleanupDone=true;return nil
}


// FileFullDirectoryInfo has a 68-byte header followed by UTF-16 filename
// bytes. Read the directory through its original CREATE_NEW handle: reopening
// by path would conflict with our own DELETE access and omit share-delete.
type civ880StageEntry struct { name string; attributes uint32 }

func civ880DecodeStageEntries(raw []byte) ([]civ880StageEntry,error) {
	var entries []civ880StageEntry
	for offset:=0;; {
		if offset<0||offset>len(raw)-68{return nil,errCiv880Observation}
		row:=raw[offset:]
		next:=binary.LittleEndian.Uint32(row[:4])
		nameBytes:=binary.LittleEndian.Uint32(row[60:64])
		if nameBytes==0||nameBytes&1!=0||nameBytes>510||uint64(nameBytes)>uint64(len(row)-68){return nil,errCiv880Observation}
		end:=68+int(nameBytes)
		wide:=make([]uint16,int(nameBytes)/2)
		for i:=range wide{wide[i]=binary.LittleEndian.Uint16(row[68+i*2:70+i*2]);if wide[i]==0{return nil,errCiv880Observation}}
		name:=syscall.UTF16ToString(wide)
		attributes:=binary.LittleEndian.Uint32(row[56:60])
		if name=="."||name==".."{
			if attributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0{return nil,errCiv880Observation}
		}else{
			if name==""||strings.ContainsAny(name,"/\\:\x00"){return nil,errCiv880Observation}
			entries=append(entries,civ880StageEntry{name:name,attributes:attributes})
			if len(entries)>5{return nil,errCiv880Observation}
		}
		if next==0{return entries,nil}
		if next&7!=0||uint64(next)<uint64(end)||uint64(next)>uint64(len(row)-68){return nil,errCiv880Observation}
		offset+=int(next)
	}
}

// The caller serializes all use of the exact held directory. Restart on each
// inspection; no path reopen and no change to the root's sharing policy.
func civ880StageEntries(ctx context.Context,root *os.File) ([]civ880StageEntry,error) {
	if ctx==nil||ctx.Err()!=nil||root==nil{return nil,errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok{return nil,errCiv880Observation}
	var info syscall.ByHandleFileInformation
	h:=syscall.Handle(root.Fd())
	if syscall.GetFileInformationByHandle(h,&info)!=nil||info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0||
		info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0{return nil,errCiv880Observation}
	var aligned [8192]uint64
	raw:=unsafe.Slice((*byte)(unsafe.Pointer(&aligned[0])),len(aligned)*8)
	defer clear(raw)
	class:=uint32(windows.FileFullDirectoryRestartInfo)
	var entries []civ880StageEntry
	for batch:=0;batch<8;batch++ {
		if ctx.Err()!=nil{return nil,errCiv880Observation}
		clear(raw)
		err:=windows.GetFileInformationByHandleEx(windows.Handle(h),class,&raw[0],uint32(len(raw)))
		runtime.KeepAlive(root)
		if ctx.Err()!=nil{return nil,errCiv880Observation}
		if errors.Is(err,syscall.ERROR_NO_MORE_FILES)||
			class==windows.FileFullDirectoryRestartInfo&&errors.Is(err,syscall.ERROR_FILE_NOT_FOUND){
			return entries,nil
		}
		if err!=nil{return nil,errCiv880Observation}
		part,err:=civ880DecodeStageEntries(raw)
		if err!=nil{return nil,err}
		entries=append(entries,part...)
		if len(entries)>5{return nil,errCiv880Observation}
		class=windows.FileFullDirectoryInfo
	}
	return nil,errCiv880Observation
}

func civ880StageInventory(ctx context.Context,root *os.File) bool {
	entries,err:=civ880StageEntries(ctx,root)
	if err!=nil||len(entries)!=5{return false}
	wanted:=map[string]bool{};for _,spec:=range civ880RoleSpecs(){wanted[spec.filename]=true}
	for _,entry:=range entries{
		if !wanted[entry.name]||entry.attributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT)!=0{return false}
		delete(wanted,entry.name)
	}
	return len(wanted)==0
}

type civ880PreparedObservation struct {
	mu sync.Mutex
	product *civ880ObservedProduct
	server *civ880ServerObservation
	parent *civ880StageParent
	stage *civ880CreatedStage
}

// The caller owns the returned object even on error. This connects the actual
// server/namespace observations to the operation-owned CREATE_NEW stage, but
// it is not a purpose issuer: the authenticated normal factory, reviewed fixed
// H1 and fixed parent qualification are still required before any routing.
func civ880PrepareObserved(ctx context.Context,authority *ghaGuardEvidenceAuthority,parent *civ880StageParent,runID int64,attempt int,jobID,artifactID int64) (*civ880PreparedObservation,error) {
	prepared:=&civ880PreparedObservation{parent:parent}
	if ctx==nil||ctx.Err()!=nil||authority==nil||authority.client==nil||parent==nil{return prepared,errCiv880Observation}
	client:=authority.client
	product,server,err:=civ880ObserveAcquisition(ctx,authority,runID,attempt,jobID,artifactID)
	if err!=nil{return prepared,err};prepared.product=product;prepared.server=server
	stage,err:=civ880CreateObservedStage(ctx,parent,product);prepared.stage=stage
	if err!=nil{return prepared,err}
	if _,err:=prepared.observeReadOnly(ctx);err!=nil{return prepared,err}
	after,err:=civ880ObserveServer(ctx,client,runID,attempt,jobID,artifactID)
	if err!=nil||after==nil||server==nil||*after!=*server||authority.client!=client||ctx.Err()!=nil{return prepared,errCiv880Observation}
	if _,err:=prepared.observeReadOnly(ctx);err!=nil{return prepared,err}
	return prepared,nil
}

// The normal authenticated transport factory, reviewed H1 binding and
// purpose-qualified parent factory do not yet exist for this source. Never
// populate this plan from command-line IDs, caller JSON or a report hash.
func civ880OpenNormalPreparedObservation(ctx context.Context) (*civ880PreparedObservation,error) {
	if ctx==nil||ctx.Err()!=nil{return nil,errCiv880Observation}
	return nil,errCiv880Observation
}

func (prepared *civ880PreparedObservation) observeReadOnly(ctx context.Context) (civ880ReadOnlyObservation,error) {
	if prepared==nil{return civ880ReadOnlyObservation{},errCiv880Observation}
	prepared.mu.Lock();defer prepared.mu.Unlock()
	return civ880ObserveCreatedStageReadOnly(ctx,prepared.product,prepared.server,prepared.stage)
}

// Close never deletes. A created namespace remains held until Cleanup has
// reconciled it; failed closes retain their exact objects for the owner.
func (prepared *civ880PreparedObservation) Close() error {
	if prepared==nil{return nil}
	prepared.mu.Lock();defer prepared.mu.Unlock()
	if prepared.stage!=nil{
		// Keep the stage and parent until explicit Cleanup confirms the
		// exact created namespace is gone; Close is not a cleanup shortcut.
		prepared.stage.mu.Lock();mustKeep:=prepared.stage.rootAttempted&&!prepared.stage.rootCollision&&!prepared.stage.cleanupDone;ownsParent:=prepared.stage.parentOwned;prepared.stage.mu.Unlock()
		if mustKeep{return errCiv880StagePreserved}
		if err:=prepared.stage.Close();err!=nil{return err};prepared.stage=nil;if ownsParent{prepared.parent=nil}
	}
	if prepared.parent!=nil{if err:=prepared.parent.Close();err!=nil{return err};prepared.parent=nil}
	if prepared.product!=nil{prepared.product.clear();prepared.product=nil}
	prepared.server=nil;return nil
}

// Cleanup is explicit: unknown identity, a foreign entry, a failed delete or
// an uncertain close leaves the same stage object owned for reconciliation.
func (prepared *civ880PreparedObservation) Cleanup(ctx context.Context) error {
	if prepared==nil{return nil};if ctx==nil||ctx.Err()!=nil{return errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	prepared.mu.Lock();defer prepared.mu.Unlock()
	if prepared.stage!=nil{
		if err:=prepared.stage.Cleanup(ctx);err!=nil{return err}
		prepared.stage.mu.Lock();ownsParent:=prepared.stage.parentOwned;prepared.stage.mu.Unlock()
		if err:=prepared.stage.Close();err!=nil{return err};prepared.stage=nil;if ownsParent{prepared.parent=nil}
	}
	if prepared.parent!=nil{if err:=prepared.parent.Close();err!=nil{return err};prepared.parent=nil}
	if prepared.product!=nil{prepared.product.clear();prepared.product=nil};prepared.server=nil
	return nil
}

func civ880OpenHeldObservation(ctx context.Context, root string) (*civ880HeldObservation,error) {
	lease := &civ880HeldObservation{root:root}
	if ctx == nil || ctx.Err() != nil || !filepath.IsAbs(root) || filepath.Clean(root) != root { return lease,errCiv880Observation }
	if _,ok := ctx.Deadline(); !ok { return lease,errCiv880Observation }
	var parents []string
	for p:=root; ; p=filepath.Dir(p) {
		parents=append(parents,p)
		if len(parents)>64 { return lease,errCiv880Observation }
		if filepath.Dir(p)==p { break }
	}
	for i:=len(parents)-1; i>=0; i-- {
		ptr,err:=syscall.UTF16PtrFromString(parents[i]);if err!=nil{return lease,errCiv880Observation}
		h,err:=syscall.CreateFile(ptr,0x20080,syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,nil,syscall.OPEN_EXISTING,0x02000000|0x00200000,0)
		if err!=nil{return lease,errCiv880Observation}
		parent:=os.NewFile(uintptr(h),parents[i]);if parent==nil{lease.unwrapped=append(lease.unwrapped,h);return lease,errCiv880Observation}
		lease.parents=append(lease.parents,parent);lease.paths=append(lease.paths,parents[i])
		if _,err:=civ880ParentObservation(parent,parents[i]);err!=nil{return lease,errCiv880Observation}
		if ctx.Err()!=nil { return lease,errCiv880Observation }
	}
	seen:=map[softwareIdentity]bool{}
	for i,spec:=range civ880RoleSpecs() {
		path:=filepath.Join(root,spec.filename)
		ptr,err:=syscall.UTF16PtrFromString(path);if err!=nil{return lease,errCiv880Observation}
		h,err:=syscall.CreateFile(ptr,syscall.GENERIC_READ,syscall.FILE_SHARE_READ,nil,syscall.OPEN_EXISTING,0x00200000,0)
		if err!=nil{return lease,errCiv880Observation}
		file:=os.NewFile(uintptr(h),path)
		if file==nil { lease.unwrapped=append(lease.unwrapped,h);return lease,errCiv880Observation }
		lease.leaves[i]=file
		actual,ok:=handlePath(h);if !ok||!sameCleanPath(actual,path){return lease,errCiv880Observation}
		id,err:=fileSoftwareIdentity(file)
		if err!=nil || seen[id] || ctx.Err()!=nil { return lease,errCiv880Observation }
		seen[id]=true
	}
	return lease,nil
}

func civ880ParentObservation(file *os.File, path string) (string,error) {
	if file==nil { return "",errCiv880Observation }
	h:=syscall.Handle(file.Fd());var info syscall.ByHandleFileInformation
	actual,ok:=handlePath(h)
	if !ok || !sameCleanPath(actual,path) || syscall.GetFileInformationByHandle(h,&info)!=nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT!=0 || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY==0 || info.VolumeSerialNumber==0 || (info.FileIndexHigh==0&&info.FileIndexLow==0) { return "",errCiv880Observation }
	acl,err:=updateFileACL(file);if err!=nil||acl=="" { return "",errCiv880Observation }
	return digest([]byte(fmt.Sprintf("%d:%d:%d:%s",info.VolumeSerialNumber,uint64(info.FileIndexHigh)<<32|uint64(info.FileIndexLow),uint64(info.CreationTime.HighDateTime)<<32|uint64(info.CreationTime.LowDateTime),acl))),nil
}

func civ880CheckHeldBytes(ctx context.Context, file *os.File, expected []byte, hash string) error {
	if ctx==nil || ctx.Err()!=nil || file==nil || len(expected)==0 || len(expected)>civ880MaxPE || !civ880LowerHex(hash,64) || digest(expected)!=hash { return errCiv880Observation }
	info,err:=file.Stat();if err!=nil || info.Size()!=int64(len(expected)) { return errCiv880Observation }
	var scratch [64<<10]byte;defer clear(scratch[:])
	h:=sha256.New()
	for offset:=0;offset<len(expected); {
		if ctx.Err()!=nil { return errCiv880Observation }
		next:=len(expected)-offset;if next>len(scratch){next=len(scratch)}
		n,readErr:=file.ReadAt(scratch[:next],int64(offset))
		if readErr!=nil || n!=next || !bytes.Equal(scratch[:n],expected[offset:offset+n]) { return errCiv880Observation }
		_,_ = h.Write(scratch[:n]);offset+=n
	}
	var tail [1]byte
	n,readErr:=file.ReadAt(tail[:],int64(len(expected)))
	if n!=0 || readErr!=io.EOF || hex.EncodeToString(h.Sum(nil))!=hash || ctx.Err()!=nil { return errCiv880Observation }
	return nil
}

func civ880HeldInventory(root string) bool {
	entries,err:=os.ReadDir(root);if err!=nil || len(entries)!=5 { return false }
	wanted:=map[string]bool{};for _,spec:=range civ880RoleSpecs(){wanted[spec.filename]=true}
	for _,entry:=range entries {
		if !wanted[entry.Name()] || entry.IsDir() || entry.Type()&os.ModeSymlink!=0 { return false }
		delete(wanted,entry.Name())
	}
	return len(wanted)==0
}

// Both passes use the same five non-write-sharing handles, not a reopen by
// pathname. ACL and ancestor snapshots are comparisons, not an assertion that
// an untrusted directory ACL grants or excludes any particular principal.
func (lease *civ880HeldObservation) inspect(ctx context.Context, product *civ880ObservedProduct) error {
	if lease==nil{return errCiv880Observation}
	lease.mu.Lock();defer lease.mu.Unlock()
	if lease.closed || product==nil || ctx==nil || ctx.Err()!=nil || len(lease.parents)==0 || len(lease.parents)!=len(lease.paths) { return errCiv880Observation }
	product.mu.Lock();defer product.mu.Unlock()
	if product.closed{return errCiv880Observation}
	if _,ok:=ctx.Deadline();!ok { return errCiv880Observation }
	parents:=make([]string,len(lease.parents))
	for i,parent:=range lease.parents { value,err:=civ880ParentObservation(parent,lease.paths[i]);if err!=nil{return err};parents[i]=value }
	if !civ880HeldInventory(lease.root) { return errCiv880Observation }
	var ids [5]softwareIdentity;var acls [5]string
	for i,spec:=range civ880RoleSpecs() {
		file:=lease.leaves[i];if file==nil || product.files[i].Role!=spec.role || product.files[i].Name!=spec.filename { return errCiv880Observation }
		path,ok:=handlePath(syscall.Handle(file.Fd()));if !ok || !sameCleanPath(path,filepath.Join(lease.root,spec.filename)) { return errCiv880Observation }
		id,err:=fileSoftwareIdentity(file);if err!=nil{return errCiv880Observation};ids[i]=id
		for j:=0;j<i;j++ {if ids[j]==id{return errCiv880Observation}}
		acl,err:=updateFileACL(file);if err!=nil||acl==""{return errCiv880Observation};acls[i]=acl
		if civ880CheckHeldBytes(ctx,file,product.files[i].data,product.files[i].SHA)!=nil || civ880ObservePEHeader(product.files[i].data,spec.role)!=nil{return errCiv880Observation}
	}
	if !civ880HeldInventory(lease.root) { return errCiv880Observation }
	for i,file:=range lease.leaves {
		id,err:=fileSoftwareIdentity(file);if err!=nil||id!=ids[i]{return errCiv880Observation}
		acl,err:=updateFileACL(file);if err!=nil||acl!=acls[i]{return errCiv880Observation}
		if civ880CheckHeldBytes(ctx,file,product.files[i].data,product.files[i].SHA)!=nil{return errCiv880Observation}
	}
	for i,parent:=range lease.parents { value,err:=civ880ParentObservation(parent,lease.paths[i]);if err!=nil||value!=parents[i]{return errCiv880Observation} }
	return nil
}

type civ880ReadOnlyObservation struct {
	Source,Tree,ManifestSHA,CargoSHA string
	RunID int64
	Attempt int
	JobID,ArtifactID int64
	FileSHA [5]string
}

// This combines the same observed server generation, selected H1 bytes, and
// already-held local objects. It opens no game, executable, DLL, or IPC. The
// result is still an observation: without authenticated reviewed H1, complete
// source proof, and operation-owned staging, it grants no downstream access.
func civ880ReadOnlyBinding(ctx context.Context, product *civ880ObservedProduct, server *civ880ServerObservation) (civ880ReadOnlyObservation,error) {
	var result civ880ReadOnlyObservation
	if ctx==nil||ctx.Err()!=nil||product==nil||server==nil||server.Run.RepositoryID!=civ880RepositoryID||server.Run.HeadRepositoryID!=civ880RepositoryID||server.Artifact.RepositoryID!=civ880RepositoryID||server.Artifact.HeadRepositoryID!=civ880RepositoryID||server.Run.Status!="completed"||server.Run.Conclusion!="success"||server.Run.WorkflowPath!=".github/workflows/rust.yml"||server.Run.WorkflowID<=0||server.Job.Status!="completed"||server.Job.Conclusion!="success"||product.Source!=server.Run.HeadSHA||product.RunID!=server.Run.RunID||product.Attempt!=int64(server.Run.Attempt)||server.Job.RunID!=server.Run.RunID||server.Job.Attempt!=server.Run.Attempt||server.Artifact.RunID!=server.Run.RunID||server.Job.JobID<=0||server.Job.CheckRunID<=0||server.Artifact.ID<=0||server.Artifact.Expired||server.Artifact.Bytes!=product.ArchiveBytes||server.Artifact.Digest!="sha256:"+product.ArchiveSHA||!civ880LowerHex(product.ArchiveSHA,64){return result,errCiv880Observation}
	if server.Artifact.Name!=fmt.Sprintf("civ6-windows-product-%s-%d-%d",server.Run.HeadSHA,server.Run.RunID,server.Run.Attempt){return result,errCiv880Observation}
	result=civ880ReadOnlyObservation{Source:product.Source,Tree:product.Tree,ManifestSHA:product.ManifestSHA,CargoSHA:product.CargoSHA,RunID:product.RunID,Attempt:server.Run.Attempt,JobID:server.Job.JobID,ArtifactID:server.Artifact.ID}
	for i,file:=range product.files{result.FileSHA[i]=file.SHA}
	return result,nil
}

func civ880ObserveReadOnly(ctx context.Context, product *civ880ObservedProduct, server *civ880ServerObservation, held *civ880HeldObservation) (civ880ReadOnlyObservation,error) {
	var empty civ880ReadOnlyObservation
	result,err:=civ880ReadOnlyBinding(ctx,product,server);if err!=nil||held==nil{return empty,errCiv880Observation}
	if held.inspect(ctx,product)!=nil{return empty,errCiv880Observation};return result,nil
}

func civ880ObserveCreatedStageReadOnly(ctx context.Context,product *civ880ObservedProduct,server *civ880ServerObservation,stage *civ880CreatedStage) (civ880ReadOnlyObservation,error) {
	var empty civ880ReadOnlyObservation
	result,err:=civ880ReadOnlyBinding(ctx,product,server);if err!=nil||stage==nil{return empty,errCiv880Observation}
	if stage.inspect(ctx,product)!=nil{return empty,errCiv880Observation};return result,nil
}

// Created stages are inspected through the original read/write handles while
// those same handles deny other writers and deletion sharing. This function
// does not reopen by pathname and never executes or loads any output.
func (stage *civ880CreatedStage) inspect(ctx context.Context,product *civ880ObservedProduct) error {
	if stage==nil||product==nil||ctx==nil||ctx.Err()!=nil{return errCiv880Observation};if _,ok:=ctx.Deadline();!ok{return errCiv880Observation}
	stage.mu.Lock();defer stage.mu.Unlock();product.mu.Lock();defer product.mu.Unlock()
	if stage.closed||stage.cleanupStarted||!stage.complete||stage.root==nil||stage.parent==nil||!stage.parentOwned||product.closed||!stage.parent.current(ctx){return errCiv880Observation}
	stamp,err:=civ880ParentObservation(stage.root,stage.rootPath);if err!=nil||stamp!=stage.rootStamp||!civ880StageInventory(ctx,stage.root){return errCiv880Observation}
	for i,spec:=range civ880RoleSpecs(){
		file:=stage.leaves[i];value:=product.files[i];if file==nil||value.Role!=spec.role||value.Name!=spec.filename{return errCiv880Observation}
		name,ok:=handlePath(syscall.Handle(file.Fd()));if !ok||!sameCleanPath(name,filepath.Join(stage.rootPath,spec.filename)){return errCiv880Observation}
		id,err:=fileSoftwareIdentity(file);if err!=nil||id!=stage.ids[i]{return errCiv880Observation}
		acl,err:=updateFileACL(file);if err!=nil||acl!=stage.acls[i]||civ880CheckHeldBytes(ctx,file,value.data,value.SHA)!=nil{return errCiv880Observation}
		exports,err:=civ880ObservePEExports(value.data,spec.role);if err!=nil||len(exports)!=len(civ880RequiredExports(spec.role)){return errCiv880Observation}
	}
	if !civ880StageInventory(ctx,stage.root){return errCiv880Observation}
	stamp,err=civ880ParentObservation(stage.root,stage.rootPath);if err!=nil||stamp!=stage.rootStamp||!stage.parent.current(ctx){return errCiv880Observation}
	for i,file:=range stage.leaves{id,err:=fileSoftwareIdentity(file);if err!=nil||id!=stage.ids[i]{return errCiv880Observation};acl,err:=updateFileACL(file);if err!=nil||acl!=stage.acls[i]||civ880CheckHeldBytes(ctx,file,product.files[i].data,product.files[i].SHA)!=nil{return errCiv880Observation}}
	return nil
}

// A close error leaves the corresponding handle referenced and the lease
// unclosed so the owner can record and reconcile the uncertain resource.
func (lease *civ880HeldObservation) Close() error {
	if lease==nil{return nil}
	lease.mu.Lock();defer lease.mu.Unlock()
	if lease.closed { return nil }
	var failures []error
	for i:=len(lease.leaves)-1;i>=0;i-- { if lease.leaves[i]!=nil { if err:=lease.leaves[i].Close();err!=nil { failures=append(failures,err) } else { lease.leaves[i]=nil } } }
	for i:=len(lease.unwrapped)-1;i>=0;i-- { if lease.unwrapped[i]!=0 { if err:=syscall.CloseHandle(lease.unwrapped[i]);err!=nil { failures=append(failures,err) } else { lease.unwrapped[i]=0 } } }
	if len(failures)>0 { return errors.Join(append([]error{errCiv880Observation},failures...)...) }
	for i:=len(lease.parents)-1;i>=0;i-- { if lease.parents[i]!=nil { if err:=lease.parents[i].Close();err!=nil { return errors.Join(errCiv880Observation,err) } else { lease.parents[i]=nil } } }
	lease.closed=true
	return nil
}
