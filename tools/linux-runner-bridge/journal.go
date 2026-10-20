package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type burstJournal struct {
	root, containerID string
	profile           normalProfile
}

type burstBinding struct {
	Schema      int    `json:"schema"`
	Repository  string `json:"repository"`
	Runner      string `json:"runner"`
	Container   string `json:"container"`
	ContainerID string `json:"containerId"`
	ID          int64  `json:"runnerId"`
}

type burstJournalEntry struct {
	binding burstBinding
	state   burstDrainState
	dir     string
	files   []string
	retired bool
}

type burstRetirement struct {
	Binding burstBinding
	State   burstDrainState
}

func journalStageName(name string) bool {
	var mask int
	_, err := fmt.Sscanf(name, "stage-%02x.json", &mask)
	return err == nil && mask >= 0 && mask <= 31 && name == fmt.Sprintf("stage-%02x.json", mask)
}

func journalDirectory(path string) error {
	s, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !s.IsDir() || s.Mode()&os.ModeSymlink != 0 {
		return errors.New("cleanup journal directory rejected")
	}
	return nil
}

func journalBytes(path string) ([]byte, error) {
	s, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !s.Mode().IsRegular() || s.Size() > 8192 {
		return nil, errors.New("cleanup journal file rejected")
	}
	return os.ReadFile(path)
}

// The synced temporary inode is linked into a previously absent name. A crash
// cannot expose half a JSON record or replace a committed stage. Temporary
// records are never authority and are ignored by the bounded reader.
func publishJournal(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil || len(data) > 8192 {
		return errors.New("cleanup journal value rejected")
	}
	if old, err := journalBytes(path); err == nil {
		if bytes.Equal(old, data) {
			return nil
		}
		return errors.New("cleanup journal publication conflicts with existing evidence")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pending-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Link(f.Name(), path); err != nil {
		if old, readErr := journalBytes(path); readErr == nil && bytes.Equal(old, data) {
			return nil
		}
		return errors.New("cleanup journal atomic publication failed")
	}
	return nil
}

func decodeJournal(path string, out any) error {
	data, err := journalBytes(path)
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("cleanup journal JSON rejected")
	}
	return nil
}

func cleanupMask(s burstDrainState) int {
	n := 0
	for i, bit := range []bool{s.LabelsWithdrawn, s.RegistrationRemoved, s.ListenerStopped, s.ProcessDrained, s.ConfigCleared} {
		if bit {
			n |= 1 << i
		}
	}
	return n
}

func validCleanupState(s burstDrainState) bool {
	return s.ID > 0 && !s.Assigned && (!s.RegistrationRemoved || s.LabelsWithdrawn) &&
		(!s.ListenerStopped || s.RegistrationRemoved) && (!s.ConfigCleared || (s.ListenerStopped && s.ProcessDrained))
}

func (j burstJournal) binding(id int64) (burstBinding, error) {
	if !j.profile.ephemeral || id <= 0 || !journalContainerID(j.containerID) {
		return burstBinding{}, errors.New("cleanup journal identity rejected")
	}
	return burstBinding{1, repo, j.profile.runner, j.profile.container, j.containerID, id}, nil
}

func journalContainerID(id string) bool {
	decoded, err := hex.DecodeString(id)
	return err == nil && len(decoded) == 32 && strings.ToLower(id) == id
}

func (j burstJournal) entries() ([]burstJournalEntry, error) {
	if !j.profile.ephemeral {
		return nil, errors.New("recovery journal is restricted to the additional profile")
	}
	if err := journalDirectory(j.root); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(j.root)
	if err != nil || len(entries) > 64 {
		return nil, errors.New("cleanup journal inventory rejected")
	}
	var result []burstJournalEntry
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") && entry.Type().IsRegular() {
			continue
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(entry.Name(), "runner-"), 10, 64)
		if err != nil || id <= 0 || entry.Name() != "runner-"+strconv.FormatInt(id, 10) {
			return nil, errors.New("unknown cleanup journal entry")
		}
		dir := filepath.Join(j.root, entry.Name())
		if journalDirectory(dir) != nil {
			return nil, errors.New("cleanup journal link/directory rejected")
		}
		files, err := os.ReadDir(dir)
		if err != nil || len(files) > 64 {
			return nil, errors.New("cleanup journal stages rejected")
		}
		var retired burstRetirement
		if err := decodeJournal(filepath.Join(dir, "retired.json"), &retired); err == nil {
			b := retired.Binding
			if b.Schema != 1 || b.Repository != repo || b.Runner != j.profile.runner || b.Container != j.profile.container || !journalContainerID(b.ContainerID) || b.ID != id || retired.State.ID != id || !validCleanupState(retired.State) || !retired.State.ConfigCleared {
				return nil, errors.New("cleanup retirement rejected")
			}
			e := burstJournalEntry{binding: b, state: retired.State, dir: dir, retired: true}
			for _, file := range files {
				name := file.Name()
				if !file.Type().IsRegular() || (name != "retired.json" && name != "binding.json" && !journalStageName(name) && !strings.HasPrefix(name, ".pending-")) {
					return nil, errors.New("unknown retired cleanup file")
				}
				e.files = append(e.files, name)
			}
			result = append(result, e)
			continue
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		var binding burstBinding
		if err := decodeJournal(filepath.Join(dir, "binding.json"), &binding); err != nil {
			if os.IsNotExist(err) {
				for _, file := range files {
					if !strings.HasPrefix(file.Name(), ".pending-") || !file.Type().IsRegular() {
						return nil, errors.New("unbound cleanup evidence rejected")
					}
				}
				continue // Directory/temporary inode only; publication never completed.
			}
			return nil, err
		}
		if binding.Schema != 1 || binding.Repository != repo || binding.Runner != j.profile.runner || binding.Container != j.profile.container || !journalContainerID(binding.ContainerID) || binding.ID != id {
			return nil, errors.New("cleanup journal binding rejected")
		}
		e := burstJournalEntry{binding: binding, state: burstDrainState{ID: id}, dir: dir}
		masks := []int{0}
		states := map[int]burstDrainState{0: e.state}
		for _, file := range files {
			if strings.HasPrefix(file.Name(), ".pending-") && file.Type().IsRegular() {
				continue
			}
			e.files = append(e.files, file.Name())
			if file.Name() == "binding.json" {
				continue
			}
			var mask int
			if _, err := fmt.Sscanf(file.Name(), "stage-%02x.json", &mask); err != nil || mask < 0 || mask > 31 || file.Name() != fmt.Sprintf("stage-%02x.json", mask) {
				return nil, errors.New("unknown cleanup stage")
			}
			var state burstDrainState
			if err := decodeJournal(filepath.Join(dir, file.Name()), &state); err != nil {
				return nil, err
			}
			if !validCleanupState(state) || state.ID != id || cleanupMask(state) != mask {
				return nil, errors.New("cleanup stage content rejected")
			}
			masks = append(masks, mask)
			states[mask] = state
		}
		sort.Ints(masks)
		previous := 0
		for _, mask := range masks {
			if previous&mask != previous {
				return nil, errors.New("cleanup stage ordering rejected")
			}
			previous = mask
		}
		e.state = states[previous]
		result = append(result, e)
	}
	sort.Slice(result, func(a, b int) bool { return result[a].binding.ID < result[b].binding.ID })
	return result, nil
}

func (j burstJournal) pending() (burstDrainState, bool, error) {
	entries, err := j.entries()
	if err != nil {
		return burstDrainState{}, false, err
	}
	var pending *burstDrainState
	for _, entry := range entries {
		if entry.state.ConfigCleared {
			continue
		}
		if pending != nil || entry.binding.ContainerID != j.containerID {
			return burstDrainState{}, false, errors.New("cleanup journal ownership/pending conflict")
		}
		state := entry.state
		pending = &state
	}
	if pending == nil {
		return burstDrainState{}, false, nil
	}
	return *pending, true, nil
}

func (j burstJournal) save(state *burstDrainState) error {
	if state.ID <= 0 {
		return nil
	} // An unbound name is never recovery authority.
	s := *state
	s.Assigned = false // Historical assignment is not live kernel evidence.
	if !validCleanupState(s) {
		return errors.New("cleanup state rejected")
	}
	b, err := j.binding(s.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(j.root, 0700); err != nil {
		return err
	}
	if err := journalDirectory(j.root); err != nil {
		return err
	}
	entries, err := j.entries()
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.state.ConfigCleared && e.binding.ID != s.ID {
			return errors.New("another pending cleanup binding exists")
		}
	}
	dir := filepath.Join(j.root, "runner-"+strconv.FormatInt(s.ID, 10))
	if err := os.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	if err := journalDirectory(dir); err != nil {
		return err
	}
	if err := publishJournal(filepath.Join(dir, "binding.json"), b); err != nil {
		return err
	}
	if err := publishJournal(filepath.Join(dir, fmt.Sprintf("stage-%02x.json", cleanupMask(s))), s); err != nil {
		return err
	}
	return j.prune()
}

func (j burstJournal) prune() error {
	entries, err := j.entries()
	if err != nil {
		return err
	}
	completed := 0
	for _, e := range entries {
		if e.state.ConfigCleared {
			completed++
		}
	}
	for _, e := range entries {
		if completed <= 8 && !e.retired {
			continue
		}
		if !e.state.ConfigCleared {
			continue
		}
		// Publish retirement before removing any earlier stage. A crash during
		// retention still has a complete, immutable recovery authority.
		if err := publishJournal(filepath.Join(e.dir, "retired.json"), burstRetirement{e.binding, e.state}); err != nil {
			return err
		}
		files, err := os.ReadDir(e.dir)
		if err != nil {
			return err
		}
		for _, file := range files {
			name := file.Name()
			if name == "retired.json" {
				continue
			}
			if !file.Type().IsRegular() || (name != "binding.json" && !journalStageName(name) && !strings.HasPrefix(name, ".pending-")) {
				return errors.New("retention encountered unknown evidence")
			}
			if err := os.Remove(filepath.Join(e.dir, name)); err != nil {
				return err
			}
		}
		if err := os.Remove(filepath.Join(e.dir, "retired.json")); err != nil {
			return err
		}
		if err := os.Remove(e.dir); err != nil {
			return err
		}
		completed--
	}
	return nil
}
