package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func skillsRevisionCommand(op, path, workspace string) skillsCreationCommand {
	cmd := skillsForwardCommand(path, workspace)
	cmd.Cmd = "& '" + skillsRuntimeTarget + "' --" + op + " '" + path + "'"
	return cmd
}

func skillsRevisionInvocation(op, path, workspace string) string {
	if (op != "prepare-skills3080-revision" && op != "apply-skills3080-revision") || skillsForwardInvocation(path, workspace) == "" {
		return ""
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(skillsRevisionCommand(op, path, workspace)) != nil {
		return ""
	}
	return "text(await tools.exec_command(" + strings.TrimSpace(b.String()) + "));"
}

func skillsRevisionCall(input, op, path, cwd string) bool {
	const prefix, suffix = "text(await tools.exec_command(", "));"
	input = strings.TrimSpace(input)
	if skillsRevisionInvocation(op, path, cwd) == "" || !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return false
	}
	var cmd skillsCreationCommand
	return skillsStrict([]byte(strings.TrimSuffix(strings.TrimPrefix(input, prefix), suffix)), &cmd) && cmd == skillsRevisionCommand(op, path, cwd)
}

// Only the exact non-yielded native tool result can issue a transaction ID.
// This projection neither decodes unrelated messages nor accepts copied logs.
func skillsRevisionNativeProjection(input io.Reader, thread, cwd string, start, end time.Time, c skillsConsumeRequest, op string) (skillsRevisionEvidence, bool) {
	var evidence skillsRevisionEvidence
	if !skillsCallID.MatchString(c.CallID) || end.Before(start) {
		return evidence, false
	}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session, called, returned := false, false, false
	var callAt time.Time
	for scan.Scan() {
		var env struct {
			Type, Timestamp string
			Payload         json.RawMessage
		}
		if json.Unmarshal(scan.Bytes(), &env) != nil {
			return evidence, false
		}
		if env.Type != "session_meta" && env.Type != "response_item" {
			continue
		}
		var p struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Cwd    string          `json:"cwd"`
			Name   string          `json:"name"`
			CallID string          `json:"call_id"`
			Input  string          `json:"input"`
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(env.Payload, &p) != nil {
			return evidence, false
		}
		if env.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return evidence, false
			}
			session = true
			continue
		}
		if p.CallID != c.CallID {
			continue
		}
		if _, ok := conversationJSON(scan.Bytes()); !ok {
			return evidence, false
		}
		at, err := time.Parse(time.RFC3339Nano, env.Timestamp)
		if !session || err != nil || at.Before(start) || at.After(end) {
			return evidence, false
		}
		switch p.Type {
		case "custom_tool_call":
			if called || returned || p.Name != "exec" || !skillsRevisionCall(p.Input, op, c.RequestPath, cwd) {
				return evidence, false
			}
			called, callAt = true, at
		case "custom_tool_call_output":
			if !called || returned || at.Before(callAt) {
				return evidence, false
			}
			var blocks []struct{ Type, Text string }
			if json.Unmarshal(p.Output, &blocks) != nil || len(blocks) != 2 || blocks[0].Type != "input_text" || blocks[1].Type != "input_text" || !strings.HasPrefix(blocks[0].Text, "Script completed\nWall time ") || !strings.HasSuffix(blocks[0].Text, "\nOutput:\n") {
				return evidence, false
			}
			wrapper, ok := conversationJSON([]byte(blocks[1].Text))
			if !ok || wrapper["exit_code"] != json.Number("0") || wrapper["session_id"] != nil {
				return evidence, false
			}
			result, ok := conversationJSON([]byte(str(wrapper["output"])))
			if !ok || result["qualified"] != true || result["operation"] != op || result["request_sha256"] != c.RequestSHA || result["general_access_granted"] != false || result["original_registry_recovered"] != false {
				return evidence, false
			}
			b, err := json.Marshal(result["revision_evidence"])
			if err != nil || !skillsStrict(b, &evidence) || evidence.Writer != c.Creator || evidence.RequestSHA != c.RequestSHA || result["manifest_sha256"] != evidence.ManifestSHA {
				return evidence, false
			}
			returned = true
		default:
			return evidence, false
		}
	}
	return evidence, scan.Err() == nil && session && called && returned
}

func skillsReadRevisionReceipt(s *skillsFS, c skillsConsumeRequest, op string, r skillsRequest, reg skillsRegistry, original skillsCreatedEvidence, current skillsProducer) (skillsRevisionEvidence, string) {
	var empty skillsRevisionEvidence
	if !skillsWriterValid(c.Creator) || !skillsDigest(c.RequestSHA) || !within(c.Creator.Workspace, c.RequestPath) || !filepath.IsAbs(c.RequestPath) || len(c.Indices) != 0 {
		return empty, "request"
	}
	job, ok := skillsReadJob(s, c.Creator.Job)
	if !ok || !skillsCreationAuthorBound(c.Creator, r.Writer, reg, job) {
		return empty, "writer"
	}
	raw, ok := s.read(c.RequestPath, 1<<20)
	var request skillsRequest
	if !ok || hashBytes(raw) != c.RequestSHA || !skillsStrict(raw, &request) || request.Domain != "skills-3080-operation-v1" || request.Writer != c.Creator || request.Revision == nil ||
		request.ManifestCommit != r.ManifestCommit || request.ManifestPath != r.ManifestPath || request.ManifestSHA != r.ManifestSHA || request.Review == nil || r.Review == nil || *request.Review != *r.Review ||
		request.Consume != nil || request.Runtime != nil || request.Result != nil || skillsProjectionSHA(request.Revision.Creation) != skillsProjectionSHA(r.Revision.Creation) || len(request.Revision.Indices) != 0 || request.Revision.Applied != nil {
		return empty, "creation_evidence"
	}
	if (op == "prepare-skills3080-revision" && request.Revision.Prepared != nil) || (op == "apply-skills3080-revision" && (request.Revision.Prepared == nil || r.Revision.Prepared == nil || skillsProjectionSHA(request.Revision.Prepared) != skillsProjectionSHA(r.Revision.Prepared))) {
		return empty, "creation_evidence"
	}
	start, err := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	end := time.Now()
	if err != nil || start.After(end) {
		return empty, "creation_evidence"
	}
	if job["status"] != "running" {
		end, err = time.Parse(time.RFC3339Nano, str(job["finished_at"]))
		if err != nil || end.Before(start) {
			return empty, "creation_evidence"
		}
	}
	input, ok := skillsNativeSnapshot(s, job)
	if !ok {
		return empty, "creation_evidence"
	}
	e, ok := skillsRevisionNativeProjection(input, str(job["thread_id"]), c.Creator.Workspace, start, end, c, op)
	if !ok || !skillsRevisionEvidenceBound(e, r, original, current) {
		return empty, "creation_evidence"
	}
	return e, ""
}
