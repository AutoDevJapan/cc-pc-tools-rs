package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeContainer struct {
	id, name string
	args     []string
	labels   map[string]string
	running  bool
	status   string
	exit     int
	output   []byte
	volume   string
}

// An in-memory Docker daemon. It models only the behavior this component relies
// on, including a CLI that is killed while the container keeps running.
type fakeDaemon struct {
	t  *testing.T
	mu sync.Mutex

	calls                                     []string
	seq                                       int
	containers                                map[string]*fakeContainer
	volumes                                   map[string]bool
	toolPresent, pullFails, imageRmFails      bool
	createMode, startMode, cpMode             string
	lsFails, failLsAfterStart                 bool
	stopStuck, killStuck, rmFails, withVolume bool
	artifact                                  []byte
	logText                                   string
	mutate                                    func(map[string]any)
	onCreate                                  func([]string)
	startReturned, stoppedBeforeDrain         bool
	stdin                                     []byte
	pending                                   *fakeContainer
	startedIDs                                []string
	createCalls                               int
	onStop                                    func()
}

// materialize completes a delayed create, as a slow daemon eventually would.
func (d *fakeDaemon) materialize() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pending == nil {
		return
	}
	for _, o := range d.containers {
		if o.name == d.pending.name {
			d.pending = nil // The name is taken: the late request fails with a conflict.
			return
		}
	}
	d.containers[d.pending.id] = d.pending
	d.pending = nil
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	return &fakeDaemon{t: t, containers: map[string]*fakeContainer{}, volumes: map[string]bool{}, startMode: "success", logText: "build finished\n", artifact: fakeImageTar(t, nil)}
}

type fakeImage struct {
	proof                                                                      map[string]string
	dropLayer, dupLayer, dupEntry, wrongDiff, badConfig, noConfig, emptyLayers bool
	extraDiff, symlink, tamperLayer, garbageLayer, secondLayer                 bool
}

func fakeImageTar(t *testing.T, proof map[string]string) []byte {
	return buildFakeImage(t, fakeImage{proof: proof})
}

// A complete Docker-archive image: manifest, config named by its digest, and a
// gzip layer named by its compressed digest with its diff_id in the config.
func buildFakeImage(t *testing.T, o fakeImage) []byte {
	t.Helper()
	var lt bytes.Buffer
	ltw := tar.NewWriter(&lt)
	name, body := "file", []byte("layer")
	if o.proof != nil {
		name = "proof"
		body, _ = json.Marshal(o.proof)
	}
	ltw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(body)), Typeflag: tar.TypeReg})
	ltw.Write(body)
	ltw.Close()
	if o.garbageLayer {
		lt.Reset()
		lt.Write(bytes.Repeat([]byte("x"), 1024))
	}
	var gzb bytes.Buffer
	gz := gzip.NewWriter(&gzb)
	gz.Write(lt.Bytes())
	gz.Close()
	layer := gzb.Bytes()
	layerName := digest(layer) + ".tar.gz"
	if o.tamperLayer {
		layer = bytes.Clone(layer)
		layer[len(layer)/2] ^= 1
	}
	diff := "sha256:" + digest(lt.Bytes())
	if o.wrongDiff {
		diff = "sha256:" + digest([]byte("other"))
	}
	layers, diffs := []string{layerName}, []string{diff}
	var extra []byte
	var extraName string
	if o.secondLayer {
		var l2 bytes.Buffer
		w := tar.NewWriter(&l2)
		w.WriteHeader(&tar.Header{Name: "file2", Mode: 0600, Size: 6, Typeflag: tar.TypeReg})
		w.Write([]byte("layer2"))
		w.Close()
		var g2 bytes.Buffer
		z := gzip.NewWriter(&g2)
		z.Write(l2.Bytes())
		z.Close()
		extra, extraName = g2.Bytes(), digest(g2.Bytes())+".tar.gz"
		layers, diffs = append(layers, extraName), append(diffs, "sha256:"+digest(l2.Bytes()))
	}
	if o.dupLayer {
		layers, diffs = append(layers, layerName), append(diffs, diff)
	}
	if o.extraDiff {
		diffs = append(diffs, diff)
	}
	if o.emptyLayers {
		layers, diffs = nil, nil
	}
	cfg, _ := json.Marshal(map[string]any{"architecture": "amd64", "os": "linux", "rootfs": map[string]any{"type": "layers", "diff_ids": diffs}})
	cfgName := "sha256:" + digest(cfg)
	if o.badConfig {
		cfgName = "sha256:" + digest([]byte("not the config"))
	}
	manifest, _ := json.Marshal([]map[string]any{{"Config": cfgName, "Layers": layers}})
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	add := func(n string, b []byte) {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0600, Size: int64(len(b)), Typeflag: tar.TypeReg})
		tw.Write(b)
	}
	add("manifest.json", manifest)
	if !o.noConfig {
		add(cfgName, cfg)
	}
	if !o.dropLayer {
		add(layerName, layer)
	}
	if o.secondLayer {
		add(extraName, extra)
	}
	if o.dupEntry {
		add(layerName, layer)
	}
	if o.symlink {
		tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "manifest.json"})
	}
	tw.Close()
	return out.Bytes()
}
func argValues(args []string, flag string) []string {
	var v []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			v = append(v, args[i+1])
		}
	}
	return v
}

func (d *fakeDaemon) callNames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.calls...)
}

func (d *fakeDaemon) called(name string) bool {
	for _, c := range d.callNames() {
		if c == name {
			return true
		}
	}
	return false
}

func (d *fakeDaemon) find(id string) *fakeContainer {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.containers[id]
}

func (d *fakeDaemon) inspectMap(c *fakeContainer) map[string]any {
	d.mu.Lock()
	defer d.mu.Unlock()
	val := func(f string) string {
		if v := argValues(c.args, f); len(v) > 0 {
			return v[0]
		}
		return ""
	}
	cpus, _ := strconv.ParseFloat(val("--cpus"), 64)
	mem, _ := strconv.ParseInt(val("--memory"), 10, 64)
	swap, _ := strconv.ParseInt(val("--memory-swap"), 10, 64)
	pids, _ := strconv.ParseInt(val("--pids-limit"), 10, 64)
	tmpfs := map[string]string{}
	var mounts []map[string]any
	for _, t := range argValues(c.args, "--tmpfs") {
		dest, opts, _ := strings.Cut(t, ":")
		tmpfs[dest] = opts
		mounts = append(mounts, map[string]any{"Type": "tmpfs", "Destination": dest})
	}
	if c.volume != "" {
		mounts = append(mounts, map[string]any{"Type": "volume", "Name": c.volume, "Destination": "/anonymous"})
	}
	pid := 0
	if c.running {
		pid = 4242
	}
	info := map[string]any{
		"Id": c.id, "Name": "/" + c.name,
		"State":  map[string]any{"Status": c.status, "Running": c.running, "ExitCode": c.exit, "Pid": pid, "OOMKilled": false},
		"Config": map[string]any{"Image": releaseBuilderTool, "User": val("--user"), "OpenStdin": true, "Labels": c.labels},
		"HostConfig": map[string]any{
			"Privileged": false, "Binds": nil, "CapAdd": nil, "CapDrop": argValues(c.args, "--cap-drop"), "SecurityOpt": argValues(c.args, "--security-opt"),
			"Devices": nil, "DeviceRequests": nil, "NanoCpus": int64(cpus * 1e9), "Memory": mem, "MemorySwap": swap, "PidsLimit": pids,
			"NetworkMode": val("--network"), "PidMode": "", "IpcMode": "private", "Init": true, "Tmpfs": tmpfs, "PortBindings": map[string]any{},
		},
		"Mounts": mounts,
	}
	if d.mutate != nil {
		d.mutate(info)
	}
	return info
}

func (d *fakeDaemon) finish(c *fakeContainer, code int) {
	d.mu.Lock()
	c.running, c.status, c.exit = false, "exited", code
	d.mu.Unlock()
}

func (d *fakeDaemon) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	name := args[0]
	if name == "container" || name == "image" || name == "volume" {
		name += " " + args[1]
	}
	d.mu.Lock()
	d.calls = append(d.calls, name)
	d.mu.Unlock()
	last := args[len(args)-1]
	switch name {
	case "image inspect":
		if !d.toolPresent {
			io.WriteString(stderr, "Error: No such image: x\n")
			return errors.New("exit status 1")
		}
		if strings.Contains(strings.Join(args, " "), "RepoDigests") {
			fmt.Fprintf(stdout, "sha256:%064d [%q]\n", 7, releaseBuilderTool)
		} else {
			fmt.Fprintf(stdout, "sha256:%064d\n", 7)
		}
	case "pull":
		if d.pullFails {
			return errors.New("exit status 1")
		}
		d.toolPresent = true
	case "image rm":
		if d.imageRmFails {
			return errors.New("exit status 1")
		}
		d.toolPresent = false
	case "create":
		if d.onCreate != nil {
			d.onCreate(args)
		}
		d.mu.Lock()
		d.createCalls++
		first := d.createCalls == 1
		d.mu.Unlock()
		switch d.createMode {
		case "refuse":
			io.WriteString(stderr, "docker: Error response from daemon: Conflict.\n")
			return errors.New("exit status 125")
		case "nothing":
			return errors.New("daemon unavailable")
		}
		d.mu.Lock()
		for _, o := range d.containers {
			if o.name == argValues(args, "--name")[0] {
				d.mu.Unlock()
				io.WriteString(stderr, "docker: Error response from daemon: Conflict. The container name is already in use.\n")
				return errors.New("exit status 125")
			}
		}
		d.seq++
		c := &fakeContainer{id: fmt.Sprintf("%064x", d.seq), name: argValues(args, "--name")[0], args: args, labels: map[string]string{}, status: "created"}
		for _, l := range argValues(args, "--label") {
			k, v, _ := strings.Cut(l, "=")
			c.labels[k] = v
		}
		if d.withVolume {
			c.volume = fmt.Sprintf("%064x", d.seq+1000)
			d.volumes[c.volume] = true
		}
		if d.createMode == "late" && first {
			d.pending = c // The daemon will finish this create after the client gave up.
		} else {
			d.containers[c.id] = c
		}
		d.mu.Unlock()
		if first && (d.createMode == "lose" || d.createMode == "late") {
			return errors.New("response lost")
		}
		fmt.Fprintln(stdout, c.id)
	case "container ls":
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.lsFails {
			return errors.New("exit status 1")
		}
		want := strings.TrimPrefix(argValues(args, "--filter")[0], "label=")
		k, v, hasValue := strings.Cut(want, "=")
		for _, c := range d.containers {
			if got, ok := c.labels[k]; ok && (!hasValue || got == v) {
				fmt.Fprintln(stdout, c.id)
			}
		}
	case "container inspect":
		c := d.find(last)
		if c == nil {
			io.WriteString(stderr, "Error: No such container\n")
			return errors.New("exit status 1")
		}
		return json.NewEncoder(stdout).Encode(d.inspectMap(c))
	case "start":
		c := d.find(last)
		d.mu.Lock()
		d.startedIDs = append(d.startedIDs, last)
		c.running, c.status = true, "running"
		d.mu.Unlock()
		in, _ := io.ReadAll(stdin)
		d.mu.Lock()
		d.stdin = in
		d.mu.Unlock()
		defer func() {
			d.mu.Lock()
			d.startReturned = true
			if d.failLsAfterStart {
				d.lsFails = true
			}
			d.mu.Unlock()
		}()
		switch d.startMode {
		case "hang":
			<-ctx.Done()
			return ctx.Err()
		case "flood":
			chunk := bytes.Repeat([]byte("x"), 1<<20)
			for i := 0; i < 40 && ctx.Err() == nil; i++ {
				stdout.Write(chunk)
			}
			return ctx.Err()
		case "fail":
			io.WriteString(stderr, d.logText)
			d.finish(c, 3)
			return errors.New("exit status 3")
		}
		io.WriteString(stdout, d.logText)
		c.output = d.artifact
		d.finish(c, 0)
	case "container stop", "container kill":
		if d.onStop != nil {
			d.onStop()
		}
		c := d.find(last)
		d.mu.Lock()
		if !d.startReturned && c.status == "running" {
			d.stoppedBeforeDrain = true
		}
		stuck := (name == "container stop" && d.stopStuck) || (name == "container kill" && d.killStuck)
		if c != nil && !stuck {
			c.running, c.status, c.exit = false, "exited", 137
		}
		d.mu.Unlock()
	case "container rm":
		if d.rmFails {
			return errors.New("exit status 1")
		}
		d.mu.Lock()
		if c := d.containers[last]; c != nil {
			delete(d.volumes, c.volume)
			delete(d.containers, last)
		}
		d.mu.Unlock()
	case "volume ls":
		d.mu.Lock()
		for v := range d.volumes {
			fmt.Fprintln(stdout, v)
		}
		d.mu.Unlock()
	case "cp":
		c := d.find(strings.TrimSuffix(strings.SplitN(last, ":", 2)[0], ""))
		if c == nil {
			c = d.find(strings.SplitN(args[1], ":", 2)[0])
		}
		switch d.cpMode {
		case "missing":
			return errors.New("exit status 1")
		case "garbage":
			io.WriteString(stdout, strings.Repeat("not a tar archive", 100))
			return nil
		case "overlimit":
			tw := tar.NewWriter(stdout)
			tw.WriteHeader(&tar.Header{Name: "output.tar", Mode: 0600, Size: productBuilderProfile.artifactMax + 1, Typeflag: tar.TypeReg})
			tw.Flush()
			<-ctx.Done()
			return ctx.Err()
		}
		tw := tar.NewWriter(stdout)
		tw.WriteHeader(&tar.Header{Name: "output.tar", Mode: 0600, Size: int64(len(c.output)), Typeflag: tar.TypeReg})
		tw.Write(c.output)
		tw.Close()
	default:
		d.t.Fatalf("unexpected docker command %q", name)
	}
	return nil
}

func (d *fakeDaemon) remaining() (containers, volumes int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.containers), len(d.volumes)
}

type buildHarness struct {
	t    *testing.T
	d    *fakeDaemon
	spec releaseBuilderSpec
	dir  string
}

func newHarness(t *testing.T) *buildHarness {
	t.Helper()
	releaseCreateRecheckPause = time.Millisecond
	t.Cleanup(func() { releaseCreateRecheckPause = time.Second })
	c := synthSnapshot(t, synthFiles(synthCompose))
	in, err := prepareReleaseBuild(c, synthPins(c))
	if err != nil {
		t.Fatal(err)
	}
	spec, err := newReleaseBuilderSpec(productBuilderProfile, in)
	if err != nil {
		t.Fatal(err)
	}
	return &buildHarness{t: t, d: newFakeDaemon(t), spec: spec, dir: t.TempDir()}
}

func (h *buildHarness) run(ctx context.Context) (releaseBuildReceipt, error) {
	return runReleaseBuilder(ctx, func(string) builderDocker { return h.d }, h.spec, h.dir)
}

func (h *buildHarness) assertDisposed(rec releaseBuildReceipt, toolPresent bool) {
	h.t.Helper()
	c, v := h.d.remaining()
	if c != 0 || v != 0 || h.d.toolPresent != toolPresent || !rec.PhaseMayRelease || rec.RecoveryRequired || !rec.ContainerAbsent || !rec.VolumesAbsent {
		h.t.Fatalf("resources not recovered: containers=%d volumes=%d tool=%v rec=%+v", c, v, h.d.toolPresent, rec.PhaseMayRelease)
	}
}

func (h *buildHarness) assertNoLeak(rec releaseBuildReceipt, err error, secrets ...string) {
	h.t.Helper()
	raw, _ := json.Marshal(rec)
	shown := string(raw) + fmt.Sprintf("%v %+v %#v", rec, rec, rec)
	if err != nil {
		shown += err.Error()
	}
	if state, e := os.ReadFile(filepath.Join(h.dir, releaseBuildStateFile)); e == nil {
		shown += string(state)
	}
	for _, s := range append(secrets, "opaque-sensitive", "Dockerfile.ci") {
		if strings.Contains(shown, s) {
			h.t.Fatalf("receipt, state or error exposes %q", s)
		}
	}
}

func TestBuilderSuccessRecoversEverythingAndKeepsArtifactInRAM(t *testing.T) {
	h := newHarness(t)
	h.d.withVolume = true
	reservedFirst := false
	h.d.onCreate = func(args []string) {
		// The exact nonce, name, image and digest are durable before the daemon is asked.
		raw, err := os.ReadFile(filepath.Join(h.dir, releaseBuildStateFile))
		var saved releaseBuildReceipt
		if err != nil || json.Unmarshal(raw, &saved) != nil || saved.Nonce == "" || saved.ContainerName != argValues(args, "--name")[0] || saved.ToolImage != releaseBuilderTool || !saved.CreateAttempted || !strings.Contains(saved.ToolImage, "@sha256:") {
			t.Error("reservation missing before create")
		}
		reservedFirst = true
	}
	rec, err := h.run(context.Background())
	if err != nil || !rec.Succeeded || !reservedFirst {
		t.Fatalf("build failed: %v %s", err, rec.Failure)
	}
	h.assertDisposed(rec, false) // The tool image was introduced by this run, so it is removed.
	if !rec.ToolImageIntroduced || !rec.ToolImageRemoved || rec.ToolImagePreexisting || !rec.OuterIsolationVerified || !rec.PIDZero || !rec.ContainerRemoved || rec.ExitCode != 0 || len(rec.VolumeNames) != 1 {
		t.Fatalf("disposition incomplete: %+v", rec.PIDZero)
	}
	if string(h.d.stdin) != string(mustVerified(t, h.spec)) {
		t.Fatal("stdin is not the verified context")
	}
	art, err := rec.verifiedArtifact()
	if err != nil || !bytes.Equal(art, h.d.artifact) || rec.ArtifactSHA256 != digest(art) || rec.ArtifactBytes != int64(len(art)) {
		t.Fatal("artifact not returned from RAM")
	}
	art[0] ^= 1
	if again, err := rec.verifiedArtifact(); err != nil || again[0] == art[0] {
		t.Fatal("artifact accessor aliases the receipt")
	}
	log, err := os.ReadFile(filepath.Join(h.dir, releaseBuildLogFile))
	if err != nil || rec.LogBytes != int64(len(log)) || rec.LogSHA256 != digest(log) || rec.LogTruncated {
		t.Fatal("log bytes or digest differ")
	}
	entries, _ := os.ReadDir(h.dir)
	for _, e := range entries {
		if e.Name() != releaseBuildStateFile && e.Name() != releaseBuildLogFile && e.Name() != releaseBuildConfigDir {
			t.Fatalf("unexpected host file %q", e.Name())
		}
	}
	if cfg, _ := os.ReadDir(filepath.Join(h.dir, releaseBuildConfigDir)); len(cfg) != 0 {
		t.Fatal("Docker client config is not empty")
	}
	h.assertNoLeak(rec, err)
	calls := h.d.callNames()
	pull, create := -1, -1
	for i, c := range calls {
		if c == "pull" {
			pull = i
		}
		if c == "create" {
			create = i
		}
	}
	if pull < 0 || create < pull || h.d.stoppedBeforeDrain {
		t.Fatal("tool image must exist before create and I/O must drain before stop")
	}
}

func mustVerified(t *testing.T, s releaseBuilderSpec) []byte {
	b, err := s.verifiedContext()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestBuilderKeepsAPreexistingToolImage(t *testing.T) {
	h := newHarness(t)
	h.d.toolPresent = true
	rec, err := h.run(context.Background())
	if err != nil || h.d.called("pull") || h.d.called("image rm") || !rec.ToolImagePreexisting || rec.ToolImageRemoved {
		t.Fatalf("preexisting image mishandled: %v", err)
	}
	h.assertDisposed(rec, true)
}

func TestBuilderFailureNeverLeaksOutputAndStillRecovers(t *testing.T) {
	h := newHarness(t)
	h.d.withVolume = true
	secret := "ghp_" + strings.Repeat("S", 36) + " password=hunter2"
	h.d.startMode, h.d.logText = "fail", "error: "+secret+"\n"
	rec, err := h.run(context.Background())
	if err == nil || rec.Succeeded || rec.Failure != "build-failed" || rec.ExitCode != 3 || err.Error() != "release build: build-failed" {
		t.Fatalf("unexpected result: %v", err)
	}
	h.assertDisposed(rec, false)
	if _, e := rec.verifiedArtifact(); e == nil || h.d.called("cp") {
		t.Fatal("failed build produced an artifact")
	}
	h.assertNoLeak(rec, err, "ghp_", "hunter2")
	log, _ := os.ReadFile(filepath.Join(h.dir, releaseBuildLogFile))
	if !strings.Contains(string(log), "hunter2") || rec.LogSHA256 != digest(log) {
		t.Fatal("raw log must stay in the private file with its digest")
	}
}

func TestBuilderCancelAndTimeoutDrainBeforeStoppingAndRecover(t *testing.T) {
	for _, mode := range []string{"cancel", "parent-deadline", "builder-deadline"} {
		h := newHarness(t)
		h.d.startMode, h.d.withVolume = "hang", true
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		switch mode {
		case "parent-deadline":
			ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
		case "builder-deadline":
			h.spec.maxDuration = 150 * time.Millisecond
		default:
			go func() {
				for !h.d.called("start") {
					time.Sleep(time.Millisecond)
				}
				cancel()
			}()
		}
		rec, err := h.run(ctx)
		want := map[string]string{"cancel": "canceled", "parent-deadline": "timeout", "builder-deadline": "timeout"}[mode]
		if err == nil || rec.Failure != want || rec.Succeeded {
			t.Fatalf("%s: got %v", mode, err)
		}
		h.assertDisposed(rec, false)
		if !rec.PIDZero || h.d.stoppedBeforeDrain || !h.d.startReturned || !h.d.called("container stop") {
			t.Fatalf("%s: stop did not follow a drained client", mode)
		}
	}
}

func TestBuilderLogOverlimitStopsTheBuilder(t *testing.T) {
	h := newHarness(t)
	h.d.startMode = "flood"
	rec, err := h.run(context.Background())
	if err == nil || rec.Failure != "log-overlimit" || !rec.LogTruncated || rec.LogBytes != releaseBuildLogLimit {
		t.Fatalf("log limit not enforced: %v %d", err, rec.LogBytes)
	}
	h.assertDisposed(rec, false)
	if fi, e := os.Stat(filepath.Join(h.dir, releaseBuildLogFile)); e != nil || fi.Size() != releaseBuildLogLimit {
		t.Fatal("private log exceeds its limit")
	}
}

func TestBuilderRecoversAnUncertainCreateByTheExactNonce(t *testing.T) {
	h := newHarness(t)
	h.d.createMode, h.d.withVolume = "lose", true
	rec, err := h.run(context.Background())
	if err == nil || rec.Failure != "create-uncertain" || !rec.UncertainCreate || h.d.called("start") {
		t.Fatalf("uncertain create mishandled: %v", err)
	}
	h.assertDisposed(rec, false)
	if !rec.ContainerRemoved {
		t.Fatal("recovered container was not removed")
	}
	h = newHarness(t)
	h.d.createMode = "refuse"
	rec, err = h.run(context.Background())
	if err == nil || rec.Failure != "create-failed" || rec.UncertainCreate || h.d.called("start") {
		t.Fatalf("create failure mishandled: %v", err)
	}
	h.assertDisposed(rec, false)
}

func TestBuilderRefusesToTouchForeignOrUnverifiableResources(t *testing.T) {
	h := newHarness(t)
	h.d.mutate = func(info map[string]any) { info["Name"] = "/someone-elses" }
	rec, err := h.run(context.Background())
	c, _ := h.d.remaining()
	if err == nil || rec.PhaseMayRelease || !rec.RecoveryRequired || c != 1 || h.d.called("container rm") || h.d.called("start") {
		t.Fatalf("foreign container touched or released: %v", err)
	}
	if !strings.HasSuffix(err.Error(), "recovery-required") {
		t.Fatal("recovery requirement not reported")
	}
	raw, e := os.ReadFile(filepath.Join(h.dir, releaseBuildStateFile))
	if e != nil || !strings.Contains(string(raw), rec.Nonce) {
		t.Fatal("ownership record not retained")
	}
}

func TestBuilderRejectsWeakerIsolationBeforeStarting(t *testing.T) {
	mutations := map[string]func(map[string]any){
		"privileged": func(i map[string]any) { i["HostConfig"].(map[string]any)["Privileged"] = true },
		"bind":       func(i map[string]any) { i["HostConfig"].(map[string]any)["Binds"] = []string{"x:/y"} },
		"cap add":    func(i map[string]any) { i["HostConfig"].(map[string]any)["CapAdd"] = []string{"SYS_ADMIN"} },
		"cap drop":   func(i map[string]any) { i["HostConfig"].(map[string]any)["CapDrop"] = []string{} },
		"security": func(i map[string]any) {
			i["HostConfig"].(map[string]any)["SecurityOpt"] = []string{"seccomp=unconfined"}
		},
		"cpu":      func(i map[string]any) { i["HostConfig"].(map[string]any)["NanoCpus"] = int64(2_000_000_000) },
		"memory":   func(i map[string]any) { i["HostConfig"].(map[string]any)["Memory"] = int64(8 << 30) },
		"swap":     func(i map[string]any) { i["HostConfig"].(map[string]any)["MemorySwap"] = int64(-1) },
		"pids":     func(i map[string]any) { i["HostConfig"].(map[string]any)["PidsLimit"] = int64(0) },
		"host net": func(i map[string]any) { i["HostConfig"].(map[string]any)["NetworkMode"] = "host" },
		"host pid": func(i map[string]any) { i["HostConfig"].(map[string]any)["PidMode"] = "host" },
		"host ipc": func(i map[string]any) { i["HostConfig"].(map[string]any)["IpcMode"] = "host" },
		"device":   func(i map[string]any) { i["HostConfig"].(map[string]any)["Devices"] = []any{map[string]any{}} },
		"gpu":      func(i map[string]any) { i["HostConfig"].(map[string]any)["DeviceRequests"] = []any{map[string]any{}} },
		"no init":  func(i map[string]any) { i["HostConfig"].(map[string]any)["Init"] = false },
		"tmpfs": func(i map[string]any) {
			i["HostConfig"].(map[string]any)["Tmpfs"] = map[string]string{"/tmp": "size=9g"}
		},
		"mount": func(i map[string]any) { i["Mounts"] = []any{map[string]any{"Type": "bind", "Destination": "/h"}} },
		"image": func(i map[string]any) { i["Config"].(map[string]any)["Image"] = "other" },
		"user":  func(i map[string]any) { i["Config"].(map[string]any)["User"] = "" },
		"ports": func(i map[string]any) {
			i["HostConfig"].(map[string]any)["PortBindings"] = map[string]any{"80/tcp": []any{}}
		},
	}
	for name, mutate := range mutations {
		h := newHarness(t)
		h.d.toolPresent = true
		h.d.mutate = mutate
		rec, err := h.run(context.Background())
		if name == "image" {
			// Not our image: the exact identity check refuses to manage it.
			if err == nil || rec.PhaseMayRelease || h.d.called("start") {
				t.Fatalf("%s accepted", name)
			}
			continue
		}
		if err == nil || rec.Failure != "isolation-mismatch" || rec.OuterIsolationVerified || h.d.called("start") {
			t.Fatalf("%s accepted: %v", name, err)
		}
		h.assertDisposed(rec, true)
	}
}

func TestBuilderArtifactBoundsAndShape(t *testing.T) {
	for mode, want := range map[string]string{"overlimit": "artifact-overlimit", "missing": "artifact-missing", "garbage": "artifact-missing"} {
		h := newHarness(t)
		h.d.cpMode = mode
		rec, err := h.run(context.Background())
		if err == nil || rec.Failure != want || rec.ArtifactBytes != 0 {
			t.Fatalf("%s: got %v %s", mode, err, rec.Failure)
		}
		h.assertDisposed(rec, false)
	}
	h := newHarness(t)
	h.d.artifact = []byte("not an image archive")
	rec, err := h.run(context.Background())
	if err == nil || rec.Failure != "artifact-invalid" || len(rec.artifact) != 0 {
		t.Fatalf("invalid image accepted: %v", err)
	}
	h.assertDisposed(rec, false)
}

func TestBuilderUnconfirmedDisposalWithholdsReleaseUntilRecovered(t *testing.T) {
	cases := map[string]func(*fakeDaemon){
		"list fails":    func(d *fakeDaemon) { d.failLsAfterStart = true },
		"rm fails":      func(d *fakeDaemon) { d.rmFails = true },
		"stop and kill": func(d *fakeDaemon) { d.startMode, d.stopStuck, d.killStuck = "hang", true, true },
		"image stuck":   func(d *fakeDaemon) { d.imageRmFails = true },
	}
	for name, setup := range cases {
		h := newHarness(t)
		h.d.withVolume = true
		setup(h.d)
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		rec, err := h.run(ctx)
		cancel()
		if err == nil || rec.PhaseMayRelease || !rec.RecoveryRequired || rec.Succeeded {
			t.Fatalf("%s: unconfirmed disposal released: %v", name, err)
		}
		if _, e := rec.verifiedArtifact(); e == nil {
			t.Fatalf("%s: artifact released with live resources", name)
		}
		// A later process recovers exactly the saved reservation.
		h.d.lsFails, h.d.rmFails, h.d.stopStuck, h.d.killStuck, h.d.imageRmFails, h.d.failLsAfterStart = false, false, false, false, false, false
		rec, err = recoverReleaseBuilder(context.Background(), func(string) builderDocker { return h.d }, h.dir)
		if err != nil || !rec.PhaseMayRelease || rec.RecoveryRequired {
			t.Fatalf("%s: recovery failed: %v", name, err)
		}
		h.assertDisposed(rec, false)
	}
}

func TestBuilderOwnedDirectoryAndInputGuards(t *testing.T) {
	h := newHarness(t)
	file := filepath.Join(h.dir, "file")
	os.WriteFile(file, []byte("x"), 0o600)
	nonEmpty := t.TempDir()
	os.WriteFile(filepath.Join(nonEmpty, "keep"), []byte("x"), 0o600)
	for name, dir := range map[string]string{"empty": "", "relative": "relative", "missing": filepath.Join(h.dir, "missing"), "file": file, "non-empty": nonEmpty, "unclean": h.dir + string(filepath.Separator) + ".." + string(filepath.Separator) + filepath.Base(h.dir)} {
		hh := newHarness(t)
		hh.dir = dir
		rec, err := hh.run(context.Background())
		if err == nil || rec.Failure != "owned-dir-rejected" || len(hh.d.callNames()) != 0 {
			t.Fatalf("%s: unsafe owned directory accepted: %v", name, err)
		}
	}
	reused := newHarness(t)
	if _, err := reused.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rec, err := reused.run(context.Background()); err == nil || rec.Failure != "owned-dir-rejected" {
		t.Fatal("a used owned directory was accepted again")
	}
	tampered := newHarness(t)
	tampered.spec.context[0] ^= 1
	if rec, err := tampered.run(context.Background()); err == nil || rec.Failure != "input-rejected" || len(tampered.d.callNames()) != 0 {
		t.Fatal("tampered context accepted")
	}
	bad := newHarness(t)
	bad.spec.dockerfile = "x; rm"
	if _, err := bad.run(context.Background()); err == nil || len(bad.d.callNames()) != 0 {
		t.Fatal("unsafe Dockerfile name accepted")
	}
	if _, err := executeAuditedReleaseBuild(context.Background(), sealedReleaseBuildInput{}, t.TempDir()); err == nil {
		t.Fatal("unprepared input executed")
	}
}

func TestBuilderToolImageUncertaintyDoesNotPullOrRemove(t *testing.T) {
	h := newHarness(t)
	h.d.toolPresent = true
	h.d.mutate = nil
	broken := &uncertainImageDocker{fakeDaemon: h.d}
	rec, err := runReleaseBuilder(context.Background(), func(string) builderDocker { return broken }, h.spec, h.dir)
	if err == nil || rec.Failure != "tool-image-unavailable" || h.d.called("pull") || h.d.called("image rm") || h.d.called("create") {
		t.Fatalf("uncertain image state acted upon: %v", err)
	}
}

type uncertainImageDocker struct{ *fakeDaemon }

func (u *uncertainImageDocker) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	if args[0] == "image" {
		u.mu.Lock()
		u.calls = append(u.calls, "image "+args[1])
		u.mu.Unlock()
		io.WriteString(stderr, "error during connect: daemon unreachable\n")
		return errors.New("exit status 1")
	}
	return u.fakeDaemon.run(ctx, stdin, stdout, stderr, args...)
}

func TestKernelProofIsReadBackFromTheBuiltImage(t *testing.T) {
	p := fixtureBuilderProfile
	good := map[string]string{"cpu.max": "50000 100000", "memory.max": "536870912", "pids.max": "128", "CapEff": "0000000000000000", "NoNewPrivs": "1"}
	if got, err := checkBuiltImage(context.Background(), fakeImageTar(t, good), p); err != nil || got["cpu.max"] != "50000 100000" {
		t.Fatalf("valid proof rejected: %v", err)
	}
	for key, bad := range map[string]string{"cpu.max": "max 100000", "memory.max": "max", "pids.max": "max", "CapEff": "000001ffffffffff", "NoNewPrivs": "0"} {
		wrong := map[string]string{}
		for k, v := range good {
			wrong[k] = v
		}
		wrong[key] = bad
		if _, err := checkBuiltImage(context.Background(), fakeImageTar(t, wrong), p); !errors.Is(err, errKernelProof) {
			t.Fatalf("%s mismatch accepted", key)
		}
	}
	if _, err := checkBuiltImage(context.Background(), fakeImageTar(t, nil), p); !errors.Is(err, errKernelProof) {
		t.Fatal("missing proof accepted")
	}
	if _, err := checkBuiltImage(context.Background(), fakeImageTar(t, map[string]string{"cpu.max": "50000 100000"}), p); !errors.Is(err, errKernelProof) {
		t.Fatal("partial proof accepted")
	}
	if _, err := checkBuiltImage(context.Background(), []byte("junk"), p); err == nil {
		t.Fatal("junk accepted as an image")
	}
	// Product images need a manifest but no probe.
	if got, err := checkBuiltImage(context.Background(), fakeImageTar(t, nil), productBuilderProfile); err != nil || got != nil {
		t.Fatalf("product image shape rejected: %v", err)
	}
}

func TestBuilderRefusesToStartWhileAnotherBuilderExists(t *testing.T) {
	for _, lsFails := range []bool{false, true} {
		h := newHarness(t)
		h.d.containers[strings.Repeat("f", 64)] = &fakeContainer{id: strings.Repeat("f", 64), name: "left-behind", labels: map[string]string{releaseBuildLabel: "other"}, status: "exited"}
		h.d.lsFails = lsFails
		rec, err := h.run(context.Background())
		want := "phase-busy"
		if lsFails {
			want = "phase-uncertain"
		}
		if err == nil || rec.Failure != want || rec.PhaseMayRelease || !rec.RecoveryRequired || h.d.called("pull") || h.d.called("create") || h.d.called("container rm") {
			t.Fatalf("%s: phase not protected: %v", want, err)
		}
		if c, _ := h.d.remaining(); c != 1 {
			t.Fatal("another builder was touched")
		}
		if lsFails {
			continue
		}
		// Recovering this run does not clear the hold while the other builder remains.
		factory := func(string) builderDocker { return h.d }
		again, err := recoverReleaseBuilder(context.Background(), factory, h.dir)
		if err == nil || again.PhaseMayRelease || !again.RecoveryRequired {
			t.Fatalf("recovery released a phase held by another builder: %v", err)
		}
		delete(h.d.containers, strings.Repeat("f", 64))
		again, err = recoverReleaseBuilder(context.Background(), factory, h.dir)
		if err != nil || !again.PhaseMayRelease {
			t.Fatalf("recovery did not release a free phase: %v", err)
		}
	}
}

func TestBuilderUnknownCreateIsResolvedOnlyByTakingTheName(t *testing.T) {
	ctx := context.Background()
	// The daemon is unreachable for both the create and the name hold.
	h := newHarness(t)
	h.d.createMode = "nothing"
	factory := func(string) builderDocker { return h.d }
	rec, err := h.run(ctx)
	if err == nil || rec.Failure != "create-uncertain" || !rec.UncertainCreate || rec.PhaseMayRelease || !rec.RecoveryRequired || h.d.called("start") {
		t.Fatalf("an unknown create released the phase: %v", err)
	}
	again, err := recoverReleaseBuilder(ctx, factory, h.dir)
	if err == nil || again.PhaseMayRelease {
		t.Fatal("recovery released an unresolved create")
	}
	h.d.createMode = "" // The daemon answers again: the name hold succeeds and is removed.
	again, err = recoverReleaseBuilder(ctx, factory, h.dir)
	if err != nil || !again.PhaseMayRelease || !again.ContainerRemoved {
		t.Fatalf("recovery did not resolve the create: %v", err)
	}
	h.assertDisposed(again, false)

	// A create the daemon finishes later can no longer win the name.
	h = newHarness(t)
	h.d.createMode = "late"
	rec, err = h.run(ctx)
	if err == nil || rec.Failure != "create-uncertain" || !rec.PhaseMayRelease || !rec.ContainerRemoved || h.d.called("start") {
		t.Fatalf("late create not resolved in the run: %v", err)
	}
	// The late request lands after the release. It can only be an inert,
	// never-started container; the next phase removes it instead of being blocked.
	h.d.materialize()
	if c, _ := h.d.remaining(); c != 1 {
		t.Fatal("expected the late container to land")
	}
	next := newHarness(t)
	next.d = h.d
	rec2, err := next.run(ctx)
	if err != nil || !rec2.Succeeded || !h.d.called("container rm") || len(h.d.startedIDs) != 1 {
		t.Fatalf("the next phase did not sweep the inert leftover: %v", err)
	}
	if c, _ := h.d.remaining(); c != 0 {
		t.Fatal("leftover containers remain")
	}

	// A create that lands just before the name hold is found by its nonce.
	h = newHarness(t)
	h.d.createMode = "late"
	calls := 0
	h.d.onCreate = func([]string) {
		calls++
		if calls == 2 {
			h.d.materialize()
		}
	}
	rec, err = h.run(ctx)
	if err == nil || !rec.PhaseMayRelease || !rec.ContainerRemoved || h.d.called("start") {
		t.Fatalf("a create that landed first was not removed: %v", err)
	}
	h.assertDisposed(rec, false)

	// A crash before the daemon answered: the saved reservation already says so.
	h = newHarness(t)
	h.d.createMode = "nothing"
	state := filepath.Join(h.dir, releaseBuildStateFile)
	var snapshot []byte
	h.d.onCreate = func([]string) { snapshot, _ = os.ReadFile(state) }
	if _, err := h.run(ctx); err == nil {
		t.Fatal("unexpected success")
	}
	var saved releaseBuildReceipt
	if json.Unmarshal(snapshot, &saved) != nil || !saved.CreateAttempted || !saved.UncertainCreate || saved.Nonce == "" {
		t.Fatal("the unconfirmed create was not durable before the request")
	}
	for _, landed := range []bool{true, false} {
		h2 := newHarness(t)
		if err := os.WriteFile(filepath.Join(h2.dir, releaseBuildStateFile), snapshot, 0o600); err != nil {
			t.Fatal(err)
		}
		if landed {
			id := strings.Repeat("e", 64)
			h2.d.containers[id] = &fakeContainer{id: id, name: saved.ContainerName, labels: map[string]string{releaseBuildLabel: saved.Nonce}, status: "created"}
		}
		again, err := recoverReleaseBuilder(ctx, func(string) builderDocker { return h2.d }, h2.dir)
		if err != nil || !again.PhaseMayRelease || !again.ContainerRemoved {
			t.Fatalf("landed=%v: recovery after a crash failed: %v", landed, err)
		}
		h2.assertDisposed(again, false)
	}
}

func TestBuilderRecoveryChecksTheWholePhaseWhateverTheSavedFlagsSay(t *testing.T) {
	h := newHarness(t)
	h.d.toolPresent = true
	if _, err := h.run(context.Background()); err != nil {
		t.Fatal(err)
	}
	id := strings.Repeat("d", 64)
	h.d.containers[id] = &fakeContainer{id: id, name: "other", labels: map[string]string{releaseBuildLabel: "someone-else"}, status: "exited"}
	again, err := recoverReleaseBuilder(context.Background(), func(string) builderDocker { return h.d }, h.dir)
	if err == nil || again.PhaseMayRelease || !again.RecoveryRequired {
		t.Fatalf("a foreign builder was ignored: %v", err)
	}
	delete(h.d.containers, id)
	if again, err = recoverReleaseBuilder(context.Background(), func(string) builderDocker { return h.d }, h.dir); err != nil || !again.PhaseMayRelease {
		t.Fatalf("free phase not released: %v", err)
	}
}

func TestBuiltImageLayersMustBeBoundedWellFormedTars(t *testing.T) {
	ctx := context.Background()
	if _, err := checkBuiltImage(ctx, buildFakeImage(t, fakeImage{garbageLayer: true}), productBuilderProfile); err == nil {
		t.Fatal("a layer that is not a tar passed with correct digests")
	}
	saved := imageUncompressedTotalLimit
	imageUncompressedTotalLimit = 1024
	t.Cleanup(func() { imageUncompressedTotalLimit = saved })
	if _, err := checkBuiltImage(ctx, buildFakeImage(t, fakeImage{}), productBuilderProfile); err == nil {
		t.Fatal("expansion beyond the total limit accepted")
	}
	// Each layer alone fits; together they exceed the shared budget.
	imageUncompressedTotalLimit = 3000
	if _, err := checkBuiltImage(ctx, buildFakeImage(t, fakeImage{}), productBuilderProfile); err != nil {
		t.Fatalf("a single layer under the limit rejected: %v", err)
	}
	if _, err := checkBuiltImage(ctx, buildFakeImage(t, fakeImage{secondLayer: true}), productBuilderProfile); err == nil {
		t.Fatal("two layers over the shared budget accepted")
	}
	imageUncompressedTotalLimit = saved
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := checkBuiltImage(cctx, buildFakeImage(t, fakeImage{}), productBuilderProfile); err == nil {
		t.Fatal("validation continued after cancellation")
	}
	// Cancellation in the middle of the reads, not only before them.
	if _, err := checkBuiltImage(&countdownCtx{Context: ctx, n: 3}, buildFakeImage(t, fakeImage{secondLayer: true}), productBuilderProfile); err == nil {
		t.Fatal("validation continued after a mid-read cancellation")
	}
	if _, err := checkBuiltImage(&countdownCtx{Context: ctx, n: 1 << 30}, buildFakeImage(t, fakeImage{secondLayer: true}), productBuilderProfile); err != nil {
		t.Fatalf("validation failed without cancellation: %v", err)
	}
}
func TestBuilderKeepsTheContainerWhenVolumeNamesCannotBeSaved(t *testing.T) {
	h := newHarness(t)
	h.d.withVolume, h.d.startMode = true, "hang"
	state := filepath.Join(h.dir, releaseBuildStateFile)
	h.d.onStop = func() { // The ownership record becomes unwritable while the builder is stopped.
		os.Remove(state)
		os.Mkdir(state, 0o700)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for !h.d.called("start") {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	rec, err := h.run(ctx)
	if c, v := h.d.remaining(); err == nil || rec.PhaseMayRelease || !rec.RecoveryRequired || h.d.called("container rm") || c != 1 || v != 1 {
		t.Fatalf("container removed without a durable volume record: %v", err)
	}
	h.d.onStop = nil
	os.Remove(state)
	raw, _ := json.Marshal(rec)
	if err := os.WriteFile(state, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := recoverReleaseBuilder(context.Background(), func(string) builderDocker { return h.d }, h.dir)
	if err != nil || !again.PhaseMayRelease {
		t.Fatalf("recovery failed: %v", err)
	}
	h.assertDisposed(again, false)
}

func TestBuilderNeverReportsSuccessWithoutABuiltArtifact(t *testing.T) {
	for _, upTo := range []int{4, 5} { // create and inspect only; then also run the build but skip the artifact
		h := newHarness(t)
		h.d.toolPresent = true
		ctx := context.Background()
		b := &builderRun{parent: ctx, spec: h.spec, dir: h.dir, rec: newBuildReceipt(h.spec)}
		if class := b.begin(func(string) builderDocker { return h.d }); class != "" {
			t.Fatal(class)
		}
		steps := []func(context.Context) string{b.phaseClear, b.toolImage, b.create, b.verifyOuter, b.build}
		for _, step := range steps[:upTo] {
			if class := step(ctx); class != "" {
				t.Fatal(class)
			}
		}
		b.dispose()
		rec, err := b.finish()
		if err == nil || rec.Succeeded || rec.Failure != "build-incomplete" || !rec.OuterIsolationVerified || rec.Started != (upTo == 5) {
			t.Fatalf("upTo %d: incomplete run reported as %q: %v", upTo, rec.Failure, err)
		}
		if _, e := rec.verifiedArtifact(); e == nil {
			t.Fatal("artifact returned for an incomplete run")
		}
		h.assertDisposed(rec, true)
	}
}

func TestBuiltImageArchiveIsValidatedCompletely(t *testing.T) {
	if _, err := checkBuiltImage(context.Background(), buildFakeImage(t, fakeImage{}), productBuilderProfile); err != nil {
		t.Fatalf("complete image rejected: %v", err)
	}
	for name, o := range map[string]fakeImage{
		"missing layer":     {dropLayer: true},
		"missing config":    {noConfig: true},
		"config not digest": {badConfig: true},
		"wrong diff id":     {wrongDiff: true},
		"duplicate layer":   {dupLayer: true},
		"duplicate entry":   {dupEntry: true},
		"extra diff id":     {extraDiff: true},
		"no layers":         {emptyLayers: true},
		"link entry":        {symlink: true},
		"tampered layer":    {tamperLayer: true},
	} {
		if _, err := checkBuiltImage(context.Background(), buildFakeImage(t, o), productBuilderProfile); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	good := buildFakeImage(t, fakeImage{})
	if _, err := checkBuiltImage(context.Background(), good[:len(good)/2], productBuilderProfile); err == nil {
		t.Fatal("truncated archive accepted")
	}
}

func TestProductEntryOnlyAcceptsTheAuditedSnapshot(t *testing.T) {
	c := synthSnapshot(t, synthFiles(synthCompose))
	in, err := prepareReleaseBuild(c, synthPins(c))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releaseBuildSpecFromInput(in); err == nil {
		t.Fatal("synthetic input became a product spec")
	}
	if rec, err := executeAuditedReleaseBuild(context.Background(), in, t.TempDir()); err == nil || rec.Failure != "input-rejected" {
		t.Fatal("synthetic input executed")
	}
	saved := auditedReleaseBuildPins
	auditedReleaseBuildPins = synthPins(c)
	t.Cleanup(func() { auditedReleaseBuildPins = saved })
	audited, err := prepareAuditedReleaseBuild(c)
	if err != nil {
		t.Fatal(err)
	}
	spec, err := releaseBuildSpecFromInput(audited)
	if err != nil || spec.profile.kind != "audited-product" {
		t.Fatalf("audited input rejected: %v", err)
	}
	auditedReleaseBuildPins = saved
	forged := in
	forged.audited = true
	if _, err := releaseBuildSpecFromInput(forged); err == nil {
		t.Fatal("input of another identity accepted")
	}
}

// countdownCtx reports cancellation after n polls.
type countdownCtx struct {
	context.Context
	n int32
}

func (c *countdownCtx) Err() error {
	if atomic.AddInt32(&c.n, -1) < 0 {
		return context.Canceled
	}
	return nil
}
