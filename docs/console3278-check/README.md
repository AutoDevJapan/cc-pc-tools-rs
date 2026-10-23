# 起動係1本の非配布照合（ToDo3462、親3278）

固定targetは `D:/CC/Tools/resident-guard/resident-guard.exe` だけ。
共有の `repair-guard` と3258/3440/3455のsourceを編集しない。
このフォルダーのGoファイルは公開済みエンジンに対するGo overlay用の保存物であり、
新しいguardやサービスの配布物ではない。既存エンジンの関数・CLI・保護処理は変更しない。

起点は本repoのcommit `0e16e81798ff4463c6ae6bcf6c4c57a695d9d13c`。
runtime差分は `model3081UpdateLeaves` と `permanent3344Sources` の固定データ2宣言だけ。
test差分は固定scopeの期待値2箇所と、本接続の狭い追加テストだけ。
既存のold/new/独立receipt、物理byte、Git root/origin/HEAD、追跡済みsource、
legacy保護、共有handle、再照合、hardlink、CLI混在拒否を維持する。
同梱の元コメントにある3081等の記述は公開エンジンの来歴を保持したもので、
本readerの対象は上記1本だけである。

旧原証拠は元登録場所の2492受領票と補助scriptであり、コピーを新しい原証拠としない。
scriptは再実行しない。歴史SHA `4515c5ba…` を現在の値と仮定しない。
新候補は親3278のHEAD `9a80b18358dd58174f1536536d12eb28d79cb806`、
resident tree `f67826d3a0e605b806ef0fdab2290f2a00494e30`、
candidate `bdd1b1f5…` と独立build receipt `faa997ab…` を固定する。
候補を再ビルドせず、共有製品の成功済みテストを繰り返さない。

受領票の型はエンジンの実契約を優先し、domainは
`model-runtime-update-3081-v1`、todo_idは `3081`、target_kindは `resident`。
`data_access_granted=false`、`original_registry_recovered=false` を保つ。
親の依頼番号3278と本check3462を、エンジン既存todo_idへ書き換えて混同しない。
CLIへの任意manifest/hash/source/grant入力は追加しない。

生成前の所有記録は `.ci-work/3462-resume.private.json` に保存する。
readerと原build結果・固定source・原受領票は親の配布まで保持する。
Go testは所有fixture領域に限定し、fixtureの実行所有・作成時刻を記録する。
本番の書込・管理者操作・停止・Task/service/restart・safe-deleteの更新は行わない。

関連Go test、vet、独立reader build、実purpose呼出し、別モデルmaxレビューを
それぞれ原記録と結ぶ。baseline不一致などでqualifiedを得られなければ、
未知hashを新baselineへ加えず、実エラーと原配布証拠を回収する具体的な次の手を親へ返す。
実結果と再開状態は `.ci-work/3462-resume.private.json` と本担当の受領票を正本とする。
本番配布と既存Task/APIのrestartは親3278が所有する。

## 実機の初回結果（2026-10-24 01:12〜01:14 JST）

installed guardと非配布readerはどちらもexit 1、stdoutは厳密に
`{"qualified":false}`、stderrは空だった。正常CLIによる拒否であり、実行審査の拒否ではない。
型付き成功受領票は得ていない。

既存のpin sessionを使う固定targetだけの診断では、旧原source/HEAD/origin/追跡済みbyte、
旧原証拠2本、新candidate source/HEAD/origin/独立build receipt/candidate実byteがすべて通った。
targetもphysical/legacy、通常read-only open、type/size/リンク数1、handle path、
照合中の同一性と最終再照合は通った。一方、固定old SHAとreviewed new SHAのどちらにも
一致しなかった。未知の現在SHAは出力せず、profileへ加えていない。

関連テストはトップレベル22件・subtest込み57件、失敗0・skip0。vet/buildもexit0。
上記の追加実機診断1件は別の実行記録であり、この57件へ混ぜていない。
診断自体のPASSを、qualified成功や配布成功とは扱わない。
原記録は `.ci-work/3462-test.jsonl`、`3462-reader-build.private.json`、
`3462-actual-purpose.private.json`、`3462-diagnosis.jsonl`。

次の有限工程は、元登録範囲にある2492以降の本物のresident配布受領票を確認し、
固定targetとの関係を独立証拠で確定すること。原2492受領票の改変・現在観測値の裸追加・
候補の自己証拠化では解決しない。未知/保護をclearへ変えず、親3278へ実拒否と保持物を返す。
