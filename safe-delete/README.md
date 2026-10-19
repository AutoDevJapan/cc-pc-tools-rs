# 所有付き一時フォルダーの退避

PCで配備済みのGo版safe-deleteのソースと既存検査を、このリポジトリーで管理する。
新規道具はGoを使うPCオーナーの指示に従う。通常のplan・quarantine・finishの
所有照合、Git保護、外部ハードリンク拒否、reparse拒否、項目数上限は維持する。
実行ファイルの隣のreceipts-goを使い、既存フォルダーの所有登録は追加しない。

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

## ToDo3004: 回収済みSDKの有限な退避

`inspect-sdk --id <ID>` は読み取り検査、`quarantine --id <ID> --reason sdk-links`
は同じドライブの削除希望への保存を行う。対象はToDo2997が収集を終えた
7155bdd37afc8e75e4dbf39509a5f18c、c0d0c333fca8d97bab2fb8219b1ee1a8、
5eac22dc8041f9b3a471dbb392f86b99だけ。元の所有記録のチェックサムを固定し、
rootの識別番号・保護対象外のguard・合成fixture/checkoutと収集証拠を確認する。
退避中はfixture専用のclaimファイルを排他で保持し、サービス停止/PID0を再確認する。
実runnerやサービスprofileには触れない。

13個の相対ファイルsymlinkはruntime/sdk/bootstrap/binの決まった名前だけ許可し、
reparse tag/relative flag、リンク自身の識別情報、文字列rustup.exe、解決後の同一
ディレクトリー内の通常ファイルを検査する。他のreparseと外部hardlinkは拒否する。
Windowsが作ったruntime/home内のINetCache/Content.IE5だけは、同じhome内のIEを
指すmount-point tagと絶対targetを照合して保存する。走査では追跡しない。
絶対targetの文字列を変更せず保管するため、退避後は元pathを指して参照が切れる。
移動後はIEディレクトリー自身の保存済み識別番号と全内容を照合する。
汎用のGit保護は維持し、この退避に限り2個の合成Git領域を全内容保存の対象にする。
元のソース/controlはGitに保持済みで、これは実製品のcheckoutではない。

全inventoryの境界とhardlinkを検査してから通常ファイルをSHA256で照合する。
移動前に識別番号・相対名・属性・サイズ・時刻・リンク数・target・全ハッシュを
preservation.jsonへ保存し、直前と移動後にも同じ情報を比較する。
通常のMove-Itemだけを使い、元path不在と全内容保存を確認した後に所有記録を
quarantinedへ更新する。削除量ではなく退避量を報告する。
検査失敗時の別経路削除や自動再移動は行わず、両方の場所と失敗段階を保持する。
同一ユーザーによる敵対的な並行置換を防ぐセキュリティ境界ではない。
