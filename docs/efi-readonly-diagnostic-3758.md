# EFIの修復必要表示の読み取り診断（ToDo3758、親3628）

2026-10-25 08:00〜08:03 JST、Codex の実job `20261025-075742-todo-3758` が確認した。
この工程の結論は「EFIのdirty bitは設定されている。ファイルシステム破損の有無・範囲は未確定」。
修復は実施していない。親3628の全ドライブ仕分け・清掃、既存6時間調査の実成功と残件は変更しない。

## 対象を固定した証拠

識別子は、Windowsが返したUniqueIdを小文字化し、UTF-8からSHA-256を計算した値だけを用いた。
対象IDは `95708725e7ce555b6e8e0f8b68e46ebc4397a844c7368cc2f736004a7ded500c`。
Get-VolumeとWin32_Volume.DeviceIDの照合はそれぞれ1件だった。

| 読み取った項目 | 実測値 |
| --- | --- |
| ディスク番号 / パーティション番号 | 0 / 1 |
| GPTの種類 / システム領域 | EFI System / true |
| パーティションのサイズ / 開始位置 | 104857600 / 1048576 bytes |
| ファイルシステム / 容量 / 空き | FAT32 / 100663296 / 64386048 bytes |
| ドライブ文字 | なし |
| Get-Volumeの状態 | Warning / Full Repair Needed |
| 実プロバイダーの数値 | HealthStatus=1、OperationalStatus=53263 |
| Win32_Volume.DirtyBitSet | true |
| 所属ディスクの状態 | Healthy / Online、GPT、4000787030016 bytes |

パーティション100MiBとファイルシステム96MiBは異なる項目の実測値であり、同一容量へ丸めていない。
08:03:46 JSTの後確認でも容量・空き・状態・dirty bit・文字なしを維持した。
GUID、ラベル、シリアル番号、個別ファイル名や内容は報告していない。

## 前回の失敗と今回の切り分け

親のbroker `20261024-225400-6046dbc30c8b7ce6e6a5ad6921210014` は終了1のまま保持する。
親の元スクリプト・結果は編集していない。

| 実行 | 実際の結果 | 判断できること |
| --- | --- | --- |
| 親のfsutil | 07:54:01 JSTに144bytesの出力を保存。親の追加確認ではError 123とローカルvolume要求 | 実行は終了したがdirty照会は失敗 |
| 親のchkdsk | 出力ファイルと最終JSONなし。30秒の期限分岐でbroker終了1 | 第2段階が期限に達した。元スクリプトは期限時に出力を保存していないため、待機理由の確定はできない |
| 合成文字列によるargv再現 | 元の引用方法では末尾のバックスラッシュが引用符へ変化。引用を外す場合と末尾を二重化する場合は原値を保持 | 元の引数の組立てに実証できる不具合がある。実EFIを使わず3ケースを確認 |
| 正しい引数のfsutil dirty query | 08:01:20 JST、終了0、49ms、dirty=true、stderr=0bytes | Win32_Volumeのdirty bitと一致。前の引数エラーを解消 |
| chkdsk、引用なし・末尾区切りあり | 08:01:36 JST、終了3、39ms、stdout=30bytes、stderr=0bytes | 期限には達していない。正常な検査結果は取得できていない |
| chkdsk、引用なし・末尾区切りなし | 08:02:04 JST、終了3、25ms、stdout=30bytes、stderr=0bytes、同一出力ハッシュ | 指定形式の変更でも終了3。これ以上同じ診断を再試行していない |

今回のnative実行はCreateNoWindowとリダイレクトを使用し、標準入力を直ちに閉じた。
修復用オプションは一切付けていない。各プロセス自身の終了を確認し、後確認でも記録したPIDと名前に一致する残留は0だった。
PIDの後確認だけから他の担当の所有を推定せず、他プロセスを停止していない。

chkdskのbroker終了0は、結果JSONを取得できたという意味に限る。内側のnative終了3を成功へ読み替えない。
取得したテキストから事前に定めた状態だけを抽出し、原テキストや個別名は保存・表示していない。
2回の30bytesの応答は状態の許可リストに一致せず、詳細な失敗理由は未確定である。
無関係なファイル名を拾わないためのこの制約が、今回の追加診断の限界となる。

親の変数 `$matches` はPowerShellの自動変数 `$Matches` と同じ名前でもあった。
今回のスクリプトは別の変数名を用いた。この競合を親の期限到達の確定原因とはしていない。

## 一次資料と解釈の限界

[Microsoftのfsutil dirtyの説明](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/fsutil-dirty)では、dirty bitは不整合の可能性を表し、未確定の書き込み、変更を確定する前の停止、破損検出などが設定の理由になる。
したがって今回のdirty=trueだけで、破損やその発生原因を確定しない。
[Win32_Volumeの説明](https://learn.microsoft.com/en-us/previous-versions/windows/desktop/vdswmi/win32-volume)でもDirtyBitSetは読み取り属性として定義されている。

[MSFT_Volumeの説明](https://learn.microsoft.com/en-us/windows-hardware/drivers/storage/msft-volume)はvolume状態の定義を提供する。
公開ページと実機の表示・列挙定義の差を同じものと仮定せず、本書の数値と表示は実機からの観測として保持する。
今回の2系統のdirty観測は、Warningを単なる誤表示と決めつけることを支持しない、というのが診断上の推論である。

[Microsoftのコマンドライン引数規則](https://learn.microsoft.com/en-us/cpp/c-language/parsing-c-command-line-arguments?view=msvc-170)は、引用符の直前のバックスラッシュが引用符の解釈を変えることを説明する。
これは合成argv再現と整合するが、出力が残っていない親chkdskの内部待機まで証明するものではない。
[ボリューム名の説明](https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-volume)でも、末尾区切りの有無によってルートディレクトリとvolume自体の扱いが異なる。
これは2形式を試した技術的根拠であり、今回のchkdskが各形式を受け入れたとの確認ではない。

[Microsoftのchkdskの説明](https://learn.microsoft.com/en-us/windows-server/administration/windows-commands/chkdsk)では、修復オプションを付けない実行は状態確認であり、終了3は検査できない場合や修復されない場合を含む。
今回は正常な検査の完了、破損の検出、問題なし、実修復のいずれも確認していない。
同資料が示すとおり、使用中の領域の読み取り検査には誤検出の可能性もある。

今回の2つのchkdskのPIDと時刻へ限定したイベントヘッダー照会は、Applicationで該当0件、Microsoft-Windows-Chkdsk/Operationalでログ不存在だった。
イベント本文やデータフィールドは読んでいない。この限定結果から、ほかの時刻・プロセス・ログにも関連イベントがないとは言わない。

## 親3628へ返す状態と次の手

今回は読み取り診断という有限工程を完了し、EFIは変更せず保持した。
破損の確定と保守作業の実施は未完であり、全ドライブ清掃の完了には含めない。
本人への技術的な質問やPC操作依頼、重複したDiscord投稿・メンションは行わない。

親はこの結果を既存の差分記録へまとめ、EFIを清掃対象に加えず、独立した仕分けと清掃を続ける。
必要なら、次の有限工程を「終了3の理由を個別名なしで取得できる診断方法の確認」に限定する。
書き込みを伴う保守へ進む場合は、その必要性・影響・実施条件を別工程で具体化する。
今回の依頼を修復・文字割当・mount・dismount・BCD変更・再起動・起動時修復予約への許可へ広げない。
新しい一般禁止や定期タスクも追加していない。

私的な証拠と再開情報はこのworktreeの `.ci-work/3758/` に保存した。
`metadata.private.json`、`argv-probe.private.json`、`dirty-query.private.json`、`chkdsk-readonly.private.json`、`chkdsk-volume-name.private.json`、`postcheck.private.json` を実観測として保持する。
最初のdirty-queryのstdout_sha256とstderr_sha256は小文字化したUTF-8のハッシュであり、原出力バイトのハッシュではない。後のchkdsk出力ハッシュはUTF-8の大文字小文字を保持する。
証拠ファイル自体のSHA-256は別のmanifestで照合する。古い成功・失敗の記録を直していない。
