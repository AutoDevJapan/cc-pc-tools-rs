// ChatGPT のブラウザ版の画面を読む・操作する処理。
// Rust 側で `(() => { <このファイル>; return <呼び出し>; })()` に包んで評価する。
// 画面の作りが変わったときは、ここと tests/fixtures/ を合わせて直す。
// 2026-10 の画面（tests/fixtures/real-*.html に記録）と、それ以前の画面（data-testid 中心）の両方を扱う。

const COMPOSER = 'textarea[name="prompt"], #prompt-textarea';
const SEND = 'button[aria-label="Send message"], button[aria-label="Send prompt"], [data-testid="send-button"], #composer-submit-button';
const STOP = 'button[aria-label="Stop generating"], button[aria-label="Stop streaming"], [data-testid="stop-button"]';
const LOGIN = '[data-mobile-auth-entry-action="login"], [data-login-button], [data-testid="login-button"]';
const USER_MSG = '[data-message-role="user"], [data-message-author-role="user"]';
const ASSISTANT_MSG = '[data-message-role="assistant"], [data-message-author-role="assistant"]';
const ANSWER_BODY = '[data-assistant-markdown], .markdown';
const ERROR_RE = /something went wrong|error (?:occurred )?(?:while )?generating|network error|問題が発生しました|エラーが発生しました/i;
const LIMIT_RE = /(?:reached|hit) (?:your|our|the) (?:\w+ )*limit|usage (?:cap|limit)|上限に達しました/i;
const BLOCKED_RE = /account (?:has been |was )?(?:deactivated|suspended|disabled|banned)|deleted or deactivated|アカウント(?:が|は)(?:停止|無効|削除)/i;

function q(s) { return document.querySelector(s); }

function all(s) { return Array.from(document.querySelectorAll(s)); }

// 閉じたダイアログや hidden の中にある要素は画面に出ていないので数えない
function shown(e) { return !e.closest('[hidden], dialog:not([open]), [aria-hidden="true"]'); }

function bodyText() { return document.body ? document.body.innerText : ''; }

function state() {
  if (/just a moment|attention required/i.test(document.title)
      || q('#challenge-form, iframe[src*="challenges.cloudflare.com"]')) return 'challenge';
  if (all(LOGIN).some(shown)) return 'logged_out';
  if (q(COMPOSER)) return 'logged_in';
  if (BLOCKED_RE.test(bodyText())) return 'blocked';
  if (/\/auth\/login|auth\.openai\.com|\/log-in/.test(location.href)) return 'logged_out';
  return document.readyState === 'complete' ? 'unknown' : 'loading';
}

// 依頼文・回答（考え中の要約を含む）・サイドバー・隠れた部分を除いた画面の文字に、
// 会話の中のエラー表示欄の文字を足したもの（エラー表示・上限表示の判定用）
function chromeText() {
  const root = (q('main') || document.body).cloneNode(true);
  root.querySelectorAll('aside, nav, dialog, [hidden], [data-message-role], [data-message-author-role]').forEach(n => n.remove());
  const notes = all('[data-message-intervention]').map(n => n.textContent).join('\n');
  return (root.textContent || '') + '\n' + notes;
}

function lastAnswer() {
  const a = all(ASSISTANT_MSG);
  return a[a.length - 1] || null;
}

function poll() {
  const last = lastAnswer();
  const rest = chromeText();
  // 2026-10 の画面は回答が終わると data-message-complete が付く
  const unfinished = !!last && last.hasAttribute('data-message-role') && !last.hasAttribute('data-message-complete');
  return {
    state: state(),
    users: all(USER_MSG).length,
    assistants: all(ASSISTANT_MSG).length,
    streaming: !!q(STOP) || unfinished,
    len: last ? last.innerText.trim().length : 0,
    error: ERROR_RE.test(rest),
    limit: LIMIT_RE.test(rest),
  };
}

function composerLength() {
  const b = q(COMPOSER);
  if (!b) return -1;
  return ('value' in b ? b.value : b.innerText).trim().length;
}

function focusComposer() {
  const b = q(COMPOSER);
  if (!b) return false;
  b.focus();
  return true;
}

// ProseMirror（以前の画面）は貼り付けを改行ごと扱えるので、まず貼り付けイベントで入れる。
// textarea は合成した貼り付けでは文字が入らないので、Rust 側が文字入力で入れ直す。
function paste(text) {
  const b = q(COMPOSER);
  if (!b) return -1;
  b.focus();
  if (b.tagName === 'TEXTAREA') return 0;
  const dt = new DataTransfer();
  dt.setData('text/plain', text);
  b.dispatchEvent(new ClipboardEvent('paste', { clipboardData: dt, bubbles: true, cancelable: true }));
  return composerLength();
}

function clickSend() {
  const b = q(SEND);
  if (!b || b.disabled) return false;
  b.click();
  return true;
}

// 最後の回答を Markdown に戻す
function answer() {
  const last = lastAnswer();
  if (!last) return '';
  const root = last.querySelector(ANSWER_BODY) || last;
  const kids = (n, d) => Array.from(n.childNodes).map(c => md(c, d)).join('');
  function list(n, d) {
    const ordered = n.tagName === 'OL';
    const start = Number(n.getAttribute('start') || 1);
    const items = Array.from(n.children).filter(li => li.tagName === 'LI').map((li, i) => {
      const body = kids(li, d + 1).replace(/\n{2,}/g, '\n').trim();
      return '  '.repeat(d) + (ordered ? (start + i) + '. ' : '- ') + body;
    });
    return (d > 0 ? '\n' : '') + items.join('\n') + (d > 0 ? '' : '\n\n');
  }
  function table(n) {
    const rows = Array.from(n.querySelectorAll('tr')).map(tr =>
      '| ' + Array.from(tr.children).map(c => kids(c, 0).trim().replace(/\|/g, '\\|').replace(/\n/g, ' ')).join(' | ') + ' |');
    if (rows.length === 0) return '';
    const cols = n.querySelector('tr').children.length;
    rows.splice(1, 0, '|' + ' --- |'.repeat(cols));
    return rows.join('\n') + '\n\n';
  }
  function md(n, d) {
    if (n.nodeType === 3) return n.nodeValue;
    if (n.nodeType !== 1) return '';
    const tag = n.tagName.toLowerCase();
    if (n.classList.contains('katex-display')) {
      const t = n.querySelector('annotation');
      return '\n$$' + (t ? t.textContent : n.textContent) + '$$\n\n';
    }
    if (n.classList.contains('katex')) {
      const t = n.querySelector('annotation');
      return '$' + (t ? t.textContent : n.textContent) + '$';
    }
    switch (tag) {
      case 'pre': {
        const code = n.querySelector('code');
        const lang = code ? ((code.className.match(/language-([\w+#.-]+)/) || [])[1] || '') : '';
        const body = (code || n).textContent.replace(/\n$/, '');
        return '\n```' + lang + '\n' + body + '\n```\n\n';
      }
      case 'code': return '`' + n.textContent + '`';
      case 'br': return '\n';
      case 'p': return kids(n, d).trim() + '\n\n';
      case 'h1': case 'h2': case 'h3': case 'h4': case 'h5': case 'h6':
        return '#'.repeat(Number(tag[1])) + ' ' + kids(n, d).trim() + '\n\n';
      case 'strong': case 'b': return '**' + kids(n, d) + '**';
      case 'em': case 'i': return '*' + kids(n, d) + '*';
      case 'a': return '[' + kids(n, d) + '](' + n.href + ')';
      case 'ul': case 'ol': return list(n, d);
      case 'blockquote': return kids(n, d).trim().split('\n').map(l => '> ' + l).join('\n') + '\n\n';
      case 'table': return table(n);
      case 'hr': return '---\n\n';
      case 'button': case 'svg': case 'style': case 'script': return '';
      default: return kids(n, d);
    }
  }
  return md(root, 0).replace(/\n{3,}/g, '\n\n').trim();
}
