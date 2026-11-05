package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"autodevjapan/shared-todo-cloud/internal/store"
)

type rowView struct {
	ID                                                  int64
	Title, Project, Status, Progress, Department, Agent string
}
type detailView struct {
	ID                                                          int64
	Title, Project, Status, Note, Progress, Agent, Dependencies string
	History                                                     []historyView
	Fingerprint                                                 string
}
type historyView struct{ At, Note string }
type pageView struct {
	Title, CSRF, RequestID, Project, Department, Status, Query, Notice, Error string
	AllocationAt                                                              string
	Revision                                                                  uint64
	Rows                                                                      []rowView
	Detail                                                                    *detailView
	Projects                                                                  []string
	Departments                                                               []string
	NextURL, PreviousURL                                                      string
	New                                                                       bool
}

var page = template.Must(template.New("page").Funcs(template.FuncMap{"statusOptions": options}).Parse(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>共有ToDo</title><link rel="stylesheet" href="/assets/app.css"></head><body><header><strong>共有ToDo</strong><form method="post" action="/logout"><input type="hidden" name="csrf" value="{{.CSRF}}"><button class="quiet">サインアウト</button></form></header><main><aside><a class="home" href="/">すべてのタスク</a><h2>部署</h2>{{range .Departments}}<a href="/?department={{urlquery .}}">{{.}}</a>{{end}}<details><summary>プロジェクト</summary>{{range .Projects}}<a href="/?project={{urlquery .}}">{{.}}</a>{{end}}</details><a href="/?status=done">完了したタスク</a></aside><section class="workspace"><div class="toolbar"><form method="get" class="filters"><input name="q" value="{{.Query}}" placeholder="番号・タスク名を検索" aria-label="タスクを検索"><input type="hidden" name="project" value="{{.Project}}"><input type="hidden" name="department" value="{{.Department}}"><select name="status" aria-label="状態"><option value="">状態：未完了</option><option value="all" {{if eq .Status "all"}}selected{{end}}>完了を含むすべて</option>{{range $key,$label := statusOptions}}<option value="{{$key}}" {{if eq $.Status $key}}selected{{end}}>{{$label}}</option>{{end}}</select><button>検索</button></form><a class="primary" href="/?new=1">＋ タスクを追加</a></div><p class="muted">配属情報の更新：{{.AllocationAt}}</p>{{if .Notice}}<p class="notice">{{.Notice}}</p>{{end}}{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}<div class="columns"><div class="table-wrap"><table><thead><tr><th>番号</th><th>タスク</th><th>プロジェクト</th><th>担当部署・Agent</th><th>状態</th><th>進捗</th></tr></thead><tbody>{{range .Rows}}<tr><td><a href="/?task={{.ID}}">{{.ID}}</a></td><td><a href="/?task={{.ID}}">{{.Title}}</a></td><td>{{.Project}}</td><td><span>{{.Department}}</span><small class="agent-name">{{.Agent}}</small></td><td><span class="badge">{{.Status}}</span></td><td>{{.Progress}}</td></tr>{{else}}<tr><td colspan="6" class="empty">条件に一致するタスクはありません。</td></tr>{{end}}</tbody></table><nav class="pager">{{if .PreviousURL}}<a href="{{.PreviousURL}}">先頭へ</a>{{end}}{{if .NextURL}}<a href="{{.NextURL}}">次のページ</a>{{end}}</nav></div>{{if or .Detail .New}}<section class="detail"><h1>{{if .New}}タスクを追加{{else}}#{{.Detail.ID}} {{.Detail.Title}}{{end}}</h1><form method="post" action="/tasks"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="request_id" value="{{.RequestID}}"><input type="hidden" name="revision" value="{{.Revision}}"><input type="hidden" name="item_fingerprint" value="{{if .Detail}}{{.Detail.Fingerprint}}{{end}}"><input type="hidden" name="id" value="{{if .Detail}}{{.Detail.ID}}{{end}}"><label>タスク名<input required name="title" value="{{if .Detail}}{{.Detail.Title}}{{end}}"></label>{{if .New}}<label>プロジェクト<select required name="project">{{range .Projects}}<option>{{.}}</option>{{end}}</select></label>{{else}}<p class="muted">{{.Detail.Project}}</p><label>担当Agent<input readonly value="{{.Detail.Agent}}"></label><label>状態<select name="status">{{if not (index (statusOptions) .Detail.Status)}}<option value="{{.Detail.Status}}" selected>{{.Detail.Status}}</option>{{end}}{{range $key,$label := statusOptions}}<option value="{{$key}}" {{if eq $.Detail.Status $key}}selected{{end}}>{{$label}}</option>{{end}}</select></label><label>進捗（確認した値）<input name="progress" type="number" min="0" max="100" value="{{.Detail.Progress}}"></label><p class="muted">依存先：{{.Detail.Dependencies}}</p>{{end}}<label>説明・作業メモ<textarea name="note" rows="10">{{if .Detail}}{{.Detail.Note}}{{end}}</textarea></label><button class="primary">保存</button></form>{{if .Detail}}<nav class="detail-links"><a href="/tasks/{{.Detail.ID}}/history">すべての更新履歴</a> <a href="/tasks/{{.Detail.ID}}/spec">元仕様ファイル</a></nav><h2>最近の更新履歴</h2>{{range .Detail.History}}<article class="history"><time>{{.At}}</time><p>{{.Note}}</p></article>{{else}}<p class="muted">表示する履歴はありません。</p>{{end}}{{end}}</section>{{end}}</div></section></main></body></html>`))

var loginPage = template.Must(template.New("login").Parse(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>共有ToDoにサインイン</title><link rel="stylesheet" href="/assets/app.css"></head><body><main class="login"><h1>共有ToDo</h1><p>社内作業の管理画面です。</p>{{if .}}<p class="error">{{.}}</p>{{end}}<form method="post" action="/login"><label>管理者パスワード<input name="token" type="password" autocomplete="current-password" required></label><button class="primary">サインイン</button></form><p><a href="/connect">登録済みのPCから接続する</a></p></main></body></html>`))

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = loginPage.Execute(w, "")
}
func (s *Server) signin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if r.ParseForm() != nil || !s.validOrigin(r) || !sameSecret(r.Form.Get("token"), s.Config.AdminToken) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(401)
		_ = loginPage.Execute(w, "サインイン情報を確認してください。")
		return
	}
	s.setSession(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (s *Server) signout(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(r)
	if !ok || r.ParseForm() != nil || !s.validOrigin(r) || !sameSecret(r.Form.Get("csrf"), s.csrf(session)) {
		http.Error(w, "操作を確認できません", 403)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "shared-todo-session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.Config.SecureCookies, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, "/login", 303)
}
func options() map[string]string {
	return map[string]string{"todo": "未着手", "doing": "作業中", "block": "判断待ち", "wait": "依存・再開待ち", "recovery_unverified": "復旧確認中", "done": "完了"}
}
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(r)
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	p := pageView{CSRF: s.csrf(session), RequestID: nonce(), Project: r.URL.Query().Get("project"), Department: r.URL.Query().Get("department"), Status: r.URL.Query().Get("status"), Query: r.URL.Query().Get("q"), Notice: r.URL.Query().Get("notice"), New: r.URL.Query().Get("new") == "1"}
	metaRev, meta, e := s.Store.Metadata()
	if e != nil {
		http.Error(w, "保存データを取得できません", 503)
		return
	}
	p.Revision = metaRev
	root := object(meta)
	projects := object(root["projects"])
	for k := range projects {
		p.Projects = append(p.Projects, k)
	}
	sort.Strings(p.Projects)
	assignments := s.allocations()
	p.Departments = s.departmentNames(assignments)
	p.AllocationAt = "未同期"
	if registry, err := s.Store.Registry("task-allocations"); err == nil {
		var info struct {
			GeneratedAt string `json:"generatedAt"`
		}
		if json.Unmarshal(registry.Document, &info) == nil && info.GeneratedAt != "" {
			p.AllocationAt = info.GeneratedAt
		}
	}
	filterStatus := p.Status
	if filterStatus == "all" {
		filterStatus = ""
	}
	list, e := s.departmentList(store.ListOptions{Status: filterStatus, OpenOnly: p.Status == "", Project: p.Project, Search: p.Query, After: num(r, "after", 0), Limit: 100}, p.Department, assignments)
	if e != nil {
		http.Error(w, "タスクを取得できません", 503)
		return
	}
	p.Revision = list.Revision
	for _, v := range list.Items {
		title := text(v.Title)
		progress := string(v.Progress)
		if progress == "" || progress == "null" {
			progress = "—"
		} else {
			progress += "%"
		}
		p.Rows = append(p.Rows, rowView{ID: v.ID, Title: title, Project: text(v.Project), Status: statusLabel(text(v.Status)), Progress: progress, Department: assignments[v.ID].Department, Agent: allocationLabel(assignments[v.ID])})
	}
	if list.HasMore {
		q := r.URL.Query()
		q.Set("after", formatID(list.NextAfter))
		p.NextURL = "/?" + q.Encode()
	}
	if num(r, "after", 0) > 0 {
		q := r.URL.Query()
		q.Del("after")
		p.PreviousURL = "/?" + q.Encode()
	}
	if id := num(r, "task", 0); id > 0 {
		v, e := s.Store.Item(id)
		if e != nil {
			p.Error = "対象のタスクを取得できません。"
		} else {
			p.Revision = v.Revision
			m := object(v.Item)
			d := &detailView{ID: id, Title: text(m["title"]), Project: text(m["project"]), Status: text(m["status"]), Note: text(m["note"]), Agent: text(m["agent"])}
			d.Fingerprint = itemFingerprint(v.Item)
			if a, found := assignments[id]; found {
				d.Agent = allocationLabel(a)
			}
			if d.Agent == "" {
				d.Agent = "担当の照合中"
			}
			if b := m["progress"]; len(b) > 0 && string(b) != "null" {
				if n, err := strconv.Atoi(string(b)); err == nil && n >= 0 && n <= 100 {
					d.Progress = strconv.Itoa(n)
				}
			}
			var deps []int64
			_ = json.Unmarshal(m["wait_on"], &deps)
			var dd []string
			for _, dep := range deps {
				dd = append(dd, formatID(dep))
			}
			d.Dependencies = strings.Join(dd, ", ")
			if d.Dependencies == "" {
				d.Dependencies = "未登録"
			}
			var hist []json.RawMessage
			_ = json.Unmarshal(m["note_history"], &hist)
			if len(hist) > 10 {
				hist = hist[len(hist)-10:]
			}
			for i := len(hist) - 1; i >= 0; i-- {
				d.History = append(d.History, historyEntry(hist[i]))
			}
			p.Detail = d
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = page.Execute(w, p)
}
func (s *Server) saveTask(w http.ResponseWriter, r *http.Request) {
	session, ok := s.session(r)
	if !ok {
		http.Error(w, "サインインが必要です", 401)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	if r.ParseForm() != nil || !s.validOrigin(r) || !sameSecret(r.Form.Get("csrf"), s.csrf(session)) {
		http.Error(w, "操作を確認できません", 403)
		return
	}
	rev, e := parseRevision(r.Form.Get("revision"))
	if e != nil {
		http.Error(w, "更新元の版を確認してください", 400)
		return
	}
	requestID := r.Form.Get("request_id")
	if requestID == "" {
		http.Error(w, "操作の識別情報が必要です", 400)
		return
	}
	title := strings.TrimSpace(r.Form.Get("title"))
	note := r.Form.Get("note")
	if title == "" {
		http.Error(w, "タスク名を入力してください", 400)
		return
	}
	id, _ := strconv.ParseInt(r.Form.Get("id"), 10, 64)
	if receipt, err := s.Store.Operation(requestID); err == nil {
		if len(receipt.CreatedIDs) > 0 {
			id = receipt.CreatedIDs[0]
		}
		http.Redirect(w, r, "/?task="+formatID(id)+"&notice="+url.QueryEscape("この操作は保存済みです。追加の変更は内容を確認してから保存してください。"), 303)
		return
	}
	req := store.UpdateRequest{RequestID: requestID, BaseRevision: rev}
	if id == 0 {
		project := r.Form.Get("project")
		currentRevision, meta, e := s.Store.Metadata()
		if e != nil {
			http.Error(w, "登録先を確認できません", 503)
			return
		}
		if _, ok := object(object(meta)["projects"])[project]; !ok {
			http.Error(w, "登録済みプロジェクトを選択してください", 400)
			return
		}
		req.Creates = []json.RawMessage{buildNewItem(project, title, note, s.now())}
		req.BaseRevision = currentRevision
	} else {
		snap, e := s.Store.Snapshot()
		if e != nil {
			http.Error(w, "更新元を取得できません", 503)
			return
		}
		if snap.Revision != rev {
			var current struct {
				Items []json.RawMessage `json:"items"`
			}
			_ = json.Unmarshal(snap.Document, &current)
			matches := false
			for _, row := range current.Items {
				if integer(object(row)["id"]) == id {
					matches = r.Form.Get("item_fingerprint") != "" && sameSecret(r.Form.Get("item_fingerprint"), itemFingerprint(row))
					break
				}
			}
			if !matches {
				s.draftError(w, r, "このタスクにほかの更新が入りました。入力した内容を残しています。タスクを開き直して最新の内容と照合してください。", 409)
				return
			}
			req.BaseRevision = snap.Revision
		}
		var percent *int
		if p := r.Form.Get("progress"); p != "" {
			n, e := strconv.Atoi(p)
			if e != nil {
				http.Error(w, "進捗を確認してください", 400)
				return
			}
			var original json.Number
			var parsed struct {
				Items []json.RawMessage `json:"items"`
			}
			_ = json.Unmarshal(snap.Document, &parsed)
			for _, row := range parsed.Items {
				m := object(row)
				if integer(m["id"]) == id {
					_ = json.Unmarshal(m["progress"], &original)
					break
				}
			}
			old, oldErr := strconv.ParseFloat(string(original), 64)
			if oldErr != nil || old != float64(n) {
				percent = &n
			}
		}
		req.Upserts, e = editDocument(snap.Document, id, title, note, r.Form.Get("status"), percent, s.now())
		if e != nil {
			http.Error(w, e.Error(), 409)
			return
		}
		if len(req.Upserts) == 0 {
			http.Redirect(w, r, "/?task="+formatID(id)+"&notice="+url.QueryEscape("変更はありません"), 303)
			return
		}
	}
	v, e := s.Store.Update(req)
	if e != nil {
		var code int = 500
		message := "保存できませんでした。入力した内容を残しています。最新のタスクを開いて確認してください。"
		var se *store.Error
		if errors.As(e, &se) {
			if se.Code == store.CodeConflict {
				code = 409
			} else if se.Code == store.CodeLocked {
				code = 423
			} else if se.Code == store.CodeInvalid && se.Reason == "document_too_large" {
				code = 413
				message = "PC連携の保存サイズ上限に達するため、履歴を保存した整理が必要です。入力した内容は残しています。"
			}
		}
		s.draftError(w, r, message, code)
		return
	}
	if len(v.CreatedIDs) > 0 {
		id = v.CreatedIDs[0]
	}
	http.Redirect(w, r, "/?task="+formatID(id)+"&notice="+url.QueryEscape("保存しました"), 303)
}
func itemFingerprint(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

var draftPage = template.Must(template.New("draft").Parse(`<!doctype html><html lang="ja"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>入力内容 · 共有ToDo</title><link rel="stylesheet" href="/assets/app.css"></head><body><main class="history-page"><p class="error">{{.Message}}</p><a href="/?task={{.ID}}">最新のタスクを開く</a><h1>入力した内容</h1><label>タスク名<input readonly value="{{.Title}}"></label><label>説明・作業メモ<textarea readonly rows="16">{{.Note}}</textarea></label><p>状態：{{.Status}} · 進捗：{{.Progress}}</p></main></body></html>`))

func (s *Server) draftError(w http.ResponseWriter, r *http.Request, message string, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = draftPage.Execute(w, struct{ Message, ID, Title, Note, Status, Progress string }{message, r.Form.Get("id"), r.Form.Get("title"), r.Form.Get("note"), statusLabel(r.Form.Get("status")), r.Form.Get("progress")})
}
func (s *Server) css(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(appCSS))
}

const appCSS = `.history-page{display:block;max-width:1000px;margin:30px auto;padding:25px;background:white}.detail-links{display:flex;gap:15px;margin-top:20px;font-size:14px}.detail-links a{color:#1268ae}.agent-name{display:block;color:#637181;font-size:12px;min-width:150px;margin-top:6px}aside summary{padding:12px;cursor:pointer}:root{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;color:#17202a;background:#f7f8fa;font-size:15px}*{box-sizing:border-box}body{margin:0}header{height:78px;border-bottom:1px solid #dce1e6;background:white;display:flex;align-items:center;justify-content:space-between;padding:0 28px}header strong{font-size:30px}main{display:flex;min-height:calc(100vh - 78px)}aside{width:220px;flex-shrink:0;border-right:1px solid #dce1e6;background:#fcfcfd;padding:20px 14px;overflow-wrap:anywhere}aside a{display:block;padding:10px 12px;margin-bottom:4px;border-radius:5px;text-decoration:none;color:#253444}aside a:hover,.home{background:#e3effb}h2{font-size:16px}.workspace{flex:1;min-width:0;padding:22px}.toolbar{display:flex;gap:16px;align-items:center;justify-content:space-between;margin-bottom:20px}.filters{display:flex;gap:10px;flex-wrap:wrap}.filters input{width:min(420px,50vw)}input,select,textarea,button,.primary{font:inherit;min-height:42px;border:1px solid #bac4cf;border-radius:5px;padding:9px 12px;background:white;color:inherit}textarea{resize:vertical;line-height:1.7}button,.primary{cursor:pointer}.primary{display:inline-block;background:#1268ae;color:white;border-color:#1268ae;text-decoration:none;white-space:nowrap}.quiet{background:none;border-color:#dce1e6}.columns{display:flex;gap:18px;align-items:flex-start}.table-wrap{flex:1;min-width:0;overflow:auto;border:1px solid #dce1e6;background:white;border-radius:6px}table{border-collapse:collapse;width:100%;min-width:650px}th,td{text-align:left;padding:13px 14px;border-bottom:1px solid #e2e6eb;vertical-align:top}th{background:#f3f5f7;font-size:14px}td a{color:inherit;text-decoration:none}tr:hover td{background:#eef5fc}.badge{display:inline-block;padding:3px 7px;border-radius:4px;background:#e7eef5;white-space:nowrap;font-size:13px}.detail{width:390px;flex-shrink:0;background:white;padding:20px;border:1px solid #dce1e6;border-radius:6px;overflow-wrap:anywhere}.detail h1{font-size:21px;margin:0 0 20px}.detail form{display:grid;gap:15px}label{display:grid;gap:7px;font-weight:600}label input,label select,label textarea{width:100%;font-weight:400}.muted{color:#637181;font-size:13px}.history{border-top:1px solid #e4e8ed;padding:12px 0;font-size:13px;white-space:pre-wrap}.history time{color:#637181}.pager{display:flex;gap:20px;padding:15px}.pager a{color:#1268ae}.notice,.error{padding:12px 15px;border:1px solid #afc7df;background:#edf6ff;border-radius:5px}.error{color:#942b35;border-color:#e6b4b9;background:#fff2f3}.empty{text-align:center;color:#637181}.login{max-width:460px;display:block;margin:10vh auto;padding:30px;background:white;border:1px solid #dce1e6;border-radius:8px;min-height:0}.login form{display:grid;gap:20px}.login h1{font-size:30px}button:focus-visible,a:focus-visible,input:focus-visible,select:focus-visible,textarea:focus-visible{outline:3px solid #64a9e6;outline-offset:2px}@media(max-width:1100px){.columns{flex-direction:column}.detail{width:100%}}@media(max-width:720px){header{padding:0 15px;height:65px}header strong{font-size:25px}main{display:block}aside{width:100%;display:flex;overflow:auto;border-right:0;border-bottom:1px solid #dce1e6;padding:8px}aside a{white-space:nowrap;font-size:13px}aside h2{display:none}.workspace{padding:14px}.toolbar{align-items:stretch;flex-direction:column}.filters input{width:100%}.filters{display:grid;grid-template-columns:1fr}.login{margin:5vh 15px}.detail{padding:15px}}`
