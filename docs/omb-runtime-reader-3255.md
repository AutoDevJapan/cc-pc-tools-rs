# 3255の通常API照合付きcontext reader

PR495の内部coreへ実データを渡すための専用reader。元PR448とPR486の条件は変更しない。
一般guard、元client保護、元の台帳・native・認証ファイルを更新しない。

## 取得資格

対象は元3255の監査役の固定会話と、元4559から同会話で実施した必要レビューに限る。
固定10実行のID・時刻・結果SHAを通常job APIで照合する。新しいreader自身のレビューは、
同会話の正常終了結果に、専用markerとreaderの実Git revisionが両方ある一件だけを扱う。
本文や設定値は取得資格やmodelの代用にしない。

資格確認済みの既存Codex 0.163.0の通常stdio APIでnative会話・turnの時刻と状態を読み、
同じ正常終了応答の全バイトSHAと一致させる。nativeファイル名の時刻やIDだけで資格を作らない。
この経路はCodex側の独立した通常APIを使い、OMB配布物四件を未確認のまま同byteと扱わない。

現在のsource codeとCodex実行物の通常guardがclearであることを前提とし、元client保護が
取得不能なら拒否する。既知clientまたは保護カテゴリに該当するnativeは必ず拒否する。
一般guardのnative判定は変更せず、通常APIで元応答・実行・固定pathが一致した場合だけ、
このmetadata射影の用途を認める。自作票・callerのtrue・要求modelだけでは認めない。

既存guardの`client_resource`は`!access_allowed`であり、分類未確認にもtrueを返す
（`repair-guard/main_windows.go`のCLI出力契約）。確認済みclientの実判定は
`ownerPolicyClassify`の`protected`/`client_project`カテゴリで、必ず拒否する。
readerはclear/unverified/protectedと各boolean・カテゴリの整合を検証し、矛盾も拒否する。
分類未確認の許可をclient-freeの判定へ変えず、固定用途と正常API照合に限る。

## 原本の読取と出力

sourceは正常APIが同じ固定会話の原本と示した、ユーザープロファイル内の固定JSONL一件。
任意path・他bot・他会話を受ける引数は設けない。読取と検証は同じWindows handleに結ぶ。
reparse、hard link、物理path不一致、読取前後の変更、close失敗は値を返さない。
書込・削除の共有を認めず、64MiB/行4MiB/深さ64・重複key拒否で有限に読む。

原Codex JSONLに実在するsession_metaのIDと、対象turn_contextのtimestamp・turn_id・model・
effortだけを小さな入力へ射影し、既存coreで判定する。要求・設定やOMBの通信包みから
contextを作らない。別turn・開始前・不明な値で補わず、欠落/null/不正/未認識を保持する。
原本や本文のHash・コピー・保存は行わない。

出力は固定実行のID・時刻と、coreの限定観測だけ。本文、path、native ID、認証値は返さない。
source_qualified/execution_verifiedは通常APIと実原本の照合後だけtrueにできる。
access_grantedは常にfalseで、呼出側が一般の原本閲覧資格を得る意味ではない。
取得不能はunavailable。現在設定や別runを過去の実行証明へ昇格させない。

## 受入条件

- Given: 元応答と通常APIのnative最終応答・時刻・固定pathが一致する。
  When: 同じ読取handleの原Codex contextのmodel/effortを射影する。
  Then: 実欄の値だけを返す。要求値と異なっても置き換えない。
- Given: 通常照合が未完、保護対象、binary不一致、原本変更、破損、曖昧context、close失敗。
  Then: nativeを開く前に拒否できるものは開かず、途中の観測も返さない。
- Given: source内の未知fieldに本文・path・私的値がある。
  Then: 固定key集合と既知model/effort以外を出力しない。

合成テスト・read-onlyレビュー・同source CI・通常の配布条件を区別する。
この仕様の保存だけでは実取得や資格確認の成功としない。
