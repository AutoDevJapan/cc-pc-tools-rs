# Grokの利用枠切れで清掃担当を再割当する（ToDo3939）

対象はAutoDevJapan/cc-pc-tools-rs。元3815/3816は親3796の有限な読み取り専用作業。
Grok Buildの利用枠切れがremote compactionのHTTP402として届き、transientのまま
同じモデルへ戻っていた。過去のjob、仕様SHA、原registryは変更しない。

## 受入条件

- 実際のprotocol error/turn.failedをquotaへ分類し、通常のtransient、取消し、認証失敗、
  本人停止を誤分類しない。一般のHTTP402だけでは枯渇と断定しない。
- 新しい有限subworkの起動時に実際の仕様本文を結び、元の仕様・世代・親・作者除外・
  worktree・権限・依存・資源・旧processの排出を通常scheduler/CLI/最終受付で照合する。
- 利用不能なGrokを別モデル名へ偽装せず、既存OAuthの新しい健康情報から別モデルを選ぶ。
  作者の恒久除外と一時的な利用不能を区別する。既存3669の受入巡回は複製しない。
- 別モデルによる最終差分レビュー、必要CI、通常マージ、現行の配布票・CAS・ACL・ロックを
  保つWindows更新後、元3815/3816の実step進行と終端結果を確認する。

## 再開記録

- 開始branch: scheduler/todo-3939-work。開始HEAD: dec7a2db。
- 実remote/APIでAutoDevJapan/cc-pc-tools-rsを確認した。作業場所のguardはclear。
- 正規statusで元3815/3816の最新失敗は0step、grok-4.7、transient。
  元の権限はread-only/never/user、browser profile、追加参照D:/CC、network=false。
- 元3757は稼働中。safe-deleteのコードと配布対象は変更しない。
- 今回の一時領域所有ID: d2c2b8ec726d2aebda4673e91d9e5aed（検証用）。
- 修正、Red/Green、レビュー、CI、マージ、配布、実再開は未完了。
