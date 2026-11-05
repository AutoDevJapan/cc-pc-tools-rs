package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
)

var historyPage = template.Must(template.New("history").Parse(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>更新履歴 · 共有ToDo</title><link rel="stylesheet" href="/assets/app.css"></head><body><main class="history-page"><a href="/?task={{.ID}}">← タスクに戻る</a><h1>#{{.ID}} 更新履歴</h1><p>{{.Total}}件{{if .Missing}} · 未取得の保管ファイル：{{.Missing}}件{{end}}</p>{{range .Rows}}<article class="history"><time>{{.At}}</time><p>{{.Note}}</p></article>{{else}}<p>表示する履歴はありません。</p>{{end}}{{if .Next}}<a href="/tasks/{{.ID}}/history?after={{.Next}}">古い履歴を表示</a>{{end}}</main></body></html>`))

func historyEntry(b json.RawMessage) historyView {
	m := object(b)
	if m == nil {
		return historyView{Note: text(b)}
	}
	at := text(m["updated"])
	if at == "" {
		at = text(m["at"])
	}
	note := text(m["note"])
	if note == "" && len(m["note"]) > 0 && string(m["note"]) != `""` {
		note = string(m["note"])
	}
	return historyView{At: at, Note: note}
}

func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id <= 0 {
		http.Error(w, "タスク番号を確認してください", 400)
		return
	}
	item, e := s.Store.Item(id)
	if e != nil {
		http.Error(w, "タスクを取得できません", 404)
		return
	}
	m := object(item.Item)
	var history, inline, references []json.RawMessage
	_ = json.Unmarshal(m["note_history_archives"], &references)
	missing := 0
	for _, reference := range references {
		reference := object(reference)
		asset, err := s.Store.Asset(text(reference["sha256"]))
		if err != nil {
			missing++
			continue
		}
		archive := object(asset.Bytes)
		var entries []json.RawMessage
		if text(archive["domain"]) != "cc-todo-note-history-v1" || integer(archive["todo_id"]) != id || json.Unmarshal(archive["entries"], &entries) != nil {
			missing++
			continue
		}
		history = append(history, entries...)
	}
	_ = json.Unmarshal(m["note_history"], &inline)
	history = append(history, inline...)
	after := 0
	if value := r.URL.Query().Get("after"); value != "" {
		after, e = strconv.Atoi(value)
	}
	if e != nil || after < 0 || after > len(history) {
		http.Error(w, "履歴の表示位置を確認してください", 400)
		return
	}
	view := struct {
		ID             int64
		Total, Missing int
		Rows           []historyView
		Next           int
	}{ID: id, Total: len(history), Missing: missing}
	for i := len(history) - 1 - after; i >= 0 && len(view.Rows) < 100; i-- {
		view.Rows = append(view.Rows, historyEntry(history[i]))
	}
	if after+len(view.Rows) < len(history) {
		view.Next = after + len(view.Rows)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = historyPage.Execute(w, view)
}

func (s *Server) specification(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.session(r); !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id <= 0 {
		http.Error(w, "タスク番号を確認してください", 400)
		return
	}
	item, e := s.Store.Item(id)
	if e != nil {
		http.Error(w, "タスクを取得できません", 404)
		return
	}
	path := text(object(item.Item)["spec"])
	if path == "" {
		http.Error(w, "元仕様ファイルは登録されていません", 404)
		return
	}
	meta, e := s.Store.ResolveAsset(path)
	if e != nil {
		http.Error(w, "元の参照は保持しています。移行時に参照先が欠落していたか、取得範囲の外にあるファイルです。", 404)
		return
	}
	asset, e := s.Store.Asset(meta.SHA256)
	if e != nil {
		http.Error(w, "元仕様ファイルを取得できません", 503)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="specification.txt"`)
	w.Header().Set("X-Content-SHA256", asset.SHA256)
	_, _ = w.Write(asset.Bytes)
}
