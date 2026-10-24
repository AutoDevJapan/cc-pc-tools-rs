# ToDo3726: Windows job.json replacement

元3700/3686の未完了と配送を維持し、cc-jobsだけを修復する。別司令塔・モデル/認証・課金・保護原本・全体queue設定は変更しない。Cargoは通常割当済み。

起点: `8ed37bbecc70ef87f8d55955f7a690f36101bfdd`。
作業branch: `scheduler/todo-3726-windows-3700-3686`。
開始job: `20261025-043658-todo-3726`。

所有記録: `9281a173abd026b4b8f04bda4a4c52f4`。
一時データはworktree直下の同IDの`codex-owned-`フォルダだけ。
ここへCargo target、完全テストログ、検査用データを保存する。
終了時はsafe-deleteのplan→別呼出し削除→finish。拒否は正規quarantine。

## 確認済み

- 最新D:/CC/AGENTS.md、owner-agent-conduct、repo AGENTS、Windows置換・共有ToDo・安全清掃・日本語報告・レビューのSkillを読んだ。
- GitHub APIで実owner `AutoDevJapan/cc-pc-tools-rs`。worktreeのguardはclear/client_resource=false。原台帳の復旧を意味しない。
- 元3700の指定2identityファイルは双方failed、pid/codex_pid不在、Archive、os error5。原job本文は読んでいない。
- 現用exe SHA256 `98e8279c8225e7fa553b5e1a182b4292003ff5a5793234f1e7bb963ed1c12084`。mtime/現checkoutからソース一致を推測しない。旧2458のsidecarは別hashで、現用の証明に使えない。
- 現sourceはtmp書込・renameを各1秒/50回再試行する。`supervise_attempt`の進捗saveが失敗するとwork全体のErrとなり、生存中の子を停止してfailedへ進む。
- Red commit `60e5bd4c`。実共有保持の進捗テストだけが失敗し、正常reader/解除/期限超過/ACL/必須保存の5対照は成功。初回のimport漏れと、ACL保持中のreadを求めたfixture不備は訂正済み。
- 表示だけの差分を延期して3秒後から再保存する修正後、6件と実プロセスの継続・wait返信・終了の計7件が成功。必須保存は厳格なまま。未知の項目も必須とする対照を追加した。
- 現用exeのコピーを同じ実プロセス試験へ入れたRedは、supervisor exit75、子終了、元snapshotはrunningを観測。完全sourceの対応は未確定。
- cc-jobs/cc-scheduler/cc-ask関連1694件は1688成功/6失敗/3skip。失敗はテスト用Tempをこのrepo内の深い所有フォルダへ置いた影響（名前推論2件、長いパス4件）。製品コードを変えず、別の所有Temp `30f2784bcabdd0e88b60ecce41d05d01` とその短縮名を使うと6件とも成功。同File IDを確認。doc3ライブラリ各0件とClippy成功。完全ログは最初の所有フォルダ、ハッシュはdocs/evidence/windows-job-snapshot-3726.json。
- テスト用Tempは`C:/Users/makhmeto/AppData/Local/Temp/codex-owned-30f2784bcabdd0e88b60ecce41d05d01`。Windows短縮名は`C:/Users/makhmeto/AppData/Local/Temp/COC1A2~1`。新しい一時フォルダとして登録済み。次のテストはこのリポジトリ外の同一フォルダを使う。清掃は長い元パスの所有記録で行う。
- native試験に開始イベント保存のバリアと、追加イベント無しのidle再保存・復旧を追加し、その1件も成功（7.179s）。製品Rustソースは76fb8af0のまま。古い固定レビューとの差分はテストと報告だけ。
- Anthropic OAuthはweekly100%でRate limited。既存xai OAuthはHealthy/weekly34%、実catalogのgrok-4.6はxhighまで対応。通常subworkの読取レビュー3729は実job `20261025-045233-todo-3729`、native thread `01a16f79-5d70-7261-82f8-340628a5836d`、2026-10-24T19:53:25.31Zのturn_contextでgrok-4.6/xhighを照合。結果は未受領。Gemini OAuthもHealthy。API keyは使わない。
- Issue468/PR469を作成。レビュー固定base8ed37bbe/head76fb8af0。正式CI40127100169はWindows順番待ち、CodeQLは一部成功/一部待ち。実配布は未実施。
- 元3700/3686へ候補/テスト/限界と残件をCAS追補し、同じmarker `[3726:windows-snapshot-candidate:76fb8af0]` を読み返し済み。両元項目の状態は保持した。
- checkpointはGUI subsystemのexeなので直接`&`呼出しだけで成功としない。CreateNoWindow/redirectしたSystem.Diagnostics.Processで終了とstdoutを待って確認した正式inspectは `eligible=false/reason=TimeoutError/saved=false/input_valid=true`。共有scheduler.lock取得期限で保存できていない。排他を削除/迂回しない。branchは最初から同じなので成果と再開手順はGitにも残る。
- その後293612d6の正式checkpointはsaved=true/全checks=trueで成功した。所有フォルダのcheckpoint-current-result.json。これ以降のHEADについても同じ正規保存を行う。
- 3729は実Grok4.6/xhighで76fb8af0を読了し、実jobは05:09:19 JST succeeded。原応答SHA256 `f1dcca9794eb4806babdf062a61e0ce1f67a09962576d22e74b7d61d861622d5`、原本は同jobのlast_message.md、所有フォルダにもreview-3729-original.md。Must1を受領した。全監視中の必須失敗を無視する提案のうち、実結果/エラー等のlive射影は延期して実exitとturn.completedをdrainし、終端へ厳格保存する形へ修正。再開ID/所有/停止/未知項目の永続化は依頼どおり必須のまま。この根拠も追加レビュー3733で確認する。
- 自己点検で、3秒待ち中のキャンセルを古い進捗がfailedへ戻すRedも実確認。state.lock下で最新世代と再開IDを照合し、最新snapshotへ自分のdeltaだけを適用する修正を追加した。キャンセル/既存終端/所有者変更を保存後に上書きしない3対照も成功。完了結果のlive保存失敗でsucceededをfailedへするRedも解消。sourceはcc-jobs内だけ。無イベント交代も必須保存へ統一した。
- 変更後のcc-jobs関連538件は全合格/1skip。最新のWindows/新回帰9件（上の3対照を含む）、cc-jobs doc0件、Clippy全targetも成功。cc-ask/cc-schedulerの旧成功はAPI/schema/CLI変更がないため再利用し、全workspaceは回さない。証拠はvalidation JSONと所有フォルダのcc-jobs-final-tests.log/final-regressions.log/final-doc.log/final-clippy.log。
- 追加差分レビュー3733は通常subwork登録済みで、未起動のため修正後の固定候補へspecを更新する。古い293612d6だけのreadbackを最新修正の承認へ使わない。実job/prompt/nativeと実際の読取HEADを照合してからMustを解消する。新HEADの正式CI/merge/実配布は未完。
- この行以降が上記予告の訂正。3733のspec更新は起動と競合し、実job20261025-052517-todo-3733は旧293612d6のnativeだけを読んだ。prompt SHA170c8cad…とGrok4.6/xhigh/nativeを照合、原scopeを保持して正常完了。原応答SHAe42791af…、旧native追加だけMust0。3739へ新04差分を登録したが、実job20261025-054857-todo-3739はOpus/maxとなりquota/429、0steps/0tokens。正規cancel --recoveryで05:57:27にjob_object終了、cancel_requested=null/recovery_cancel保存を確認。本人停止としていない。
- 3739の同じ読取は代替3743（job20261025-060006-todo-3743、実Grok4.6/xhigh、prompt SHAe99e06cb…、head04受領確認）が06:16:01に完了した。原応答SHA68929fe9031e08d256e21c9c88cedfbdbad68f4944fa50ef8f29374bb4bf1176、Must1。原3739は読んだとせず、3743の実読を紐付けて完了した。原本は各jobのlast_message.md、所有フォルダにもreview-3733-original.md / review-3743-original.md。
- 3743のMustは、diskの再開IDだけ更新されると監視中の照合で停止するのに終端は再開IDを照合せずfailedを上書きする点。PID同一・thread_idだけ変更のRedと、自分と同じ新thread_idへdiskが先に更新された場合にfalse-failureとなるRedを実確認（resume-id-red-confirmed.log）。基準snapshotを終端まで保つProgressPersistenceへ照合を集約し、owner_current/resume_currentを両段階で使う形へ修正した。thread_id/resume_sessionはfresh==基準かfresh==自分なら受入、第三の値は上書きしない。新回帰10件成功（5制御対照と一致IDの陽性対照含む）、関連検査とClippyを実行中。
- 作業中にmainへ入った既レビューのquota修復等を通常mergeし、c7dbe4598d14b2eab92aad02b4be4a64a7481cd4へ統合・pushした。jobs.rsの修復本体は04から変更0、共有依存に新quota moduleが追加されていたため、旧基盤で実行物を生成しない。統合後のquota/回帰141件は全成功。c7のcheckpointはTimeoutError、成果はGitHubと作業フォルダへ保持。最新再開JSONは所有フォルダのCURRENT-RESUME.json。新しい再開IDの修正候補は次のcommitと正規checkpointへ保存し、通常ToDoで追加差分だけを確認する。

## 残り

1. 受入1/2は上記の関連テスト・Red/Greenで確認済み。追加変更が無ければ同じ全件を重ねない。原2件の実拒否元は未確定のまま保持する。
2. 受領済み3729の対応を含む、新しい固定候補の追加レビュー3733を通常ToDoで実施する。元のLuna指定を拡大しない。
3. CIと別モデルレビューは登録だけで成功扱いしない。最新版の正式CI成功を確認する。受入1/2の達成としてprogress40を記録した。
5. 固定HEADの正式CI成功後PRを通常merge。並行変更を取り込み、現用のsource/hashと比較して最新merged sourceを正規配布。実Windowsの継続・結果返信・終了を確認。
6. 原3700/3686へ具体結果と残件を正規に返し、checkpointを保存。自分のToDo状態はschedulerが更新する。

本人の判断・PC操作は不要。登録・テスト追加だけで完了にしない。
