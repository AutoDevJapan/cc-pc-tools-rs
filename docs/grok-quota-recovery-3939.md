# Grokの利用枠切れで清掃担当を再割当する（ToDo3939）

対象はAutoDevJapan/cc-pc-tools-rs。元3815/3816は親3796の有限な読み取り専用作業。
対応Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/543 。
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

## 実装とテスト

実protocolのGrok Build balance errorをquotaへ分類し、後続の一般的なturn.failedで
quota/取消し/認証失敗を消さない。一般のHTTP402と接続リセットはtransientのまま。
CLIの一回限りの復旧hintは本当のgrok-4.7を受け付け、作者の恒久除外へは追加しない。

正常schedulerの元job・世代・権限・排出・元仕様本文の照合を通し、新しい既存OAuthの
健康情報から作者と異なる利用可能なモデルを選ぶ。新しいread-only researchも起動時の
元仕様をfinite_subworkへ結ぶ。元jobがtransientでも、正確なGrok balance errorと既存の
元仕様SHAを持つreviewは読取りだけで照合する。元の分類は保存されたまま。
元仕様SHAのない旧researchは元jobへ補完せず、次の通常の実行で実本文を結ぶ。

最初のRedは3件すべてassert/受付失敗を再現した。修正後のGrok回帰7件は成功。
既存の有限作業を正常CLIで登録し、todo Startによる世代更新・予約解除後も同じscopeで
最終queue admissionへ届くことを確認した。登録・受付はnativeの実操作成功とは区別する。

関連4クレートの実行中、no-action取消しのstate.lock競合がOS error33として露出した。
この経路は非待機のtry_acquireを使うため、競合をtodo.lock同様のdeferredとして返すよう
修正した。元jobと取消し意図は変えず、次の正規再試行に戻す。個別の競合回帰も追加した。
