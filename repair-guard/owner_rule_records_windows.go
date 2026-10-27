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
}

func ownerReadRecordObject(ctx context.Context, d *jsontext.Decoder, kind string) (ownerRecordMetadata, bool) {
	var m ownerRecordMetadata
	first, err := d.ReadToken()
	if err != nil || first.Kind() != '{' {
		return m, false
	}
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil || key.Kind() != '"' || ctx.Err() != nil {
			return m, false
		}
		name := key.String()
		scalar := name == "id" || name == "todo_id" || name == "project" || name == "cwd" || name == "spec" ||
			kind == "work_owner" && (name == "repository" || name == "worktree") || kind == "scheduler" && (name == "prompt" || name == "worktree")
		if scalar {
			token, err := d.ReadToken()
			if err == nil && token.Kind() == 'n' && name != "id" && name != "todo_id" {
				continue
			}
			if err != nil || token.Kind() != '"' && !((name == "id" || name == "todo_id") && token.Kind() == '0') {
				return m, false
			}
			value := token.String()
			if len(value) > 32768 || strings.ContainsRune(value, '\ufffd') {
				return m, false
			}
			switch name {
			case "id":
				m.id = strings.Clone(value)
			case "todo_id":
				m.todo = strings.Clone(value)
			case "project":
				m.project = strings.Clone(value)
			case "cwd":
				m.cwd = strings.Clone(value)
			case "repository":
				m.repository = strings.Clone(value)
			case "worktree":
				m.worktree = strings.Clone(value)
			case "spec":
				m.spec = strings.Clone(value)
			case "prompt":
				m.prompt = strings.Clone(value)
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
			if name == "work_owner" {
				m.repository, m.worktree = sub.repository, sub.worktree
			} else {
				m.prompt = sub.prompt
				if m.worktree == "" {
					m.worktree = sub.worktree
				}
			}
		} else if kind == "record" && name == "spec_history" {
			start, err := d.ReadToken()
			if err != nil || start.Kind() != '[' {
				return m, false
			}
			for d.PeekKind() != ']' {
				sub, ok := ownerReadRecordObject(ctx, d, "history")
				if !ok || len(m.refs) >= 4096 {
					return m, false
				}
				m.refs = append(m.refs, sub.spec)
			}
			if _, err := d.ReadToken(); err != nil {
				return m, false
			}
		} else if d.SkipValue() != nil {
			return m, false
		}
	}
	_, err = d.ReadToken()
	return m, err == nil && ctx.Err() == nil
}

func ownerTaskRecordProjection(ctx context.Context, input io.Reader, id string) (ownerRecordMetadata, bool) {
	d := jsontext.NewDecoder(&ownerProjectNestingReader{input: ownerDeadlineReader{ctx, input}})
	start, err := d.ReadToken()
	if err != nil || start.Kind() != '{' {
		return ownerRecordMetadata{}, false
	}
	var found ownerRecordMetadata
	count := 0
	for d.PeekKind() != '}' {
		key, err := d.ReadToken()
		if err != nil || key.Kind() != '"' {
			return found, false
		}
		if key.String() != "items" {
			if d.SkipValue() != nil {
				return found, false
			}
			continue
		}
		start, err := d.ReadToken()
		if err != nil || start.Kind() != '[' {
			return found, false
		}
		for d.PeekKind() != ']' {
			m, ok := ownerReadRecordObject(ctx, d, "record")
			if !ok {
				return found, false
			}
			if m.id == id {
				found = m
				count++
			}
		}
		if _, err := d.ReadToken(); err != nil {
			return found, false
		}
	}
	if _, err := d.ReadToken(); err != nil {
		return found, false
	}
	_, err = d.ReadToken()
	return found, err == io.EOF && count == 1 && ctx.Err() == nil
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
	return result, ok && err == nil && os.SameFile(before, after) && before.Size() == after.Size() && before.ModTime() == after.ModTime() && currentPlain(path, false) && !legacyMatch(path) && ctx.Err() == nil
}

func ownerRuleStoredContent(ctx context.Context, path string, projects object, access ownerSourceAccess, config ownerRuleConfig) (ownerPathVerdict, bool) {
	for _, root := range config.mixedStores {
		if root != "" && within(root, path) {
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
			d := jsontext.NewDecoder(ownerDeadlineReader{ctx, input})
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
		m, ok := ownerReadMetadataFile(ctx, config.todoStore, func(input io.Reader) (ownerRecordMetadata, bool) { return ownerTaskRecordProjection(ctx, input, id) })
		if !ok {
			return ownerRuleUnknown("stored_record_metadata_unavailable"), true
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
	v := ownerRuleWith(ctx, m.cwd, projects, access, config, nil)
	if v.AccessAllowed {
		return ownerRuleClear("verified_self_record_producer")
	}
	return v
}
