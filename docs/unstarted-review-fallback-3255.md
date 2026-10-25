# 未起動3649のモデル切替を既存受付で扱うための修復

元3255の次工程。PR509の読取専用metadataはmerge b6b50ad4と実取得1回で受入済み。
3649の保存要求はclaude-opus-5-5/max、両originはabsent、jobs0、
resource_waitはquota_exhausted、not_beforeは1793030399.891だった。
この値やsource版の記録を、現在のOAuth枠や本人固有指定の証明にしない。

元3387/job20261024-201241の正常Codex APIで、登録したcommandExecutionを確認した。
同じitemはtodo addのmodel/effortを明示し、origin引数は付けていない。
同itemの正常stdoutはadded3648とadded3649を返した。
元のorigin欄はabsentのまま保ち、登録元を後からpolicyへ書き換えない。

## 先に準備する判定規則

cc-schedulerの `unstarted_review` は、同じ3649だけの切替計画を判定する純粋関数。
ファイル、ネットワーク、口座、秘密値、共有ToDo、プロセス、状態を読み書きしない。
typedな入力がそろわない場合は固定の理由だけを返す。
この関数自体は、入力の真正性や利用資格を検証する入口ではない。
値の供給と再照合は以下の通常scheduler側に実装する必要がある。

対象は、未起動・実行履歴空・停止/依存なし・現在の登録予約が固定173cのreviewを
指す同じ3649/localai-web-rs。元の保存要求Opus/max・originabsentを保持する。
出所は元3387の同job/thread/turn/command itemと原command/stdout SHAに結ぶ。
上流の新しい本人固有指定が未確認なら不適格とし、origin欠落だけで切り替えない。
新たなToDo、レビュー、予約key、別のworktree、再delegateは作らない。

移行元のAnthropic枠は、現在の通常OAuth更新でverified/exhaustedであることが必要。
移行先のOpenAI枠はverified/availableで、APIキーや追加支出を使わないことが必要。
観測は60秒以内、未来は1秒まで。対象モデルはgpt-6-luna/maxで、実作者と同じ
モデルなら不適格。カタログに載っていることだけで利用可能とはしない。

元quota期限に等しいnot_beforeだけを切替対象とする。別の期限、wait_ready、
停止、未完依存、起動済みjobは切替計画へ含めない。本人固有指定は保持する。
入力状態SHAが変わった場合は再計画を要し、期限やoriginを推測で書き換えない。

## 通常schedulerへの残る接続

1. 正常な元job/commandの固定証拠を検証し、現在の登録・spec・固定173c・予約を
   検証する。自作receiptやrequest引数だけを検証済み出所へ昇格させない。
2. 通常quota更新と作者モデルの実行証拠を取得して判定へ渡す。
3. scheduler.lockとtodoの通常CAS内で、元行と予約・spec・停止依存を再照合する。
   変えるのは今回の選択とquota専用期限だけ。元要求・originabsent・受付・所有・
   旧期限と理由は原記録/遷移履歴へ保持し、通常prepare/優先度/資源/最終受付を通す。
4. jobs側でも同scope・同仕様・世代・元proof・新しいOAuth集約を検証し、通常の
   review既定へ戻さない。元3669のpost-run復旧を未起動caseへ偽装しない。
5. 差分の関連Cargoテスト/doctest/Clippy、別モデルレビュー、同head CI、通常merge、
   実行版のsource/並行変更/Hash対応後に反映し、実3649の開始と原結果を確認する。

現在の割当資源はなし。重いCargoは実行しない。判定規則のRustソースと回帰試験を
準備し、rustfmtまでを行う。Cargo割当の正規変更口不足を親へ返し、既済3648の
資源を流用しない。判定規則を保存した段階で接続・配布・実起動を完成扱いにしない。
元private PR17のCI供給・元2191要求Hashと原レビューは別の未完として保持する。
