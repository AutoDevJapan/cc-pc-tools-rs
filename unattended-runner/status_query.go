package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var pureStatusQuestion = regexp.MustCompile(`^(?:今(?:って|は)?|いま)(?:作業|仕事)(?:してる|しています)(?:の|か|のか)?[?？]$`)

func isPureStatusQuestion(text string) bool {
	return pureStatusQuestion.MatchString(strings.TrimSpace(text))
}

// A pinned, already-verified answer is an observation, not a new send request.
// In particular it cannot authorize any different product or old question.
func (r *Runner) verifyExistingStatusAnswer(ctx context.Context, c ProductReportConfig, req *Request) bool {
	if !isPureStatusQuestion(req.Text) {
		return false
	}
	bytes, err := os.ReadFile(c.Source.AnswerReceipt)
	if err != nil || len(bytes) > 1<<20 || bytesHash(bytes) != c.Source.AnswerReceiptSHA {
		return false
	}
	var p struct {
		Domain    string `json:"domain"`
		Todo      uint64 `json:"todo"`
		Thread    string `json:"thread_id"`
		Source    string `json:"source_message_id"`
		Owner     string `json:"owner_id"`
		SourceSHA string `json:"source_body_sha256"`
		BodySHA   string `json:"body_sha256"`
		Stage     string `json:"stage"`
		Message   string `json:"message_id"`
	}
	if json.Unmarshal(bytes, &p) != nil || p.Domain != "owner-request-status-response-v1" || p.Stage != "verified" || p.Todo != c.Todo || p.Thread != req.Thread || p.Source != req.Message || p.Owner != r.State.Destination.Owner || p.Message != c.Source.AnswerMessage {
		return false
	}
	var source, answer Message
	if r.get(ctx, "/channels/"+req.Thread+"/messages/"+req.Message, &source) != nil || source.ID != req.Message || source.Channel != req.Thread || source.Author.ID != r.State.Destination.Owner || source.Author.Bot || bytesHash([]byte(source.Content)) != p.SourceSHA {
		return false
	}
	if r.get(ctx, "/channels/"+req.Thread+"/messages/"+p.Message, &answer) != nil || answer.ID != p.Message || answer.Channel != req.Thread || answer.Author.ID != r.State.Destination.Bot || !answer.Author.Bot || answer.Flags&4096 == 0 || answer.MentionEveryone || len(answer.Mentions) != 0 || bytesHash([]byte(answer.Content)) != p.BodySHA {
		return false
	}
	if answer.Reference.Message != "" && answer.Reference.Message != req.Message || answer.Reference.Channel != "" && answer.Reference.Channel != req.Thread || answer.Reference.Guild != "" && answer.Reference.Guild != r.State.Destination.Guild {
		return false
	}
	created, e := time.Parse(time.RFC3339Nano, answer.Timestamp)
	original, e2 := time.Parse(time.RFC3339Nano, req.Created)
	return e == nil && e2 == nil && !created.Before(original) && !created.After(r.Now().Add(time.Minute))
}

type statusQueryObservation struct {
	At       string `json:"observed_at"`
	Running  int    `json:"running"`
	Waiting  int    `json:"waiting"`
	Complete int    `json:"complete_records"`
	Unknown  int    `json:"unverified"`
}

func (r *Runner) statusQueryBody(req *Request, o statusQueryObservation) string {
	return fmt.Sprintf("状態: 継続中\n作業状況を確認しました。稼働を確認できた担当は%d件、待機は%d件、作業項目の完了記録は%d件、未確認は%d件です。\n確認時刻: %s\n元の投稿: https://discord.com/channels/%s/%s/%s", o.Running, o.Waiting, o.Complete, o.Unknown, o.At, r.State.Destination.Guild, req.Thread, req.Message)
}

// A pure status question needs only verified owner provenance and read-only
// task/process metadata. It never waits for protected product execution scope.
func (r *Runner) answerStatusQuery(ctx context.Context, req *Request) error {
	if err := r.validateRequest(ctx, req); err != nil {
		return err
	}
	if !isPureStatusQuestion(req.Text) {
		req.Phase = "captured"
		return r.save()
	}
	// An invalid unrelated configuration must never erase knowledge that this
	// source already has an answer. Invalid matching proof defers, never resends.
	var prior *ProductReportConfig
	for i := range r.Config.ProductReports {
		c := &r.Config.ProductReports[i]
		if c.Source.Message == req.Message && (c.Source.Purpose == "status_question" || c.Source.AnswerMessage != "" || c.Source.AnswerReceipt != "" || c.Source.AnswerReceiptSHA != "") {
			if prior != nil {
				return errors.New("ambiguous existing status answer")
			}
			prior = c
		}
	}
	if prior != nil {
		c := *prior
		if validateProductReports([]ProductReportConfig{c}) != nil {
			return errors.New("existing status answer configuration deferred")
		}
		row, err := r.todo(c.Todo)
		if err != nil || row.Project != c.Project || !r.verifyExistingStatusAnswer(ctx, c, req) {
			return errors.New("existing status answer verification deferred")
		}
		req.Todo = c.Todo
		req.Reported = "status"
		req.Phase = "status_answered"
		return r.save()
	}
	id := "status-query-" + req.Key
	path := filepath.Join(r.Config.Home, "notices", id+".json")
	var o statusQueryObservation
	if err := readJSON(path, 1<<20, &o); err != nil {
		if !os.IsNotExist(err) {
			return errors.New("status observation unavailable")
		}
		o.At = r.Now().UTC().Format(time.RFC3339Nano)
		targets := make(map[uint64]string)
		if validateProductReports(r.Config.ProductReports) == nil {
			for _, c := range r.Config.ProductReports {
				targets[c.Todo] = c.Project
			}
		}
		for _, known := range r.State.Requests {
			if known != nil && known.Todo != 0 && known.Author == r.State.Destination.Owner && known.Source == "discord-api-v10" && known.Key == requestKey(known.Thread, known.Message) {
				if _, exists := targets[known.Todo]; !exists {
					targets[known.Todo] = r.Config.Project
				}
			}
		}
		for todo, project := range targets {
			row, err := r.todo(todo)
			if err != nil || row.Project != project {
				o.Unknown++
				continue
			}
			if todo == productRetryID && !validProductRetryProof(row.Qualification.Receipt, r.Now()) {
				o.Unknown++
				continue
			}
			s, e := r.productSnapshot(row)
			if e != nil {
				o.Unknown++
				continue
			}
			switch s.Stage {
			case "running":
				o.Running++
			case "queued", "waiting", "owner_wait":
				o.Waiting++
			case "complete":
				o.Complete++
			default:
				o.Unknown++
			}
		}
		if err := publish(path, o); err != nil {
			return err
		}
	}
	stamp, err := time.Parse(time.RFC3339Nano, o.At)
	if err != nil || stamp.After(r.Now().Add(time.Minute)) || o.Running < 0 || o.Waiting < 0 || o.Complete < 0 || o.Unknown < 0 {
		return errors.New("status observation invalid")
	}
	body := r.statusQueryBody(req, o)
	link, err := r.recoverProductNotice(ctx, id, body, req.ProgressThread)
	if errors.Is(err, errNoticeMissing) {
		bodyPath := filepath.Join(r.Config.Home, "notices", id+".txt")
		if err := writeText(bodyPath, body); err != nil {
			return err
		}
		if err := r.reserveNotice("requests"); err != nil {
			return err
		}
		args := []string{"recovery-log", "--request-id", id, "--title", "依頼の進捗", "--body-file", bodyPath}
		if req.ProgressThread != "" {
			args = append(args, "--thread", req.ProgressThread)
		}
		_, _ = r.Run(ctx, "D:/CC/Tools/discord-ask/discord_ask.exe", args...)
		link, err = r.recoverProductNotice(ctx, id, body, req.ProgressThread)
	}
	if err != nil {
		return err
	}
	req.ProgressThread = link.Thread
	req.Reported = "status"
	req.Phase = "status_answered"
	return r.save()
}
