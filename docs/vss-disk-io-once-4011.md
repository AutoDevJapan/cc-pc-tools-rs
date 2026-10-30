# ファイル名を残さない単発のdisk I/O診断

`tools/vss-4011/diskio-once` は、保持失敗の切り分けに使う20秒だけの診断候補。
まだ実機でtraceを開始していない。テストやビルドの成功を、実測成功とは扱わない。
新しい監視・常駐・定期タスクとして登録しない。

## 測定範囲

通常のC/D volume extentを取得し、DiskNumber・ByteOffset・TransferSizeで区画を区別する。
リアルタイムのSystemTraceProviderでDISK_IOとNO_SYSCONFIGだけを指定する。
ETLのパスは指定せず、ファイル名・ファイル作成・プロセス開始・コマンドラインのイベントは有効にしない。
callbackはDiskIoのWriteだけを選び、TDHで4個の数値を読む。生payloadやFileObjectを保存しない。

IssuingThreadIdごとにメモリ内で集計し、終了時にthread/processの作成時刻を確認する。
現在のthreadが最初のイベントより後に作られていれば再利用の可能性があるため未帰属とする。
実行物のパスは分類にだけ使い、出力するのは区画・既知の分類・バイト数・イベント数だけ。
終了したthreadや権限不足は未帰属にし、名前や私的パスを出さない。
Systemの遅延書込みを元アプリへ戻す推定は行わない。

## 資源と終了

説明的な固定のセッション名を使い、別の実行が既にあれば上書き・停止せず開始失敗として終える。
新しいGUIDで今回の所有を識別し、終了時はGUIDを照合したnative handleだけを停止する。
同名でもGUIDが異なるものは保持する。メモリ集計のkey数は最大4096。
通常は20秒でSTOPし、残りのcallbackが終了してから集計結果を読む。
別の有限helperがpipeの終了や期限を受けて今回のGUIDだけを照合し、異常時の回収を補助する。
このhelperも常駐せず、タイマーの上限は75秒。開始・STOP・buffer排出・helper終了・セッション不存在を別々に記録する。
OS停止やnative callが戻らない場合の回収は未検証であり、無人障害復帰を保証しない。

落ちたイベント、未解釈、key上限、区画変更、未確認のSTOPや排出があれば不完全とする。
イベント0を「書込みなし」の成功へ読み替えない。
完全な短いsampleでも、過去の25の書込み元や根本原因を証明するものではない。
その場の数値と、前後のVSS使用・割当・イベント照会を照合して次の切り分けを決める。

## 実行手順

固定版の別の確認が終わるまで、実機のtraceを開始しない。
実行は既存管理者窓口から `run-once.ps1` を呼び、ActorJob・SourceHead・ReviewTodo・新しい絶対NativeExeを渡す。
実HEADとclean source、source hash、読取共有handleを確認し、そのsourceからfresh buildする。
一度きりのrequest/resultを追記専用で保持し、結果不明でも同じ工程を再試行しない。
trace自体はVSS・保存先・上限・予約領域・OSの設定を変更しない。

ABIのsize/offset、区画の境界とoverflow、匿名分類、非数値propertyの拒否、
同名別GUID・query失敗・不存在の保持を関連テストで確認する。
実機ではSTOPとセッション不存在まで確認し、未帰属量が大きければ書込み元を名指ししない。

一次資料:

- [DiskIo_TypeGroup1](https://learn.microsoft.com/en-us/windows/win32/etw/diskio-typegroup1)
- [SystemTraceProvider](https://learn.microsoft.com/en-us/windows/win32/etw/configuring-and-starting-a-systemtraceprovider-session)
- [StartTrace](https://learn.microsoft.com/en-us/windows/win32/api/evntrace/nf-evntrace-starttracew)
- [ControlTrace](https://learn.microsoft.com/en-us/windows/win32/api/evntrace/nf-evntrace-controltracew)
- [ProcessTrace](https://learn.microsoft.com/en-us/windows/win32/api/evntrace/nf-evntrace-processtrace)
- [TdhGetProperty](https://learn.microsoft.com/en-us/windows/win32/api/tdh/nf-tdh-tdhgetproperty)
