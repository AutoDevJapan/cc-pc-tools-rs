# 同じ3686で、個別noticeと改変表示の照合を直してWindows版へ反映する

元3080 / Issue283の同じ16資料の拡張に必要な有限修復である。
本人の限定修復a・残る別モデルレビューのLuna/max指定・既存OAuth枠・原資料保持を保つ。
新しい本人判断やPC操作、新費用、APIキー、一般guardの解除は要らない。
元3686の既存担当を継続し、新しい実装ToDoや司令塔を増やさない。

親は19件の可視本文と7つの個別noticeを実読し、次の4案28ファイルをPR614へ具体化した。
新規配置はまだ0であり、既存の150件や成功済みの21ファイルを再作成しない。
旧head20e4のCreator-bound全文4184は指摘0だったが、独立した全文4115がApacheの各改訂ファイルの改変表示をMust1、元の取得roleと個別noticeの役割説明をSuggestion1として指摘した。
親は各改訂指示の表示と、原参照を保持する固定表示prefixを実装した。
この新manifestは、旧レビューの受入を流用せず、親が改めて全文確認を受ける。

## 直す実際の境界

現行の公開source `repair-guard/skills3080_source_windows.go` は、選択したlicense行のroleを `license_or_notice` に限定する。
しかし固定保存済み6mileの次の個別LICENCE.mdは、元のcontent-indexで `support_text` と記録されている。
この原資料を上書きしてroleを変えてはいけない。
固定した個別本文と同じsource/SHA/bytes/blobを照合し、この1記録の古い分類と実際のnoticeの役割を正規に対応させる。
任意のsupport_text、同名の別ファイル、別repo・版・path・SHA・bytes・blobへ拡大しない。

- repository: `6mile/skills`
- commit: `920714f17d81e0a95c7a9bc51faf0a7921cd982d`
- path: `skills/skywyze/rlm-controller/LICENCE.md`
- original role: `support_text`
- bytes: `10752`
- SHA-256: `120701352845f72eda11433beb40df387026e41c5a9c6b9c9cef0c6c624f6004`
- Git blob: `0093542cc130c66e50a69f86839a5989484a6db3`
- content path: `objects/120701352845f72eda11433beb40df387026e41c5a9c6b9c9cef0c6c624f6004.gz`

本文は当該packageの190行のApache-2.0 noticeで、同じライセンスのbyteを出力へ保持している。
モデルが直接読まなかった2行は既知の公開Apache URIと機械照合しただけであり、未読の意味や独立著者確認を完了へ変えない。
既存のcatalog/index/tree/commitの全体pin、元の取得範囲、sourceと個別noticeの適用範囲、出力内のnotice全byte保持を維持する。

もう1つは `skills3080_masked_source_windows.go` の元参照照合である。
各原行または対応するWITHHELD markerという現在の照合を保持し、下の固定改変表示prefixの後ろへ従前と同じpayloadをそのまま置けるようにする。
元rawの末尾改行、UTF-8/NUL/CR、元行数、marker位置、少なくとも実可視行と伏字の双方がある条件は、prefixを除いた同じpayloadへ適用する。
未知のprefix、prefixの改変・重複、任意の本文追加・削除・置換や、原伏字の解除を許可しない。
既存のprefixなし発行・現在の読戻しは保持する。

固定prefixは、次のASCII1行と空行をLFで結んだ2行である。

```text
Modification notice: The distributor changed this reference only by replacing withheld source lines with "[WITHHELD physical line N]" markers. All remaining source lines and their original positions are preserved below.

```

この表示形式の照合は、新しい権利付与やsourceの分類許可ではない。
全てのsource・writer・review・merged source・新規不在・現在handle/buffer・個別notice・namespace・親子/世代の条件を引き続き照合する。
中央の未知資料や元client設定、既存の647件の取得審査保留を開かない。

## 同じ担当で完遂すること

1. 自分の登録spec・実job・作業場所・guard・実Git所有者AutoDevJapan/cc-pc-tools-rsとAGENTS/CLAUDE、最新の保存済み本人指示を読む。
   元のsourceと途中成果・旧版を保持し、必要な修復だけを既存の自分のworktreeへ実装する。
2. 関連Goテストで、現行が正当な注記付き参照と上の個別noticeを拒む事実をRedで確認してから修復する。
   既存正常系、元tuple各項目の不一致、別support_textや名前だけの偽notice、未知/壊れた/重複prefix、marker移動・新本文・改行/encoding不一致等の負例を含める。
   sourceの実取得や中央書込みをテストから始めない。
3. 旧配布で成功後凍結した3712/3896と、親の成功済み4129/4083を編集・再起動しない。
   元3686に属する未凍結の既存レビューを確認し、正規の同じ親子・immutable originと現在の実作者/launch親を分けて、Luna/max・両origin owner_requestで同じ最終headのコード確認を受ける。
   親3080の4184/4115を自分3686のレビュー親へ偽装しない。
   独立した2セッションのselfとラバーダックで、同じ最終headを確認する。
   `SKILLS3080_IMPLEMENTATION_REVIEW <head>` と `SKILLS3080_CODE_ACCEPTANCE`、原応答、実native/時刻/cwd、Must/Suggestion/Nitすべて0を照合する。
   旧レビュー原文は保持し、登録を実読と数えない。
4. 必要な関連Goテストとvet、全修正指摘、同じ正確headのCI成功を確認して通常PRをマージする。
   失敗は原因を直す。
   重いCargo・GPU・Civ6は使わず、同じ有効な結果を無意味に繰り返さない。
5. ソフト更新前に現在の配布物と既存の並行変更を読む。
   親が候補作成前に独立照合した現物の組は、同じ公開入力に含めた `docs/evidence/skills-3080-guard-before-notice-representation175645.json` にある。
   まだ同じ組なら、その実証された既存ソフトだけを、正常なsource/レビュー/CI/mergeを通して更新元baselineへ保持する。
   候補が申告した旧SHA、未知の配布物や任意のcaller値を受入しない。
   並行更新で現物が変わっていたら既存の実配布証拠と照合し、未知のまま上書き・rollbackしない。
6. 受入済みmerged sourceから、自分の実作業場所内にWindows/amd64のGo CLIを生成する。
   buildinfoのrevision・modified=false、reviewed guardの全ビルド入力、実writer3686、候補SHAと現物を、既存の正常な `--verify-skills3080-update` で照合する。
   不足する条件は原因を直し、資格の偽装や直接ファイル上書きで通さない。
   旧版を保持して通常のWindows置換・実hash/sourceの読戻しを完了する。
   console/UACを出さず、必要な管理者処理だけ既存CC-AdminBrokerを使う。
7. 親へsource/CI/merge/配布と実CLI確認を別々に返す。
   親が新4案のmanifest、Creator-bound全文レビュー、不在、通常CreateNew、現在の28buffer/ID、発見と現在の指示の明示適用を確認する。
   このソフト修復の完了を全16資料の完了や中央配置の完了にしない。

原資料・元indexのrole・原log/job/spec/native・取得647拒否・旧backupは保持する。
既存の正常発行のsource、古い作成時ID・hash、旧レビューも改変しない。
既存の原保護台帳が復旧したとは報告せず、通常の正規審査・排他を維持する。
新しいowner質問や新費用は不要であり、引継ぎだけで終了せず、同じ元3686で修復から実配布まで続ける。
