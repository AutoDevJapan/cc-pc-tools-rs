# ToDo3751 再開情報

- 担当: 20261025-070552-todo-3751、元3277の依頼。
- 開始HEAD: 2e173041612ec7ae4736ab3bd04293fa7bc27492。
- 実owner: gh repo viewでAutoDevJapan/cc-pc-tools-rsを確認。
- 元1171はrecovery_unverified、3277はwait_on=[3751]。両方の状態を保持する。
- 正規guardの3277 receiptは元3638/実装3650、spec SHA 7d526ccfc04dae7df0b9a486a52e1feabaec566378cbfc093353b85684b94d2d。
- 元1171の復元仕様SHA: ae91e338c9d49ec5444c2582963498926e41a40ce0916d3fa240eba42bb41f34。
- 3277実jobは20261025-063120-todo-3277、現在succeeded。実cwdは保存scheduler.worktreeと一致。
- 原因: Rust橋渡しがscheduler-item/jobを渡さず、Goは元rowのrecovery_unverifiedで登録前に拒否する。
- 残り: 回帰テスト、実装修復、別モデルレビュー、CI、merge、現用版との照合と配布、固定質問の通常CLI配送、元3277へ結果。
- 所有済み一時領域: a9123966bdd042a795ceabc93f16c41c（safe-delete）。
- 固定要求: normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862。
- 本文/decisionは依頼にある私的な原ファイルを使用し、公開repoへ複製しない。

## 実装候補とテスト（2026-10-25）

- Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/478 。
- Red: TestConversationActorCLIRequiresCompletePairが、担当入力を受け付けない理由で失敗。
- Green: Go会話テスト全件、go vet、正規ToDo CLIの新ビルドを使うactor結合テストが成功。
- 結合テストで、元row不変更、依存待ち中の全文配送と重複抑止、依存完了後の質問待ちと回答解除、ロック前の元本人停止競合の拒否を確認。
- 別project/actor/source/root/spec/job、本人停止、同要求の本文/decision変更を拒否。
- 同actorの通常の次jobでも元の質問・元送信jobの証跡を保持。実反映の記録後だけ質問を閉じる。
- Rust nextest（cc-ask/cc-todo、1171件）は実行中。doctest/Clippy、別モデルレビュー、CI、配布と実配送はこれから。
- 初回Rustコンパイルはserdeの直接依存がないため失敗。既存の厳密な文字列envelope検証へ修正し、依存追加なしで再コンパイル成功。
- Goの既存固定エラーテストは出力互換性の差で失敗。未知の失敗は従来出力を保ち、確認できる固定段階だけ追加する形へ直して該当テスト成功。
- cargo fmtによる対象外ファイルの書式変更は除去した。

## 指示文までの配送確認

- PR: https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/479 。
- 最初の候補26a27dbaでRust1171件成功、doctest成功（対象0件）、Clippy -D warnings成功。
- 追加照合で依存待ち中のメモ保存だけでは再開プロンプトに全文が入らないことを発見。有限actor scopeでの正規editに、同じロック内の指示文更新を追加。
- 追加後: cc-todoの関連48件成功。新ビルドの正規ToDo CLIを使うactor結合テスト全件で、指示文の全文と編集回答の置換まで成功。
- 別モデルレビューToDo3754を正規subworkで登録。現在は通常スケジューラの起動待ち。依頼時のslash付きモデル名はCLIの入力検証に拒否されたため、slashなしのモデル名で登録した。正規審査拒否ではない。
- statusでbusyを確認。共有scheduler/runnerを停止せず、ロックも削除していない。CIと残る関連確認を続ける。

## 2026-10-27の再開

- 実job: 20261027-012938-todo-3751。前回候補ae0b5cb9の全CI成功を確認。
- 読取レビュー3754は実job20261027-010852-todo-3754で完了。対応するnative turn_contextのモデルanthropic/claude-fable-5-1/maxを原セッションから照合。原応答SHA e3a5be717bfe774ab2426aaf38f03a0e4a147e3d891f81234c7972f1a236c498。
- Mustは「反映後のdoneで質問closeを拒否する」。3ケース（担当done/元done/両方done）でconversation_application_deferredのRedを確認。
- 元の正規receiptを不変保存し、同じ質問の閉鎖/既回答の読返しと、新しい送信/待機/回答配送の条件を分離。現在のpath guard、同じproject/root/仕様/job、本人停止は保持。
- 質問待ち中のblockに対して正規起動の再確認が失敗する点も同じ保存receiptで扱う。新しい質問の資格には流用しない。
- 指示文への回答二重挿入、編集回答の実反映記録の参照先、別ToDoの非文字列指示による全行読取失敗を修正。関連回帰テストを追加。
- main234d04dcを0d3df9fcへ統合。会話の原文末尾保持と添付原票修復の変更を残し、2ファイルの競合を解消。
- 新生成先の所有ID504ad34a587c845feddc796dc749b5dd、結果保持の所有IDc70592238da879c169ed518241d19e10。旧所有領域は原証拠として保持。
- 修正後の別モデル差分レビュー、同じ最終版のCI、マージ、現用更新、固定質問の実配送は引き続き確認中。

## 差分レビュー3988とCIの期限

- 3988は548706caの差分を読了し、todo/doing/waitの待機登録・未配送指示にも過去receiptを使っている点をMustとして返した。
- 判定を共通の一箇所に集め、現在の正規APIが使える状態では必ず実照合する。過去receiptは同じ質問のblockと反映後の閉鎖/読返しだけに限定。取消後の待機登録/新指示配送で2ケースのRedを確認し、修正後にGo/native/race成功。
- 質問待ち行へのeditが全文を先に指示文へ入れないことと、通常の依存待ちでは一度だけ正規化した全文を入れることを、Rustの実CLIテストへ追加して成功。CIでnative用環境変数が無くても実行される。
- 初回配布前の実indexでは1171/3277に結ぶ質問0件、actor付き0件を確認。旧actor receiptの移行は必要ない。
- PRのrepair-guard CIは10分のjob期限でgo test中に取消。APIの原ログはテスト失敗を示さず、期限によるoperation canceledだけを記録していた。
- 同じ333個のトップレベルテストを実機でJSON診断。7分で87番目まで進み、個別の失敗は0。7製品の実Git照合の1グループだけでも約3分半かかる。全件を省かず、CIのjob上限25分・Go上限20分・verbose進捗を設定し、期限時にGoの診断が先に残るようにした。新期限での全件成功は最終CIで確認する。
- 検証用パス変数の誤りでユーザーフォルダへ作った一時入力3件（2,692 byte）は、作成イベント/時刻/全SHA/リンク不在を確認して自分のTempへ回収済み。元の既存データ変更0、Discord送信0。正しい所有領域で要求ID/両SHAを再確認した。
