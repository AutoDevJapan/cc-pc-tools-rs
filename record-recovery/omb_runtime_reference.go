package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const omb3255Bot = "52137063-f227-4e4f-b0ff-c91661cb8950"
const omb3255Task = "62780a26-01a6-4464-bb87-c7f3fa09df81"
const omb3255NativeThread = "01a16e1b-2add-70a0-b9c6-eed2f08acfe1"
const omb3255ReaderMarker = "3255-runtime-reader-review-v1"
const omb3255SourceRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3255-godd"
const omb3255Codex = "D:/CC/Tools/codex-progress-tui/codex.exe"
const omb3255CodexSHA = "618bf6dcd243463d9058f0ce68b9e43ba2a2b2ac75c715f87ad40d06e453433f"

var omb3255RuntimeUnavailable = errors.New("omb_runtime_metadata_unavailable")

type omb3255ExpectedRoot struct {
	ID            string
	Start, Finish int64
	SHA           string
}

func omb3255ExpectedRoots() []omb3255ExpectedRoot {
	return []omb3255ExpectedRoot{
		{"4559e952-315a-4053-aee9-125d627cb639", 1792848636233, 1792848928582, "1a658f6d9f640bdce848c6276a40c9213f884fe4f01067c54bb350b81a6a250e"},
		{"8bd78cbf-3ed3-4b5c-8f87-4d791f8e6a2a", 1792885731542, 1792885929806, "b70572b4094905a25d82f0be25c633300e7a3511773d57f838a6965c9a34ec6b"},
		{"f0aba1a1-5387-4360-bfbc-d24daab8c5b1", 1792886114268, 1792886228521, "578f3f3b31dd9551388d78f10b81dc527c0fa1d044f2728ed1fbda583837a392"},
		{"42a51568-5abf-406d-8e8b-b5d8cf206be5", 1792889233775, 1792889336241, "6068352433cf515acd5a8a8e7534ba5a54b1ff8cf7f83619cfd428c75ce7e837"},
		{"5f04078f-6251-4f21-88c0-1766da7fa397", 1792889474004, 1792889514547, "3c3bb03259fa2f791ef57f12ff21c73fe26dee62af2473d76df977639287efe4"},
		{"12cd0423-e392-43c5-bbaa-70cf4fe31f45", 1792906444714, 1792906604636, "f16b8e30d47285002d4a719bfba75c698662ed18643684b45b9b573d0f71c1ec"},
		{"d5016190-1fe1-4af2-9fce-2bcdc27e2f60", 1792906703194, 1792906744559, "8f765795161da5be4b485ce7b72809fc2cd574df629e885ad50730fb6779671b"},
		{"52ef5176-3f6c-429a-b5a3-7a4124708fb0", 1792908928181, 1792909013394, "9a6d5d5fc4006ce793a2f04ba8fe7c3010b2eda9b38b467610d85d0dc3afede6"},
		{"6a573c2d-6f09-4bf8-8b55-f3ff2861322d", 1792909070946, 1792909140454, "18c604b5ad182d724bcb7e241e811546dd04b1976645d20ab6cc7dca0ce0ee67"},
		{"7ca1e6e2-d92a-4824-acf7-32fe7f900356", 1792911581971, 1792911732909, "803b54fc916377cf8a8b6aa30445100370edabff2abc2b664b669c94b93c0276"},
		{"6725f3b5-6599-4948-af90-09997fb922cd", 1792917963844, 1792918443665, "74bc4b2807726761bc46a65e8260952a46f7c3ed48835afbac83811defd6999b"},
		{"99a7ab65-f323-4713-af30-430ac8353e44", 1792918682533, 1792918902897, "783c2e86813c3a0c3b78f28c44f707bb76270e965d84df8f5054ce66a392041d"},
		{"597bb81b-9c79-4253-bc2c-ebd4a1698432", 1792920365040, 1792920470454, "ea9a10404079d46d3b41f38832b31ffea5729a649604847b056bb0143a01a322"},
		{"eb52e183-c1b3-459e-b1b1-345b4284e9b7", 1792920764387, 1792920906296, "087770860ea9d7a3cda28d39b32128dc9dbc1e77050cee621a14f7f18608009b"},
		{"0f97d9c1-fbce-4189-b6ce-60761a9ec6a7", 1792922527431, 1792922742447, "92c4f727c0d6f0e8696ab1297ebb887bcd9ad9b97bd2412076a80c85467b9f9c"},
	}
}

func omb3255Int(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return -1
	}
	for _, c := range n.String() {
		if c < '0' || c > '9' {
			return -1
		}
	}
	i, err := strconv.ParseInt(n.String(), 10, 64)
	if err != nil {
		return -1
	}
	return i
}

func omb3255SecondsMS(value any) (int64, bool) {
	seconds := omb3255Int(value)
	if seconds < 0 || seconds > math.MaxInt64/1000 {
		return 0, false
	}
	return seconds * 1000, true
}

func omb3255Decode(data []byte) (obj, error) {
	if len(data) == 0 || len(data) > 8<<20 || !utf8.Valid(data) || !omb3255UniqueContextJSON(data) {
		return nil, omb3255RuntimeUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value obj
	if decoder.Decode(&value) != nil || value == nil {
		return nil, omb3255RuntimeUnavailable
	}
	return value, nil
}

func omb3255Object(v any) obj {
	if value, ok := v.(obj); ok {
		return value
	}
	value, _ := v.(map[string]any)
	return value
}
func omb3255Array(v any) []any { value, _ := v.([]any); return value }
func omb3255Text(v any) string { value, _ := v.(string); return value }
func omb3255SamePath(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(strings.ReplaceAll(a, "/", string(filepath.Separator))), filepath.Clean(strings.ReplaceAll(b, "/", string(filepath.Separator))))
}

// The normal Codex API can return a Win32 extended local-drive path. Admit
// only an equivalent spelling of the fixed native path, then hand the fixed
// ordinary path to the handle reader. UNC/device/namespace aliases stay denied.
func omb3255AdmitNativePath(raw, expected string) (string, bool) {
	value := strings.ReplaceAll(raw, `\`, "/")
	if strings.HasPrefix(value, "//?/") {
		value = value[4:]
		if len(value) < 3 || !((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) || value[1:3] != ":/" {
			return "", false
		}
	} else if strings.HasPrefix(value, "//") {
		return "", false
	}
	if strings.ContainsRune(value, 0) {
		return "", false
	}
	for index, component := range strings.Split(value, "/") {
		if (component == "" && index > 0) || component == "." || component == ".." || strings.TrimRight(component, " .") != component ||
			(strings.Contains(component, ":") && !(index == 0 && len(component) == 2 && component[1] == ':')) {
			return "", false
		}
	}
	if !omb3255SamePath(value, strings.ReplaceAll(expected, `\`, "/")) {
		return "", false
	}
	return expected, true
}
func omb3255TextSHA(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

type omb3255MetadataRPC struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	out      *bufio.Scanner
	sequence int64
}

func omb3255StartMetadataRPC(ctx context.Context) (*omb3255MetadataRPC, error) {
	return omb3255StartMetadataRPCAt(ctx, omb3255SourceRoot)
}

func omb3255StartMetadataRPCAt(ctx context.Context, sourceRoot string) (*omb3255MetadataRPC, error) {
	cmd := exec.CommandContext(ctx, omb3255Codex, "app-server", "--listen", "stdio://")
	hideCommand(cmd)
	cmd.WaitDelay = time.Second
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY", "OPENMAUSBOT_CHATGPT_TOKEN", "OPENMAUSBOT_COMPANY_API_KEY":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Dir = sourceRoot
	cmd.Stderr = io.Discard
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, omb3255RuntimeUnavailable
	}
	if cmd.Start() != nil {
		input.Close()
		return nil, omb3255RuntimeUnavailable
	}
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 4096), 8<<20)
	rpc := &omb3255MetadataRPC{cmd: cmd, in: input, out: scanner}
	_, err = rpc.Call("initialize", obj{"clientInfo": obj{"name": "todo3255-runtime-metadata", "version": "1"}, "capabilities": obj{"experimentalApi": true}})
	if err == nil {
		err = json.NewEncoder(input).Encode(obj{"method": "initialized"})
	}
	if err != nil {
		rpc.Close()
		return nil, omb3255RuntimeUnavailable
	}
	return rpc, nil
}

func (rpc *omb3255MetadataRPC) Call(method string, params obj) (obj, error) {
	switch method {
	case "initialize", "thread/read", "thread/turns/list", "thread/items/list":
	default:
		return nil, omb3255RuntimeUnavailable
	}
	rpc.sequence++
	if json.NewEncoder(rpc.in).Encode(obj{"id": rpc.sequence, "method": method, "params": params}) != nil {
		return nil, omb3255RuntimeUnavailable
	}
	for i := 0; i < 32 && rpc.out.Scan(); i++ {
		value, err := omb3255Decode(rpc.out.Bytes())
		if err != nil {
			return nil, err
		}
		if omb3255Int(value["id"]) != rpc.sequence {
			continue
		}
		if value["error"] != nil {
			return nil, omb3255RuntimeUnavailable
		}
		result := omb3255Object(value["result"])
		if result == nil {
			return nil, omb3255RuntimeUnavailable
		}
		return result, nil
	}
	return nil, omb3255RuntimeUnavailable
}

func (rpc *omb3255MetadataRPC) Close() error {
	inputErr := rpc.in.Close()
	waitErr := rpc.cmd.Wait()
	if inputErr != nil || waitErr != nil {
		return omb3255RuntimeUnavailable
	}
	return nil
}

func omb3255NormalGET(ctx context.Context, endpoint string) (obj, error) {
	if !strings.HasPrefix(endpoint, "/api/") {
		return nil, omb3255RuntimeUnavailable
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:38799"+endpoint, nil)
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, omb3255RuntimeUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, omb3255RuntimeUnavailable
	}
	return omb3255Decode(data)
}

func omb3255RootValid(row obj) bool {
	return omb3255Text(row["botId"]) == omb3255Bot && omb3255Text(row["threadId"]) == omb3255Task &&
		omb3255Text(row["id"]) == omb3255Text(row["rootId"]) && omb3255Int(row["depth"]) == 0 &&
		omb3255Text(row["status"]) == "completed" && omb3255Text(row["outcome"]) == "done" && omb3255Int(row["executions"]) == 1
}

func omb3255ChooseRoots(job obj, revision string, commitMS int64) ([]omb3255ExpectedRoot, error) {
	return omb3255ChooseRootsFor(job, revision, commitMS, omb3255ExpectedRoots())
}

func omb3255ChooseRootsFor(job obj, revision string, commitMS int64, wanted []omb3255ExpectedRoot) ([]omb3255ExpectedRoot, error) {
	if omb3255Text(job["threadId"]) != omb3255Task {
		return nil, omb3255RuntimeUnavailable
	}
	byID := map[string][]obj{}
	var own []omb3255ExpectedRoot
	for _, v := range omb3255Array(job["nodes"]) {
		row := omb3255Object(v)
		id := omb3255Text(row["id"])
		byID[id] = append(byID[id], row)
		result := omb3255Text(row["result"])
		if revision != "" && commitMS > 0 && omb3255RootValid(row) && omb3255Int(row["startedAt"]) >= commitMS && strings.Contains(result, omb3255ReaderMarker) && strings.Contains(result, revision) {
			own = append(own, omb3255ExpectedRoot{id, omb3255Int(row["startedAt"]), omb3255Int(row["finishedAt"]), omb3255TextSHA(result)})
		}
	}
	for _, expected := range wanted {
		rows := byID[expected.ID]
		if len(rows) != 1 || !omb3255RootValid(rows[0]) || omb3255Int(rows[0]["startedAt"]) != expected.Start || omb3255Int(rows[0]["finishedAt"]) != expected.Finish || omb3255TextSHA(omb3255Text(rows[0]["result"])) != expected.SHA {
			return nil, omb3255RuntimeUnavailable
		}
	}
	if len(own) > 1 {
		return nil, omb3255RuntimeUnavailable
	}
	if len(own) == 1 {
		for _, expected := range wanted {
			if expected.ID == own[0].ID {
				return nil, omb3255RuntimeUnavailable
			}
		}
		wanted = append(wanted, own[0])
	}
	sort.Slice(wanted, func(i, j int) bool { return wanted[i].Start < wanted[j].Start })
	return wanted, nil
}

func omb3255CollectRefs(ctx context.Context, rpc *omb3255MetadataRPC, revision string, commitMS int64) ([]omb3255RuntimeRef, string, error) {
	return omb3255CollectRefsFrom(func(endpoint string) (obj, error) { return omb3255NormalGET(ctx, endpoint) }, rpc.Call, revision, commitMS, omb3255ExpectedRoots())
}

func omb3255CollectRefsFrom(get func(string) (obj, error), call func(string, obj) (obj, error), revision string, commitMS int64, expectedRoots []omb3255ExpectedRoot) ([]omb3255RuntimeRef, string, error) {
	if get == nil || call == nil || len(expectedRoots) == 0 || len(expectedRoots) > 15 {
		return nil, "", omb3255RuntimeUnavailable
	}
	profile, err := get("/api/bots?messages=0")
	if err != nil {
		return nil, "", err
	}
	cwd := ""
	matches := 0
	for _, v := range omb3255Array(profile["bots"]) {
		bot := omb3255Object(v)
		if omb3255Text(bot["id"]) != omb3255Bot {
			continue
		}
		for _, value := range omb3255Array(bot["tasks"]) {
			task := omb3255Object(value)
			if omb3255Text(task["threadId"]) == omb3255Task {
				matches++
				busy, present := task["busy"].(bool)
				if !present || busy || omb3255Text(task["activity"]) != "idle" {
					return nil, "", omb3255RuntimeUnavailable
				}
				cwd = omb3255Text(task["cwd"])
			}
		}
	}
	if matches != 1 || cwd == "" {
		return nil, "", omb3255RuntimeUnavailable
	}
	job, err := get("/api/threads/" + omb3255Task + "/job?scope=job&since=1792848636233&full=1")
	if err != nil {
		return nil, "", err
	}
	requested, err := omb3255ChooseRootsFor(job, revision, commitMS, expectedRoots)
	if err != nil {
		return nil, "", err
	}
	native, err := call("thread/read", obj{"threadId": omb3255NativeThread, "includeTurns": false})
	if err != nil {
		return nil, "", err
	}
	thread := omb3255Object(native["thread"])
	if omb3255Text(thread["id"]) != omb3255NativeThread || omb3255Int(thread["createdAt"]) != 1792848636 || !omb3255SamePath(omb3255Text(thread["cwd"]), cwd) || len(omb3255Array(thread["turns"])) != 0 {
		return nil, "", omb3255RuntimeUnavailable
	}
	path, pathOK := omb3255AdmitNativePath(omb3255Text(thread["path"]), omb3255ExpectedNativePath())
	if !pathOK {
		return nil, "", omb3255RuntimeUnavailable
	}
	turns, err := call("thread/turns/list", obj{"threadId": omb3255NativeThread, "limit": 32, "sortDirection": "asc", "itemsView": "notLoaded"})
	if err != nil {
		return nil, "", err
	}
	var refs []omb3255RuntimeRef
	for _, expected := range requested {
		var candidates []obj
		for _, v := range omb3255Array(turns["data"]) {
			turn := omb3255Object(v)
			start, startOK := omb3255SecondsMS(turn["startedAt"])
			end, endOK := omb3255SecondsMS(turn["completedAt"])
			if startOK && endOK && start >= expected.Start-999 && end <= expected.Finish+999 && end >= start && omb3255Text(turn["status"]) == "completed" && omb3255Text(turn["itemsView"]) == "notLoaded" && len(omb3255Array(turn["items"])) == 0 && turn["error"] == nil {
				candidates = append(candidates, turn)
			}
		}
		if len(candidates) != 1 {
			return nil, "", omb3255RuntimeUnavailable
		}
		turnID := omb3255Text(candidates[0]["id"])
		if !omb3255ContextID(turnID) {
			return nil, "", omb3255RuntimeUnavailable
		}
		items, err := call("thread/items/list", obj{"threadId": omb3255NativeThread, "turnId": turnID, "limit": 4, "sortDirection": "desc"})
		if err != nil {
			return nil, "", err
		}
		finals := 0
		for _, v := range omb3255Array(items["data"]) {
			entry := omb3255Object(v)
			item := omb3255Object(entry["item"])
			if omb3255Text(entry["turnId"]) == turnID && omb3255Text(item["type"]) == "agentMessage" && omb3255Text(item["phase"]) == "final_answer" && omb3255TextSHA(omb3255Text(item["text"])) == expected.SHA {
				finals++
			}
		}
		if finals != 1 {
			return nil, "", omb3255RuntimeUnavailable
		}
		refs = append(refs, omb3255RuntimeRef{expected.ID, expected.Start, expected.Finish, omb3255NativeThread, turnID, expected.SHA})
	}
	if !omb3255RuntimeRefsValid(refs) {
		return nil, "", omb3255RuntimeUnavailable
	}
	return refs, path, nil
}

func omb3255ExpectedNativePath() string {
	profile, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(profile, ".codex", "sessions", "2026", "10", "24", "rollout-2026-10-24T22-30-36-"+omb3255NativeThread+".jsonl")
}

func omb3255BuildIdentity() (string, int64) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", 0
	}
	revision, stamp, clean := "", "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			stamp = setting.Value
		case "vcs.modified":
			clean = setting.Value == "false"
		}
	}
	when, err := time.Parse(time.RFC3339, stamp)
	if !clean || len(revision) != 40 || err != nil {
		return "", 0
	}
	if _, err := hex.DecodeString(revision); err != nil {
		return "", 0
	}
	return revision, when.UnixMilli()
}

func omb3255RuntimeStage(value string) string {
	switch value {
	case "platform_unavailable", "dependency_unavailable", "source_identity_unavailable", "source_guard_unavailable", "binary_guard_unavailable", "binary_open_unavailable", "binary_identity_unavailable", "metadata_reader_unavailable", "metadata_reader_close_unavailable", "normal_binding_unavailable", "source_open_unavailable", "source_read_unavailable", "source_recheck_unavailable", "source_close_unavailable", "native_size_unavailable", "native_encoding_unavailable", "native_line_unavailable", "native_json_unavailable", "native_row_unavailable", "native_payload_unavailable", "native_identity_unavailable", "native_header_unavailable", "native_turn_unavailable", "native_context_repeated", "native_context_conflicting", "native_context_time_unavailable", "context_unavailable", "observed", "observed_repeated":
		return value
	}
	return "unavailable"
}

func runOMB3255RuntimeCLI(args []string, output io.Writer, acquire func() omb3255RuntimeAcquisition) int {
	if len(args) != 0 {
		_ = json.NewEncoder(output).Encode(obj{"ok": false, "code": "omb_runtime_metadata_invalid_arguments"})
		return 2
	}
	var acquisition omb3255RuntimeAcquisition
	if acquire != nil {
		acquisition = acquire()
	}
	records := acquisition.records
	status := "unavailable"
	allObserved := len(records) > 0
	for _, record := range records {
		if record.Observation.Status == "observed" || record.Observation.Status == "partial" {
			status = "partial"
		}
		allObserved = allObserved && record.Observation.Status == "observed"
	}
	if allObserved {
		status = "observed"
	}
	result := struct {
		Kind              string                 `json:"kind"`
		Status            string                 `json:"status"`
		AcquisitionState  string                 `json:"acquisition_state"`
		Records           []omb3255RuntimeRecord `json:"records"`
		GuardChanged      bool                   `json:"general_guard_changed"`
		DataAccessGranted bool                   `json:"data_access_granted"`
	}{"omb-review-runtime-metadata-3255", status, omb3255RuntimeStage(acquisition.state), records, false, false}
	if json.NewEncoder(output).Encode(result) != nil {
		return 1
	}
	return 0
}
