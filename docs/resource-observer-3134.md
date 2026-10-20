# ビルドとCPU・メモリの観測API

Issue #303 / ToDo3134。最終画面統合・正式配布は3137、既存画面修復は3084が所有する。画像案は3145へ正規に引き継ぎ、3135はその完了待ち。本人の実回答前にUIを起動しない。これはバックエンドの仕様であり、画像承認や画面・Prd配布の完了証拠ではない。

## 入出力と受入条件

`GET /api/resources` は既存のloopback専用サーバーから、直近の常駐収集結果だけを返す。HTTP要求ではOS走査、ファイル走査、Docker起動をしない。既存のOperations更新で15秒ごとに収集し、新サービスは増やさない。

- `schemaVersion: 1`, `state: collecting | ready | partial | stale | unavailable`、`sampledAt`（UTC epoch ms）、`ageMs`、`sampleIntervalMs` を返す。45秒超の結果はstaleにし、現在のCPU・メモリ差・実行状態は未確認へ戻す。
- `host` は全論理CPUを100%としたCPU時間差、物理メモリの全量・空き・使用量を返す。CPU数はプロセスの割当制限と分けてOSから取得する。初回CPUはnull。CPU時間の巻き戻り、同じ時刻、不十分な観測間隔、GetSystemTimesが全体を覆えない64論理CPU超の環境ではnull。
- 全体CPUと上位プロセスは、掲載範囲と標本時刻が異なる。合計の差を特定の原因の比率として計算しない。VM内・OSカーネル・未取得/未掲載のプロセス・時刻差の寄与は、この観測だけでは分離できない。残りの原因は未確認として表示する。
- `processes` は上位のCPU・私有メモリ・直前の増加量・履歴内の増加量とビルドのプロセスを最大128件返す。選定後の配列はPID順。PID、作成時刻、確認できた親PID、CPU時間差、私有メモリ、working set、直前との差、履歴内の最古の同じ個体との差と観測幅を持つ。作成時刻はFILETIMEの10進**文字列**とし、JavaScriptの整数丸めを避ける。取得拒否はnullと`metricsUnavailable`で示す。`ready`はOS観測が更新できた意味で、すべてのプロセスを取得できた意味ではない。PID再利用を跨いで差を取らない。
- 私有メモリはcommit量であり、物理メモリ使用量とは異なる。working setは共有ページを含むため合算しない。プロセスやグループの私有メモリ総和を物理使用量の内訳として断定しない。
- `builds` は認識したビルドツールの最上位プロセスを最大64件返す。実CPU時間が増えたときは`processing`、現在ジョブと同じPID/作成時刻の正規Cargo markerで確認したときだけ`executing`/`waiting`とする。それ以外は`unverified`。sccacheサーバーの生存だけをビルドに数えない。実行数・待機数・未確認数は分け、架空の進捗率は作らない。
- 仕事との対応は既存Operations収集の現在ジョブから固定フィールドだけを追加投影する。本文・履歴・コマンドライン・環境変数・パス・認証情報は出さない。作成時刻で一致したジョブの親子関係だけを使い、競合する対応は未確認にする。製品名は既存の公開カタログだけを使う。
- Cargoの`resource-waits/cargo-<PID>.json`は現在ジョブに結び付いた固定メタデータだけ読む。64件/ジョブ、合計256件、各4KiBまで。リンク、破損、取得拒否、範囲超過を空の正常観測へ置き換えない。自由文のreasonは返さない。
- `docker` は60秒ごと、期限4秒、出力64KiB以下の既存`docker stats --no-stream`から、コンテナID・CPU・メモリだけ取得する。同時に1つまでの有限ワーカーを既存の常駐プロセス内で動かし、Operationsの更新やHTTPを待たせない。名前・コマンドは取らない。CPUはDockerの1コア=100%という別基準。VM/コンテナの統計はホストと**別の層**として返し、二重加算しない。製品やCIとの対応が確認できなければ未確認とする。
- 履歴は41回、1回4096プロセス、約10分を上限とする。再起動時は履歴蓄積中。増加は観測区間での差であり、リークや全体増加の単独原因と断定しない。並行作業3129/3131の時刻と分けて解釈する。

## 検証

合成入力で、初回null、32CPUでのCPU正規化、PID/親PID再利用、アクセス拒否、履歴幅・上限、ジョブ/marker照合、待機と実行の区別、キャッシュの古化、VMの別層を検証する。Redは空の観測実装に対する振る舞いテストで確認し、Green後は実Windows native観測とHTTPルートの受入を行う。別モデルレビュー、関連クレートのnextest/Clippy/doctestと必要CIを記録する。

一次資料: [Windowsの私有メモリとworking set](https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-process_memory_counters_ex)、[全CPUの時間カウンター](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-getsystemtimes)。
