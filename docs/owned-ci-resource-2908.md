# ToDo2736の作成済みWindows CI資源を分類する

2026-10-19の本人指定による有限工程。原2736の保存済み作成・準備証拠を照合し、未起動の検査資源1件を通常の分類へ戻す。消失した原保護名簿の復旧ではない。

対象は `owned2736_windows.go` に固定したnonceの資源だけ。未知の兄弟、別の生成物、存在しない子は対象外。期限は2026-10-21 00:00 UTCまでとする。

判定には次をすべて要求する。

- 原修復worktreeの物理パスとAutoDevJapan/cc-pc-tools-rsのGit origin。
- 同worktreeに保存した6件の作成・plan・prepare・source所有証跡の固定SHA256。
- 元jobのID、2736への所属、完了、元作業場所・branch・repository、および通常ToDo原本の同job履歴・作業場所。
- 作成証跡のnonceに一致する唯一のパスと、実体のNTFS volume/file ID。作成証跡の日時はmkdir後の記録日時であり、NTFS作成日時と同一視しない。
- 指定パスの実在、全構成要素のリンク・reparse不在、現在の実denyに該当しないこと。

分類で対象資源の本文は開かない。保存証跡だけを上限付きで読み、対象の実体には属性取得専用handleを使う。CLIの出力項目は従来どおりで、機微な値を追加しない。新しい設定・許可台帳・CLIの許可引数は作らない。

分類成立だけで削除・runner登録・CI成功とはしない。元2736は既存collectorのSID/service/process/remote照合、単一試行journal、原log保存と回収後条件を維持して続行する。2736の待機先は正常CLIで全体2825から2908へ置き換える。2908完了後、同じ途中成果で通常のschedulerから再開する。

検査はrepair-guardのGo test、関連race、vet、buildと公式CI。別モデルの固定差分レビュー後に通常mergeし、最新mainの並行変更を含む正規実行物を配布する。配布後の正規scrub391で実分類と未知資源の保護維持を確認する。
