// ChatGPT のブラウザ版の画面の作り（2026年時点の data-testid と要素の並び）を模した試験用の画面。
// body の data-scenario で回答の結果を切り替える: ok / error / limit / slow / expire
(function () {
  const scenario = document.body.dataset.scenario || 'ok';
  const box = document.getElementById('prompt-textarea');
  const form = document.getElementById('composer');
  const thread = document.getElementById('thread');
  const model = new URLSearchParams(location.search).get('model') || 'auto';
  let send = null;

  // 本物と同じく、入力が空のあいだは送信ボタンが無い
  function syncSend() {
    const empty = !box.innerText.trim();
    if (empty && send) { send.remove(); send = null; }
    if (!empty && !send) {
      send = document.createElement('button');
      send.dataset.testid = 'send-button';
      send.id = 'composer-submit-button';
      send.textContent = 'Send';
      send.addEventListener('click', submit);
      form.appendChild(send);
    }
  }

  // ProseMirror と同じく、貼り付けは1行ごとの段落にする
  box.addEventListener('paste', e => {
    e.preventDefault();
    box.innerHTML = '';
    e.clipboardData.getData('text/plain').split('\n').forEach(line => {
      const p = document.createElement('p');
      p.textContent = line;
      box.appendChild(p);
    });
    syncSend();
  });
  box.addEventListener('input', syncSend);

  function turn(role) {
    const d = document.createElement('div');
    d.dataset.messageAuthorRole = role;
    thread.appendChild(d);
    return d;
  }

  function submit() {
    const text = box.innerText;
    const user = turn('user');
    user.textContent = text;
    box.innerHTML = '';
    syncSend();
    setTimeout(() => respond(text), 300);
  }

  function respond(text) {
    const stop = document.createElement('button');
    stop.dataset.testid = 'stop-button';
    form.appendChild(stop);
    if (scenario === 'error' || scenario === 'limit') {
      setTimeout(() => {
        stop.remove();
        const e = document.createElement('div');
        e.className = 'text-token-text-error';
        e.textContent = scenario === 'error'
          ? 'Something went wrong while generating the response.'
          : "You've reached our limit of messages per hour. Please try again later.";
        thread.appendChild(e);
      }, 500);
      return;
    }
    if (scenario === 'expire') {
      setTimeout(() => {
        stop.remove();
        const b = document.createElement('button');
        b.dataset.testid = 'login-button';
        b.textContent = 'Log in';
        document.body.appendChild(b);
      }, 500);
      return;
    }
    const a = turn('assistant');
    // 考え中の要約は回答の本文（.markdown）の外にある。上限・エラーの判定に混ぜないことを確かめる
    const thought = document.createElement('div');
    thought.textContent = 'Thought about the usage limit and why something went wrong';
    a.appendChild(thought);
    const md = document.createElement('div');
    md.className = 'markdown prose';
    a.appendChild(md);
    const esc = s => s.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c]));
    const parts = [
      '<h3>Answer</h3>',
      '<p>Model: <code>' + esc(model) + '</code> and <strong>bold</strong>.</p>',
      '<pre><div><div>rust</div><button>Copy code</button></div><div><code class="language-rust">fn main() {\n    println!("hi");\n}\n</code></div></pre>',
      '<ul><li>first</li><li>second<ul><li>nested</li></ul></li></ul>',
      '<ol><li>one</li><li>two</li></ol>',
      '<table><thead><tr><th>k</th><th>v</th></tr></thead><tbody><tr><td>a</td><td>1</td></tr></tbody></table>',
      '<p>Echo:</p><blockquote><p>' + text.split('\n').filter(l => l.trim()).map(esc).join('<br>') + '</p></blockquote>',
    ];
    let i = 0;
    const timer = setInterval(() => {
      md.insertAdjacentHTML('beforeend', parts[i++]);
      if (i === parts.length) {
        clearInterval(timer);
        if (scenario !== 'slow') setTimeout(() => stop.remove(), 400);
      }
    }, 250);
  }
})();
