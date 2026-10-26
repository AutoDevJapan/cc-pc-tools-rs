# 既知の作成済みメタデータ1点を改訂する

今回の範囲は、親3080/job20261026-090101が正規CreateNewした
design-reference-evidenceのindex27だけである。PR536の完了は保持する。
新しいSkillの加算は0。旧52出力の51点は変えず、旧メタデータを
`references/history/sources-before-revision1.json`へ移し、提案53 snapshotを作る。

`skills-3080-public-repair-v1`の任意フィールド`purpose_path`と`purpose`は、
同じspecが選択するPUBLIC Gitファイルの全文・長さ・SHA256・blobと一致する
ときだけ使える。通常レビューには付けられない。これは起動時の目的配送であり、
ツール操作の資格、本人停止、一般guard、元台帳を変更しない。目的なしの旧specも維持する。

新しい`skills-3080-operation-v1`の`revision`は次を持つ。

- `creation`: 元CreateNewの`skillsConsumeRequest`。creator、creation_request、
  creation_request_sha256、native_call_idを指定する。indicesは付けない。
- `prepared`: prepareの同じ形式の原要求・原native参照。applyとconsumeで必要。
- `applied`: applyの原要求・原native参照。consumeだけに必要。
- `indices`: consumeで読む提案53 snapshot内のindex。現在bufferを返す。

上位のmanifestは提案53の公開main包含済みcommit/path/SHAを指定する。
reviewは全文内容レビュー3931を使う。旧3917/3925や結果3930のauthorityは改変しない。
旧作成要求SHA50a697af、原call_c8e903093277429c87d06622d240f64f、
旧manifest fc92df3f、新manifest 3d27a707、旧8896B/50a6aa00、新10097B/e0607f08を固定した。
他のファイル・root・ID・receipt・backup・書込内容を要求へ渡す入口はない。

1. `--qualify-skills3080-revision <absolute-request>`で原作成のnative、原job/spec、
   現世代・本人停止、原ディレクトリと現在ID、旧bytes、新旧manifest、正式内容レビュー、
   PUBLIC main、原資料とnoticeを照合する。staging/historyが既にあれば拒否する。
2. 返されたcanonical invocationで`--prepare-skills3080-revision`を1回実行する。
   新historyディレクトリと`sources-revision1.pending.json`を衝突拒否で新規作成する。
   旧ファイルは変更しない。完了した同じnativeの返却が新IDを発行する。
3. prepareを参照する新しい要求を、同じcanonical形式の
   `--apply-skills3080-revision`で実行する。保持した旧IDのファイルを履歴へ移し、
   保持した新IDのpendingファイルを元の場所へ移す。各renameで置換を認めない。
   各境界で現世代と本人停止を再確認する。
4. 両nativeを参照する`--consume-skills3080-revision`で現在の実bufferを読む。
   新index27と履歴index52を含め、原51点のIDも元CreateNewに結ぶ。
   発行者と現在の読取runtimeを分ける。歴史上の物理的連続性は主張しない。

prepare完了後の4境界（旧版退避前・退避後・新版本文の公開前・公開後）で
実プロセスを終了し、同じprepare nativeから復旧するテストを行う。
反映後のapply再実行は変更0・already_appliedを返し、renameを繰り返さない。
先行作成・別ID・別bytes・alias・junction・hardlink・保護対象は拒否する。
prepare自体がnative完了を返す前に失敗した場合、旧版は元の場所に残す。
発行証拠のないstaging/historyは自動採用・削除せず、未発行の残存として保持する。
この状態を成功済み又は全中断経路の自動復旧済みと報告しない。

renameは保持した元handleと移動先ディレクトリhandleを使う。
[Microsoftの仕様](https://learn.microsoft.com/windows-hardware/drivers/ddi/ntifs/nf-ntifs-ntsetinformationfile)
に従いFileRenameInformation/DELETE accessを使い、ReplaceIfExistsをfalseにする。
既存ファイルの一般書込権限・書換えAPIは追加しない。

Windows配布では、既に独立受入済みaaf16eac/52d417を、
[前回配布と親92bufferの照合票](evidence/skills-3080-guard-installed-52d417.json)に結ぶ。
未知の実行物hashをbaselineへ足さない。配布前には正式別モデルレビュー、
同headのCI終了、通常mergeを必要とする。新ソースのWindows実行物と原版backupを照合し、
親が入力PR539/mainを受け入れた後、元担当が53実体・構造・発見・適用と結果PR538を受け入れる。
実装候補や入力案の作成だけで今回又は元16資料全体を完了扱いしない。
