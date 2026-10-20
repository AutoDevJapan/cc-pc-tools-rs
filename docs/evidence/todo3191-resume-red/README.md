# ToDo3191: 再開時の設定を調べる有限な追加Red検査

元ToDo3081 / Issue277の追加検査だけを扱う。製品の修正、Green、既存2331件の再検査、全workspace、配布、PR作成とマージは対象外。比較用ソースをmainへ取り込まない。

- 固定ソース: `6d7358a3b9334ad92ae3fc822e2c81b23af9f831`。
- 親ソース: `08b038ce6ed9d02503ed17f21b1480589e5f95a8`。
- 差分は `cc-job-policy/tests/resume_policy.rs` の66行と `cc-ask/tests/resume.rs` の24行だけ。製品コードの変更はない。
- GitHub APIで実所有者 `AutoDevJapan`、repo `cc-pc-tools-rs` を確認済み。指定の元作業場所と今回の作業場所はguard false。
- 保存済みの最新指示、両作業場所のAGENTS、モデル・effort・有限再試行の方針、関連するSkillとCargo文書を確認した。両ルートにCLAUDE.mdは見当たらない。
- 実行は共有Cargoゲート経由。safe-deleteで所有を登録した専用targetを使い、sccacheを共用する。ビルドとテストの上限10並列をゲートがさらに制限する。
- APIキー、認証情報、実providerへの推論、GPU、Civ6、Discord送信は使わない。選択したDiscord検査は既存のSharedFakeを利用する。

## 再開時の注意

2026-10-21 05:42:11〜05:42:53 JSTに指定の2コマンドを各1回だけ実行した。実行HEADは `d2001637ff137cdece47fa714d6011cd9885fa79`。固定ソースに証拠用ファイルだけを加えた版で、製品コードと検査ソースは固定ソースと同じ。両方のCargo終了コードは100で、パイプやログ保存処理の終了コードではない。共有ゲートのプロセスから取得し、ゲートのCargo終了コード伝播と対応するテスト失敗ログを照合した。

| コマンド | 実行したテスト | 成功 | 製品assertの失敗 | コンパイル失敗 | 選択外 |
| --- | ---: | ---: | ---: | ---: | ---: |
| cc-job-policy / resume_policy | 4 | 2 | 2 | 0 | 0 |
| cc-ask / resume / 指定の1テスト | 1 | 0 | 1 | 0 | 82 |

全5テストが開始し、実Redは3件、陽性対照は2件。新APIを仮定したコンパイル失敗ではない。各コマンドの開始・終了時刻、引数、専用target、ゲートの実ファイルSHA、上限、stdout/stderrの場所は対応するintent/resultに保存した。実行時の共有設定はビルド上限8。要求値はビルド・テストとも10で、ゲートによる制限を受ける。短時間で終了したため実際の子プロセス引数と瞬間並列数は採取できず、設定値を実測値と呼ばない。

## 到達したassertと未到達部分

1. `policy_migration_starts_fresh_and_a_later_requeue_cannot_restore_the_old_context`: 最初のLuna入力でモデルSolとeffort maxのassertは通り、15行の `resume_session.is_null()` で失敗。古いセッションを除去できない製品挙動の実Red。後続のcwd・sandbox・prompt・開始時刻・予算保持、古いthreadからの再構成防止、新会話保持、Astra入力と既存Sol入力は未到達。
2. `haiku_does_not_acquire_an_unverified_max_requirement`: Haikuのモデル保持は通り、65行のeffort比較で実際 `max`、期待 `high` として失敗。未確認のmax要件をHaikuへ付ける実Red。
3. `individual_owner_selection_keeps_the_model_effort_and_compatible_context`: ジョブ全体が元と等しいassertに成功。本人のLuna/highと互換セッションの保持を確認した陽性対照。
4. `an_eligible_same_model_capacity_attempt_keeps_its_context_and_durable_budget`: ジョブ全体が元と等しいassertに成功。同モデルのセッション、追加再試行回数2、待機期限12345、履歴、全体予算等を保持する陽性対照。
5. `every_discord_resume_launcher_applies_policy_before_passing_a_native_session`: policy入力で偽ランチャーへの1回の起動は通り、719行のモデル比較で実際Luna、期待Solとして失敗。共通起動入口が古いモデルを渡す実Red。後続のeffort・resume_session・sandbox・cwdのassertとowner_request入力は未到達。元コードがhigh/sessionを渡すことは読取で確認できるが、この実行でそれらの後続assertが通ったとは扱わない。

対象は既存公開APIによる設定変換と偽ランチャーへの引数で、実native会話の復元・実providerの推論・上流の実効effort/Fastを確認したものではない。stdoutは両コマンドとも0バイトで正常。nextestがstderrへ出したコンパイル経過、全テスト状態と失敗詳細を切らずに保存した。ログはGitの改行変換を無効にし、SHA256を記録する。

## 残作業と引き継ぎ

この追加checkの検査は完了。再実行しない。元ToDo3081が実装修正・Green・別モデルレビュー・正式CI・マージ・配布を扱う。今回の3失敗は修正しておらず、未到達部分の成功も主張しない。既存2331件と過去Redは繰り返していない。独立したPRは作成せず、mainへマージしない。

証拠のcommit/push確認後に所有生成物を片付け、その結果を保存して実HEADをschedulerへcheckpointする。共有ToDoの状態はschedulerが更新する。途中で再開した場合は保存済み結果と整理・push・checkpointの記録を読み、完了済み検査を起動し直さない。
