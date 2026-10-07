// ChatGPT のブラウザ版の画面を読む・操作する処理。
// Rust 側で `(() => { <このファイル>; return <呼び出し>; })()` に包んで評価する。
// 画面の作りが変わったときは、ここと tests/fixtures/ の模擬画面を合わせて直す。

const COMPOSER = '#prompt-textarea';
const SEND = '[data-testid="send-button"], #composer-submit-button';
const STOP = '[data-testid="stop-button"]';
const ERROR_RE = /something went wrong|error (?:occurred )?(?:while )?generating|network error|問題が発生しました|エラーが発生しました/i;
const LIMIT_RE = /(?:reached|hit) (?:your|our|the) (?:\w+ )*limit|usage (?:cap|limit)|上限に達しました/i;
const BLOCKED_RE = /account (?:has been |was )?(?:deactivated|suspended|disabled|banned)|deleted or deactivated|アカウント(?:が|は)(?:停止|無効|削除)/i;

function q(s) { return document.querySelector(s); }

function bodyText() { return document.body ? document.body.innerText : ''; }

function state() {
  if (/just a moment|attention required/i.test(document.title)
      || q('#challenge-form, iframe[src*="challenges.cloudflare.com"]')) return 'challenge';
  if (q('[data-testid="login-button"]')) return 'logged_out';
  if (q(COMPOSER)) return 'logged_in';
  if (BLOCKED_RE.test(bodyText())) return 'blocked';
  if (/\/auth\/login|auth\.openai\.com|\/log-in/.test(location.href)) return 'logged_out';
  return document.readyState === 'complete' ? 'unknown' : 'loading';
}

// 依頼文と回答の本文を除いた画面の文字（エラー表示・上限表示の判定用）
function chromeText() {
  const root = (q('main') || document.body).cloneNode(true);
  root.querySelectorAll('[data-message-author-role="user"], .markdown').forEach(n => n.remove());
  return root.textContent || '';
}

function poll() {
  const all = s => Array.from(document.querySelectorAll(s));
  const a = all('[data-message-author-role="assistant"]');
  const last = a[a.length - 1];
  const rest = chromeText();
  return {
    state: state(),
    users: all('[data-message-author-role="user"]').length,
    assistants: a.length,
    streaming: !!q(STOP),
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

// ProseMirror は貼り付けを改行ごと扱えるので、まず貼り付けイベントで入れる。
function paste(text) {
  const b = q(COMPOSER);
  if (!b) return -1;
  b.focus();
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
  const a = document.querySelectorAll('[data-message-author-role="assistant"]');
  const last = a[a.length - 1];
  if (!last) return '';
  const root = last.querySelector('.markdown') || last;
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
