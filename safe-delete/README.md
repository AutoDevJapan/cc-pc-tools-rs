# 所有付き一時フォルダーの退避

PCで配備済みのGo版safe-deleteのソースと既存検査を、このリポジトリーで管理する。
新規道具はGoを使うPCオーナーの指示に従う。通常のplan・quarantine・finishの
所有照合、Git保護、外部ハードリンク拒否、reparse拒否、項目数上限は維持する。
実行ファイルの隣のreceipts-goを使い、既存フォルダーの所有登録は追加しない。

## ToDo3174の終了済みWindows検査資源

固定ID4378ff571926b0ddbec4ed99368a6405の`quarantine --reason windows`だけに、
正規collect後の原記録・source・公開API・全属性一覧・停止SCM・回収marker・
client guardを確認する資格を設ける。既存claim lockを保持し、SDKのopaque reparse
13件のリンク先を読まず、通常Move-Itemの前後で全3,626項目の実体保持を確認する。
原receipt bytesと属性一覧を退避先へ保存し、保持確認後だけ原receiptを閉じる。
planと他のrootの境界は従来どおり。詳しい受入条件は
[有限な退避の仕様](../docs/todo3174-ci-quarantine.md)に記録する。

## ToDo2307のGemini試写の一時データ

`quarantine --id daab67bf8e2c4e2e7d805c3cf9a06a6d --reason hardlink`
だけに、完成動画と実体を共有する一時名の保持検査を追加する。
元のplanとquarantineは補助コマンド自身がHARD_LINKを返した。OSや自動承認レビューの
拒否ではない。通常の削除を再実行せず、許可設定も変更しない。

元のGo所有記録のチェックサム、project/content/作成時刻、固定のTemp直下パス、
volume 2229119083、rootのfile ID 9570149211999247を照合する。
固定した1784担当worktreeは読み取りだけ。完成音声・字幕映像・JSONは保持する。
client guardで元パスと固定した完成動画を照合してから、通常ファイルの識別情報を読む。

対象は根の中の13項目・27,518,293 byte。
外部ハードリンクはverified-preview.mp4の1名だけで、固定した完成動画
aoi-full-preview.mp4とvolume/file ID（0x100000003bc801）・サイズ9,536,831 byte・
リンク数2・属性・作成/更新時刻が同じ場合に限る。他の共有名、Git領域、
reparse、内容・識別番号の変化は拒否する。外部の名前を列挙・移動・書き換えない。

全項目の識別番号・相対名・サイズ・属性・時刻・リンク数と、全通常ファイルのSHA256を
保存し、移動直前に再照合する。完成動画も読み取りでSHA256とメタデータを照合する。
新しい同じCドライブの削除希望へ、従来の通常Move-Itemでフォルダーを移動する。
移動後に根の識別番号と全一覧・全内容・完成動画が一致した場合だけ、元の所有記録を
quarantinedへ更新する。退避先にpreservation.jsonと理由.txtを保存する。

実行担当は対象を使うジョブ・書き込み処理が終了していることを直前に確認する。
検査後の同一ユーザーによるファイル置換を防ぐセキュリティ境界ではない。
移動後の検証や記録で失敗した場合は、未完了の理由と両方の場所を残す。
元パス不在だけでfinishして完了へ読み替えず、保存したpreservation.jsonを使って調べる。
自動再移動・別経路の削除・所有記録の作り直しはしない。

```powershell
go test -race -v ./...
go vet ./...
go build -trimpath -o <所有付き検査フォルダー>/safe-delete.exe .
```

公開CIはWindowsで同じ全Go検査・vet・buildを実行する。
試験の共有名はt.TempDir内の架空データだけで、実物の所有IDを登録しない。
