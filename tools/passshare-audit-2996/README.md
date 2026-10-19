# PassShareの依存API受入（ToDo2996）

これは監査修復の補助検査で、製品ソースや製品本体の正式CIではない。
固定候補と一次資料、実行結果は`docs/todo2996-audit-progress.md`に記録する。

主要依存を候補と同じ版へ固定し、updater登録、ZIP展開と親パス拒否、plistの
XML/binary往復、両reqwestのHTTPSを検査する。APIへのHTTPS GETは公開crate情報だけを
読み、通常の証明書検証を使う。認証値、APIキー、個人情報は送らない。
Tauriのbuilder生成だけで、window/event loopやGPUを起動しない。

この独立manifestと製品の依存graph全体が同じという証明ではない。quinnは製品で
optionalのため、受入manifestで同版を直接有効化してコンパイルを確認する。
Windows上の独立test harnessへCommon Controls6の依存manifestを埋め込む。
通常の製品側はtauri-buildが組み込むため、製品ファイルの追加変更は不要。

再現する場合はCargo割当と共有ゲートを使う。専用の所有付き作業先へこのフォルダと
検査証拠`.ci-work/acceptance-Cargo.lock`をCargo.lockとしてコピーする。
本工程の生成先は同じ所有rootのtargetを使い、次の順序で実行した。

```text
cargo check --locked -p quinn-proto -p reqwest@0.12.28 -p reqwest@0.13.4
cargo nextest run --locked
```

doctestは0件。補助コードの実行で製品の正式CI成功や配布を主張しない。
ソースのライセンス・署名・公開設定を変更する機能は無い。
