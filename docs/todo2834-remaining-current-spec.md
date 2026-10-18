# 本人指定の残る製品を現在の継承作業として動かす（元2834）

これは元2834から切り分ける独立した実装・製品再始動工程。原本探索や受付登録だけで終わらず、実担当の操作と成果更新まで確認する。原保護台帳、192件、原の質問・manifestを復旧したと扱わない。

## 既に済んだことと所有範囲

- 前提2829はdone、共通runtimeとproduct-ciの正式更新は完了。親2834の分類修正PR221は2026-10-19 02:38 JSTに通常merge、merge/main `c9f0b26df6014c5473e110996b58cda18dff151e`。Go guardのmodule tree `357e98d512873776e116ca1217989177f1ceec6e`、SonnetレビューMust0、公式CI成功を確認して配布した。
- 配布済み `D:/CC/Tools/client-records/scrub391.exe` のSHA256は `BAE6A3603D47549C506A9FEC6C62E217BD77F593A3EAED45846E17659A8E2F5A`。以後の並行変更を確認せずこの値の版へ戻さない。親の証拠は `D:/CC/Work/unattended-operations-20261013-wt/todo-2834-work/guard221-install.private.json`。
- 現分類はGoDD AOの新担当2839と指定handoff1ファイルだけ。AOのPR1897/初回導入の実修復と担当2839は親2834が追う。この子で同じAO担当・レビュー・修復を増やさない。
- 元2639/2640/2641/2642/1626と子、特に2757/2736/2737、2836/2792、2843は既存の担当を維持。これらのsource/CI/配備を重複しない。2792やprepared stateを編集しない。
- 情報システム部のOperations受入2842は実画面とAPIを確認してdone、親が原応答を受領済み。同じ画面受入を繰り返さない。runtime版・正確な15秒周期・通信数の未測定は保持する。

## 現在の本人指定と優先順位

`D:/CC/AGENTS.md` と `D:/CC/Policies/current-user-product-inventory-transcribed-20261019.md` を読む。一覧は現在の本人指定の転記であり原台帳ではない。名前を作らず、GoDD・自社サービス・情報システム・既依頼の発信を照合する。Skillsは情報システム部の内部部署。受託→万世記→情報システム→発信→Skills→GoDD→自社サービスの順位を維持し、GoDD内はMCP α→Press→AO→13a13e1→Rosetta→Genesis→Virtual Streamer。上位の実行可能な工程へ安全な区切りで枠を譲る。

親の `current-products.private.json` はGoDD 13a13e1/Rosetta/Genesis/Virtual StreamerとAOの実GitHub所有者・repository ID・Git top-levelと全祖先reparse不在の証拠。再実行時は現情報を再確認する。`product-inventory2834.private.json` と `ambiguous-registration2834.private.json` は指定名の登録メタ情報だけ。保存statusの件数は生存担当ではなく、登録なし・複数root・origin未確認を未確認のまま区別する。未知の名称・認証付きremoteや秘密を表示しない。

## 実装と実行

1. まず正式CLIの現在のguard、元ToDo・実job・現在のGitHub PR/Issue/既存Prdを照合する。済んだ実装を再実装しない。実所有者AutoDevJapan/BM1314/mashfrombandと物理worktreeをRAM内で検証できた本人指定対象だけを扱う。未分類の製品機能を先に変更しない。
2. 現PR221の限定分類を基礎に、残る明示製品の現在の継承先を通常 `todo add --spec` で少数ずつ登録できるようにする。必要ならGo guardを有限な宣言の集合へ整理し、製品の登録project・元root・新担当ID・不変spec SHA・GitHubの実所有と実体を束ねる。単に似たフォルダ名や任意の入力manifestを信頼しない。旧番号のworktreeを一括許可せず、別repo・別common・別branch・リンクによる脱出と既存denyを保持する。未知192件や原名簿に触れず、新しい現在の継承記録だと明示する。
3. 親はPR221後のguard sourceをもう並行編集しない。この子だけが追加の分類実装を所有する。最新main/配布sourceの並行変更を保持し、関連Go検査・必要な実CLI受入・別モデルMust0・公式CI・通常merge・正式runtime更新まで担当する。旧binaryをコピーして成功としない。source修正が不要な範囲は余計な仕組みを増やさない。
4. 正常な共有ToDo/schedulerの受付で、上位の未再開対象から製品の実担当を進める。各specへ原依頼・元との関係・完了済み・具体的な残作業・現許可範囲を渡す。正確な原対応を確認できる場合は元担当・元場所で続行する。確認できなければ新しい現在の継承として区別し、元記録へ正規CLIで後継を残す。raw todo.jsonの編集、完了・本人回答の偽造は禁止。
5. まず少数で、分類→受付→実操作→sourceや受入成果の更新→次の自然巡回で続行を確認する。その後に独立した下位を通常の優先順で進める。一斉再起動・新しい無意味な監視ループ・直接の子Codex CLIを作らない。報告だけが続く担当は既存の有限な実操作停滞修復を使う。
6. GoDD Agent/Creatorsの未指定順位や、登録がない現在の製品を、本人が不要と言ったことにしない。登録と所有の対応は自分で調べる。原denyや他Orgを発見した対象だけを止め、ほかは継続する。本人にしか決められない未指定の方向・新費用・権利・顧客との新約束だけ、元回答を読んでDiscordへ具体的に1回質問する。技術的な分類や内部実装を本人に聞かない。
7. 発信では旧2475は締切未達による公開中止としてdoneであり、制作成功や継続資格として流用しない。旧2459には192.36秒の確保音源、実聴取未完、送信失敗と原2467待ち、過去の正規審査によるログアウト拒否がある。同じ拒否を別経路で迂回したり再生成したりせず、現在の既存契約・正規経路で進められる独立工程を選ぶ。ElevenLabs/neural frames/Suno、完成楽曲180秒以上を維持する。
8. 製品別の無通知Discordへ実成果と未確認を反映する。親2834へsource/PR/CI/配布/実job/元との後継/実成果/自然巡回を通常CLIで返す。登録・起動を実進捗や全製品完成としない。

新規コードはGoを基本とし、既存Rustは維持。GoDD製品内はRust。GPU・Civ6・本人PC操作・新費用・AI APIキー・非Prd・他Org環境・Rakko以外のドメイン事業者を使わない。重いCargoが必要な独立作業は --needs cargo の共有ToDoへ登録する。この調整担当自身にはCargo資源を要求していない。

## 完了条件

本人の現在の一覧の停止対象を、実担当・依存・現在の限定範囲・残る未確認に照合する。既存5案件とAO2839を重複せず、残る上位対象の実担当が成果を更新し、次の自然巡回で続行する。関連sourceのMust0/CI/merge/正式配布を確認し、製品別無通知報告と正規の再開記録を保持する。未実行が残るのに全件完了にせず、具体的な担当・依存を残す。
