# ToDo4143: CI受領先の用途確認

元3888の既存Windows CIに必要な、作成時に空だった一つの所有rootを確認する。
ソースの修復・レビュー・CI・マージ・固定候補の親3387への引渡しが本工程の範囲であり、
配布、原SDKの再認定・消費、runnerの起動、元PR17のCI成功とは区別する。

## 正規の入口と境界

`scrub391 --verify-ci3888-runtime <absolute-root>` は読み取り専用で用途を確認し、
同じ確認を `--guard-path <absolute-root>` のdirectory verdictにも適用する。
対象は元依頼で独立に観測されたowned ID `540085cc356bbd10a9fc49a191dfcd1b`だけ。
別root、child、親Temp、旧runtime、別actor、別ToDoへ許可を広げない。
directory verdictは作業場所の用途を示し、childの内容・SDK・credentialへの一般権限を付与しない。

`scrub391 --verify-ci3888-runtime-creation` は、固定の公開create stdoutだけを検証し、
SHA256と厳密な型・フィールド条件の成立をbooleanで返す。callerからpathや証拠bytesを受け取らず、
issuerの現在票、root、原SDKには触れず、用途・アクセス・SDK資格はすべてfalseを返す。
これは実装と公開作成証拠の整合確認であり、同rootの実用途受入を代行する入口ではない。

次を別々に確認する。

1. 元3888のnormal create stdoutの固定SHA256と、通常issuerが保持する現在の同じ所有記録が一致する。
   所有票をこの実装担当が直接読むことはせず、正規CLIの内部で固定メタデータだけを確認する。
2. version3/state owned/project/content/producerを型付きで照合し、uint64を丸めない。
   元作成jobは通常の独立した実装担当として、元ToDo・実worktree・branch・repositoryへ結び付く。
3. root・Tempのownerと、作成時の全祖先およびrootのvolume/file ID/birth・非reparseを現在の物理対象と照合し、
   非追従のhandleを判定終了まで保持する。Windowsの上位祖先をユーザー所有とは仮定しない。
4. 元3387/3888/3894の独立した登録・原job・停止条件を保持し、現在の3888の通常割当、
   実job、processの生存と出生時刻、実process ancestry、実ソースを確認する。
5. 元repo/PR17/head/tree/run/workflow/labelsと公式公開SDK assetの通常GitHub metadataを再確認する。
   これはSDK receiptや原物を読む操作ではなく、SDK利用資格も新しい期限も作らない。

noteのmarker、呼出側JSON、root名、plan成功だけでは成功しない。
原保護設定を生成・回復扱いにせず、元SDKの期限・Binding・issuer・registrationを変更しない。
失敗時は固定reasonとfalseだけを返し、保護設定・元票・秘密値・OS error本文を出力しない。
ソースの750ms期限切れ・用途未確認・branch/origin不一致と、readerのmerge確認失敗を別のreasonで返す。

共有ToDo・job・公開create stdout・issuerのファイルは、各読取の直後に閉じる。
Git/APIの照合中も通常の原子的保存を進められ、最後まで保持するのは物理確認用のdirectory handleだけ。
外部照合後に現在の割当・本人停止・job/process・原lineage・ソースと同じ所有bytesを読み直し、
変化や再読失敗があれば成功を返さない。これは判定中の全時刻の権限を証明する仕組みではなく、
この一回の照合終了時に現在の根拠を確かめる処理であり、SDK消費時の正規確認は別に必要である。

## 受入条件

- 正規helpに入口があり、重複・混在・不足・別rootの引数を拒否する。
- 同じ独立作成証拠、物理対象、通常の現在3888だけが用途確認を通る。
- 各証拠の欠落・差替え、identity変更、停止、別actor、別ToDo、process不一致を個別に拒否する。
- 正常directory verdictがchildの権限へ波及しない。
- 関連Go test/race/vet、同finalの別セッションself/duckの全指摘解消、必要CIを確認する。
- 親3387が単一writerの通常software CAS/AdminBroker配布を行い、元3888が同rootの正常受入を行う。
  この最後の実配布・実受入は本工程では未実施であり、候補だけで合格にしない。

原45分・最大2job・共有Cargo・公式SDK内部configure/run/remove・samebuffers・durable consumed/exit0の
元契約は元3888が保持する。本工程ではSDK消費、runner/token取得、元CI起動、現用更新、Discord送信を行わない。

## 配布前比較の既知ソフトウェアと統合

元3387の通常note（元3888の履歴末尾にも保存されたPR594の配布報告）は、
source `61de98373f8cb481302394be11d7d75cfef08a52`、
SHA256 `16386d55e418ce271cfed923edede1e7472bb93aa691b15409bcdb99753b9899`、
13,315,072 bytes、2026-10-27 22:04:27 JSTの正常配布を記録している。
4143は通常GitHub APIでPR594の同head・merge `86fc9f06ca01a6cb8a11720fcfb5a884c5209f56`を照合し、
現用ソフトウェアのSHA256を属性・内容の通常比較で独立に照合した。
このpairは、その後のmainの正常更新で `official3894NativePredecessor*` として両updaterへ追加済みであり、
[mainで保存された独立証拠](evidence/skills-3080-guard-before-native609-16386.json)と既存の照合テストを使う。
4143の重複した定数・テストを除き、現在のmainの後継pair・正常配布経路を保持する。
16386dが現在も現用であるとは仮定せず、候補は取り込んだmainの変更も含めてレビューする。
未知hashを自動採用せず、配布担当はさらに直前の正常CASと物理identityを確認する。
