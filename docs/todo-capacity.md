# 共有ToDoの容量契約

共有原本の上限は64MiB（67,108,864 bytes）。`cc-job-policy::todo_store` が実読取を上限+1 byteで打ち切り、超過ならJSONを解析せず拒否する。上限ちょうどは許可する。BOMを含めたファイルのbyte数で判定し、解析には読んだ同じバイト列を使う。原本の項目・未知の欄・仕様の参照・needs・subwork・受付識別子・履歴は保持し、縮小や一部読取で復旧しない。

CLIは従来の1スペースJSON保存を維持する。完成した保存文字列のUTF-8 byte数を同じ上限で確認してから、バックアップ・一時ファイル・置換へ進む。schedulerとDiscord復旧処理の直接保存も同じ上限を確認する。新規CLIストアの作成はNotFoundだけを初期状態として扱い、容量超過・破損・アクセス拒否は初期化しない。

受付のGET/POSTと巡回は共通readerを使い、HTTP 503の `todo_store_too_large`、`todo_store_invalid_json`、`state_unavailable` で容量・JSON/構造・IOを区別する。保存が上限を超える受付も同じ容量理由で拒否する。受付原本の16MiBとHTTP本文の128KiBは別の契約で、その上限は維持する。canonicalの照合、delegate/dispatch/subwork、担当・依存・資源・除外の検査は変えない。

関連readerはCLI、受付、scheduler、jobのToDo読取、Discordツールのtodo.json、実画面のoperations、仕事量集計。原本を読む段階では秘密の伏字処理をせず、HTTP応答と保存するプロンプトの境界で従来の伏字処理を行う。仕事量は原本を読めないとき、警告と直前の集計を保持し、一部だけ再集計しない。

2026-10-12の修復時に17,479,319 bytes / 2,444項目 / 92プロジェクトを確認した。JSON最上位はitems・next_id・projects・stall_console_retry_at・stall_observed・stalls。サイズの主因は項目と保存済み履歴であり、受付や履歴の削除は行わない。64MiBは旧16MiBの4倍の有限上限であり、上限へ達したときは保存方式の変更を別途検討する。本文やクライアント名を調査出力へ出さない。

合成JSONの検査で旧16MiB超の全欄保持、正規canonical受付、64MiB境界、超過時の原本/受付保持、破損、IO拒否を確認する。実原本の内容をテスト用に複製しない。
