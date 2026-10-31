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
比較対象は、モデルとeffort、および順序付きの指示・tool schema・入力本文のprojectionである。
正常に保存された今回の実行入力と、同じnative handleのdeveloper/bootstrap/toolを、内容・metadata・順序を保って比較する。
その入力項目では、通常のrequest準備が再生成するitem IDと空のphase表現以外を比較から外さない。
traceのturnはnativeの最初のtask_startedとturn_contextにも一致させる。
tool_choice、parallel_tool_calls、includeなどのprovider設定は、このnative比較による認証の対象には含めない。
request全体の全fieldがnativeと一致したという保証にはしない。
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
通常safe-deleteのplanが返した削除を別の通常呼出しで実行し、成功後にfinishと元root不在を確認する。
実行前拒否なら同じ削除を繰り返さず、通常quarantineを使い、保持・未回収・実回収を分ける。
将来この世代を再受領する必要があれば、削除前に保持を継続する。削除したsourceをmetadataだけで資格化し直さない。

公開Skillの実配置、現在bufferでの発見と適用、liveとcompletedの実使用は、この入力資格と別に確認する。
準備や起動、記録との一致だけを依頼全体の完了にしない。
