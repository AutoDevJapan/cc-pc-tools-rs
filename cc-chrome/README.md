# Chromeのタブの後片付け

## 担当が実行中でも工程が終わったタブを閉じる

`owner_chrome close <job-id> <record-uuid> <工程終了の根拠>` は、chrome-tabs（またはchrome-emergency-tabs）の記録を1枚だけ選び、従来と同じ所有照合・close・履歴保存を行う。担当の終了まで待たない。UUIDは記録のファイル名から取得する。URLや画面の名前で所有者を推測しない。他のタブには適用しない。

先に記録の全identityを含む終了要求を保存する。接続失敗や保存失敗はfailedとして返し、既存の毎分sweepが担当の実行中でも再試行する。closeの成功はclosed、既に無いことの確認はgone、必要なタブはkeptに分ける。実行中の記録にも読み取りだけの照合を行い、既に無いタブの記録を整理する。照合失敗から不在を推測しない。

オーナーが残すよう明示した納品タブは `owner_chrome keep <job-id> <record-uuid> <オーナーの指示>` で記録する。keepは担当終了後のsweepでも保持し、closeは拒否する。直近の工程で使うタブはclose要求を出さず保持する。finishで実行中の担当のタブを一括終了しない。

ブラウザ操作の工程が終わるたび、作成記録とinventoryのinstance・generation・tabを照合し、不要になったものだけcloseする。終了・失敗経路でも結果を確認する。新しい監視ループやChrome全体の終了は不要。
