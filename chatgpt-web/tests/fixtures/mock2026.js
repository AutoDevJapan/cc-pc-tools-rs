// 2026-10 の ChatGPT のブラウザ版（real-*.html に記録）の作りを模した試験用の画面。
// textarea[name=prompt]、送信と停止を兼ねる1つのボタン（aria-label が切り替わる）、li[data-message-role]、
// 回答の本文 [data-assistant-markdown]、回答の完了で付く data-message-complete、会話の中のエラー欄 [data-message-intervention]。
// body の data-scenario: ok / error / slow
(function () {
  const scenario = document.body.dataset.scenario || 'ok';
  const box = document.querySelector('textarea[name="prompt"]');
  const button = document.getElementById('submit');
  const thread = document.querySelector('ol[data-conversation-transcript]');
  const model = new URLSearchParams(location.search).get('model') || 'auto';
  let busy = false;

  button.addEventListener('click', () => {
    if (busy || !box.value.trim()) return;
    const text = box.value;
    const user = turn('user');
    user.innerHTML = '<h4 data-message-attribution>You said:</h4><p data-user-message-copy></p>';
    user.querySelector('p').textContent = text;
    box.value = '';
    busy = true;
    button.setAttribute('aria-label', 'Stop generating');
    setTimeout(() => respond(text), 300);
  });

  function turn(role) {
    const li = document.createElement('li');
    li.dataset.messageRole = role;
    thread.appendChild(li);
    return li;
  }

  function respond(text) {
    const a = turn('assistant');
    a.innerHTML = '<h4 data-message-attribution>ChatGPT said:</h4><div><div data-assistant-markdown></div><div data-message-intervention></div></div>';
    const md = a.querySelector('[data-assistant-markdown]');
    if (scenario === 'error') {
      setTimeout(() => {
        a.querySelector('[data-message-intervention]').textContent = 'Something went wrong while generating the response.';
        a.setAttribute('data-message-complete', '');
        button.setAttribute('aria-label', 'Send message');
      }, 500);
      return;
    }
    const lines = text.split('\n').filter(l => l.trim()).map(l => l.replace(/[&<>]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;' }[c])));
    const parts = [
      '<h3 dir="ltr">Answer</h3>',
      '<p dir="ltr">Model: <code>' + model + '</code></p>',
      '<ul data-assistant-stream-block><li><p dir="ltr"> Apple </p></li><li><p dir="ltr"> Banana </p></li></ul>',
      '<pre dir="ltr" data-assistant-stream-block><code>fn main() {}</code><span data-message-content-controls><button aria-label="Copy code"></button></span></pre>',
      '<p dir="ltr">' + lines.join('<br>') + '</p>',
    ];
    let i = 0;
    const timer = setInterval(() => {
      md.insertAdjacentHTML('beforeend', parts[i++]);
      if (i === parts.length) {
        clearInterval(timer);
        if (scenario === 'slow') return;
        // 停止ボタンが送信ボタンに戻ってから、少し遅れて完了の印が付く
        setTimeout(() => button.setAttribute('aria-label', 'Send message'), 200);
        setTimeout(() => { a.setAttribute('data-message-complete', ''); busy = false; }, 2500);
      }
    }, 250);
  }
})();
