# ToDo 2765: GoDD PressのCodeQL指摘

対象はPR 175の固定candidate `b2b6d47b7f9e17617d16a9079a70de272bc3c04c`、tree `dac21168dfdb55b007da5c06f36c05e66de0a9f7`、base `4dc5c332d54d19325f122df1f0e4a52e9dd3a629`です。candidate treeのルートにある`AGENTS.md`を読みました。tree内に`CLAUDE.md`はありませんでした。親2642の最新メモも読み、固定candidateとPRの対象が一致することを確認しました。

## 判定

| ルール | 件数 | 実害を確認 | 根拠付き非該当 | 追加確認が必要 |
| --- | ---: | ---: | ---: | ---: |
| `rust/hard-coded-cryptographic-value` | 5 | 0 | 3 | 2 |
| `js/functionality-from-untrusted-source` | 4 | 0 | 0 | 4 |
| **Total** | **9** | **0** | **3** | **6** |

「実害を確認: 0件」は、この有限確認で実害を確定できなかった件数です。実害がないと断定するものではありません。

## 根拠と未確認事項

- 開始時のGitHub API照合で、実repo ID 1386702183、owner AutoDevJapan、private、non-fork、admin権限を確認しました。PR 175はopenで、head/baseのSHAとrefが固定値に一致しました。candidate commitから指定treeが得られました。
- 公式CodeQL run 38893812340は5 jobすべて成功しました。4言語のanalysisは固定candidateに結び付き、errorは空でした。結果はRust 5件、JavaScript/TypeScript 4件、Python 0件、Actions 0件です。
- 指定証拠4ファイルのbyte数とSHA-256は、親manifestと一致しました。SARIFのSHA-256はRust `6ECC565EB09C2062663F74591DF3D3A5645C641C33A9C4C5AA0FE6F3E140DA06`、JavaScript/TypeScript `D3FD55A52FE38A2E7F265BE8060CE909DABD89F2F488D2DA17978CF90D654B3F`です。公式summaryは`409D26808C2337794B9E02CA0EC5D9FEF97DA69CC0E57CB59303E590EA32282D`、log proofは`F3D077C99F8B43B1A4FB915FC6DFFF1CBB98A8CD95D0D9E6C7CAD1D0A782EBE2`でした。公式log proofは1,266,489 byte、SHA-256 `601038a9accdb98b0477451eb32abf8aca05f66f0c24812d757cb9f57ed63fc2`です。
- Rust: 5件すべてがテスト領域のソースで、`#[test]`が付いた関数の中にありました。5関数内にネットワーク呼び出し、プロセス起動、コンソール出力、暗号処理は見つかりませんでした。3関数にファイルI/Oはありません。残る2関数にはローカルファイルI/Oがありますが、固定値が確認したファイル書き込みへ直接渡る形はありませんでした。正確な書き込み先と間接的な値の流れは証明できていないため、この2件は追加確認とします。残る3件は、テスト専用関数に閉じている根拠から製品の暗号処理には非該当と判定しました。
- JavaScript/HTML: 4件は同じテスト領域のHTML blobに対応します。文書には外部script参照が5つあり、SRI属性とCSP meta policyはありませんでした。テストソースから参照されていることは確認しましたが、ブラウザーがこの文書を読み込むかは証明できませんでした。通信の横取り、ネットワーク遮断、接続先の許可リストも確認できませんでした。fixtureがソースpackageや配布物に含まれるかも未確認です。そのため4件とも追加確認とし、テスト領域にあることだけで安全とは判断していません。
- 追跡中にrepository検索APIが利用できなくなりました。Cargo、CI、ブラウザー、外部scriptは実行していません。この確認でテストコードから外部サービスへの通信は行っていません。

## 親側の追加確認案

- Rustの2件は、固定値からローカルファイル操作までの流れを追い、隔離したテスト用一時フォルダーだけに書くことを確認してください。隔離されていなければ、テスト専用の生成値へ置き換え、testビルドの中だけで使ってください。
- HTMLの4件は、固定されたローカルscriptを使うか、外部scriptにintegrity情報を付けてください。fixtureのテスト中は外部通信を遮断し、遮断をassertしてください。fixtureがrelease/packageに入らないことも確認し、入る場合は除外してください。

GoDD Pressのソース、PR、CI、release、SARIF記録は変更していません。製品テストも実行していません。ソース位置とblobの対応は`todo-2765-private-evidence.json`に保存しました。このファイルは今回のcommitに含めず、公開報告にも転記しないでください。

## 再開時の状態

今回の有限確認は完了しました。6件は親側で未確認点を解消してください。この結果を再利用するときはPRの固定状態を再照合し、headが変わっていれば流用しないでください。親2642の完了を依存待ちにはしないでください。
