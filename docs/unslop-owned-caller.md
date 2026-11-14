# キャプチャ処理の所有を保つ呼出し口

[取得したsessionの移管と回収](unslop-normal-capture.md)に続く呼出し口です。

この変更では、キャプチャの結果を保存し、使った資源を解放するまで、同じ処理オブジェクトに所有を保たせます。
通常の実行元、保存先の用途認可、継続するsupervisorの接続はまだありません。通常入口は失敗を返し、ビルドやworkerを開始しません。

## 何を保持するか

呼出し口は、取得済みの保存先、provider、controller、回収担当をまとめて持ちます。
providerからは、既存のOpenRetained検証を通ったsessionだけを受け取ります。借用したバッファや同じhashから所有権を作り直しません。

開始後にもう一度Beginを呼んでも、キャプチャは増えません。
Resumeは同じ世代の保存・移管・回収を続けます。開始記録の保存に失敗し、キャプチャがまだ一度も始まっていない場合に限り、同じcontrollerで開始を再試行します。

~~~mermaid
flowchart TD
  A[取得済みrootとprovider] --> B[同じcallerが一度だけ開始]
  B --> C[結果を保存]
  C --> D{入力が残っているか}
  D -->|残っている| E[同じsessionを回収担当へ移す]
  E --> F[同じflightを回収]
  F --> G[解放結果を保存]
  G --> H[保存先を閉じる]
  D -->|解放済み| H
  C -->|保存失敗| C
  G -->|保存失敗| G
~~~

## 結果の読み方

| 項目 | 意味 |
| --- | --- |
| result_saved | 同じキャプチャ結果を保存できたか |
| receipt_published | キャプチャ側が成果の公開を観測したか |
| owned_inputs_retained | 元の入力や回収対象への参照が残っているか |
| cleanup_pending | このcallerの保存・回収・解放が終わっていないか |
| cleanup_complete | 必要な保存、入力の解放、rootのCloseがすべて終わったか |
| caller_reason | 次に必要な処理を示す固定の理由 |
| capture_cleanup_failed | キャプチャ入力のCloseに失敗したか。toolchain側とは別に残す |
| capture_cleanup_refs_retained | その失敗に備えて、元の入力への参照を保持しているか |
| root_cleanup_failed | 保存先のCloseに失敗したか |
| pending_close_failed | 成果の受け渡しに使ったpendingのCloseに失敗したか |
| pending_abort_failed | そのpendingのAbortに失敗したか |
| pending_cleanup_refs_retained | pendingの失敗に備えて、元のexportなどへの参照を保持しているか |

途中の結果は、公開済みでもqualifiedを返しません。cleanup_completeになってから、元の結果に従って成功・失敗を返します。
後から回収に成功しても、失敗したビルドを成功へ変更しません。
生のエラー文、パス、sessionの内容は返却項目へ入れません。

キャプチャ側のCloseが失敗した場合は、toolchain側のCloseと結果保存が成功していてもcleanup_completeを返しません。
sessionのClose結果は、そのCloseを実際に行った所有wrapper内で記録します。
後のpending.Closeが別のエラー理由を返しても、この失敗事実は失いません。
Build中に取得した入力も、Close前に同じオブジェクトへの参照を控え、失敗後に失わないようにします。
pendingのCloseとAbortはそれぞれ一度だけ行い、失敗事実と実exportへの参照を別に保持します。
公開後にpendingのCloseが失敗しても、receipt_publishedを消したり、同じ成果を再度Commitしたりしません。
未回収のflightを閉じられなかった場合は、その後の同ownerによる正常回収を続けます。

## 失敗後の扱い

| 状況 | 残すものと次の処理 |
| --- | --- |
| キャプチャ後の保存失敗 | 同じ結果を保存し直す。buildやCommitを増やさない |
| 移管前の保存失敗 | 元sessionと移管用slotを保持する |
| 移管済みの取消 | 同じownerとticketにだけ取消を伝え、別のcontextで回収を続ける |
| Closeの失敗 | 無効になったcapabilityと、残った参照・reportを別々に保持する。Closeを繰り返さない |
| 解放結果の保存失敗 | 既に閉じた資源を再度閉じず、report保存とForgetを続ける |
| sessionとerrorの同時返却 | 元sessionとleaseを保持する。専用回収口がない間は完了を返さない |

保存先はキャプチャ用ディレクトリーと別にします。
同じ物理ディレクトリーを別のwrapperで渡しても、二つ目の所有として受け付けません。
この照合は取得済み資源の重複を防ぐもので、用途認可やACL検査の代わりにはなりません。

初期化に失敗して未使用rootを閉じる場合も、Closeの失敗を捨てません。
戻り値のerrorは、元の初期化失敗をerrors.Isで確認でき、cleanup失敗は別の固定errorと型付き結果で確認できます。
失敗したrootへの参照を保持し、別callerが使っているrootを閉じたり、同じCloseを繰り返したりしません。

## 通常入口の未接続

固定のcapture-unslop入口は、試行を識別するkeyだけを受け付けます。
path、実行物、argv、PID、認可を示すboolなどを追加しても、実行には進みません。
実ownerの発行元がない現在はcapture_owner_unavailableを返し、jobもworkerも作りません。
保存済みのnative種別を既存のモデル実行へ流すことも拒否します。

この候補には、supervisorの起動・通信・取消配送の実装は含まれていません。
CLI内のmapはプロセス終了後には存続しません。通常接続には、実際に存続する所有プロセスが開始前からroot・provider・workerを受け取る経路が別途必要です。
その経路とrootの用途認可が成立するまで、通常factoryのnilと失敗を保ちます。
