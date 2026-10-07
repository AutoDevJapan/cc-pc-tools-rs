import { TOKEN } from "./config.js";
import { poll } from "./core.js";

let busy = false;
async function tick() {
  // Chrome 110+: extension API calls reset the 30-second idle timer.
  // This heartbeat remains independent of an ongoing batch and server outages.
  await chrome.runtime.getPlatformInfo();
  if (busy) return;
  busy = true;
  try { await poll(chrome.bookmarks, TOKEN); }
  catch (e) { console.warn("副業ブックマーク: 次回に再試行します", e.message); }
  finally { busy = false; }
}
chrome.runtime.onInstalled.addListener(() => void tick());
chrome.runtime.onStartup.addListener(() => void tick());
setInterval(() => void tick(), 20_000);
void tick();
