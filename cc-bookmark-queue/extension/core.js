export const BASE = "http://127.0.0.1:17643";
export const FOLDERS = ["案件・業務管理", "LLMモデル", "ローカルLLM・推論", "AIゲートウェイ・基盤", "コーディングエージェント", "Claude Code・Codex 拡張", "スキル・仕様駆動", "MCP・記憶・ブラウザ操作", "エージェント・Bot", "論文・学習資料", "ノーコード・サイト構築", "デザイン・UI", "開発ライブラリ・インフラ", "3Dモデル・ゲーム", "画像生成・OCR", "音声・音楽", "動画生成サービス", "動画生成OSS・編集", "マーケティング・集客", "文章・スライド生成", "通貨取引関連", "調査・お得情報"];

export function canonicalUrl(input) {
  const u = new URL(input);
  const parts = u.pathname.replace(/^\/+|\/+$/g, "").split("/");
  if (u.protocol !== "https:" || u.hostname !== "github.com" || u.username || u.password || u.port || parts.length !== 2 || parts.some(p => !/^[\w.-]+$/.test(p))) throw Error("GitHub URLが不正です");
  parts[1] = parts[1].replace(/\.git$/, "");
  if (parts.some(p => !p || p === "." || p === "..")) throw Error("リポジトリ名が不正です");
  return `https://github.com/${parts[0].toLowerCase()}/${parts[1].toLowerCase()}`;
}
function onlyFolder(nodes, title) {
  const found = nodes.filter(n => !n.url && n.title === title);
  if (found.length !== 1 || found[0].unmodifiable) throw Error(`${title}フォルダが見つからないか、複数あります`);
  return found[0];
}
async function sideFolder(api) {
  const [root] = await api.getTree();
  const matches = [];
  for (const bar of root.children ?? []) {
    if (bar.folderType !== "bookmarks-bar" && !(bar.folderType === undefined && bar.id === "1")) continue;
    for (const business of bar.children ?? []) {
      if (business.url || business.title !== "ビジネス") continue;
      for (const side of business.children ?? []) {
        if (!side.url && side.title === "副業" && !bar.unmodifiable && !business.unmodifiable && !side.unmodifiable) matches.push(side);
      }
    }
  }
  if (matches.length !== 1) throw Error("ブックマーク バー > ビジネス > 副業を一意に確認できません");
  return matches[0];
}
function flatten(node) { return [node, ...(node.children ?? []).flatMap(flatten)]; }

export async function addBookmark(api, entry) {
  if (!entry || !/^[a-f0-9]{64}$/.test(entry.id) || typeof entry.name !== "string" || new TextEncoder().encode(entry.name).length > 512 || entry.name.trim() !== entry.name || /[\u0000-\u001f\u007f-\u009f\ufeff]/.test(entry.name) || !/^\S.* – .*\S$/u.test(entry.name) || !FOLDERS.includes(entry.folder)) throw Error("キューが不正です");
  const url = canonicalUrl(entry.url);
  if (url !== entry.url) throw Error("キューのURLが正規化されていません");
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(url));
  const id = [...new Uint8Array(digest)].map(b => b.toString(16).padStart(2, "0")).join("");
  if (entry.id !== id) throw Error("キューIDが一致しません");
  const side = await sideFolder(api);
  const [tree] = await api.getTree();
  const existing = flatten(tree).find(n => {
    if (!n.url) return false;
    try { return canonicalUrl(n.url) === url; } catch { return false; }
  });
  if (existing) return { id, status: "exists", bookmark_id: existing.id };
  let children = await api.getChildren(side.id);
  const matches = children.filter(n => !n.url && n.title === entry.folder);
  if (matches.length > 1) throw Error("用途フォルダが複数あります");
  if ((await sideFolder(api)).id !== side.id) throw Error("副業フォルダが移動しました");
  let folder = matches.length ? onlyFolder(children, entry.folder) : await api.create({ parentId: side.id, title: entry.folder });
  const [current] = await api.get(folder.id);
  if (!current || current.url || current.parentId !== side.id || current.title !== entry.folder || current.unmodifiable || (await sideFolder(api)).id !== side.id) throw Error("登録先が副業の外へ移動しました");
  const saved = await api.create({ parentId: folder.id, title: entry.name, url });
  const [verified] = await api.get(saved.id);
  if (!verified || verified.parentId !== folder.id || verified.url !== url) throw Error("登録結果を確認できません");
  return { id, status: "created", bookmark_id: saved.id };
}

export async function poll(api, token, fetcher = fetch) {
  const request = async (path, options = {}) => {
    const response = await fetcher(BASE + path, { ...options, headers: { Authorization: `Bearer ${token}`, ...(options.headers ?? {}) }, credentials: "omit", cache: "no-store", redirect: "error", signal: AbortSignal.timeout(8000) });
    if (!response.ok) throw Error(`キュー通信失敗 (${response.status})`);
    return response.json();
  };
  const { entries } = await request("/pending");
  if (!Array.isArray(entries) || entries.length > 32) throw Error("キュー応答が不正です");
  for (const entry of entries) {
    try {
      const result = await addBookmark(api, entry);
      await request("/result", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(result) });
    } catch (e) { console.warn("副業ブックマーク: 登録を保留しました", e.message); }
  }
}
