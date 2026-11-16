//go:build windows

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"golang.org/x/sys/windows"
)

func civ880Fixture(t *testing.T) ([]map[string]any, civ880CargoPackageIDs, string) {
	t.Helper()
	root := filepath.Clean(filepath.Join(t.TempDir(), "owned-target"))
	ids := civ880CargoPackageIDs{Autotest:"path+file:///fixture/civ6-autotest-rs#0.1.0", Bootstrap:"path+file:///fixture/civ6-bootstrap-rs#0.1.0", Bridge:"path+file:///fixture/civ6-native-bridge-rs#0.1.0"}
	var records []map[string]any
	for _, spec := range civ880RoleSpecs() {
		file := filepath.Join(root,"release",spec.filename)
		kind, crateType := []string{"bin"}, []string{"bin"}
		var exe any = file
		if spec.dll { kind=[]string{"cdylib","rlib"};crateType=[]string{"cdylib","rlib"};exe=nil }
		records=append(records,map[string]any{
			"reason":"compiler-artifact", "package_id":civ880PackageID(ids,spec.packageName),
			"target":map[string]any{"name":spec.target,"kind":kind,"crate_types":crateType},
			"profile":map[string]any{"opt_level":"3","debug_assertions":false,"overflow_checks":false,"debuginfo":nil,"test":false},
			"filenames":[]string{file},"executable":exe,"fresh":false,"features":[]string{},
		})
	}
	return records,ids,root
}

// The fixture has the same seven-entry shape as the supplied publisher-v6
// source. Its origin and PE export metadata are deliberately not authorities.
func civ880ProductArchiveFixture(t *testing.T, mutate func(map[string]any,map[string]any,map[string][]byte)) []byte {
	t.Helper()
	var rows,records []any
	payloads:=make(map[string][]byte)
	for _,spec:=range civ880RoleSpecs(){
		payload,exports:=civ880FixtureCompletePE(t,spec)
		payloads[spec.filename]=payload
		kind:=[]string{"bin"};if spec.dll{kind=[]string{"cdylib","rlib"}}
		recordSHA:=digest([]byte("record "+spec.role))
		rows=append(rows,map[string]any{"name":spec.filename,"bytes":len(payload),"sha256":digest(payload),"target":"x86_64-pc-windows-msvc","kind":func()string{if spec.dll{return "cdylib"};return "bin"}(),"exports":exports,"cargo_record_sha256":recordSHA})
		records=append(records,map[string]any{
			"filename":civ880ObservedRelative(spec),"cargo_record_sha256":recordSHA,
			"package":map[string]any{"name":spec.packageName,"version":"0.1.0","manifest":"tools/"+spec.packageName+"/Cargo.toml","id_sha256":digest([]byte("package "+spec.packageName))},
			"target":map[string]any{"name":spec.target,"kind":kind,"crate_types":kind},
			"profile":map[string]any{"opt_level":"3","debuginfo":nil,"debug_assertions":false,"overflow_checks":false,"test":false},
			"features":[]string{},"fresh":false,
		})
	}
	sourceRows:=[]any{map[string]any{"name":"tools/Cargo.lock","bytes":3,"sha256":strings.Repeat("c",64),"git_blob_sha1":strings.Repeat("d",40)}}
	sourceDigest:=digest([]byte(strings.Repeat("c",64)+" "+strings.Repeat("d",40)+" 3 tools/Cargo.lock\n"))
	vendorSum:=strings.Repeat("f",64);vendorRows:=[]any{map[string]any{"name":"Cargo.toml","bytes":3,"sha256":vendorSum,"git_blob_sha1":nil}}
	vendorDigest:=digest([]byte(vendorSum+" - 3 Cargo.toml\n"))
	var profile []any;for _,name:=range civ880ProfileDefaults{profile=append(profile,map[string]any{"name":name,"present":false,"sha256":nil})};profile=append(profile,map[string]any{"name":"CARGO_PROFILE_DEV_DEBUG","present":true,"sha256":strings.Repeat("2",64)},map[string]any{"name":"CARGO_PROFILE_TEST_DEBUG","present":true,"sha256":strings.Repeat("3",64)})
	inputs:=map[string]any{"source_inventory_sha256":sourceDigest,"cargo_lock_sha256":strings.Repeat("c",64),"source_file_count":1,"inventory_format":"sha256-lines-v2","source_files":sourceRows,"configs":[]any{},"vendors":[]any{map[string]any{"name":"serde","version":"1.0.0","source_kind":"crates.io","storage_kind":"registry-extracted","sha256":vendorDigest,"manifest_sha256":vendorSum,"checksum_metadata_sha256":nil,"files":vendorRows}},"profile_environment":profile,"environment":[]any{map[string]any{"name":"CARGO_INCREMENTAL","sha256":strings.Repeat("1",64)},map[string]any{"name":"CARGO_PROFILE_DEV_DEBUG","sha256":strings.Repeat("2",64)},map[string]any{"name":"CARGO_PROFILE_TEST_DEBUG","sha256":strings.Repeat("3",64)}}}
	clone:=func(value map[string]any)map[string]any{b,err:=json.Marshal(value);if err!=nil{t.Fatal(err)};var out map[string]any;if json.Unmarshal(b,&out)!=nil{t.Fatal("fixture JSON clone")};return out}
	cargo:=map[string]any{"schema":"civ6-selected-cargo-artifacts/v1","records":records,"inputs_before":clone(inputs),"inputs_after":clone(inputs)}
	binArgs:=[]string{"cargo","+1.95.0","build","--locked","--release","--manifest-path","tools/Cargo.toml","--workspace","--bins","--message-format=json-render-diagnostics"}
	dllArgs:=[]string{"cargo","+1.95.0","build","--locked","--release","--manifest-path","tools/Cargo.toml","--package","civ6-native-bridge-rs","--package","civ6-bootstrap-rs","--lib","--target","x86_64-pc-windows-msvc","--message-format=json-render-diagnostics"}
	manifest:=map[string]any{"schema":"civ6-windows-product-artifacts/v1","repository":"AutoDevJapan/civ6-banseiki","repository_id":civ880RepositoryID,"source_sha":civ880SourceCommit,"source_tree":civ880SourceTree,"target":"x86_64-pc-windows-msvc","producer":map[string]any{"run_id":8,"run_attempt":1,"job":"test","runner_os":"Windows","runner_arch":"X64","workflow_blob":strings.Repeat("a",40)},"compiler":map[string]any{"toolchain":"1.95.0","host":"x86_64-pc-windows-msvc","rustc_sha256":strings.Repeat("a",64),"cargo_sha256":strings.Repeat("b",64),"rustup_manifest_sha256":strings.Repeat("c",64)},"commands":map[string]any{"bins":map[string]any{"argv":binArgs,"exit_code":0,"cargo_json_sha256":strings.Repeat("d",64),"stderr_sha256":digest(nil)},"dlls":map[string]any{"argv":dllArgs,"exit_code":0,"cargo_json_sha256":strings.Repeat("e",64),"stderr_sha256":digest(nil)}},"inputs":inputs,"source_declared_prepare_version":1,"files":rows}
	manifest["context_sha256"]=strings.Repeat("f",64);manifest["completion_sha256"]=strings.Repeat("e",64)
	manifest["compiler"].(map[string]any)["cargo_dispatcher_sha256"]=strings.Repeat("d",64)
	manifest["compiler"].(map[string]any)["rustup_dispatcher_sha256"]=strings.Repeat("e",64)
	if mutate!=nil{mutate(manifest,cargo,payloads)}
	cargoBytes,err:=json.Marshal(cargo);if err!=nil{t.Fatal(err)}
	manifest["cargo_artifacts_sha256"]=digest(cargoBytes)
	manifestBytes,err:=json.Marshal(manifest);if err!=nil{t.Fatal(err)}
	payloads["manifest.json"]=manifestBytes;payloads["cargo-artifacts.json"]=cargoBytes
	var archive bytes.Buffer
	w:=zip.NewWriter(&archive)
	for name,payload:=range payloads{entry,err:=w.Create(name);if err!=nil{t.Fatal(err)};if _,err=entry.Write(payload);err!=nil{t.Fatal(err)}}
	if err:=w.Close();err!=nil{t.Fatal(err)}
	return archive.Bytes()
}

func TestCiv880ProductArchiveComparesPublisherRolesWithoutGrant(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	archive:=civ880ProductArchiveFixture(t,nil)
	product,err:=civ880ObserveProductArchive(ctx,archive);if err!=nil{t.Fatal(err)}
	defer product.clear()
	for i,spec:=range civ880RoleSpecs(){if product.files[i].Role!=spec.role||product.files[i].Name!=spec.filename||len(product.files[i].data)==0{t.Fatalf("role %s missing",spec.role)}}
	if product.Source!=civ880SourceCommit||product.Tree!=civ880SourceTree{t.Fatal("source facts differ")}
	if _,err:=civ880ObserveProductArchive(context.Background(),archive);err==nil{t.Fatal("missing deadline accepted")}
}

func TestCiv880ProductArchiveRejectsMismatchAndIncompleteInventory(t *testing.T){
	for _,name:=range []string{"missing","extra","changed-bytes","changed-hash","crossed-role","cargo-record","duplicate-role","wrong-package","wrong-source-format","wrong-kind","unknown-exit","bad-command","bad-compiler","bad-features","missing-export","forwarded-export","wrong-export-kind","wrong-export-claim","null-exports","changed-after","inventory-digest","inventory-path","coherent-inventory-digest","coherent-vendor-file","coherent-env-omission","coherent-profile-mismatch"}{
		t.Run(name,func(t *testing.T){
			archive:=civ880ProductArchiveFixture(t,func(m,c map[string]any,files map[string][]byte){
				rows:=m["files"].([]any);records:=c["records"].([]any)
				switch name{
				case "missing":delete(files,"civ6-autotest.exe")
				case "extra":files["extra.exe"]=[]byte{1}
				case "changed-bytes":files["civ6-autotest.exe"][1]^=1
				case "changed-hash":rows[0].(map[string]any)["sha256"]=strings.Repeat("d",64)
				case "crossed-role":records[0].(map[string]any)["filename"]=records[1].(map[string]any)["filename"]
				case "cargo-record":records[0].(map[string]any)["cargo_record_sha256"]=strings.Repeat("f",64)
				case "duplicate-role":rows[0]=rows[1]
				case "wrong-package":records[0].(map[string]any)["package"].(map[string]any)["name"]="other"
				case "wrong-source-format":m["source_sha"]="not-a-commit"
				case "wrong-kind":rows[0].(map[string]any)["kind"]="cdylib"
				case "unknown-exit":m["commands"].(map[string]any)["bins"].(map[string]any)["exit_code"]=nil
				case "bad-command":m["commands"].(map[string]any)["bins"].(map[string]any)["argv"]=[]string{"cargo","test"}
				case "bad-compiler":m["compiler"].(map[string]any)["host"]="different"
				case "bad-features":records[0].(map[string]any)["features"]=[]string{"../invalid"}
				case "missing-export":p:=files["civ6_native_bridge_rs.dll"];p[0x800]^=1;rows[3].(map[string]any)["sha256"]=digest(p)
				case "forwarded-export":p:=files["civ6_native_bridge_rs.dll"];binary.LittleEndian.PutUint32(p[0x500:0x504],0x2001);rows[3].(map[string]any)["sha256"]=digest(p)
				case "wrong-export-kind":p:=files["civ6_native_bridge_rs.dll"];binary.LittleEndian.PutUint32(p[0x500:0x504],0x2900);rows[3].(map[string]any)["sha256"]=digest(p)
				case "wrong-export-claim":exports:=rows[3].(map[string]any)["exports"].([]any);exports[0].(map[string]any)["rva"]=1
				case "null-exports":rows[0].(map[string]any)["exports"]=nil
				case "changed-after":c["inputs_after"].(map[string]any)["cargo_lock_sha256"]=strings.Repeat("e",64)
				case "inventory-digest":m["inputs"].(map[string]any)["source_inventory_sha256"]=strings.Repeat("e",64)
				case "inventory-path":m["inputs"].(map[string]any)["source_files"].([]any)[0].(map[string]any)["name"]="../outside"
				case "coherent-inventory-digest","coherent-vendor-file","coherent-env-omission","coherent-profile-mismatch":
					for _,input:=range []map[string]any{m["inputs"].(map[string]any),c["inputs_before"].(map[string]any),c["inputs_after"].(map[string]any)}{
						switch name{
						case "coherent-inventory-digest":input["source_inventory_sha256"]=strings.Repeat("e",64)
						case "coherent-vendor-file":input["vendors"].([]any)[0].(map[string]any)["files"].([]any)[0].(map[string]any)["sha256"]=strings.Repeat("e",64)
						case "coherent-env-omission":input["environment"]=[]any{}
						case "coherent-profile-mismatch":for _,item:=range input["profile_environment"].([]any){row:=item.(map[string]any);if row["name"]=="CARGO_PROFILE_DEV_DEBUG"{row["sha256"]=strings.Repeat("a",64)}}
						}
					}
				}
			})
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
			if result,err:=civ880ObserveProductArchive(ctx,archive);err==nil{result.clear();t.Fatal("invalid product archive accepted")}
		})
	}
}

func TestCiv880NestedDuplicateKeysAreRejected(t *testing.T){
	for _,raw:=range []string{`{"top":{"value":1,"value":2}}`,`{"rows":[{"id":1,"id":2}]}`,`{"x":1} {"y":2}`} {
		if civ880UniqueJSON([]byte(raw)){t.Fatal("ambiguous nested JSON accepted")}
	}
	if !civ880UniqueJSON([]byte(`{"top":{"a":1,"b":[1,2,3]}}`)){t.Fatal("valid nested JSON rejected")}
}

func TestCiv880ProfileDistinguishesAbsentFromEmpty(t *testing.T){
	var rows []map[string]any;for _,name:=range civ880ProfileDefaults{rows=append(rows,map[string]any{"name":name,"present":false,"sha256":nil})}
	raw,_:=json.Marshal(rows);if !civ880ProfileProjection(raw){t.Fatal("default absence projection rejected")}
	rows[0]["present"]=true;rows[0]["sha256"]=digest(nil);raw,_=json.Marshal(rows);if !civ880ProfileProjection(raw){t.Fatal("present empty value rejected")}
	rows[0]["present"]=false;raw,_=json.Marshal(rows);if civ880ProfileProjection(raw){t.Fatal("absent value retained a digest")}
	rows[0]["sha256"]=nil;rows=rows[:len(rows)-1];raw,_=json.Marshal(rows);if civ880ProfileProjection(raw){t.Fatal("missing default absence accepted")}
}

func civ880CaptureMapFixture(phase, stdout, stderr string, stderrBytes int64) map[string]any {
	return map[string]any{
		"schema": "civ6-native-capture/v3", "evidence_kind": "historical_capture_snapshot", "phase": phase,
		"native_exit_code": 0, "collection_complete": true, "stop_reason": "completed",
		"capture_stop_reason": "completed", "start_returned": true, "root_exit_observed": true,
		"failure_stage": nil, "exception_kind": nil, "exception_hresult": nil, "native_error_code": nil,
		"result_save_state": "saved", "result_bytes": 1024, "result_sha256": strings.Repeat("a", 64),
		"secondary_failure_count": 0, "stdout_state": "owned_hashed", "stderr_state": "owned_hashed",
		"stdout_bytes": 42, "stdout_sha256": stdout, "stderr_bytes": stderrBytes, "stderr_sha256": stderr,
	}
}

func civ880CaptureLineFixture(t *testing.T, event map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	line := append([]byte("2026-11-16T00:00:00.000Z CIV_PRODUCT_NATIVE_CAPTURE "), raw...)
	return append(line, '\n')
}


func TestCiv880CaptureV3ActualPublisherObservation(t *testing.T) {
	const observed = `2026-11-16T08:04:30.1385263Z CIV_PRODUCT_NATIVE_CAPTURE {"schema":"civ6-native-capture/v3","evidence_kind":"historical_capture_snapshot","phase":"bins","native_exit_code":0,"collection_complete":true,"stop_reason":"completed","capture_stop_reason":"completed","start_returned":true,"root_exit_observed":true,"failure_stage":null,"exception_kind":null,"exception_hresult":null,"native_error_code":null,"result_save_state":"saved","result_bytes":4263,"result_sha256":"4addfdc46785e5d41039c107c9dfeb800e25b4045486b64f7c49c994deb7771b","secondary_failure_count":0,"stdout_state":"owned_hashed","stderr_state":"owned_hashed","stdout_bytes":283648,"stdout_sha256":"f8250a7794c042d5ad3bb064a90b8d2c374a47d05cabe0c5cbe9364dff3e1da6","stderr_bytes":7357,"stderr_sha256":"42b8cf44284037f29b94827ae540506b2a894c74312000753658c5d891a6b2a1"}
2026-11-16T08:04:54.0556317Z CIV_PRODUCT_NATIVE_CAPTURE {"schema":"civ6-native-capture/v3","evidence_kind":"historical_capture_snapshot","phase":"dlls","native_exit_code":0,"collection_complete":true,"stop_reason":"completed","capture_stop_reason":"completed","start_returned":true,"root_exit_observed":true,"failure_stage":null,"exception_kind":null,"exception_hresult":null,"native_error_code":null,"result_save_state":"saved","result_bytes":4380,"result_sha256":"54c66481ed4b8a7d5ef9a6fb7daab297cf321ab04c0747a3e1d62b6374471a08","secondary_failure_count":0,"stdout_state":"owned_hashed","stderr_state":"owned_hashed","stdout_bytes":22380,"stdout_sha256":"fa44d3f0af31b7a4e3f44eb460ed80027e6f891e33ae4542f5168c2e86778f9a","stderr_bytes":722,"stderr_sha256":"c278d068565adbc23f8ff7c1a5f07887c28f9a6d9e5573c08c9ccbb7378104fc"}
`
	events, err := civ880CaptureEvents([]byte(observed))
	if err != nil {
		t.Fatal("actual publisher historical observations rejected", err)
	}
	product := &civ880ObservedProduct{
		Streams: [2]civ880StreamDigests{
			{
				StdoutSHA: "f8250a7794c042d5ad3bb064a90b8d2c374a47d05cabe0c5cbe9364dff3e1da6",
				StderrSHA: "42b8cf44284037f29b94827ae540506b2a894c74312000753658c5d891a6b2a1",
			},
			{
				StdoutSHA: "fa44d3f0af31b7a4e3f44eb460ed80027e6f891e33ae4542f5168c2e86778f9a",
				StderrSHA: "c278d068565adbc23f8ff7c1a5f07887c28f9a6d9e5573c08c9ccbb7378104fc",
			},
		},
	}
	if !civ880CapturesMatch(events, product) {
		t.Fatal("actual historical stream digests differ")
	}
	product.Streams[1].StdoutSHA = strings.Repeat("f", 64)
	if civ880CapturesMatch(events, product) {
		t.Fatal("actual historical record accepted different product stream")
	}
}

func TestCiv880CaptureV3HistoricalShapeAndRequiredFacts(t *testing.T) {
	normal := func() map[string]any {
		return civ880CaptureMapFixture("bins", strings.Repeat("d", 64), digest(nil), 0)
	}
	dlls := civ880CaptureLineFixture(t, civ880CaptureMapFixture("dlls", strings.Repeat("e", 64), digest(nil), 0))
	pair := func(event map[string]any) []byte {
		return append(civ880CaptureLineFixture(t, event), dlls...)
	}
	if _, err := civ880CaptureEvents(pair(normal())); err != nil {
		t.Fatal("normal historical observation rejected", err)
	}
	for key := range normal() {
		t.Run("missing-"+key, func(t *testing.T) {
			event := normal()
			delete(event, key)
			if _, err := civ880CaptureEvents(pair(event)); err == nil {
				t.Fatal("missing required snapshot field accepted")
			}
		})
	}
	for key, value := range normal() {
		t.Run("wrong-type-"+key, func(t *testing.T) {
			event := normal()
			if value == nil {
				event[key] = "missing-original-diagnostic"
			} else {
				event[key] = map[string]any{"wrong": true}
			}
			if _, err := civ880CaptureEvents(pair(event)); err == nil {
				t.Fatal("wrong snapshot field type accepted")
			}
		})
	}
	for key := range normal() {
		t.Run("case-alias-"+key, func(t *testing.T) {
			event := normal()
			value := event[key]
			delete(event, key)
			event[strings.ToUpper(key)] = value
			if _, err := civ880CaptureEvents(pair(event)); err == nil {
				t.Fatal("case-folded field alias replaced a required exact key")
			}
		})
	}
	for _, control := range []struct {
		name, key string
		value     any
	}{
		{"old-schema", "schema", "civ6-native-capture/v2"},
		{"invented-current-receipt", "evidence_kind", "current_receipt"},
		{"native-failure", "native_exit_code", 23},
		{"unknown-exit", "native_exit_code", nil},
		{"incomplete", "collection_complete", false},
		{"cancel", "stop_reason", "cancelled"},
		{"deadline", "capture_stop_reason", "deadline"},
		{"start-not-returned", "start_returned", false},
		{"root-not-observed", "root_exit_observed", false},
		{"cleanup-primary", "failure_stage", "result_publication_release"},
		{"cleanup-exception", "exception_kind", "io"},
		{"cleanup-hresult", "exception_hresult", -2146232800},
		{"cleanup-native-code", "native_error_code", 32},
		{"unavailable-publication", "result_save_state", "unavailable"},
		{"missing-result-bytes", "result_bytes", nil},
		{"zero-result-bytes", "result_bytes", 0},
		{"bad-result-digest", "result_sha256", "not-a-digest"},
		{"secondary-diagnostic", "secondary_failure_count", 1},
		{"raw-not-owned", "stdout_state", "not_owned"},
		{"stderr-unavailable", "stderr_state", "owned_unavailable"},
		{"stdout-empty", "stdout_bytes", 0},
		{"unknown-field", "extra", false},
	} {
		t.Run(control.name, func(t *testing.T) {
			event := normal()
			event[control.key] = control.value
			if _, err := civ880CaptureEvents(pair(event)); err == nil {
				t.Fatal("failed or ambiguous snapshot accepted")
			}
		})
	}
	raw := civ880CaptureLineFixture(t, normal())
	duplicated := bytes.Replace(raw, []byte(`"native_exit_code":0`), []byte(`"native_exit_code":0,"native_exit_code":0`), 1)
	if _, err := civ880CaptureEvents(append(duplicated, dlls...)); err == nil {
		t.Fatal("duplicate snapshot key accepted")
	}
	legacy := []byte("2026-11-16T00:00:00Z CIV_PRODUCT_NATIVE_CAPTURE {\"schema\":\"civ6-native-capture/v2\",\"phase\":\"bins\",\"exit_code\":0}\n")
	if _, err := civ880CaptureEvents(append(legacy, dlls...)); err == nil {
		t.Fatal("legacy v2 record was silently upgraded")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if prepared, err := civ880OpenNormalPreparedObservation(ctx); prepared != nil || err == nil {
		t.Fatal("historical records created a normal purpose")
	}
}

func TestCiv880CaptureEventsPreserveEmptyStderrAndRejectAmbiguity(t *testing.T){
	makeEvent:=func(phase,stdout,stderr string,stderrBytes int64)[]byte{return civ880CaptureLineFixture(t,civ880CaptureMapFixture(phase,stdout,stderr,stderrBytes))}
	bins:=makeEvent("bins",strings.Repeat("d",64),digest(nil),0);dlls:=makeEvent("dlls",strings.Repeat("e",64),digest(nil),0)
	product:=&civ880ObservedProduct{Streams:[2]civ880StreamDigests{{StdoutSHA:strings.Repeat("d",64),StderrSHA:digest(nil)},{StdoutSHA:strings.Repeat("e",64),StderrSHA:digest(nil)}}}
	lines:=append(append([]byte(nil),bins...),dlls...);events,err:=civ880CaptureEvents(lines);if err!=nil||!civ880CapturesMatch(events,product){t.Fatal("matching captures rejected",err)}
	if _,err:=civ880CaptureEvents(append(lines,bins...));err==nil{t.Fatal("duplicate phase accepted")}
	if _,err:=civ880CaptureEvents(append(bins,makeEvent("dlls",strings.Repeat("e",64),strings.Repeat("f",64),0)...));err==nil{t.Fatal("empty stderr with wrong digest accepted")}
	product.Streams[0].StderrSHA=strings.Repeat("f",64);if civ880CapturesMatch(events,product){t.Fatal("manifest stderr mismatch accepted")}
}

func TestCiv880CaptureV3RejectsMalformedExtraPhaseAndSkipsOtherProducerPhases(t *testing.T) {
	pair := func() []byte {
		bins := civ880CaptureLineFixture(t, civ880CaptureMapFixture("bins", strings.Repeat("d", 64), digest(nil), 0))
		dlls := civ880CaptureLineFixture(t, civ880CaptureMapFixture("dlls", strings.Repeat("e", 64), digest(nil), 0))
		return append(bins, dlls...)
	}
	for _, badPhase := range []any{nil, false, 1, []any{}, map[string]any{}, ""} {
		event := civ880CaptureMapFixture("metadata", strings.Repeat("d", 64), digest(nil), 0)
		event["phase"] = badPhase
		if _, err := civ880CaptureEvents(append(pair(), civ880CaptureLineFixture(t, event)...)); err == nil {
			t.Fatal("malformed extra phase was silently skipped")
		}
	}
	for _, phase := range []string{"git", "rustup", "metadata", "generator", "unknown"} {
		event := civ880CaptureMapFixture(phase, strings.Repeat("d", 64), digest(nil), 0)
		event["native_exit_code"] = 23
		event["collection_complete"] = false
		event["stop_reason"] = "cancelled"
		event["capture_stop_reason"] = "cancelled"
		event["start_returned"] = false
		event["root_exit_observed"] = false
		event["result_save_state"] = "unavailable"
		event["result_bytes"] = nil
		event["result_sha256"] = nil
		event["failure_stage"] = "result_publication_release"
		event["exception_kind"] = "io"
		event["exception_hresult"] = -2146232800
		event["secondary_failure_count"] = 1
		if _, err := civ880CaptureEvents(append(pair(), civ880CaptureLineFixture(t, event)...)); err != nil {
			t.Fatal("valid nonselected producer phase was subjected to bins/dlls success requirements", err)
		}
	}
}

func TestCiv880OriginRequiresOneExactJobLogRecord(t *testing.T){
	record:=civ880OriginObservation{Schema:"civ6-product-artifact-origin/v1",RepositoryID:civ880RepositoryID,Source:strings.Repeat("a",40),Tree:strings.Repeat("b",40),WorkflowBlob:strings.Repeat("c",40),RunID:8,Attempt:1,JobKey:"test",RunnerOS:"Windows",ArtifactID:11,ArtifactName:"fixed",UploadSHA:strings.Repeat("d",64),ManifestSHA:strings.Repeat("e",64),CargoSHA:strings.Repeat("f",64)}
	raw,err:=json.Marshal(record);if err!=nil{t.Fatal(err)}
	line:=append([]byte("2026-11-16T00:00:00.000Z CIV_PRODUCT_ARTIFACT_ORIGIN "),raw...);line=append(line,'\n')
	got,err:=civ880OriginFromJobLog(line);if err!=nil||got!=record{t.Fatal("valid job log origin rejected",err)}
	if _,err:=civ880OriginFromJobLog(append(append([]byte(nil),line...),line...));err==nil{t.Fatal("duplicate origin accepted")}
	if _,err:=civ880OriginFromJobLog(append([]byte("echo CIV_PRODUCT_ARTIFACT_ORIGIN "),raw...));err==nil{t.Fatal("command echo counted as origin")}
	bad:=bytes.Replace(raw,[]byte(`"artifact_id":11`),[]byte(`"artifact_id":11,"artifact_id":12`),1)
	if _,err:=civ880OriginFromJobLog(append([]byte("2026-11-16T00:00:00.000Z CIV_PRODUCT_ARTIFACT_ORIGIN "),bad...));err==nil{t.Fatal("duplicate origin field accepted")}
}

func TestCiv880InventoryCanonicalBytesAndOrder(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	row:=map[string]any{"name":"src/lib.rs","bytes":3,"sha256":strings.Repeat("0",64),"git_blob_sha1":nil}
	raw,err:=json.Marshal([]any{row});if err!=nil{t.Fatal(err)}
	_,sum,err:=civ880Inventory(ctx,raw,false)
	if err!=nil||sum!="1330e3a6132b2d61ac33e1be474cb10428f12cedaad825f94a209631fcc1e4e1"{t.Fatal("published inventory test vector differs",err,sum)}
	if _,_,err:=civ880Inventory(ctx,raw,true);err==nil{t.Fatal("source without Git blob accepted")}
	first:=map[string]any{"name":"z/file","bytes":1,"sha256":strings.Repeat("a",64),"git_blob_sha1":strings.Repeat("b",40)}
	second:=map[string]any{"name":"a/file","bytes":2,"sha256":strings.Repeat("c",64),"git_blob_sha1":strings.Repeat("d",40)}
	before,_:=json.Marshal([]any{first,second});after,_:=json.Marshal([]any{second,first})
	_,one,e1:=civ880Inventory(ctx,before,true);_,two,e2:=civ880Inventory(ctx,after,true)
	if e1!=nil||e2!=nil||one==two{t.Fatal("inventory order not bound")}
	for _,name:=range []string{"../file","/absolute","a//b","a/./b","a\\b","c:file","a\nb"}{
		bad:=map[string]any{"name":name,"bytes":1,"sha256":strings.Repeat("a",64),"git_blob_sha1":strings.Repeat("b",40)};wire,_:=json.Marshal([]any{bad})
		if _,_,err:=civ880Inventory(ctx,wire,true);err==nil{t.Fatalf("invalid name accepted: %q",name)}
	}
	duplicate,_:=json.Marshal([]any{first,first});if _,_,err:=civ880Inventory(ctx,duplicate,true);err==nil{t.Fatal("duplicate input accepted")}
}

func TestCiv880GitNamespaceBindsActualTreeAndEveryBlob(t *testing.T){
	const root="67168373e5904af5b860d8fac236ccd2f1f678b0"
	const nested="aef17a27228603d3774273323759a3558214c4aa"
	const one="4a58007052a65fbc2fc3f910f2855f45a4058e74"
	const two="e1d65540f4fdf72431ec47e001282cc7e8ed7c0c"
	const github="a5ca652c9b5651f97d96d4524b8f77de7a2c03ef"
	const workflows="e17f9c5dcc941b9ff6f575e8c43ea1bafc331125"
	const workflow="a1e5ef79bbf56e1ded3e230b67dcb4561ca95998"
	for _,mode:=range []string{"normal","changed-blob","truncated-tree","missing-source","extra-source","wrong-size","different-commit","wrong-repository","duplicate-entry","wrong-workflow-blob"}{
		t.Run(mode,func(t *testing.T){
			product:=&civ880ObservedProduct{Source:strings.Repeat("a",40),Tree:root,WorkflowBlob:workflow,SourceFiles:[]civ880SourceRecord{{Name:"top.txt",Bytes:6,SHA256:"b6a98d9ce9a2d9149288fa3df42d377c3e42737afdcdaf714e33c0a100b51060",GitBlobSHA1:one},{Name:"dir/Cargo.lock",Bytes:4,SHA256:"f44e64e75f3948e9f73f8dfa94721c4ce8cbb4f265c4790c702b2d41cfbf2753",GitBlobSHA1:two},{Name:".github/workflows/rust.yml",Bytes:9,SHA256:"8d3fb39ce523c6fc1dafec39102aef44e599f72fc3a22cf4c95612234571a570",GitBlobSHA1:workflow}}}
			if mode=="missing-source"{product.SourceFiles=product.SourceFiles[:1]};if mode=="extra-source"{product.SourceFiles=append(product.SourceFiles,civ880SourceRecord{Name:"extra.txt",Bytes:4,SHA256:product.SourceFiles[1].SHA256,GitBlobSHA1:two})};if mode=="wrong-size"{product.SourceFiles[0].Bytes++};if mode=="wrong-workflow-blob"{product.WorkflowBlob=strings.Repeat("d",40)}
			var mu sync.Mutex;blobRequests:=0
			client:=&http.Client{Transport:civ880RoundTrip(func(req *http.Request)(*http.Response,error){
				w:=httptest.NewRecorder();var body any
				switch req.URL.Path{
				case "/repos/AutoDevJapan/civ6-banseiki":id:=civ880RepositoryID;if mode=="wrong-repository"{id++};body=map[string]any{"id":id}
				case "/repos/AutoDevJapan/civ6-banseiki/git/commits/"+product.Source:tree:=root;if mode=="different-commit"{tree=strings.Repeat("e",40)};body=map[string]any{"sha":product.Source,"tree":map[string]any{"sha":tree}}
				case "/repos/AutoDevJapan/civ6-banseiki/git/trees/"+root:
					entries:=[]any{map[string]any{"path":".github","mode":"040000","type":"tree","sha":github},map[string]any{"path":"dir","mode":"040000","type":"tree","sha":nested},map[string]any{"path":"top.txt","mode":"100644","type":"blob","sha":one,"size":6}}
					if mode=="duplicate-entry"{entries=append(entries,entries[1])};body=map[string]any{"sha":root,"truncated":mode=="truncated-tree","tree":entries}
				case "/repos/AutoDevJapan/civ6-banseiki/git/trees/"+nested:body=map[string]any{"sha":nested,"truncated":false,"tree":[]any{map[string]any{"path":"Cargo.lock","mode":"100644","type":"blob","sha":two,"size":4}}}
				case "/repos/AutoDevJapan/civ6-banseiki/git/trees/"+github:body=map[string]any{"sha":github,"truncated":false,"tree":[]any{map[string]any{"path":"workflows","mode":"040000","type":"tree","sha":workflows}}}
				case "/repos/AutoDevJapan/civ6-banseiki/git/trees/"+workflows:body=map[string]any{"sha":workflows,"truncated":false,"tree":[]any{map[string]any{"path":"rust.yml","mode":"100644","type":"blob","sha":workflow,"size":9}}}
				case "/repos/AutoDevJapan/civ6-banseiki/git/blobs/"+one:
					mu.Lock();blobRequests++;mu.Unlock();payload:=[]byte("alpha\n");if mode=="changed-blob"{payload=[]byte("alphx\n")};_,_ = w.Write(payload);return w.Result(),nil
				case "/repos/AutoDevJapan/civ6-banseiki/git/blobs/"+two:mu.Lock();blobRequests++;mu.Unlock();_,_ = w.Write([]byte("beta"));return w.Result(),nil
				case "/repos/AutoDevJapan/civ6-banseiki/git/blobs/"+workflow:mu.Lock();blobRequests++;mu.Unlock();_,_ = w.Write([]byte("workflow\n"));return w.Result(),nil
				default:t.Fatal("unexpected Git fixture endpoint")
				}
				if err:=json.NewEncoder(w).Encode(body);err!=nil{t.Fatal(err)};return w.Result(),nil
			})}
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
			err:=civ880VerifyH1GitNamespace(ctx,client,product)
			if mode=="normal"{mu.Lock();count:=blobRequests;mu.Unlock();if err!=nil||count!=3{t.Fatal("exact source namespace rejected",err)}}else if err==nil{t.Fatal("modified namespace accepted")}
		})
	}
}

func civ880WriteHeldFixture(t *testing.T, root string, product *civ880ObservedProduct) {
	t.Helper()
	for _,file:=range product.files { if err:=os.WriteFile(filepath.Join(root,file.Name),file.data,0600);err!=nil{t.Fatal(err)} }
}

func TestCiv880HeldObservationReadsTheSameFiveFilesAndRetainsPartialOwner(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)}
	defer product.clear()
	root:=physicalSoftwareFixture(t);civ880WriteHeldFixture(t,root,product)
	lease,err:=civ880OpenHeldObservation(ctx,root)
	if err!=nil{if lease!=nil{_ = lease.Close()};t.Fatal(err)}
	defer func(){if err:=lease.Close();err!=nil{t.Error(err)}}()
	if err:=lease.inspect(ctx,product);err!=nil{t.Fatal(err)}
	server:=&civ880ServerObservation{Run:civ880RunProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,HeadSHA:product.Source,WorkflowID:7,WorkflowPath:".github/workflows/rust.yml",RunID:product.RunID,Attempt:int(product.Attempt),Status:"completed",Conclusion:"success"},Job:civ880JobProjection{RunID:product.RunID,Attempt:int(product.Attempt),JobID:9,CheckRunID:10,Status:"completed",Conclusion:"success"},Artifact:civ880ArtifactProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,RunID:product.RunID,ID:11,Name:fmt.Sprintf("civ6-windows-product-%s-%d-%d",product.Source,product.RunID,product.Attempt),Bytes:product.ArchiveBytes,Digest:"sha256:"+product.ArchiveSHA}}
	readback,err:=civ880ObserveReadOnly(ctx,product,server,lease);if err!=nil||readback.FileSHA[0]!=product.files[0].SHA{t.Fatal("held read-only comparison failed",err)}
	wrong:=*server;wrong.Run.HeadSHA=strings.Repeat("e",40);if _,err:=civ880ObserveReadOnly(ctx,product,&wrong,lease);err==nil{t.Fatal("different server generation accepted")}
	if file,err:=os.OpenFile(filepath.Join(root,product.files[0].Name),os.O_WRONLY,0);err==nil{file.Close();t.Fatal("held output unexpectedly allowed a writer")}
	if err:=os.WriteFile(filepath.Join(root,"extra"),[]byte("x"),0600);err!=nil{t.Fatal(err)}
	if lease.inspect(ctx,product)==nil{t.Fatal("extra namespace entry accepted")}
}

func TestCiv880HeldObservationRejectsChangedBytesAndOwnsOpenFailure(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)}
	defer product.clear()
	root:=physicalSoftwareFixture(t);civ880WriteHeldFixture(t,root,product)
	changed:=append([]byte(nil),product.files[1].data...);changed[len(changed)-1]^=1
	if err:=os.WriteFile(filepath.Join(root,product.files[1].Name),changed,0600);err!=nil{t.Fatal(err)}
	lease,err:=civ880OpenHeldObservation(ctx,root)
	if err!=nil{if lease!=nil{_ = lease.Close()};t.Fatal(err)}
	if lease.inspect(ctx,product)==nil{t.Fatal("changed bytes accepted")}
	if err:=lease.Close();err!=nil{t.Fatal(err)}
	if err:=os.Remove(filepath.Join(root,product.files[4].Name));err!=nil{t.Fatal(err)}
	partial,err:=civ880OpenHeldObservation(ctx,root)
	if err==nil||partial==nil{t.Fatal("missing leaf did not preserve partial owner")}
	if err:=partial.Close();err!=nil{t.Fatal(err)}
}

func TestCiv880CreateNewStageOwnsFiveAndReadback(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)};defer product.clear()
	root:=physicalSoftwareFixture(t);parent,err:=civ880OpenStageParentAt(ctx,root)
	if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal(err)}
	stage,err:=civ880CreateObservedStageNamed(ctx,parent,product,"civ880-00000000000000000000000000000001")
	if err!=nil{if stage!=nil{_ = stage.Cleanup(ctx);_ = stage.Close()};t.Fatal(err)}
	defer func(){cleanupCtx,cleanupCancel:=context.WithTimeout(context.Background(),5*time.Second);defer cleanupCancel();if err:=stage.Cleanup(cleanupCtx);err!=nil{t.Error("owned stage cleanup unconfirmed",err);return};if err:=stage.Close();err!=nil{t.Error(err)}}()
	if err:=parent.Close();err==nil{t.Fatal("transferred parent closed by former owner")}
	if err:=stage.inspect(ctx,product);err!=nil{t.Fatal(err)}
	server:=&civ880ServerObservation{Run:civ880RunProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,HeadSHA:product.Source,WorkflowID:7,WorkflowPath:".github/workflows/rust.yml",RunID:product.RunID,Attempt:int(product.Attempt),Status:"completed",Conclusion:"success"},Job:civ880JobProjection{RunID:product.RunID,Attempt:int(product.Attempt),JobID:9,CheckRunID:10,Status:"completed",Conclusion:"success"},Artifact:civ880ArtifactProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,RunID:product.RunID,ID:11,Name:fmt.Sprintf("civ6-windows-product-%s-%d-%d",product.Source,product.RunID,product.Attempt),Bytes:product.ArchiveBytes,Digest:"sha256:"+product.ArchiveSHA}}
	readback,err:=civ880ObserveCreatedStageReadOnly(ctx,product,server,stage);if err!=nil||readback.FileSHA[4]!=product.files[4].SHA{t.Fatal("created stage readback failed",err)}
	if file,err:=os.OpenFile(filepath.Join(stage.rootPath,product.files[0].Name),os.O_WRONLY,0);err==nil{file.Close();t.Fatal("created output accepted a competing writer")}
	if err:=stage.Close();!errors.Is(err,errCiv880StagePreserved){t.Fatal("close did not retain uncollected stage",err)}
	stagePath:=stage.rootPath
	if err:=stage.Cleanup(ctx);err!=nil{t.Fatal(err)}
	if _,err:=os.Lstat(stagePath);!errors.Is(err,os.ErrNotExist){t.Fatal("stage remains after cleanup",err)}
	if err:=stage.Close();err!=nil{t.Fatal(err)}
	if stage.inspect(ctx,product)==nil{t.Fatal("closed stage accepted")}
}

func TestCiv880CreateNewStagePreservesExistingCollision(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)};defer product.clear()
	root:=physicalSoftwareFixture(t);name:="civ880-00000000000000000000000000000002";foreign:=filepath.Join(root,name)
	if err:=os.Mkdir(foreign,0700);err!=nil{t.Fatal(err)};if err:=os.WriteFile(filepath.Join(foreign,"foreign"),[]byte("retain"),0600);err!=nil{t.Fatal(err)}
	parent,err:=civ880OpenStageParentAt(ctx,root);if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal(err)}
	stage,err:=civ880CreateObservedStageNamed(ctx,parent,product,name);if err==nil{_ = stage.Close();t.Fatal("existing namespace accepted")}
	if stage==nil{t.Fatal("partial owner lost")};if !stage.rootCollision{t.Fatal("existing namespace collision not classified")};if err:=stage.Cleanup(ctx);err!=nil{t.Fatal(err)};if err:=stage.Close();err!=nil{t.Fatal(err)}
	got,err:=os.ReadFile(filepath.Join(foreign,"foreign"));if err!=nil||string(got)!="retain"{t.Fatal("foreign namespace changed",err)}
}

func TestCiv880NormalProviderAndUnknownParentFailClosed(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	if result,err:=civ880OpenNormalPreparedObservation(ctx);err==nil||result!=nil{t.Fatal("unconnected normal source minted a result")}
	parent,err:=civ880OpenStageParentAt(ctx,filepath.Clean(filepath.Join(t.TempDir(),"absent")))
	if err==nil||parent==nil{t.Fatal("unknown parent accepted")};if err:=parent.Close();err!=nil{t.Fatal(err)}
	prepared,err:=civ880PrepareObserved(ctx,nil,nil,8,1,9,11)
	if err==nil||prepared==nil||prepared.stage!=nil{t.Fatal("nil authority accepted")};if err:=prepared.Close();err!=nil{t.Fatal(err)}
}

func TestCiv880PreparedCloseRetainsUnknownStageOwnership(t *testing.T){
	stage:=&civ880CreatedStage{rootAttempted:true,rootPath:`C:\fixture\unknown`}
	prepared:=&civ880PreparedObservation{stage:stage}
	if err:=prepared.Close();!errors.Is(err,errCiv880StagePreserved)||prepared.stage!=stage||stage.closed{t.Fatal("unknown stage was released without reconciliation",err)}
}

func TestCiv880PartialStageKeepsCreatedFileAndParentOwnership(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)};defer product.clear()
	root:=physicalSoftwareFixture(t);parent,err:=civ880OpenStageParentAt(ctx,root)
	if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal(err)}
	stage,err:=civ880CreateObservedStageWithHook(ctx,parent,product,"civ880-00000000000000000000000000000003",func(i int)error{if i==0{return errCiv880Observation};return nil})
	if err==nil||stage==nil||stage.root==nil||stage.leaves[0]==nil{if stage!=nil{_ = stage.Cleanup(ctx);_ = stage.Close()};t.Fatal("partial owned stage was not retained")}
	if stage.inspect(ctx,product)==nil{t.Fatal("partial stage counted as complete")}
	if err:=parent.Close();err==nil{t.Fatal("parent ownership was not transferred")}
	path:=filepath.Join(stage.rootPath,product.files[0].Name)
	if err:=stage.Close();!errors.Is(err,errCiv880StagePreserved){t.Fatal("partial output owner released",err)}
	if err:=stage.Cleanup(ctx);err!=nil{t.Fatal(err)}
	if _,err:=os.Lstat(path);!errors.Is(err,os.ErrNotExist){t.Fatal("partial output remains",err)}
	if err:=stage.Close();err!=nil{t.Fatal(err)}
}

func TestCiv880CleanupResumesAfterDispositionWithoutDroppingOwner(t *testing.T){
	for _,stopAt:=range []string{"leaf_marked","root_marked"}{t.Run(stopAt,func(t *testing.T){
		ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
		product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil));if err!=nil{t.Fatal(err)};defer product.clear()
		root:=physicalSoftwareFixture(t);parent,err:=civ880OpenStageParentAt(ctx,root);if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal(err)}
		stage,err:=civ880CreateObservedStage(ctx,parent,product);if err!=nil{if stage!=nil{_ = stage.Cleanup(ctx);_ = stage.Close()};t.Fatal(err)}
		defer func(){cleanupCtx,cleanupCancel:=context.WithTimeout(context.Background(),5*time.Second);defer cleanupCancel();if err:=stage.Cleanup(cleanupCtx);err!=nil{t.Error("owned cleanup unconfirmed",err);return};if err:=stage.Close();err!=nil{t.Error(err)}}()
		injected:=false;err=stage.cleanupWithHook(ctx,func(phase string,_ int)error{if phase==stopAt&&!injected{injected=true;return errCiv880Observation};return nil})
		if !injected||!errors.Is(err,errCiv880StagePreserved)||stage.root==nil||stage.parent==nil{t.Fatal("failed disposition lost owner",err)}
		if stage.inspect(ctx,product)==nil{t.Fatal("stage remained readable after cleanup began")}
		if err:=stage.Close();!errors.Is(err,errCiv880StagePreserved){t.Fatal("unconfirmed stage closed",err)}
		if err:=stage.Cleanup(ctx);err!=nil{t.Fatal("cleanup could not resume",err)}
		if _,err:=os.Lstat(stage.rootPath);!errors.Is(err,os.ErrNotExist){t.Fatal("stage path remains",err)}
		if err:=stage.Close();err!=nil{t.Fatal(err)}
	})}
}

type civ880RoundTrip func(*http.Request)(*http.Response,error)
func (roundTrip civ880RoundTrip) RoundTrip(request *http.Request)(*http.Response,error){return roundTrip(request)}

func TestCiv880ServerObservationBindsRunAttemptJobAndArtifact(t *testing.T){
	producer:=strings.Repeat("a",40);hash:=strings.Repeat("b",64)
	steps:=[]map[string]any{{"number":1,"name":"Capture Windows product build inputs","status":"completed","conclusion":"success"},{"number":2,"name":"Build all tools","status":"completed","conclusion":"success"},{"number":3,"name":"Upload source-bound Windows product software","status":"completed","conclusion":"success"},{"number":4,"name":"Record Windows product artifact origin","status":"completed","conclusion":"success"}}
	for _,mode:=range []string{"normal","wrong-attempt","failed-step","extra-artifact","wrong-server-digest","missing-job"}{
		t.Run(mode,func(t *testing.T){
			calls:=0
			client:=&http.Client{Transport:civ880RoundTrip(func(request *http.Request)(*http.Response,error){
				calls++
				if request.URL.Host!="api.github.com"||request.Method!="GET"{t.Fatal("unexpected server destination")}
				var payload any
				switch {
				case strings.HasSuffix(request.URL.Path,"/actions/runs/8"):
					attempt:=1;if mode=="wrong-attempt"{attempt=2}
					payload=map[string]any{"id":8,"run_attempt":attempt,"head_sha":producer,"head_branch":"fixture","event":"pull_request","path":".github/workflows/rust.yml","workflow_id":7,"status":"completed","conclusion":"success","repository":map[string]any{"id":civ880RepositoryID},"head_repository":map[string]any{"id":civ880RepositoryID}}
				case strings.HasSuffix(request.URL.Path,"/actions/runs/8/attempts/1/jobs"):
					local:=make([]map[string]any,len(steps));for i,step:=range steps{local[i]=map[string]any{};for k,v:=range step{local[i][k]=v}}
					if mode=="failed-step"{local[1]["conclusion"]="failure"}
					jobID:=9;if mode=="missing-job"{jobID=19}
					payload=map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":jobID,"run_id":8,"name":"test (windows-latest)","status":"completed","conclusion":"success","check_run_url":"https://api.github.com/repos/AutoDevJapan/civ6-banseiki/check-runs/10","steps":local}}}
				case strings.HasSuffix(request.URL.Path,"/actions/runs/8/artifacts"):
					artifactDigest:="sha256:"+hash;if mode=="wrong-server-digest"{artifactDigest="sha256:bad"}
					artifact:=map[string]any{"id":11,"name":"civ6-windows-product-"+producer+"-8-1","digest":artifactDigest,"expired":false,"expires_at":time.Now().UTC().Add(time.Hour),"size_in_bytes":100,"workflow_run":map[string]any{"id":8,"repository_id":civ880RepositoryID,"head_repository_id":civ880RepositoryID,"head_sha":producer}}
					items:=[]any{artifact};if mode=="extra-artifact"{items=append(items,artifact)}
					payload=map[string]any{"total_count":len(items),"artifacts":items}
				default:t.Fatal("unexpected metadata path")
				}
				w:=httptest.NewRecorder();w.Header().Set("Content-Type","application/json");if err:=json.NewEncoder(w).Encode(payload);err!=nil{t.Fatal(err)};return w.Result(),nil
			})}
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
			got,err:=civ880ObserveServer(ctx,client,8,1,9,11)
			if mode=="normal"{if err!=nil||got==nil||got.Job.CheckRunID!=10||got.Artifact.Digest!="sha256:"+hash||calls!=3{t.Fatal("normal matching observation failed",err)}}else if err==nil{t.Fatal("mismatched server observation accepted")}
		})
	}
}

func TestCiv880DownloadUsesServerDigestAndCredentialFreeObjectRequest(t *testing.T){
	raw:=[]byte("synthetic archive bytes")
	for _,mode:=range []string{"normal","wrong-length","wrong-digest","unsafe-redirect","object-error"}{
		t.Run(mode,func(t *testing.T){
			apiCalls,objectCalls:=0,0
			api:=&http.Client{Transport:civ880RoundTrip(func(req *http.Request)(*http.Response,error){apiCalls++;if req.URL.Host!="api.github.com"{t.Fatal("API host changed")};w:=httptest.NewRecorder();location:="https://unit.blob.core.windows.net/a";if mode=="unsafe-redirect"{location="https://example.invalid/a"};w.Header().Set("Location",location);w.WriteHeader(http.StatusFound);return w.Result(),nil})}
			object:=&http.Client{Transport:civ880RoundTrip(func(req *http.Request)(*http.Response,error){objectCalls++;if req.Header.Get("Authorization")!=""||req.Header.Get("Cookie")!=""{t.Fatal("credential forwarded")};w:=httptest.NewRecorder();if mode=="object-error"{w.WriteHeader(http.StatusForbidden)}else{_,_ = w.Write(raw)};return w.Result(),nil})}
			observation:=&civ880ServerObservation{Artifact:civ880ArtifactProjection{ID:11,Bytes:int64(len(raw)),Digest:"sha256:"+digest(raw)}}
			if mode=="wrong-length"{observation.Artifact.Bytes++};if mode=="wrong-digest"{observation.Artifact.Digest="sha256:"+strings.Repeat("a",64)}
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
			got,err:=civ880DownloadObserved(ctx,api,object,observation)
			if mode=="normal" {if err!=nil||!bytes.Equal(got,raw)||apiCalls!=1||objectCalls!=1{t.Fatal("download observation failed",err)}} else if err==nil {clear(got);t.Fatal("invalid server object accepted")}
			if mode=="unsafe-redirect"&&objectCalls!=0{t.Fatal("untrusted destination contacted")}
			clear(got)
		})
	}
}

func TestCiv880AcquisitionRechecksSameServerGenerationAroundArchive(t *testing.T){
	for _,mode:=range []string{"normal","changed-after","nil-authority","source-rejected","wrong-origin-digest","wrong-origin-manifest"}{
		t.Run(mode,func(t *testing.T){
			archive:=civ880ProductArchiveFixture(t,nil);runReads,apiCalls,objectCalls:=0,0,0
			fixtureCtx,fixtureCancel:=context.WithTimeout(context.Background(),5*time.Second);fixtureProduct,fixtureErr:=civ880ObserveProductArchive(fixtureCtx,archive);fixtureCancel();if fixtureErr!=nil{t.Fatal(fixtureErr)}
			origin:=civ880OriginObservation{Schema:"civ6-product-artifact-origin/v1",RepositoryID:civ880RepositoryID,Source:fixtureProduct.Source,Tree:fixtureProduct.Tree,WorkflowBlob:fixtureProduct.WorkflowBlob,RunID:8,Attempt:1,JobKey:"test",RunnerOS:"Windows",ArtifactID:11,ArtifactName:"civ6-windows-product-"+civ880SourceCommit+"-8-1",UploadSHA:fixtureProduct.ArchiveSHA,ManifestSHA:fixtureProduct.ManifestSHA,CargoSHA:fixtureProduct.CargoSHA}
			var captureLines []byte
			for i,phase:=range []string{"bins","dlls"}{captureLines=append(captureLines,civ880CaptureLineFixture(t,civ880CaptureMapFixture(phase,fixtureProduct.Streams[i].StdoutSHA,fixtureProduct.Streams[i].StderrSHA,0))...)};fixtureProduct.clear()
			if mode=="wrong-origin-digest"{origin.UploadSHA=strings.Repeat("e",64)};if mode=="wrong-origin-manifest"{origin.ManifestSHA=strings.Repeat("e",64)}
			originBytes,err:=json.Marshal(origin);if err!=nil{t.Fatal(err)};jobLog:=append(captureLines,[]byte("2026-11-16T00:00:00.000Z CIV_PRODUCT_ARTIFACT_ORIGIN ")...);jobLog=append(jobLog,originBytes...);jobLog=append(jobLog,'\n')
			api:=&http.Client{Transport:civ880RoundTrip(func(req *http.Request)(*http.Response,error){
				apiCalls++;w:=httptest.NewRecorder()
				if strings.HasSuffix(req.URL.Path,"/actions/artifacts/11/zip"){w.Header().Set("Location","https://unit.blob.core.windows.net/a");w.WriteHeader(http.StatusFound);return w.Result(),nil}
				if strings.HasSuffix(req.URL.Path,"/actions/jobs/9/logs"){w.Header().Set("Location","https://unit.blob.core.windows.net/log");w.WriteHeader(http.StatusFound);return w.Result(),nil}
				var payload any
				switch{
				case strings.HasSuffix(req.URL.Path,"/actions/runs/8"):
					runReads++;head:=civ880SourceCommit;if mode=="changed-after"&&runReads>1{head=strings.Repeat("d",40)}
					payload=map[string]any{"id":8,"run_attempt":1,"head_sha":head,"head_branch":"fixture","event":"pull_request","path":".github/workflows/rust.yml","workflow_id":7,"status":"completed","conclusion":"success","repository":map[string]any{"id":civ880RepositoryID},"head_repository":map[string]any{"id":civ880RepositoryID}}
				case strings.HasSuffix(req.URL.Path,"/actions/runs/8/attempts/1/jobs"):
					steps:=[]any{map[string]any{"number":1,"name":"Capture Windows product build inputs","status":"completed","conclusion":"success"},map[string]any{"number":2,"name":"Build all tools","status":"completed","conclusion":"success"},map[string]any{"number":3,"name":"Upload source-bound Windows product software","status":"completed","conclusion":"success"},map[string]any{"number":4,"name":"Record Windows product artifact origin","status":"completed","conclusion":"success"}}
					payload=map[string]any{"total_count":1,"jobs":[]any{map[string]any{"id":9,"run_id":8,"name":"test (windows-latest)","status":"completed","conclusion":"success","check_run_url":"https://api.github.com/repos/AutoDevJapan/civ6-banseiki/check-runs/10","steps":steps}}}
				case strings.HasSuffix(req.URL.Path,"/actions/runs/8/artifacts"):
					payload=map[string]any{"total_count":1,"artifacts":[]any{map[string]any{"id":11,"name":"civ6-windows-product-"+civ880SourceCommit+"-8-1","digest":"sha256:"+digest(archive),"expired":false,"expires_at":time.Now().UTC().Add(time.Hour),"size_in_bytes":len(archive),"workflow_run":map[string]any{"id":8,"repository_id":civ880RepositoryID,"head_repository_id":civ880RepositoryID,"head_sha":civ880SourceCommit}}}}
				default:t.Fatal("unexpected acquisition endpoint")
				}
				if err:=json.NewEncoder(w).Encode(payload);err!=nil{t.Fatal(err)};return w.Result(),nil
			})}
			object:=&http.Client{Transport:civ880RoundTrip(func(req *http.Request)(*http.Response,error){objectCalls++;w:=httptest.NewRecorder();if req.URL.Path=="/log"{_,_ = w.Write(jobLog)}else{_,_ = w.Write(archive)};return w.Result(),nil})}
			var authority *ghaGuardEvidenceAuthority;if mode!="nil-authority"{authority=&ghaGuardEvidenceAuthority{client:api}}
			ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
			verifyCalls:=0
			product,server,err:=civ880ObserveAcquisitionWithVerifier(ctx,authority,object,8,1,9,11,func(context.Context,*http.Client,*civ880ObservedProduct)error{verifyCalls++;if mode=="source-rejected"{return errCiv880Observation};return nil})
			if mode=="normal"{if err!=nil||product==nil||server==nil||apiCalls!=8||objectCalls!=2||runReads!=2{t.Fatal("matching acquisition observation failed",err)};product.clear()}else if err==nil{product.clear();t.Fatal("unqualified acquisition accepted")}
			if mode=="nil-authority"&&(apiCalls!=0||objectCalls!=0){t.Fatal("request occurred without owner")}
			if mode=="source-rejected"&&(verifyCalls!=1||runReads!=1){t.Fatal("source failure was not observed before second server pass")}
		})
	}
}

func civ880Wire(t *testing.T, records []map[string]any, success bool) []byte {
	t.Helper()
	var b bytes.Buffer
	for _, record := range records {
		line,err:=json.Marshal(record);if err!=nil{t.Fatal(err)}
		b.Write(line);b.WriteByte('\n')
	}
	fmt.Fprintf(&b,"{\"reason\":\"build-finished\",\"success\":%t}\n",success)
	return b.Bytes()
}

func civ880Clone(t *testing.T, records []map[string]any) []map[string]any {
	t.Helper()
	raw,err:=json.Marshal(records);if err!=nil{t.Fatal(err)}
	var result []map[string]any
	if err:=json.Unmarshal(raw,&result);err!=nil{t.Fatal(err)}
	return result
}

func civ880PhaseWires(t *testing.T, records []map[string]any, binSuccess, dllSuccess bool) ([]byte, []byte) {
	t.Helper()
	var bins, dlls []map[string]any
	for _, record := range records {
		target, ok := record["target"].(map[string]any)
		if !ok { bins=append(bins,record);continue }
		name,_:=target["name"].(string)
		dll:=false
		for _,spec:=range civ880RoleSpecs(){if spec.target==name{dll=spec.dll;break}}
		if dll {dlls=append(dlls,record)} else {bins=append(bins,record)}
	}
	return civ880Wire(t,bins,binSuccess),civ880Wire(t,dlls,dllSuccess)
}

func TestCiv880CargoObservationRequiresAllFixedRoles(t *testing.T) {
	records,ids,root:=civ880Fixture(t)
	bins,dlls:=civ880PhaseWires(t,records,true,true)
	output,err:=civ880ObserveCargoStreams(bins,dlls,ids,root)
	if err!=nil{t.Fatal(err)}
	for i,spec:=range civ880RoleSpecs(){
		if output[i].Role!=spec.role||output[i].PackageID!=civ880PackageID(ids,spec.packageName)||output[i].Target!=spec.target||len(output[i].Candidates)!=1{t.Fatalf("role %s not retained",spec.role)}
	}
	// A projection is not a purpose issuer or a held-object capability.
}

func TestCiv880CargoFreshIsRecordedNotTreatedAsACompilerRun(t *testing.T) {
	records,ids,root:=civ880Fixture(t)
	records[0]["fresh"]=true
	bins,dlls:=civ880PhaseWires(t,records,true,true)
	output,err:=civ880ObserveCargoStreams(bins,dlls,ids,root)
	if err!=nil||!output[0].Fresh{t.Fatal("Cargo fresh state was not preserved")}
}

func TestCiv880CargoDependencyObservationDoesNotReplaceDLLPhase(t *testing.T) {
	records,ids,root:=civ880Fixture(t)
	bins:=civ880Wire(t,append(append([]map[string]any(nil),records[:3]...),records[3]),true)
	dlls:=civ880Wire(t,records[3:],true)
	if _,err:=civ880ObserveCargoStreams(bins,dlls,ids,root);err!=nil{t.Fatal("library dependency prevented selected DLL phase")}
	if _,err:=civ880ObserveCargoStreams(bins,civ880Wire(t,records[3:4],true),ids,root);err==nil{t.Fatal("binary-phase dependency replaced missing DLL output")}
}

func TestCiv880CargoObservationRejectsInvalidOrIncompleteInputs(t *testing.T) {
	base,ids,root:=civ880Fixture(t)
	for _,name:=range []string{"missing","duplicate","package","kind","test-profile","wrong-path","outside-root","bin-failure","dll-failure","missing-bin-finish","missing-dll-finish","after-finish"}{
		t.Run(name,func(t *testing.T){
			records:=civ880Clone(t,base)
			binSuccess,dllSuccess:=true,true
			switch name {
			case "missing":records=records[:4]
			case "duplicate":records=append(records,records[0])
			case "package":records[0]["package_id"]="unrelated-package"
			case "kind":records[0]["target"].(map[string]any)["kind"]=[]string{"example"}
			case "test-profile":records[0]["profile"].(map[string]any)["test"]=true
			case "wrong-path":records[0]["filenames"]=[]string{filepath.Join(root,"release","other.exe")}
			case "outside-root":p:=filepath.Join(filepath.Dir(root),"outside","civ6-autotest.exe");records[0]["filenames"]=[]string{p};records[0]["executable"]=p
			case "bin-failure":binSuccess=false
			case "dll-failure":dllSuccess=false
			}
			bins,dlls:=civ880PhaseWires(t,records,binSuccess,dllSuccess)
			if name=="missing-bin-finish"{bins=bins[:bytes.LastIndex(bins,[]byte("{\"reason\":\"build-finished\""))]}
			if name=="missing-dll-finish"{dlls=dlls[:bytes.LastIndex(dlls,[]byte("{\"reason\":\"build-finished\""))]}
			if name=="after-finish"{bins=append(bins,[]byte("{\"reason\":\"compiler-artifact\"}\n")...)}
			if _,err:=civ880ObserveCargoStreams(bins,dlls,ids,root);err==nil{t.Fatal("invalid Cargo observation accepted")}
		})
	}
	bins,dlls:=civ880PhaseWires(t,base,true,true)
	if _,err:=civ880ObserveCargoStreams(append(append([]byte(nil),bins...),[]byte("non-json tool output\n")...),dlls,ids,root);err!=nil{t.Fatal("non-JSON output after build finish was misclassified")}
	wire:=bytes.Replace(bins,[]byte("\"reason\":\"compiler-artifact\""),[]byte("\"reason\":\"compiler-artifact\",\"reason\":\"compiler-artifact\""),1)
	if _,err:=civ880ObserveCargoStreams(wire,dlls,ids,root);err==nil{t.Fatal("duplicate key accepted")}
	if _,err:=civ880ObserveCargoStreams(bytes.Repeat([]byte{'x'},civ880MaxCargoStream+1),dlls,ids,root);err==nil{t.Fatal("oversize stream accepted")}
	longLine:=bytes.Repeat([]byte{'x'},civ880MaxCargoLine+1)
	longLine=append(longLine,'\n');longLine=append(longLine,bins...)
	if _,err:=civ880ObserveCargoStreams(longLine,dlls,ids,root);err==nil{t.Fatal("oversize line accepted")}
}

func civ880FixturePE(dll bool) []byte {
	raw:=make([]byte,512)
	copy(raw,[]byte("MZ"));binary.LittleEndian.PutUint32(raw[0x3c:0x40],0x80)
	copy(raw[0x80:],[]byte{'P','E',0,0})
	binary.LittleEndian.PutUint16(raw[0x84:0x86],0x8664)
	binary.LittleEndian.PutUint16(raw[0x86:0x88],1)
	binary.LittleEndian.PutUint16(raw[0x94:0x96],112)
	flags:=uint16(0x0002);if dll{flags|=0x2000};binary.LittleEndian.PutUint16(raw[0x96:0x98],flags)
	binary.LittleEndian.PutUint16(raw[0x98:0x9a],0x20b)
	return raw
}

func civ880FixtureCompletePE(t *testing.T, spec civ880RoleSpec) ([]byte,[]any) {
	t.Helper()
	raw:=make([]byte,0x1400);put16:=func(p int,v uint16){binary.LittleEndian.PutUint16(raw[p:p+2],v)};put32:=func(p int,v uint32){binary.LittleEndian.PutUint32(raw[p:p+4],v)}
	copy(raw,[]byte("MZ"));put32(0x3c,0x80);copy(raw[0x80:],[]byte{'P','E',0,0});put16(0x84,0x8664);put16(0x86,2);put16(0x94,0xf0)
	flags:=uint16(0x22);if spec.dll{flags|=0x2000};put16(0x96,flags);put16(0x98,0x20b);put32(0x98+108,16);put32(0x98+112,0x2000);put32(0x98+116,0x800)
	for _,section:=range []struct{offset int;rva,size,raw,flags uint32}{{0x188,0x1000,0x200,0x200,0x60000020},{0x1b0,0x2000,0x1000,0x400,0x40000040}}{put32(section.offset+8,section.size);put32(section.offset+12,section.rva);put32(section.offset+16,section.size);put32(section.offset+20,section.raw);put32(section.offset+36,section.flags)}
	required:=civ880RequiredExports(spec.role);if required==nil{t.Fatal("unknown role")}
	put32(0x400+20,uint32(len(required)));put32(0x400+24,uint32(len(required)));put32(0x400+28,0x2100);put32(0x400+32,0x2200);put32(0x400+36,0x2300)
	var exports []any;nameAt:=0x800
	for i,want:=range required{
		rva:=uint32(0x2900+i*8);kind:="data";if want.function{rva=uint32(0x1000+i*8);kind="function"}
		put32(0x500+i*4,rva);put32(0x600+i*4,uint32(nameAt-0x400+0x2000));put16(0x700+i*2,uint16(i));copy(raw[nameAt:],want.name);nameAt+=len(want.name)+1
		exports=append(exports,map[string]any{"name":want.name,"kind":kind,"rva":rva})
	}
	if exports==nil{exports=[]any{}}
	return raw,exports
}

func TestCiv880PEObservationChecksMachineAndRoleWithoutLoading(t *testing.T) {
	for _,spec:=range civ880RoleSpecs(){if err:=civ880ObservePEHeader(civ880FixturePE(spec.dll),spec.role);err!=nil{t.Fatal(spec.role,err)}}
	for _,name:=range []string{"dos","signature","machine","dll","optional","sections","truncated","role"}{
		t.Run(name,func(t *testing.T){
			raw:=civ880FixturePE(false);role:="autotest"
			switch name{
			case "dos":raw[0]=0
			case "signature":raw[0x80]=0
			case "machine":binary.LittleEndian.PutUint16(raw[0x84:0x86],0x014c)
			case "dll":binary.LittleEndian.PutUint16(raw[0x96:0x98],0x2002)
			case "optional":binary.LittleEndian.PutUint16(raw[0x98:0x9a],0x010b)
			case "sections":binary.LittleEndian.PutUint16(raw[0x86:0x88],97)
			case "truncated":raw=raw[:0x99]
			case "role":role="arbitrary"
			}
			if civ880ObservePEHeader(raw,role)==nil{t.Fatal("invalid PE header accepted")}
		})
	}
}

func TestCiv880ProjectionComparisonIsNotAuthority(t *testing.T) {
	archive:=[]byte("synthetic archive")
	sourceInputs:=[]byte("synthetic source inputs")
	outputManifest:=[]byte("synthetic output manifest")
	sum:=sha256.Sum256(archive);hash:=hex.EncodeToString(sum[:])
	sourceSum:=sha256.Sum256(sourceInputs);outputSum:=sha256.Sum256(outputManifest)
	producer:=strings.Repeat("a",40)
	run:=civ880RunProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,HeadSHA:producer,WorkflowID:7,WorkflowPath:".github/workflows/rust.yml",RunID:8,Attempt:1,Status:"completed",Conclusion:"success"}
	job:=civ880JobProjection{RunID:8,Attempt:1,JobID:9,CheckRunID:10,Status:"completed",Conclusion:"success"}
	artifact:=civ880ArtifactProjection{RepositoryID:civ880RepositoryID,HeadRepositoryID:civ880RepositoryID,RunID:8,ID:11,Name:"synthetic",Bytes:int64(len(archive)),Digest:"sha256:"+hash}
	pub:=civ880PublicationProjection{RepositoryID:civ880RepositoryID,SourceCommit:civ880SourceCommit,SourceTree:civ880SourceTree,ProducerRevision:producer,WorkflowID:7,WorkflowPath:run.WorkflowPath,RunID:8,Attempt:1,JobID:9,CheckRunID:10,ArtifactID:11,ArtifactName:"synthetic",SourceInputsSHA256:hex.EncodeToString(sourceSum[:]),OutputManifestSHA256:hex.EncodeToString(outputSum[:]),ArchiveSHA256:hash}
	if err:=civ880CompareUntrustedProjections(run,job,artifact,pub,sourceInputs,outputManifest,archive);err!=nil{t.Fatal(err)}
	badInputs:=append([]byte(nil),sourceInputs...);badInputs[0]^=1
	if civ880CompareUntrustedProjections(run,job,artifact,pub,badInputs,outputManifest,archive)==nil{t.Fatal("changed source inputs accepted")}
	badOutputs:=append([]byte(nil),outputManifest...);badOutputs[0]^=1
	if civ880CompareUntrustedProjections(run,job,artifact,pub,sourceInputs,badOutputs,archive)==nil{t.Fatal("changed output manifest accepted")}
	changed:=append([]byte(nil),archive...);changed[0]^=1
	if civ880CompareUntrustedProjections(run,job,artifact,pub,sourceInputs,outputManifest,changed)==nil{t.Fatal("changed archive accepted")}
	badJob:=job;badJob.JobID++
	if civ880CompareUntrustedProjections(run,badJob,artifact,pub,sourceInputs,outputManifest,archive)==nil{t.Fatal("different job accepted")}
	badArtifact:=artifact;badArtifact.Expired=true
	if civ880CompareUntrustedProjections(run,job,badArtifact,pub,sourceInputs,outputManifest,archive)==nil{t.Fatal("expired artifact accepted")}
	badPub:=pub;badPub.SourceCommit=strings.Repeat("d",40)
	if civ880CompareUntrustedProjections(run,job,artifact,badPub,sourceInputs,outputManifest,archive)==nil{t.Fatal("different source accepted")}
}

func civ880DirectoryRecordFixture(t *testing.T, names ...string) []byte {
	t.Helper()
	var result []byte
	for i,name:=range names {
		wide,err:=syscall.UTF16FromString(name)
		if err!=nil {t.Fatal(err)}
		wide=wide[:len(wide)-1]
		length:=68+len(wide)*2
		stride:=(length+7)&^7
		row:=make([]byte,stride)
		if i+1<len(names){binary.LittleEndian.PutUint32(row[:4],uint32(stride))}
		if name=="."||name==".."{binary.LittleEndian.PutUint32(row[56:60],syscall.FILE_ATTRIBUTE_DIRECTORY)}
		binary.LittleEndian.PutUint32(row[60:64],uint32(len(wide)*2))
		for j,value:=range wide{binary.LittleEndian.PutUint16(row[68+j*2:70+j*2],value)}
		result=append(result,row...)
	}
	return result
}

func TestCiv880HeldDirectoryDecoderRejectsMalformedRecords(t *testing.T) {
	good:=civ880DirectoryRecordFixture(t,".","..","first.exe","second.dll")
	got,err:=civ880DecodeStageEntries(good)
	if err!=nil||len(got)!=2||got[0].name!="first.exe"||got[1].name!="second.dll" {
		t.Fatal("valid directory records rejected",err)
	}
	for _,change:=range []func([]byte)[]byte{
		func(b []byte)[]byte{return b[:67]},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[:4],1);return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[:4],uint32(len(b)));return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[60:64],3);return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[60:64],0);return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[60:64],512);return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint16(b[68:70],0);return b},
		func(b []byte)[]byte{binary.LittleEndian.PutUint32(b[56:60],0);return b},
	} {
		changed:=change(append([]byte(nil),good...))
		if _,err:=civ880DecodeStageEntries(changed);err==nil{t.Fatal("malformed directory record accepted")}
	}
	tooMany:=civ880DirectoryRecordFixture(t,"1","2","3","4","5","6")
	if _,err:=civ880DecodeStageEntries(tooMany);err==nil{t.Fatal("unbounded inventory accepted")}
}

func TestCiv880CreatedDirectoryUsesHeldEnumerationWithoutRelaxingSharing(t *testing.T) {
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	product,err:=civ880ObserveProductArchive(ctx,civ880ProductArchiveFixture(t,nil))
	if err!=nil{t.Fatal(err)}
	defer product.clear()
	root:=physicalSoftwareFixture(t)
	parent,err:=civ880OpenStageParentAt(ctx,root)
	if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal(err)}
	stage,err:=civ880CreateObservedStage(ctx,parent,product)
	if err!=nil{if stage!=nil{_ = stage.Cleanup(ctx);_ = stage.Close()};t.Fatal("held stage creation",err)}
	defer func(){
		cleanup,cancelCleanup:=context.WithTimeout(context.Background(),5*time.Second);defer cancelCleanup()
		if err:=stage.Cleanup(cleanup);err!=nil{t.Error("exact owned cleanup",err);return}
		if err:=stage.Close();err!=nil{t.Error(err)}
	}()
	// The legacy path opener must still conflict with our DELETE-bearing
	// owner; the fix must not relax sharing or drop that owner.
	if _,err:=os.ReadDir(stage.rootPath);!errors.Is(err,windows.ERROR_SHARING_VIOLATION){
		t.Fatal("expected path-reopen sharing conflict",err)
	}
	for i:=0;i<3;i++{
		entries,err:=civ880StageEntries(ctx,stage.root)
		if err!=nil||len(entries)!=5||!civ880StageInventory(ctx,stage.root){
			t.Fatal("same held handle did not restart enumeration",err)
		}
	}
	extra,err:=civ880NtCreateNew(syscall.Handle(stage.root.Fd()),"foreign.txt",false,stage.security)
	if err!=nil{t.Fatal("owned foreign-entry fixture",err)}
	foreign:=os.NewFile(uintptr(extra),filepath.Join(stage.rootPath,"foreign.txt"))
	if foreign==nil{_ = syscall.CloseHandle(extra);t.Fatal("foreign fixture handle unavailable")}
	if stage.Cleanup(ctx)==nil{_ = foreign.Close();t.Fatal("unknown entry was silently removed")}
	if err:=civ880Disposition(foreign);err!=nil{_ = foreign.Close();t.Fatal(err)}
	if err:=foreign.Close();err!=nil{t.Fatal(err)}
	if !civ880StageInventory(ctx,stage.root){t.Fatal("exact inventory did not recover after fixture reconciliation")}
	if err:=stage.Cleanup(ctx);err!=nil{t.Fatal("normal owned cleanup after foreign fixture removal",err)}
	if _,err:=os.Lstat(stage.rootPath);!errors.Is(err,os.ErrNotExist){t.Fatal("owned stage not removed",err)}
}

func TestCiv880OriginRequiresExactNonNullKeys(t *testing.T) {
	valid:=map[string]any{
		"repository_id":civ880RepositoryID,"schema":"civ6-product-artifact-origin/v1",
		"source_sha":strings.Repeat("a",40),"source_tree":strings.Repeat("b",40),
		"workflow_blob":strings.Repeat("c",40),"run_id":8,"run_attempt":1,
		"job_key":"test","runner_os":"Windows","artifact_id":11,"artifact_name":"fixture",
		"upload_reported_sha256":strings.Repeat("d",64),"manifest_sha256":strings.Repeat("e",64),
		"cargo_metadata_sha256":strings.Repeat("f",64),
	}
	line:=func(value map[string]any)[]byte{
		raw,err:=json.Marshal(value);if err!=nil{t.Fatal(err)}
		return append([]byte("2026-11-16T00:00:00Z CIV_PRODUCT_ARTIFACT_ORIGIN "),raw...)
	}
	if _,err:=civ880OriginFromJobLog(line(valid));err!=nil{t.Fatal("exact origin refused",err)}
	for key,value:=range valid{
		for _,mode:=range []string{"alias","null","missing"} {
			changed:=map[string]any{};for k,v:=range valid{changed[k]=v}
			switch mode {
			case "alias":delete(changed,key);changed[strings.ToUpper(key)]=value
			case "null":changed[key]=nil
			case "missing":delete(changed,key)
			}
			if _,err:=civ880OriginFromJobLog(line(changed));err==nil{t.Fatalf("origin %s %s accepted",key,mode)}
		}
	}
}

func TestCiv880CaptureErrorDoesNotReturnPartialSelectedRecords(t *testing.T) {
	bins:=civ880CaptureMapFixture("bins",strings.Repeat("a",64),strings.Repeat("b",64),1)
	dlls:=civ880CaptureMapFixture("dlls",strings.Repeat("c",64),strings.Repeat("d",64),1)
	bad:=civ880CaptureMapFixture("extra",strings.Repeat("e",64),strings.Repeat("f",64),1)
	bad["phase"]=nil
	raw:=append(civ880CaptureLineFixture(t,bins),'\n')
	raw=append(raw,civ880CaptureLineFixture(t,dlls)...)
	raw=append(raw,'\n')
	raw=append(raw,civ880CaptureLineFixture(t,bad)...)
	result,err:=civ880CaptureEvents(raw)
	if err==nil {t.Fatal("malformed tail accepted")}
	for _,event:=range result{if event.Schema!=""||event.Phase!=""||event.ExitCode!=nil{t.Fatal("partial selected records escaped with error")}}
}

func TestCiv880ConsumedFileCloseFailureNeverClaimsALiveLease(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	name:=filepath.Join(physicalSoftwareFixture(t),"owned-close.txt")
	if err:=os.WriteFile(name,[]byte("owned"),0600);err!=nil{t.Fatal(err)}
	file,err:=os.Open(name);if err!=nil{t.Fatal(err)}
	calls:=0;unknown:=false
	err=civ880CloseFileOnce(&file,&unknown,func(value *os.File)error{
		calls++
		// Model the Go contract: descriptor is consumed although the close
		// result is an error. No OS CloseHandle failure is claimed here.
		if err:=value.Close();err!=nil{return err}
		return errors.New("synthetic consumed-descriptor close error")
	})
	if !errors.Is(err,errCiv880CloseUnknown)||file!=nil||!unknown||calls!=1{
		t.Fatal("failed close retained a misleading live File pointer",err)
	}
	if err:=civ880CloseFileOnce(&file,&unknown,func(*os.File)error{calls++;return nil});
		!errors.Is(err,errCiv880CloseUnknown)||calls!=1{
		t.Fatal("consumed descriptor was retried or unknown result erased",err)
	}
	stage:=&civ880CreatedStage{closeUnknown:true,rootAttempted:true,rootCreated:true}
	if err:=stage.Cleanup(ctx);!errors.Is(err,errCiv880CloseUnknown){t.Fatal("unknown close accepted as cleanup",err)}
	if err:=stage.Close();!errors.Is(err,errCiv880CloseUnknown){t.Fatal("unknown close accepted as release",err)}
	if stage.inspect(ctx,&civ880ObservedProduct{})==nil{t.Fatal("unknown close minted a readable stage")}
	normal,err:=os.Open(name);if err!=nil{t.Fatal("independent ordinary file unavailable",err)}
	normalUnknown:=false
	if err:=civ880CloseFileOnce(&normal,&normalUnknown,func(value *os.File)error{return value.Close()});err!=nil||
		normal!=nil||normalUnknown{t.Fatal("normal close was permanently disabled",err)}
}

func TestCiv880PreparedClosePreservesUnknownOutcomeAcrossWrappers(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),5*time.Second);defer cancel()
	stage:=&civ880CreatedStage{closeUnknown:true,rootAttempted:true,rootCreated:true}
	parent:=&civ880StageParent{}
	prepared:=&civ880PreparedObservation{stage:stage,parent:parent}
	for _,operation:=range []func()error{
		func()error{return prepared.Cleanup(ctx)},
		prepared.Close,
		func()error{return prepared.Cleanup(ctx)},
		prepared.Close,
	}{
		err:=operation()
		if !errors.Is(err,errCiv880StagePreserved)||!errors.Is(err,errCiv880CloseUnknown)||
			prepared.stage!=stage||prepared.parent!=parent||stage.closed||parent.closed{
			t.Fatal("prepared wrapper hid unknown close outcome or lost ownership",err)
		}
	}
	// A preserved namespace before any failed close remains a distinct state.
	liveStage:=&civ880CreatedStage{rootAttempted:true}
	live:=&civ880PreparedObservation{stage:liveStage}
	if err:=live.Close();!errors.Is(err,errCiv880StagePreserved)||errors.Is(err,errCiv880CloseUnknown){
		t.Fatal("retryable live state was conflated with consumed-close uncertainty",err)
	}
	normal:=&civ880PreparedObservation{}
	if err:=normal.Cleanup(ctx);err!=nil{t.Fatal("normal empty cleanup refused",err)}
	if err:=normal.Close();err!=nil{t.Fatal("normal close disabled",err)}
}

func TestCiv880NativeSynchronousLeafRequiresExplicitSynchronize(t *testing.T){
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second);defer cancel()
	parent,err:=civ880OpenStageParentAt(ctx,physicalSoftwareFixture(t))
	if err!=nil{if parent!=nil{_ = parent.Close()};t.Fatal("native fixture parent",err)}
	defer func(){if err:=parent.Close();err!=nil{t.Error("native fixture parent close",err)}}()
	parentFile:=parent.handles[len(parent.handles)-1]
	rootName:="civ880-00000000000000000000000000000004"
	rootHandle,err:=civ880NtCreateNew(syscall.Handle(parentFile.Fd()),rootName,true,0)
	if err!=nil{t.Fatal("native fixture directory",err)}
	root:=os.NewFile(uintptr(rootHandle),filepath.Join(parentFile.Name(),rootName))
	if root==nil{_ = syscall.CloseHandle(rootHandle);t.Fatal("native fixture directory wrapper")}
	defer func(){
		if err:=civ880Disposition(root);err!=nil{t.Error("native fixture directory disposition",err)}
		if err:=root.Close();err!=nil{t.Error("native fixture directory close",err)}
	}()
	// The old regular-file mask used generic rights, which are not a substitute
	// for an explicit SYNCHRONIZE bit when synchronous native I/O is requested.
	legacy,legacyErr:=civ880NtCreateRelative(syscall.Handle(root.Fd()),"legacy-sync.bin",0,0xC0010000,0x80,0x60)
	if legacyErr==nil||legacy!=0&&legacy!=syscall.InvalidHandle{
		// A failed control must still reconcile only the handle it created.
		file:=os.NewFile(uintptr(legacy),filepath.Join(root.Name(),"legacy-sync.bin"))
		if file!=nil{_ = civ880Disposition(file);_ = file.Close()}else if legacy!=0&&legacy!=syscall.InvalidHandle{_ = syscall.CloseHandle(legacy)}
		t.Fatal("old synchronous access mask unexpectedly created a file",legacyErr)
	}
	var failed *civ880NtCreateFailure
	if !errors.As(legacyErr,&failed)||failed.status!=0xc000000d{
		t.Fatal("old synchronous access mask did not reach native INVALID_PARAMETER",legacyErr)
	}
	if _,err:=os.Lstat(filepath.Join(root.Name(),"legacy-sync.bin"));!errors.Is(err,os.ErrNotExist){
		t.Fatal("rejected old native call left a namespace entry",err)
	}
	current,err:=civ880NtCreateNew(syscall.Handle(root.Fd()),"current-sync.bin",false,0)
	if err!=nil||current==0||current==syscall.InvalidHandle{t.Fatal("explicit synchronous access was rejected",err)}
	file:=os.NewFile(uintptr(current),filepath.Join(root.Name(),"current-sync.bin"))
	if file==nil{_ = syscall.CloseHandle(current);t.Fatal("current native file wrapper unavailable")}
	defer func(){if file!=nil{_ = civ880Disposition(file);_ = file.Close()}}()
	id,err:=fileSoftwareIdentity(file);if err!=nil||id.FileID==""{t.Fatal("same native file identity unavailable",err)}
	raw:=[]byte("same-handle synchronous native write and readback")
	if err:=civ880CheckedWrite(ctx,file,raw);err!=nil{t.Fatal("same native file write",err)}
	if err:=civ880CheckHeldBytes(ctx,file,raw,digest(raw));err!=nil{t.Fatal("same native file readback",err)}
	if contender,err:=os.OpenFile(file.Name(),os.O_WRONLY,0);!errors.Is(err,windows.ERROR_SHARING_VIOLATION){
		if contender!=nil{_ = contender.Close()}
		t.Fatal("synchronous repair relaxed write sharing",err)
	}
	if err:=civ880Disposition(file);err!=nil{t.Fatal("same native file disposition",err)}
	unknown:=false
	name:=file.Name()
	if err:=civ880CloseOriginalFile(&file,&unknown);err!=nil||unknown{t.Fatal("same native file close",err)}
	if _,err:=os.Lstat(name);!errors.Is(err,os.ErrNotExist){t.Fatal("same native file remained after marked close",err)}
}
