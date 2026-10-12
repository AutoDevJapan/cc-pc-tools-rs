//! Render music inside the same Console document, without changing broker forms.
use regex::Regex;
use std::sync::LazyLock;

const INDEX: &str = include_str!("index.html");
const MUSIC_SLOT: &str = "<!-- console-music-view -->";

/// Authorize only compiled Console scripts, never scripts from broker/user
/// content. Preserve the broker's media, form, frame and other restrictions.
/// https://www.w3.org/TR/CSP/#match-element-to-source-list
pub fn music_script_policy(policy: &str) -> String {
    use base64::Engine;
    static HASHES: LazyLock<String> = LazyLock::new(|| {
        let scripts = Regex::new(r"(?s)<script>(.*?)</script>").unwrap();
        [
            INDEX,
            include_str!("workload.html"),
            include_str!("console_shell.html"),
        ]
        .into_iter()
        .flat_map(|html| scripts.captures_iter(html))
        .map(|capture| {
            // HTML parsing normalizes CRLF and lone CR before CSP hashing.
            let source = capture[1].replace("\r\n", "\n").replace('\r', "\n");
            let digest = ring::digest::digest(&ring::digest::SHA256, source.as_bytes());
            format!(
                "'sha256-{}'",
                base64::engine::general_purpose::STANDARD.encode(digest)
            )
        })
        .collect::<Vec<_>>()
        .join(" ")
    });
    let directives: Vec<_> = policy
        .split(';')
        .map(str::trim)
        .filter(|part| !part.is_empty())
        .collect();
    let name = |directive: &str| {
        directive
            .split_whitespace()
            .next()
            .unwrap_or("")
            .to_ascii_lowercase()
    };
    let has_default = directives
        .iter()
        .any(|directive| name(directive) == "default-src");
    if !has_default
        && !directives
            .iter()
            .any(|directive| matches!(name(directive).as_str(), "script-src" | "script-src-elem"))
    {
        return policy.to_owned();
    }
    let inherited = directives
        .iter()
        .find(|directive| name(directive) == "default-src")
        .map(|directive| {
            directive
                .split_whitespace()
                .skip(1)
                .collect::<Vec<_>>()
                .join(" ")
        })
        .unwrap_or_default();
    let mut has_scripts = false;
    let mut result = Vec::new();
    for directive in directives {
        if matches!(name(directive).as_str(), "script-src" | "script-src-elem") {
            has_scripts |= name(directive) == "script-src";
            let values = directive
                .split_whitespace()
                .skip(1)
                .filter(|value| *value != "'none'")
                .collect::<Vec<_>>()
                .join(" ");
            result.push(format!("{} {values} {}", name(directive), *HASHES));
        } else {
            result.push(directive.to_owned());
        }
    }
    if !has_scripts && has_default {
        let values = inherited
            .split_whitespace()
            .filter(|value| *value != "'none'")
            .collect::<Vec<_>>()
            .join(" ");
        result.push(format!("script-src {values} {}", *HASHES));
    }
    result.join("; ")
}

pub fn index() -> String {
    render(INDEX.replace(MUSIC_SLOT, "<section id=\"view-music\" class=\"view\" data-music-loaded=\"false\" aria-label=\"音楽制作 · Suno\"><p class=\"console-music-note\">音楽制作を選ぶと、曲の下書きと候補を読み込みます。</p></section>"))
}

fn render(html: String) -> String {
    crate::console_ui::decorate_html(&html.replace(
        "</body>",
        &format!("{}\n</body>", include_str!("workload.html")),
    ))
}

/// The broker owns content, explicit form actions, CSRF and media URLs. Only
/// its document frame is replaced. Styles stay within the music content:
/// https://developer.chrome.com/docs/css-ui/at-scope
pub fn music(html: &str) -> Option<String> {
    static BODY: LazyLock<Regex> =
        LazyLock::new(|| Regex::new(r"(?is)<body\b[^>]*>(.*)</body\s*>").unwrap());
    static HEAD: LazyLock<Regex> =
        LazyLock::new(|| Regex::new(r"(?is)<head\b[^>]*>(.*?)</head\s*>").unwrap());
    static ASSETS: LazyLock<Regex> = LazyLock::new(|| {
        Regex::new(r"(?is)<style\b([^>]*)>(.*?)</style\s*>|<script\b[^>]*>.*?</script\s*>").unwrap()
    });
    let body = BODY.captures(html)?.get(1)?.as_str();
    let mut assets = String::new();
    if let Some(head) = HEAD.captures(html).and_then(|capture| capture.get(1)) {
        for asset in ASSETS.captures_iter(head.as_str()) {
            if let Some(css) = asset.get(2) {
                assets.push_str(&format!(
                    "<style{}>@scope (.console-music-body) {{{}}}</style>",
                    &asset[1],
                    css.as_str()
                ));
            } else {
                assets.push_str(asset.get(0)?.as_str());
            }
        }
    }
    let section = format!(
        "<section id=\"view-music\" class=\"view active\" data-music-loaded=\"true\" aria-label=\"音楽制作 · Suno\">{assets}<p class=\"console-music-note\">下書き・曲候補の選択を扱います。MVと投稿は未接続です。</p><div class=\"console-music-body\">{body}</div></section>"
    );
    let html = INDEX
        .replace(
            "id=\"view-kanban\" class=\"view active operations\"",
            "id=\"view-kanban\" class=\"view operations\"",
        )
        .replace(
            "data-view=\"kanban\" class=\"active\"",
            "data-view=\"kanban\"",
        )
        .replace(
            "<a href=\"/music\" data-view=\"music\"",
            "<a href=\"/music\" data-view=\"music\" class=\"active\" aria-current=\"page\"",
        )
        .replace(
            "<h2 id=\"page-title\">Operations</h2>",
            "<h2 id=\"page-title\">音楽制作 · Suno</h2>",
        )
        .replace(
            "<p id=\"page-subtitle\">司令塔・部署・製品ごとの作業状況</p>",
            "<p id=\"page-subtitle\">曲の下書きと生成候補を選ぶ</p>",
        )
        .replace(MUSIC_SLOT, &section);
    Some(render(html))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn operations_fetch_single_flight_timeout_backoff_and_detail_paging() {
        use std::{
            io::Write,
            process::{Command, Stdio},
        };
        // Run the compiled page's real fetch functions with deterministic transport
        // and clock failures. This is a regression check, not browser acceptance.
        let script = r#"
const assert=require('node:assert/strict'),vm=require('node:vm');
const html=JSON.parse(require('node:fs').readFileSync(0,'utf8'));
let now=100000,calls=0,mode='good',pending=[],timers=new Map(),sequence=0;
const button={disabled:false},events={};
const good={fetchedAt:123,departments:[],sources:[],sourceOk:true};
const ctx={AbortController,console,Date:{now:()=>now},window:{addEventListener:(k,f)=>events[k]=f},
  $:()=>button,publishConsoleSource(){},renderOperations(){},renderOpsDetail(){},
  setTimeout:(f,ms)=>{const id=++sequence;timers.set(id,{f,at:now+ms});return id},clearTimeout:id=>timers.delete(id),
  fetch:async(path,options)=>{calls++;assert.equal(options.cache,'no-store');
    if(mode==='down')throw new TypeError('Failed to fetch');
    if(mode==='hang')return new Promise((resolve,reject)=>{pending.push(resolve);options.signal.addEventListener('abort',()=>reject(Object.assign(new Error('aborted'),{name:'AbortError'})),{once:true})});
    const body=mode==='invalid'?{departments:[]}:path.includes('/workers/')?{ok:true,id:'todo-1',detailVersion:mode==='stale'?'new':'v1',updatedAt:'same minute',note:'全文',history:[{note:'page'+new URL('http://test'+path).searchParams.get('offset')}],nextOffset:path.endsWith('offset=0')?10:null}:good;
    return {ok:true,json:async()=>body};}};
vm.createContext(ctx);
const line=prefix=>html.split('\n').find(line=>line.trim().startsWith(prefix));
const between=(a,b)=>html.slice(html.indexOf(a),html.indexOf(b,html.indexOf(a)));
vm.runInContext(line('const opsState=')+'\n'+line('async function api(')+'\n'+between('    function applyOpsProtection(', '    async function refreshOperations(')+'\n'+between('    async function refreshOperations(', '    function publishConsoleSource(')+'\n'+between('    async function loadOpsDetail(', '    function openOpsDetail(')+'\n globalThis.ops=opsState;',ctx);
const tick=async()=>{for(let i=0;i<12;i++)await Promise.resolve()};
const advance=async(ms)=>{now+=ms;for(const [id,t]of [...timers])if(t.at<=now){timers.delete(id);t.f()}await tick()};
(async()=>{
  mode='hang';const first=ctx.refreshOperations();await tick();await ctx.refreshOperations();assert.equal(calls,1);assert.equal(button.disabled,true);
  await advance(12000);await first;assert.equal(ctx.ops.loading,false);assert.equal(button.disabled,false);assert.equal(ctx.ops.snapshot,null);assert(ctx.ops.error.includes('タイムアウト'));assert.equal(ctx.ops.retryAt,now+15000);assert.equal(timers.size,0);
  mode='good';await ctx.refreshOperations();assert.equal(calls,1);await advance(15000);await ctx.refreshOperations();assert.equal(calls,2);assert.equal(ctx.ops.error,'');assert.equal(ctx.ops.failures,0);
  const last=ctx.ops.snapshot,received=ctx.ops.receivedAt;
  mode='down';await ctx.refreshOperations({force:true});assert.equal(ctx.ops.snapshot,last);assert.equal(ctx.ops.receivedAt,received);assert(ctx.ops.error.includes('最後に取得'));
  for(const wait of [30000,60000,60000]){await ctx.refreshOperations({force:true});assert.equal(ctx.ops.retryAt,now+wait)}
  mode='invalid';await ctx.refreshOperations({force:true});assert.equal(ctx.ops.snapshot,last);assert(ctx.ops.error.includes('形式'));
  mode='good';await ctx.refreshOperations({force:true});assert.equal(ctx.ops.error,'');assert.equal(ctx.ops.retryAt,0);
  mode='hang';const hidden=ctx.refreshOperations();await tick();events.pagehide();await hidden;assert.equal(ctx.ops.loading,false);assert.equal(timers.size,0);
  mode='good';await ctx.refreshOperations({force:true});assert.equal(ctx.ops.error,'');
  const worker={id:'todo-1',updatedAt:'same minute',detailVersion:'v1',detailAvailable:true};
  await ctx.loadOpsDetail(worker);assert.equal(ctx.ops.details.get(worker.id).data.history.length,1);
  await ctx.loadOpsDetail(worker,10);assert.equal(ctx.ops.details.get(worker.id).data.history.length,2);assert.equal(ctx.ops.details.get(worker.id).data.nextOffset,null);
  mode='down';await ctx.loadOpsDetail(worker,10);assert.equal(ctx.ops.details.get(worker.id).data.history.length,2);assert(ctx.ops.details.get(worker.id).error);
  mode='stale';await ctx.loadOpsDetail(worker);assert.equal(ctx.ops.details.get(worker.id).data,null);assert(ctx.ops.details.get(worker.id).error.includes('更新'));
  mode='hang';const detail=ctx.loadOpsDetail(worker);await tick();const before=calls;await ctx.loadOpsDetail(worker);assert.equal(calls,before);await advance(12000);await detail;assert.equal(ctx.ops.details.get(worker.id).loading,false);assert.equal(timers.size,0);
  assert(html.includes('setInterval(()=>{refresh();refreshOperations()},15000)'));
  assert(html.includes('productChoices.get(productKey):filtering'));
  console.log('operations transport regression passed');
})().catch(error=>{console.error(error);process.exitCode=1});
"#;
        let mut command = Command::new("node");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command
            .args(["-e", script])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        child
            .stdin
            .take()
            .unwrap()
            .write_all(serde_json::to_string(INDEX).unwrap().as_bytes())
            .unwrap();
        let output = child.wait_with_output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }

    #[test]
    fn music_shares_real_views_and_keeps_broker_controls_verbatim() {
        let controls = "<main><form action='/music/project/import' method='post' enctype='multipart/form-data'><input name='csrf' value='fixture'><input type='file' name='audio'></form><audio controls src='/music/project/audio/clip'></audio><a href='/music/project'>履歴</a><script>/* existing broker script */</script></main>";
        let document = format!(
            "<!doctype html><html><head><style>button{{color:red}}@media(max-width:560px){{.row{{display:block}}}}</style></head><BoDy>{controls}</BoDy></html>"
        );
        let page = music(&document).unwrap();
        assert!(page.contains(controls));
        assert!(page.contains("@scope (.console-music-body) {button{color:red}@media(max-width:560px){.row{display:block}}}"));
        assert!(page.contains("id=\"view-music\" class=\"view active\""));
        for view in ["kanban", "chat", "minimax", "workload", "systems"] {
            assert_eq!(page.matches(&format!("id=\"view-{view}\"")).count(), 1);
        }
        assert_eq!(page.matches("<aside class=\"sidebar\">").count(), 1);
        assert_eq!(page.matches("id=\"page-title\"").count(), 1);
        assert_eq!(page.matches("id=\"console-shell-style\"").count(), 1);
        assert!(!page.contains("<iframe"));
        assert!(page.contains("id=\"view-kanban\" class=\"view operations\""));
    }

    #[test]
    fn normal_console_does_not_activate_music_or_move_questions() {
        let page = index();
        assert!(page.contains("id=\"view-music\" class=\"view\" data-music-loaded=\"false\""));
        assert!(page.contains("id=\"view-kanban\" class=\"view active operations\""));
        assert!(
            page.find("id=\"ops-commander\"").unwrap()
                < page.find("id=\"ops-departments\"").unwrap()
        );
        assert!(
            page.find("id=\"ops-departments\"").unwrap()
                < page.find("id=\"ops-questions\"").unwrap()
        );
        assert!(page.contains("<details class=\"ops-sources\">"));
        assert!(music("not an HTML document").is_none());
    }

    #[test]
    fn music_policy_preserves_other_restrictions_and_does_not_authorize_unknown_scripts() {
        use base64::Engine;
        let original = "default-src 'self'; style-src 'self' 'unsafe-inline'; media-src 'self' https://*.suno.com https://*.suno.ai; form-action 'self'; frame-ancestors 'none'; base-uri 'none'";
        let policy = music_script_policy(original);
        for directive in original.split(';').map(str::trim) {
            assert!(policy
                .split(';')
                .map(str::trim)
                .any(|part| part == directive));
        }
        let source = "untrusted_fixture()";
        let digest = ring::digest::digest(&ring::digest::SHA256, source.as_bytes());
        assert!(!policy.contains(&base64::engine::general_purpose::STANDARD.encode(digest)));
        assert!(policy.contains("script-src 'self' 'sha256-"));
        assert!(!policy.contains("script-src 'unsafe-inline'"));
        let strict = music_script_policy(
            "default-src 'none'; script-src 'none'; script-src-elem 'none'; script-src-attr 'none'",
        );
        assert!(strict.contains("script-src-elem  'sha256-"));
        assert!(strict.contains("script-src-attr 'none'"));
        assert!(strict.contains("default-src 'none'"));
        assert_eq!(
            music_script_policy("frame-ancestors 'none'; script-src-attr 'none'"),
            "frame-ancestors 'none'; script-src-attr 'none'"
        );
        assert!(!music_script_policy("script-src-elem 'none'").contains("script-src "));
    }

    #[test]
    fn shared_navigation_restores_music_on_back_forward_and_reload() {
        use std::io::Write;
        use std::process::{Command, Stdio};
        // Execute the existing browser functions with fixture DOM/history. This
        // checks routing behavior; it is not a substitute for visual acceptance.
        let script = r#"
const assert = require('node:assert/strict');
const vm = require('node:vm');
const input = JSON.parse(require('node:fs').readFileSync(0, 'utf8'));
for (const source of input.scripts) new vm.Script(source);
async function loaderScenario() {
  const host={dataset:{musicLoaded:'false'},childNodes:[],replaceChildren(...nodes){this.childNodes=nodes},append(...nodes){this.childNodes.push(...nodes)}};
  let requests=0,reply;
  const ctx={window:{},q:()=>host,make:(tag,cls,text)=>({tag,cls,text}),document:{importNode:node=>({...node})},AbortController,setTimeout,clearTimeout,
    DOMParser:class {parseFromString(text){return {querySelector:()=>text==='valid'?{childNodes:[{tag:'FORM',draft:''},{tag:'AUDIO',currentTime:0}]}:null}}},
    fetch:()=>{requests++;return new Promise(resolve=>reply=resolve)},
  };
  vm.createContext(ctx);vm.runInContext(input.loader,ctx);
  const first=ctx.window.loadConsoleMusic(),second=ctx.window.loadConsoleMusic();
  assert.equal(first,second);assert.equal(requests,1);
  reply({ok:true,text:async()=>'valid'});await first;
  assert.equal(host.dataset.musicLoaded,'true');assert.equal(host.childNodes[0].tag,'FORM');
  const form=host.childNodes[0],audio=host.childNodes[1];form.draft='unfinished lyrics';audio.currentTime=17;
  await ctx.window.loadConsoleMusic();assert.equal(requests,1);assert.equal(host.childNodes[0],form);assert.equal(audio.currentTime,17);
  host.dataset.musicLoaded='false';const broken=ctx.window.loadConsoleMusic();reply({ok:false,text:async()=>'invalid'});await broken;
  assert.equal(host.childNodes[1].tag,'button');assert.equal(typeof host.childNodes[1].onclick,'function');
  const retry=host.childNodes[1].onclick();reply({ok:true,text:async()=>'valid'});await retry;
  assert.equal(host.dataset.musicLoaded,'true');assert.equal(requests,3);
}
for (const source of input.scripts) {
  const hash=require('node:crypto').createHash('sha256').update(source.replace(/\r\n?/g,'\n')).digest('base64');
  assert(input.policy.includes("'sha256-"+hash+"'"),'Every compiled Console script must be allowed by the actual music policy');
}
function scenario(hasMusic,fromRoot=false) {
  const views = ['kanban','chat','minimax','workload','systems',...(hasMusic?['music']:[])];
  const node = view => ({id:'view-'+view,dataset:{view},active:false,attrs:{},classList:{toggle(_,on){this.owner.active=on}},setAttribute(k,v){this.attrs[k]=v},removeAttribute(k){delete this.attrs[k]},focus(){this.focused=true}});
  const panels=views.map(node),links=views.map(node); [...panels,...links].forEach(n=>n.classList.owner=n);
  const title=node('title'),subtitle=node('subtitle'),listeners={},stack=[];
  let refreshes=0;
  const ctx={URL,location:{href:hasMusic&&!fromRoot?'http://localhost:10012/music/project':'http://localhost:10012/'},
    $:selector=>selector==='#page-title'?title:selector==='#page-subtitle'?subtitle:panels.find(n=>'#'+n.id===selector),
    $$:selector=>selector==='.view'?panels:links,
    refreshOperations(){refreshes++},refresh(){},CustomEvent:class {constructor(type,options){this.type=type;this.detail=options.detail}},
    window:{dispatchEvent(){},addEventListener(name,fn){listeners[name]=fn}},
  };
  ctx.history={pushState(_a,_b,url){stack.push(ctx.location.href);ctx.location.href=String(url)},replaceState(_a,_b,url){ctx.location.href=String(url)}};
  vm.createContext(ctx); vm.runInContext(input.switcher+'\n'+input.popstate+'\n'+input.boot,ctx);
  const active=()=>panels.filter(n=>n.active).map(n=>n.dataset.view);
  assert.deepEqual(active(),[hasMusic&&!fromRoot?'music':'kanban']);
  if(!hasMusic){ctx.switchView('music');assert.deepEqual(active(),['kanban']);return}
  if(fromRoot){ctx.switchView('music');assert.deepEqual(active(),['music']);assert.equal(new URL(ctx.location.href).pathname,'/');assert.equal(new URL(ctx.location.href).searchParams.get('view'),'music')}
  assert.equal(title.textContent,'音楽制作 · Suno');
  assert.equal(links.find(n=>n.dataset.view==='music').attrs['aria-current'],'page');
  ctx.switchView('systems',{focus:true});assert.deepEqual(active(),['systems']);assert.equal(title.focused,true);
  assert.equal(new URL(ctx.location.href).searchParams.get('view'),'systems');
  const forward=ctx.location.href;ctx.location.href=stack.pop();listeners.popstate();assert.deepEqual(active(),['music']);
  ctx.location.href=forward;listeners.popstate();assert.deepEqual(active(),['systems']);
  vm.runInContext(input.boot,ctx);assert.deepEqual(active(),['systems']);
  ctx.switchView('kanban');assert.deepEqual(active(),['kanban']);assert.equal(refreshes,fromRoot?2:1);
  ctx.switchView('music');assert.deepEqual(active(),['music']);assert.equal(links.find(n=>n.dataset.view==='music').attrs['aria-current'],'page');
}
scenario(true);scenario(false);scenario(true,true);
loaderScenario().then(()=>console.log('music routing regression passed')).catch(error=>{console.error(error);process.exitCode=1});
"#;
        let source_line = |prefix: &str| {
            INDEX
                .lines()
                .find(|line| line.trim_start().starts_with(prefix))
                .unwrap()
        };
        let page = music("<html><body><main>fixture</main></body></html>").unwrap();
        let scripts: Vec<_> = Regex::new(r"(?s)<script>(.*?)</script>")
            .unwrap()
            .captures_iter(&page)
            .map(|capture| capture[1].to_owned())
            .collect();
        let input = serde_json::json!({
            "switcher":source_line("function switchView("),
            "popstate":source_line("window.addEventListener('popstate'"),
            "boot":source_line("switchView(new URL(location.href)"),
            "scripts":scripts,
            "policy":music_script_policy("default-src 'self'; form-action 'self'"),
            "loader":include_str!("console_shell.html").split("    let musicLoading;").nth(1).unwrap().split("    const names=").next().map(|source| format!("let musicLoading;{source}")).unwrap(),
        });
        let mut command = Command::new("node");
        #[cfg(windows)]
        {
            use std::os::windows::process::CommandExt;
            command.creation_flags(0x08000000);
        }
        let mut child = command
            .args(["-e", script])
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .unwrap();
        child
            .stdin
            .take()
            .unwrap()
            .write_all(input.to_string().as_bytes())
            .unwrap();
        let output = child.wait_with_output().unwrap();
        assert!(
            output.status.success(),
            "{}",
            String::from_utf8_lossy(&output.stderr)
        );
    }
}
