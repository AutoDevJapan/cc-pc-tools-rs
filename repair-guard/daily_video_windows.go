package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const dailyVideoRoot = "D:/CC/Work/youtube-daily"
const dailyVideoActorRoot = "D:/CC/Work/unattended-operations-20261013"
const dailyVideoActorSpec = "D:/CC/Tools/todo/specs/2906-2b6dffb90a3d435aac994315e80de304.md"
const dailyVideoActorSpecSHA = "565497e5c5cab152350e6df269d00f27e300a0a1ea25e17537954601b5b5c895"
const dailyVideoToolRoot = "D:/CC/Tools/youtube-daily"
const dailyVideoJobsRoot = "D:/CC/Tools/codex-jobs/jobs"

var dailyVideoChannels = map[string]string{
	"beats": "UCls5XR8qyPBKbdpJD7mvv9w", "autophobia": "UCb2x5kHgVY7r3ETv2OpsyDA",
	"empirecats": "UC-2-7ve5Jzl5WdNIdcnuMWw", "autodevjapan": "UCHx8d3sqQca1FK4_gV-deGw",
	"hajime": "UCae39uaPKj9-b0aMqNBA5Hg", "rakuo": "UCoZl6CC1kNlKdj9tIJr0zhQ",
	"yukuhashi": "UCd3X3Ch1d6o_-S6Dokj05MQ", "mash": "UCzQBBiceYneATFx2VTsJ_Wg",
}

var dailyVideoJobName = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[A-Za-z0-9_.-]+$`)

type dailyVideoIO struct {
	root, specs, jobs, toolRoot string
	at                          time.Time
	readObject                  func(string, int64) (object, error)
	read                        func(string, int64) ([]byte, error)
	actor                       func(object) bool
	protected                   func(string) bool
}

func currentDailyVideoPathAllowed(path string) bool {
	io := dailyVideoIO{root: dailyVideoRoot, specs: currentTodoSpecsRoot, jobs: dailyVideoJobsRoot,
		toolRoot: dailyVideoToolRoot, at: time.Now(), readObject: readObject, read: readBounded, protected: legacyMatch}
	io.actor = func(db object) bool {
		return dailyVideoActorAllowed(db, dailyVideoActorRoot, dailyVideoActorSpec, dailyVideoActorSpecSHA, sourceVerified, legacyMatch)
	}
	return dailyVideoPathAllowedWith(path, io)
}

func dailyVideoActorAllowed(db object, root, spec, sha string, source func(string) bool, protected func(string) bool) bool {
	actor := itemAt(db, 2906)
	if actor == nil || actor["dispatchable"] != true || actor["stopped"] == true || actor["owner_stopped"] == true {
		return false
	}
	switch str(actor["status"]) {
	case "todo", "doing", "wait", "done":
	default:
		return false
	}
	return source(root) && currentQualification(db, "cc-pc-tools-rs", 2906, root, spec, sha, protected)
}

func dailyVideoParts(path string, io dailyVideoIO) (string, string, string, bool) {
	rel, err := filepath.Rel(io.root, path)
	if err != nil || rel == "." || rel == ".." || strings.Contains(rel, ":") || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", "", false
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) < 2 || dailyVideoChannels[parts[0]] == "" {
		return "", "", "", false
	}
	day, err := time.Parse("2006-01-02", parts[1])
	jst := time.FixedZone("JST", 9*60*60)
	lastDay, _ := time.Parse("2006-01-02", io.at.In(jst).AddDate(0, 0, 2).Format("2006-01-02"))
	if err != nil || day.Format("2006-01-02") != parts[1] || parts[1] < "2026-10-19" || day.After(lastDay) {
		return "", "", "", false
	}
	return filepath.Join(io.root, parts[0], parts[1]), parts[0], parts[1], true
}

// Older daily jobs link through the authoritative ToDo.jobs and exact cwd.
// When newer launchers publish project/todo_id, those must also agree.
func dailyVideoJobOwned(row object, directory string, io dailyVideoIO) bool {
	jobs, ok := row["jobs"].([]any)
	if !ok {
		return false
	}
	if len(jobs) > 8 {
		jobs = jobs[len(jobs)-8:]
	}
	for _, ref := range jobs {
		id := str(ref)
		if !dailyVideoJobName.MatchString(id) {
			continue
		}
		path := filepath.Join(io.jobs, id, "job.json")
		if io.protected(path) || !currentPlain(path, false) {
			continue
		}
		job, err := io.readObject(path, 1<<20)
		if err != nil || job["id"] != id || !same(str(job["cwd"]), directory) {
			continue
		}
		if value := job["project"]; value != nil {
			project, ok := value.(string)
			if !ok || (project != "" && project != "youtube-daily") {
				continue
			}
		}
		if job["todo_id"] != nil && num(job["todo_id"]) != num(row["id"]) {
			continue
		}
		if value := job["registered_cwd"]; value != nil {
			registered, ok := value.(string)
			if !ok || (registered != "" && !same(registered, io.toolRoot)) {
				continue
			}
		}
		return true
	}
	return false
}

func dailyVideoProductionOwned(db object, directory, channel, day string, io dailyVideoIO) bool {
	if !same(str(obj(db["projects"])["youtube-daily"]), io.toolRoot) {
		return false
	}
	rows, _ := db["items"].([]any)
	prefix := "[youtube-daily " + channel + " " + day + "]"
	for _, value := range rows {
		row := obj(value)
		if row["project"] != "youtube-daily" || !strings.HasPrefix(str(row["title"]), prefix) || row["dispatchable"] != true || row["stopped"] == true || row["owner_stopped"] == true {
			continue
		}
		switch str(row["status"]) {
		case "todo", "doing", "wait", "done":
		default:
			continue
		}
		spec := str(row["spec"])
		if !currentSubworkSpecPath(spec, num(row["id"]), io.specs, io.protected, currentPlain) {
			continue
		}
		bytes, err := io.read(spec, 1<<20)
		if err != nil {
			continue
		}
		body := strings.ReplaceAll(string(bytes), `\`, "/")
		if !strings.Contains(body, "key="+channel+", channel_id="+dailyVideoChannels[channel]) || !strings.Contains(strings.ToLower(body), strings.ToLower(strings.ReplaceAll(directory, `\`, "/")+"/publication.json")) {
			continue
		}
		if dailyVideoJobOwned(row, directory, io) {
			return true
		}
	}
	return false
}

func dailyVideoPathAllowedWith(path string, io dailyVideoIO) bool {
	directory, channel, day, ok := dailyVideoParts(path, io)
	if !ok || io.protected(path) || io.protected(directory) || !currentPlain(directory, true) {
		return false
	}
	db, err := io.readObject(productStore, 64<<20)
	if err != nil || !dailyVideoProductionOwned(db, directory, channel, day, io) || !io.actor(db) {
		return false
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return filepath.Base(path) != "." && currentPlain(filepath.Dir(path), true) && !io.protected(filepath.Dir(path))
	} else if err != nil || !currentNoReparse(path) {
		return false
	}
	resolved, err := physical(path)
	return err == nil && within(directory, resolved) && !io.protected(resolved)
}
