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

準備を保存した段階では2コマンドとも未実行。`run-checks.ps1` を一度だけ実行し、各 `.intent.json` と `.result.json`、完全なstdout/stderrを照合する。intentだけがある場合は既存プロセスとログを調べ、同じ検査を無条件に起動しない。

実行後に、成功、製品assertの失敗、コンパイル失敗、後続assert未到達、選択外を分けて追記する。証拠をcommit/pushした後、所有生成物をsafe-deleteのplan、別呼び出しの通常削除、finishで片付け、実HEADをschedulerへ保存する。自分のToDoの状態はschedulerが更新する。
