# 既存Claude Max経路の確認（ToDo2495・親2488）

2026-10-13、既存アカウントによるOpenCodexとGoDD AOのモデル応答を確認した。
今回の担当はOAuth更新・再ログイン・認証ファイルの変更を実施していない。
引き継ぎ時点の401から復旧した原因と時刻は、この確認だけでは確定できない。

## 範囲と実行条件

- 元依頼は親2488の無人運用点検。同じ本人・サービス・Claude Max利用枠の復旧と有限検証。
- 正規CLIの実行場所は `D:/CC/Work/operations-owner-replies-2474`。
- 両作業場所は保護判定で `client_resource=false` を確認した。原保護名簿の復旧を意味しない。
- Gitの実際のremoteは `AutoDevJapan/cc-pc-tools-rs`。この記録以外のソースは変更していない。
- APIキーの追加・指定、追加契約、GPUモデル起動、Cargo、サービス再起動は実施していない。
- 認証ファイル・cookie・OAuthのcode/state/tokenは取得・表示・記録していない。
- 自身の共有ToDo状態はスケジューラーが更新する。独立した確認は共有ToDoで登録する。

## 引き継ぎと現在の状態

引き継ぎでは、Claude CodeはMaxでログイン済み、OpenCodexの既存Anthropic
アカウントは `Refresh failed`、`needsReauth=true`、モデル呼び出しは401だった。
親が正規loginを1回開始済みと報告したが、その完了や復旧原因は未確認である。

今回の正規CLIの観測結果:

- Claude Codeの `auth status --json` は `loggedIn=true`、`claude.ai`、`firstParty`、`max`。
- OpenCodex 2.83.0の既存Anthropic OAuthアカウントは1件、引き継ぎと同じID、active、`Healthy`。
- `account list` と `account current` に `needsReauth=true` は返らなかった。
  未返却を明示的なfalseや今後の更新保証には読み替えない。
- `status --json` の実接続先は10100。CLIと稼働proxyの版は一致。
- Anthropicのpoolは有効。モデル別アカウント固定はなし。
- live catalogで `anthropic/claude-sonnet-5-5` は有効。
- `api explain` のchat経路はAnthropic Messagesへ直接転送する計画で、候補はeligible。
  これは計画表示であり、単独では実応答の証拠ではない。

## 実応答

送信した本文は両検査とも `Reply with OK.` のみ。最大出力16トークン。
コード、所有者のデータ、ログ、認証値は検査入力に含めていない。

| 検査 | 経路・モデル | 確認した結果 |
|---|---|---|
| 正規CLI `access test`、chat | OpenCodex 10100、`anthropic/claude-sonnet-5-5` | 終了0、応答 `OK`、finish_reason=stop、入力43・出力4トークン |
| Responsesの有限検査 | 稼働GoDD AO 10101 `/v1/responses`、同じ明示モデル | 成功、status=completed、応答 `OK.`、入力43・出力5トークン |

GoDD AO 10101の既存health APIは `godd-ao-semantic-proxy`、status=ok、version=0.6.2。
同じlistenerのモデル一覧に上記SonnetとHaiku 5.5、Opus 5.5があった。
稼働listenerへの既存HTTP経路で検査し、worker・GPU・新サーバーは起動していない。

CLIの `godd-ao ready` は `not-ready` を返した。これは既存10101 listenerの
health成功とモデル実応答成功とは別の観測であり、起動管理全体の正常を主張しない。
OpenCodexの起動診断もat-riskで、再起動後の無人復帰はこの担当の確認範囲外。
親2488はモデル認証の現時点の復旧と、起動監視の未解決事項を区別する。

## ブラウザと後片付け

Chromeの既存 `http://localhost:10012` タブを正規Computer Useで選択しようとしたが、
`Tab not found in browser 1`。入力操作は行われず、新規タブは作成していない。
CLI検査で実応答を確認できたため、認証フローの追加開始やブラウザ再試行は不要だった。
自作タブ・一時フォルダー・検査コンテナ・起動プロセスは0件で、個別close対象はない。

## 再開と残る工程

モデル経路の実応答条件は達成した。別モデルによるこの単一文書の読み取り確認は
共有ToDo2496（Haiku 5.5）に登録した。保存済みspecと親2495への関連付けを確認した。
登録はレビュー完了を意味しない。担当起動と確認結果は未受領である。
確認結果を受領し、実害のある指摘を修正して親2488へ結果を返す。
PRはこの記録だけで作り、レビュー後にCI対象の有無と固定HEADを確認してマージする。
新たな401/403が発生したときだけ正規CLIのhealthと現在の手順を再確認する。
既存のGPT経路、review CLI、親のソース、定期タスクは変更しない。
