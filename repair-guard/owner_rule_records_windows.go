package main

// Mixed operational stores contain both self work and client work. Classify
// one record by producer/registration metadata, never by the stores' D:/CC
// location. Prompt, reply, credential and source bodies are skipped as streams.
import (
	"context"
	"encoding/json/jsontext"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type ownerRecordMetadata struct {
	id, todo, project, cwd, repository, worktree, spec, prompt string
	refs                                                       []string
	ids                                                        []string
	invalid                                                    bool
}

func ownerReadRecordObject(ctx context.Context, d *jsontext.Decoder, kind string) (ownerRecordMetadata, bool) {
	var m ownerRecordMetadata
	if d.PeekKind() != '{' {
		m.invalid = true
		return m, d.SkipValue() == nil
	}
	if _, err := d.ReadToken(); err != nil {
		return m, false
	}
	seen := map[string]bool{}
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil || key.Kind() != '"' || ctx.Err() != nil {
			return m, false
		}
		name := strings.Clone(key.String())
		scalar := kind == "record" && (name == "id" || name == "todo_id" || name == "project" || name == "cwd" || name == "spec") || kind == "history" && name == "spec" ||
			kind == "work_owner" && (name == "repository" || name == "worktree") || kind == "scheduler" && (name == "prompt" || name == "worktree")
		nested := kind == "record" && (name == "work_owner" || name == "scheduler" || name == "spec_history")
		if scalar || nested {
			if seen[name] {
				m.invalid = true
			}
			seen[name] = true
		}
		if scalar {
			k := d.PeekKind()
			if k == 'n' && name != "id" || name == "spec" && (k == 't' || k == 'f') {
				if d.SkipValue() != nil {
					return m, false
				}
				continue
			}
			if k != '"' && !((name == "id" || name == "todo_id") && k == '0') {
				m.invalid = true
				if d.SkipValue() != nil {
					return m, false
				}
				continue
			}
			token, err := d.ReadToken()
			if err != nil {
				return m, false
			}
			value := strings.Clone(token.String())
			if len(value) > 32768 || strings.ContainsRune(value, '\ufffd') {
				m.invalid = true
				continue
			}
			switch name {
			case "id":
				if len(m.ids) < 16 {
					m.ids = append(m.ids, value)
				} else {
					return m, false
				}
				m.id = value
			case "todo_id":
				m.todo = value
			case "project":
				m.project = value
			case "cwd":
				m.cwd = value
			case "repository":
				m.repository = value
			case "worktree":
				m.worktree = value
			case "spec":
				m.spec = value
			case "prompt":
				m.prompt = value
			}
		} else if kind == "record" && (name == "work_owner" || name == "scheduler") {
			if d.PeekKind() == 'n' {
				if d.SkipValue() != nil {
					return m, false
				}
				continue
			}
			sub, ok := ownerReadRecordObject(ctx, d, name)
			if !ok {
				return m, false
			}
			m.invalid = m.invalid || sub.invalid
			if name == "work_owner" {
				m.repository, m.worktree = sub.repository, sub.worktree
			} else {
				m.prompt = sub.prompt
				if m.worktree == "" {
					m.worktree = sub.worktree
				}
			}
		} else if kind == "record" && name == "spec_history" {
			if d.PeekKind() != '[' {
				m.invalid = true
				if d.SkipValue() != nil {
					return m, false
				}
				continue
			}
			if _, err := d.ReadToken(); err != nil {
				return m, false
			}
			for d.PeekKind() != ']' {
				sub, ok := ownerReadRecordObject(ctx, d, "history")
				if !ok {
					return m, false
				}
				m.invalid = m.invalid || sub.invalid
				if len(m.refs) >= 4096 {
					m.invalid = true
				} else {
					m.refs = append(m.refs, sub.spec)
				}
			}
			if _, err := d.ReadToken(); err != nil {
				return m, false
			}
		} else if d.SkipValue() != nil {
			return m, false
		}
	}
	_, err := d.ReadToken()
	return m, err == nil && ctx.Err() == nil
}
func ownerTaskRecordProjection(ctx context.Context, input io.Reader, id string) (ownerRecordMetadata, bool) {
	_, m, ok := ownerTaskRegistrationProjection(ctx, input, id)
	return m, ok && m.matchesID(id) && !m.invalid
}

func ownerTaskRegistrationProjection(ctx context.Context, input io.Reader, id string) (object, ownerRecordMetadata, bool) {
	d := jsontext.NewDecoder(&ownerProjectNestingReader{input: ownerDeadlineReader{ctx, input}}, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
	start, err := d.ReadToken()
	if err != nil || start.Kind() != '{' {
		return nil, ownerRecordMetadata{}, false
	}
	var found ownerRecordMetadata
	var projects object
	count := 0
	seen := map[string]bool{}
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil || key.Kind() != '"' {
			return nil, found, false
		}
		name := strings.Clone(key.String())
		if seen[name] {
			return nil, found, false
		}
		seen[name] = true
		if key.String() == "projects" {
			start, err := d.ReadToken()
			if err != nil || start.Kind() != '{' {
				return nil, found, false
			}
			projects = object{}
			for d.PeekKind() != '}' {
				name, err := d.ReadToken()
				if err != nil || name.Kind() != '"' {
					return nil, found, false
				}
				n := strings.Clone(name.String())
				value, err := d.ReadToken()
				if err != nil || value.Kind() != '"' || len(n) > 4096 || len(value.String()) > 32768 || strings.ContainsRune(n, '\ufffd') || strings.ContainsRune(value.String(), '\ufffd') || len(projects) >= 4096 {
					return nil, found, false
				}
				if _, duplicate := projects[n]; duplicate {
					return nil, found, false
				}
				projects[n] = strings.Clone(value.String())
			}
			if _, err := d.ReadToken(); err != nil {
				return nil, found, false
			}
			continue
		}
		if key.String() != "items" {
			if d.SkipValue() != nil {
				return nil, found, false
			}
			continue
		}
		start, err := d.ReadToken()
		if err != nil || start.Kind() != '[' {
			return nil, found, false
		}
		for d.PeekKind() != ']' {
			m, ok := ownerReadRecordObject(ctx, d, "record")
			if !ok {
				return nil, found, false
			}
			if m.matchesID(id) {
				found = m
				count++
			}
		}
		if _, err := d.ReadToken(); err != nil {
			return nil, found, false
		}
	}
	if _, err := d.ReadToken(); err != nil {
		return nil, found, false
	}
	_, err = d.ReadToken()
	if count != 1 {
		found.invalid = true
	}
	return projects, found, err == io.EOF && ctx.Err() == nil
}

func ownerReadMetadataFile(ctx context.Context, path string, read func(io.Reader) (ownerRecordMetadata, bool)) (ownerRecordMetadata, bool) {
	var empty ownerRecordMetadata
	if ctx.Err() != nil || legacyMatch(path) || !currentPlain(path, false) {
		return empty, false
	}
	before, err := os.Lstat(path)
	if err != nil || before.Size() > 1<<30 {
		return empty, false
	}
	// Same shared-delete metadata reader used by the ordinary ToDo snapshot.
	f, err := retirement3250OpenShared(path)
	if err != nil {
		return empty, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return empty, false
	}
	result, ok := read(io.LimitReader(f, 1<<30))
	after, err := os.Lstat(path)
	return result, ok && !result.invalid && err == nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime().Equal(after.ModTime()) && currentPlain(path, false) && !legacyMatch(path) && ctx.Err() == nil
}

func ownerRuleStoredContent(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig) (ownerPathVerdict, bool) {
	for _, root := range []string{config.jobStore, config.specStore, config.promptStore} {
		if root != "" && !within(root, path) && ownerRuleMixedStorePath(root, path) {
			return ownerRuleProtected("mixed_client_record_store"), true
		}
	}
	for _, root := range config.mixedStores {
		if ownerRuleMixedStorePath(root, path) {
			if access.conversation != nil && access.conversation(path) {
				return ownerRuleClear("verified_conversation_record"), true
			}
			if access.operational != nil && access.operational(path) {
				return ownerRuleClear("verified_operational_receipt"), true
			}
			return ownerRuleProtected("mixed_client_record_store"), true
		}
	}
	if config.jobStore != "" && within(config.jobStore, path) {
		rel, _ := filepath.Rel(config.jobStore, path)
		if rel == "." {
			return ownerRuleProtected("mixed_client_record_store"), true
		}
		job := strings.Split(rel, string(os.PathSeparator))[0]
		if !ownerOwnedJob.MatchString(job) {
			return ownerRuleUnknown("stored_record_metadata_unavailable"), true
		}
		m, ok := ownerReadMetadataFile(ctx, filepath.Join(config.jobStore, job, "job.json"), func(input io.Reader) (ownerRecordMetadata, bool) {
			d := jsontext.NewDecoder(ownerDeadlineReader{ctx, input}, jsontext.AllowDuplicateNames(true), jsontext.AllowInvalidUTF8(true))
			m, ok := ownerReadRecordObject(ctx, d, "record")
			_, err := d.ReadToken()
			return m, ok && err == io.EOF
		})
		if !ok || m.id != job {
			return ownerRuleUnknown("stored_record_metadata_unavailable"), true
		}
		return ownerRuleRecordOrigin(ctx, m, projects, access, config), true
	}
	for _, store := range []string{config.specStore, config.promptStore} {
		if store == "" || !within(store, path) {
			continue
		}
		if same(store, path) {
			return ownerRuleProtected("mixed_client_record_store"), true
		}
		if !same(filepath.Dir(path), store) {
			return ownerRuleUnknown("stored_record_reference_unverified"), true
		}
		id, _, ok := strings.Cut(filepath.Base(path), "-")
		if _, err := strconv.ParseUint(id, 10, 64); err != nil || !ok {
			return ownerRuleUnknown("stored_record_reference_unverified"), true
		}
		var m ownerRecordMetadata
		if config.taskMetadata != nil && config.taskMetadataID == id {
			m, ok = *config.taskMetadata, config.taskMetadata.matchesID(id) && !config.taskMetadata.invalid
		} else {
			m, ok = ownerReadMetadataFile(ctx, config.todoStore, func(input io.Reader) (ownerRecordMetadata, bool) { return ownerTaskRecordProjection(ctx, input, id) })
		}
		if !ok {
			return ownerRuleUnknown("stored_record_reference_unverified"), true
		}
		refs := append([]string{m.spec, m.prompt}, m.refs...)
		matched := false
		for _, ref := range refs {
			matched = matched || ref != "" && same(ref, path)
		}
		if !matched {
			return ownerRuleUnknown("stored_record_reference_unverified"), true
		}
		if m.cwd == "" {
			m.cwd = m.worktree
		}
		if root, ok := projects[m.project].(string); ok && m.cwd == "" {
			m.cwd = root
		}
		return ownerRuleRecordOrigin(ctx, m, projects, access, config), true
	}
	return ownerPathVerdict{}, false
}

func ownerRuleMixedStorePath(root, path string) bool {
	if root == "" {
		return false
	}
	if within(root, path) {
		return true
	}
	parent := filepath.Dir(root)
	if !within(parent, path) {
		return false
	}
	rel, err := filepath.Rel(parent, path)
	return err == nil && strings.HasPrefix(strings.ToLower(strings.Split(rel, string(os.PathSeparator))[0]), strings.ToLower(filepath.Base(root))+".")
}

func ownerRuleRecordedPath(path string, config ownerRuleConfig) bool {
	for _, root := range append(append([]string{}, config.mixedStores...), config.jobStore, config.specStore, config.promptStore) {
		if ownerRuleMixedStorePath(root, path) {
			return true
		}
	}
	return false
}

func (m ownerRecordMetadata) matchesID(id string) bool {
	if m.id == id {
		return true
	}
	for _, candidate := range m.ids {
		if candidate == id {
			return true
		}
	}
	return false
}

func ownerRuleRecordOrigin(ctx context.Context, m ownerRecordMetadata, projects object, access ownerSourceAccess, config ownerRuleConfig) ownerPathVerdict {
	if access.membership(m.project, m.repository) == "client_project" || m.cwd != "" && ownerRuleDevelopment(m.cwd, config.dev) == "client" {
		return ownerRuleProtected("client_record_producer")
	}
	if !filepath.IsAbs(m.cwd) || m.worktree != "" && !same(m.cwd, m.worktree) {
		return ownerRuleUnknown("stored_record_origin_unverified")
	}
	for _, root := range []string{config.jobStore, config.specStore, config.promptStore} {
		if root != "" && within(root, m.cwd) {
			return ownerRuleUnknown("stored_record_origin_unverified")
		}
	}
	if m.repository != "" {
		_, valid := ownerOriginRepository("https://github.com/" + m.repository)
		if !valid {
			return ownerRuleProtected("client_record_producer")
		}
		id, actual, owner, verified := access.identity(m.repository)
		if !verified || id <= 0 || !strings.EqualFold(actual, m.repository) || !ownerSelfGitHubOwner(owner) {
			return ownerRuleUnknown("stored_record_origin_unverified")
		}
		if id == 1156778979 {
			return ownerRuleProtected("client_repository_id")
		}
	}
	producerConfig := config
	producerConfig.recordProducer = true
	v := ownerRuleWith(ctx, m.cwd, projects, access, producerConfig, nil)
	if v.AccessAllowed {
		return ownerRuleClear("verified_self_record_producer")
	}
	if v.Status == "protected" {
		return v
	}
	return ownerRuleUnknown("stored_record_origin_unverified")
}
