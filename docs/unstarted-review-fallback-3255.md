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
origin欠落だけで切り替えない。元のAI登録との一致に加え、同3255へこの有限修復の
続行を結んだ親の正規返信を、そのmessage ID/全文SHA/所属threadへ照合する。
これを本人の指示や全上流の読了の証明へ昇格させない。初期草稿の
`current_owner_scope_verified` はこの意味を超えていたため削除する。
本人固有指定・新しいowner_instruction・停止・仕様変更が記録されていれば不適格。
元のmodel_origin/effort_originの欠落は、切替後も欠落のまま保持する。
新たなToDo、レビュー、予約key、別のworktree、再delegateは作らない。

移行元のAnthropic枠は、現在の通常OAuth更新でverified/exhaustedであることが必要。
移行先は正常なOpenCodex namespaceへ結んだ具体的な公開selectorを選ぶ。そのselectorが
参照する一意のアカウント自身について、codex OAuth/認証あり/追加支出無効/正常な
利用可能quotaを確認する。別のavailableアカウントで未知の選択先を代用しない。
privateなアカウントIDやmappingは保持・出力せず、公開selectorだけをcapsuleへ残す。
jobs登録と最終admissionは同じ公開selectorを再検証し、実launchにもそのnamespaceを使う。
観測は60秒以内、未来は1秒まで。対象モデルはgpt-6-luna/maxで、実作者と同じ
モデルなら不適格。カタログに載っていることだけで利用可能とはしない。

元quota期限に等しいnot_beforeだけを切替対象とする。別の期限、wait_ready、
停止、未完依存、起動済みjobは切替計画へ含めない。本人固有指定は保持する。
入力状態SHAが変わった場合は再計画を要し、期限やoriginを推測で書き換えない。

## 通常schedulerへの接続

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

現在の割当資源はなし。PCから重いCargoは実行しない。既存PRのCIは専用の通常受付と
資源ゲートで実行されるため、その受付・開始・終了をこの担当へのCargo割当と区別する。
既済3648の資源を流用しない。未確認のCIを成功扱いにせず、ソースを保存しただけで
コンパイル・配布・実起動を完成扱いにしない。
元private PR17のCI供給・元2191要求Hashと原レビューは別の未完として保持する。

## 固定の正常入力と一件のCAS

`record-recovery review-registration-metadata` は追加引数を拒否する。
読取資格がある固定Codex実行物を保持し、通常のthread/read、turns/list、items/listだけを
使う。元3387の固定thread/turn、開始終了時刻、cwd、commandExecutionのID、正常終了と
原command/stdoutの全byte SHAを照合する。itemsは最大4ページ、各100件までで、
カーソルはAPIの返却値だけを使う。原command/stdout・他の本文・pathは出力しない。
保存済み調査票を入力にしない。source/binary guard、実行物の同handle SHAと前後同一性、
通常RPC終了に失敗したらunavailableとし、部分的な成功を返さない。元nativeは開かない。
この結果は原操作との一致であり、全上流の本人発言を読んだ証明や切替許可ではない。

`scheduler prepare-unstarted-review --id 3649` は検証だけを行う。
`--apply --expected-state-sha256 <SHA256>` を付けた場合も起動はしない。
正常producer、現在のGit共通領域/branch/clean、元作者2件のnative_turn_contextモデル、
固定spec SHAと次の二つのGit対象、
現在のOAuth集約、既存3649のjob/process不在を確認する。scheduler.lockとtodo.lockを使い、
行全体が一致する場合だけ切替capsuleを保存し、quota専用の期限/理由をcapsule内へ移す。
元のmodel/effort/originの欠落、予約key/branch/worktree、依存、仕様、noteは書き換えない。
公開APIは任意JSON patchや任意producer pathを受け取らない。

レビュー対象は固定173c/tree cad96aであり、実行用の独立checkoutは保存済み1497a932で
ある。2026-10-26の通常guard/Git metadataでもHEAD1497a932を確認した。元のcheckoutを
173cへ変更しない。固定対象treeと実行checkout HEADを別々に確認し、jobs最終受付でも
再確認する。単に173cのobjectが存在することだけを現在のHEAD一致とはしない。
specは通常ToDoが生成する `D:/CC/Tools/todo/specs/3649-<32桁hex>.md` の名前空間だけを
読取前に許す。リンク・reparse pointを拒否し、開いたstream自体を1MiB+1で制限する。
この限定と固定内容SHAは、別のpathや別ToDoのspecを読む許可ではない。

PC上のCargoを直接起動せず配布物を得るため、既存Build speedの正常資源受付と
package-toolsをこのbranchの有限な配布に使う。通常テスト成功後だけjobs/schedulerの
2クレートをrelease buildし、実revision/tree/2実行物SHA/容量/run IDを付けて1日保持する。
既存ケースの5実行物と混ぜず、既存の共有資源ゲートと容量設定は保持する。
共有workflow変更を全件テストへする既存選択規則は保ち、その理由を実行票へ残す。

通常prepareはこのcapsuleを再検証してから元Opusのquota早期returnを通り、Luna/maxを
新しい選択として保存する。元要求のoriginをpolicyへ補完する操作ではない。
既存のモデルカタログ・優先度・資源・source guard・別モデル条件は維持する。
jobs側も、登録前と最終queue→state→todoの受付で、元正常登録、現在spec、同予約、
scope、元作者2件とfresh OAuthを再照合する。Lunaをreview既定へ戻さない条件は、この
capsuleを独立に再検証した場合だけである。実行済みjobのquota復旧には偽装しない。
失敗時は既存の有限な技術再確認へ戻し、本人指定や別の待機を解除しない。

Goの合成テストは正常API対応・変更/失敗/重複・カーソル循環・ページ上限・非出力を扱う。
Rustの回帰は行全体のCAS、欠落/nullの区別、元指定とquota履歴の保持、停止/依存、
新job登録前後、scope/作者/予約/現期限の変更を扱う。実normal APIでの受入、Rustの
関連テスト・Clippy・別モデルレビュー・同head CI・配布と実3649はそれぞれ別に確認する。
