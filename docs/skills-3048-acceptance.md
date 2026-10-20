# ToDo3048の受入記録

元仕様: 3048-372030eaaf114c6aaaac9a69ba9a8400.md。Issue [258](https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/258)、PR [263](https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/263)。

## 最終構成と測定

D:/CC/Skillsへ新規4件と既存10件の改善を実配置した。新規はObsidian固有Markdown、Bases、JSON Canvas、業務概念の整理。新規ディレクトリの不在は2026-10-20 03:35:10 UTCと配置直前に確認した。CLI設計は既存coding-agent-toolsのcli-creator資料と同用途と判明したため既存へ統合し、今回だけ作った別名を.trashへ退避した。最終manifestと新Codexの発見記録には退役した別名を含めない。

|指標|変更前|変更後|範囲|
|---|---:|---:|---|
|現役直下SKILL.md|266|270|ドット配下・synced・退避を除く|
|.system直下|5|5|未変更|
|直近7日更新の現役直下|4|18|親の初回測定と実配置後の測定|
|既存10件の入口bytes|50,791|13,290|同じ10ファイル|
|新規4件の入口bytes|0|3,924|4ファイル|
|最終入口の最大bytes|—|2,812|14ファイル|
|対象のcatalog行bytes|1,858|2,493|name・description・pathの同じ代表形式|

新規4件のcatalog行は650 bytes、既存descriptionの変更を含む対象全体の純増は635 bytes。[測定式](skills-3048-metrics.json)を記録した。これはUTF-8の代表行の量で、トークン数やモデル全体の予算ではない。入口の短縮も応答品質や速度の改善率を実測したものではない。

既存10件の変更前全文は.trash/todo3048-originals-20261020へ保持し、すべて変更前SHA-256と一致した。CLI資料と索引の変更前も同所へ保全した。従来の詳細は対象のreferencesへ分離した。元のCLI資料にあった新規Python・Rust既定・APIキー引数等は現行のGo・OAuth方針に合わせ、過去のBlender試用の事実は保持した。

3つのエージェントのskillsパスがD:/CC/Skillsへのjunctionであることを確認し、変更していない。対象とreferencesは通常のディレクトリ。既知のGTO registryのslugとの一致は0件だった。これは全サーバーの所有監査ではない。管理された配布物と暗号化registryは変更していない。

## 16資料と適用範囲

[採否表](skills-3048-research.md)と[固定commit・取得時刻・実読パス・ハッシュ](skills-3048-sources.json)を残した。README、許諾、構成、代表本文の関連節を調査した。収録物すべての監査は主張しない。6mileのtree取得は切詰めありと記録し、明示した代表本文で採否を判断した。新規のpublic由来Skillは固定commit・許諾原文・適用上の変更を保持した。

内部prompt、バックアップ集のSkill本体、ブランド固有tokensや素材は転用していない。mizchiのREADMEにある未記載時MITの説明を確認したうえで、既存との重複と適用条件を理由に参考へ限定した。Web分類は配置前に閲覧し、配置後のHTML再取得はハッシュ用と明記した。JSON CanvasとObsidianの公式仕様は16資料とは別の補助出典として記録した。

未知のinstall、新規MCP、追加課金、AI APIキーの呼出しはない。補助コードは標準ライブラリだけのGo。文書の検査のために製品全体やRust workspaceのビルドは追加していない。

## 構造Red/Greenと実配置

最初の新規5候補で配置前に構造検査を実行し、未配置が理由のexit 1を確認した。CLI候補は後で既存へ統合した。実出力の抜粋:

```text
obsidian-markdown/SKILL.md: missing/unreadable
obsidian-bases/SKILL.md: missing/unreadable
json-canvas/SKILL.md: missing/unreadable
domain-modeling/SKILL.md: missing/unreadable
exit status 1
```

CLI資料の統合時には、Skill内で有効な親への相対リンクを誤って拒否する欠陥も検査で先に再現した。

```text
--- FAIL: TestParentReferenceInsideSkill
valid parent reference rejected: [sample: link path escape]
FAIL
```

基準をリンク元のフォルダからSkillのルートへ直し、有効な親参照は通し、Skill外への参照は拒否する検査を通した。

最終版で実行する検査は次の3つ。実結果は固定候補の記録とCIで対応付ける。

- `go -C tools/skills-3048-check test ./...`
- `go -C tools/skills-3048-check vet ./...`
- `go -C tools/skills-3048-check run . --manifest ../../docs/skills-3048-manifest.json`

実配置は14件・70ファイル。name、60字以内の日本語description、短い入口、全manifestファイルのSHA、旧本文を含む相対リンクを検査した。絶対パスや外部URLの全到達性はこの検査器の対象外。正例/負例の機械検査は、manifestに非空で異なる2つの依頼があることだけを確認する。適用の意味は独立レビューで別に確認する。

CIが確認するのはGo検査器の反例と公開receiptの形式・有限な対象集合で、D:/CC/SkillsをCIから検査したわけではない。実配置のハッシュ・相対リンク・descriptionはこのPCで別に検査した。公式Python validatorは配布Python環境が確認できず未実行。代わりにGo検査とCodex自身の解析を実行した。追加のPython環境は導入していない。

## 発見と文脈予算

新しいapp-serverへinitializeし、skills/listをforceReloadで実行した。[発見記録](skills-3048-discovery.json)では14件有効、解析エラー0、観測された警告0。退役したCLI別名は0件。モデルは呼んでいない。

一覧RPCはターンのプロンプトを組み立てないため、それだけを文脈予算の証明にしない。初回の独立レビューは実ターンを開始し、実行時turn_contextでanthropic/claude-sonnet-5-5を確認した。skills context budget警告は観測しなかった。モデルのservice tierに関する別の警告はあった。全モデル・将来の全catalogに対する予算適合の保証はしない。

## 独立レビューと修正

[初回原応答](skills-3048-review-initial.md)は18件の依頼を独立判断し、67ファイルのハッシュ、description、相対リンク等を読取で照合した。原応答が指す5新規・9既存は初回候補であり、最終構成とは区別する。

Mustは2件: レビュー中の未コミット文書との差、入口から落ちた保全・顧客秘密保護。前者は最終版を固定して再レビューし、後者は入口へ復帰した。意味のある追加指摘も反映した。

- CLI生成の重複を既存へ統合し、旧CLI資料の言語・認証方針も修正した。
- ハッシュ必須を既定とし、旧本文の相対リンクも検査する。空files・入口欠落・依頼欠落・重複target/file等の反例を追加した。
- sourceとtargetの対応を大小文字を正規化して検査し、Web出典もmanifestへ追加した。
- 補助出典、Web再取得、catalog行の量、CIと実配置の検査範囲を明示した。
- ObsidianのブロックIDの文字制約を公式で確認して追記した。
- 画像案の担当Skillを示し、仕様開発Skillの詳細を参照へ移した。

既存の専門Skillと同じ短い要点は、関連資料を選ぶための説明として扱う。別名Skillは増やさず、旧本文は履歴として保持する。コピー件数や機械検査の合格を適用の品質の代わりにしない。最終の再レビュー結果はPRと元ToDoへ記録する。
