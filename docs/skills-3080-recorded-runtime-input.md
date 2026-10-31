# 正常起動に結び付けた記録済み入力を照合する

`world_state`とdeveloper/bootstrapが同じnativeにあるだけでは、そのlog内で両方を変更した場合の独立した根拠にはならない。
新しいtyped public reviewとconsumerでは、通常runtimeがモデル要求を構成した時に別に保存する入力を照合する。
一般guard、元の保護台帳、既存Skillやbackup、宣言外の資料に対する制約は変更しない。

## 正常な親の準備と起動

元3080の現在の実親だけが、登録済みの同じpublic taskに対して次の操作を行う。
元author、現在のcreator、source issuer、実consumerを同じ人物や世代へ読み替えない。

1. 新しい公開対象を使うspecを通常の共有ToDoへ保存する。
   `render-skills3080-runtime-source`は、そのrequestに対する固定PSI hidden呼出しを返す。
2. `prepare-skills3080-runtime-source`は、親と対象taskの通常登録、保存spec、直前job、停止状態を確認する。
   正常な親の未完了のcanonical呼出しを確認してから、相関用nonceと所有付きディレクトリを新規作成する。
   authorやPUBLIC files/manifestを変えず、nonceだけを加えた新しいspecを返す。
3. 返されたspecを同じ共有ToDoへ通常登録し、終了していたtaskは通常のtodo CLIで再開する。
   `start-skills3080-runtime-source`は、現在specと親・対象の状態を再確認し、通常のscheduler CLIだけで起動する。
   `CODEX_ROLLOUT_TRACE_ROOT`は、その対象1件のdispatch子プロセスだけへ渡す。
   起動受付と入力記録の完成、本文の利用成功はそれぞれ別に扱う。

同じrequest pathのprepareは親job内で一度だけ受け付け、再実行で所有rootを増やさない。
正常prepareの結果にはrequest実体のNTFS識別番号も保持する。
所有ID付きの部分失敗にも同じrequest実体IDを返し、元の正常canonical呼出しに結ぶ。
部分失敗を別の表記で再実行しても、同じrequest実体から所有rootを重ねて作らない。
外部の作成処理より前に、親jobとrequest実体IDに結ぶ試行記録を既存`.ci-work`へCreateNewで永続化する。
作成コマンドのタイムアウト・異常終了・出力欠落でもこの記録を残し、同じjob/request実体の再作成を拒否する。
所有IDが分からない失敗でも、試行を開始した事実とrequest実体IDを正規の失敗結果へ残す。
試行ファイルが削除されても、元のcanonical native結果との同一実体照合で再作成を拒否する。
元の準備呼出しの出力が不正・未完了で実体を判別できない場合も、新しい準備を始めず照合を優先する。
作成結果が不明な状態を、記録の削除や書き換えで再試行可能にしない。結果と残った所有物を照合して保持理由を報告する。
別の準備には別のrequest実体を使い、同じrequestを上書きして複数taskへ使い回さない。
表記の大文字小文字・区切り文字や名前が変わっても、同じ実体の正常準備済みrequestを再利用してrootを増やさない。
失敗や部分準備は元の所有記録を保持して調べる。完了を照合できる別taskや意図した新requestは別に扱う。
通常dispatchが未受付だったstartの再試行は維持する。

準備の結果は、親の正規tool call/resultと同じrequest、nonce、spec SHA、directory ID、既知producerへ結ぶ。
callerが任意の既存bundleやbootstrap digestを指定する入口にはしない。
nonceは相関値であり、新しい権限や資料の許可ではない。

## 独立した入力記録

既知Codex runtimeのRollout Traceは、モデルclientが構成したrequest objectを、transport呼出しの前に別payloadへ保存する。
payloadを保存してから、その参照を順序付きeventへ記録する。
これはnativeの本文を後からhashして新しい基準にする方法とは異なる。
[固定した実装](https://github.com/openai/codex/blob/360319d8dee62d87e2d59fc0c21805d79519cfed/codex-rs/rollout-trace/src/inference.rs)と
[model client](https://github.com/openai/codex/blob/360319d8dee62d87e2d59fc0c21805d79519cfed/codex-rs/core/src/client.rs)に基づく。

guardが確認するのは、専用rootにある最初の**記録済みモデル可視request object**である。
記録はbest-effortであり、最初の実送信、ネットワーク送信成功、provider受領の証明にはしない。
完全な論理WebSocket requestが記録される場合もあり、wire byteとの一致は主張しない。
明示された差分、`previous_response_id`、`generate=false`、既存sessionのresumeは資格対象にしない。
記録や通常dispatchとの対応が欠ければ資格を拒否する。

manifest、順序付きevent、thread/turn/model、payload kindと相対名を確認してから、最初の入力payloadだけを開く。
完了済みjobのevent記録は書き込み共有なしで開く。
稼働中は観測した既存範囲を共有byte-range lockで上書きから保護し、範囲の後への追記だけを許す。
payload照合後にも選択した最初の要求までのevent bytesが一致し、元の長さより短くなっていないことを確認する。
比較対象は、モデルとeffort、および順序付きの指示・tool schema・入力本文のprojectionである。
正常に保存された今回の実行入力と、同じnative handleのdeveloper/bootstrap/toolを、内容・metadata・順序を保って比較する。
その入力項目では、通常のrequest準備が再生成するitem IDと空のphase表現以外を比較から外さない。
traceのturnはnativeの最初のtask_startedとturn_contextにも一致させる。
tool_choice、parallel_tool_calls、includeなどのprovider設定は、このnative比較による認証の対象には含めない。
request全体の全fieldがnativeと一致したという保証にはしない。
この0.165 Responses Lite形式では、既知のGPTモデルslugとbuilt-in provider名`OpenAI`を要求する。
[固定runtimeのprovider定義](https://github.com/openai/codex/blob/360319d8dee62d87e2d59fc0c21805d79519cfed/codex-rs/model-provider-info/src/lib.rs#L79)を対応の根拠とする。
登録側はbare名、`openai`、`main`、既存の小文字account別名を同じOpenAI系へ対応付ける。
記録側のmodelはbare slugと完全一致させ、他providerのprefixを末尾だけで同一扱いしない。
未知の提供元や別runtime形式をこの入力証明へ推定で追加しない。一般のモデル登録規則は変更しない。
本文・tool schema・checkpointを同時に変更しても、別のruntime入力とは一致しない。
旧形式や入力記録のない完了jobへ、この新しい証明を後付けしない。

ソフトウェアの通常code reviewと、元の修復担当の結果報告は、従来の登録prompt/final/model/readonly資格を保持する。
それらの報告を新しいSkill本文や新しい作成の閲覧権限へ昇格しない。
typed public content reviewとconsumerの本文受領・実使用には、新しい入力記録を必要とする。

## privateな生成物の扱い

Rollout Traceは応答やtool情報も自動生成し得るため、所有付きのprivateな診断生成物として扱う。
readerは入力requestと必要なmetadataだけを扱い、response、agent result、tool結果、reducer stateを開かない。
原finalは正常receiverからだけ受領し、別payloadやcopyをその回収経路にしない。
指示本文とtool引数を返却・公開せず、証拠には型・時刻・長さ・hash・一致判定を残す。
実行担当は、専用rootごとに親のprepare結果、所有ID、通常の実job、保持理由と次の回収条件を記録する。
live入力検証、正常completed receiver、必要な同buffer検証が残る間は、入力原本を含むrootを保持する。
これらの検証が終わり、同じ世代の再受領を終え、通常jobと実producerの終了を確認してから回収する。
記録は本文を含めず、検証結果、入力のhash・識別番号、元の所有IDと実測容量を残す。
回収前には、workspace内の永続した検証記録そのものを開き、hashとsource世代・最後のconsumerを照合する。
その記録に結ぶ通常のresult requestとconsume requestを正規CLIで再検証し、最終受領と現在の同bufferの成功を確認する。
正規receiverは、実行物・起動・入力・原結果の検証を終えたSourceの所有ID・実体IDをmetadataへ返す。
検査用Go実行物から本番実行物限定の検査を迂回せず、この正常receiverが返すmetadataを回収対象と照合する。
完了を示すbooleanやhashの書式だけでは回収計画へ進めない。
意味上の確認は、実finalに実在する適用例・非適用例の引用、対象index、親の判断理由を記録し、実際の最終返信hashと照合する。
引用は行・段落の文脈を保持し、実consume buffer内の規則の引用と、適用する理由・適用しない理由を別々に残す。
短い単語の一致や空の理由だけでは通さない。意味の妥当性は、その具体的な記録を親が確認する。
通常safe-deleteのplanが返した削除を別の通常呼出しで実行し、成功後にfinishと元root不在を確認する。
実行前拒否なら同じ削除を繰り返さず、通常quarantineを使い、保持・未回収・実回収を分ける。
将来この世代を再受領する必要があれば、削除前に保持を継続する。削除したsourceをmetadataだけで資格化し直さない。

公開Skillの実配置、現在bufferでの発見と適用、liveとcompletedの実使用は、この入力資格と別に確認する。
準備や起動、記録との一致だけを依頼全体の完了にしない。
