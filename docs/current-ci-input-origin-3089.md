# 元3059の正式CI入力の出所を照合する

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/281

目的は元3056/3068/3059の同じ正式検査に必要な3入力の取り扱いを証明すること。
一般の保護分類、原保護設定の未復旧、入力内容の機密性は変更しない。
原入力の作成者は前の3059担当であり、helperが生成したとは記録しない。

## 入出力と境界

- 入力は元の停止記録と、独立した元の保存仕様・原証拠への固定参照。
  実行plan、review、acceptanceは元の場所に保持し、コピーや再配置をしない。
- 資格はtask、repo/PR、固定head/tree/base/run/attempt/job、原証拠、期待bytesを
  結ぶ。形式や自己申告hashだけでは資格を作らない。
- 検査した同じbytesを正式helperが消費する。資格の出力は用途と固定hashだけ。
  内容、私的path、秘密値をstdoutやGitへ出さない。
- 元の証拠が足りなければ具体的な不足コードを返し、対象入力を実行しない。
  一般guardの分類を安全/publicへ変更しない。
- 元939の新attemptを取り消さない。元995の実CI成功は元3059の別条件として残す。

## 受入条件

1. 同じ元依頼、独立証拠、固定source/run/job、元の3入力だけが資格を得る。
2. 別task/head/tree/run/job、変更済み入力、未知file、リンク差替えを拒否する。
3. 元証拠の欠落・差替え、許可flagの自己申告、別の場所への包み直しを拒否する。
4. 検査と消費は同じbytesを使い、出力へ入力本文やpathを含めない。
5. 既存のlive API、queue、隔離、runner割当、原source、回収条件を維持する。

## 初期照合

元outcomeとそこが指定する最終receiptのSHA一致を確認。
独立した元3068のreceiptのSHAと、元3056、固定head/treeの結び付きを確認。
原入力3件は正式guardで保護/未分類のまま、本文未読・未コピー。
元の保存仕様の固定hash、36成果とreview/acceptanceとの対応は追加照合中。

共有登録点には着手せず、画像の出所修復3082へ変更範囲を事前配送した。
通常schedulerへの統合後は正式consumer内で照合する設計を選んだ。
repair-guardのadapter候補とその依存を外し、一般の分類や共有mainは変更しない。
CLI受入はproduct-ciのmainを実際に起動して確かめる。

## 独立検査のRed

旧readerは、通常の証拠ファイルにハードリンクを追加しても受け入れた。
その後、同じ作業場所で別の実装担当の変更を確認したため、こちらの検査を
`inputqual/scheduler_link_audit_test.go`へ移し、実装ファイルの並行編集を止めた。

`go test -count=1 -run '^TestPurposeRejectsHardLinkedOriginals$' ./inputqual`
は5件ともRed。各検査は独立証拠を持つ通常fixtureの資格成功を先に確認し、
plan、review、acceptance、元仕様、元receiptのハードリンクをそれぞれ追加する。
入力資格の拒否だけでリンク検査が空振りになる構成を避けている。

実装担当へ検査の所有と統合を配送した。私的pathの公開コードへの直書き、
入力内originalと独立原証拠との対応、失敗時の具体不足理由も確認対象に残す。
これは実装中の独立検査であり、指定された別モデルの正式レビューを代替しない。

## 通常schedulerで統合した資格検証

原仕様・先行receipt・独立3068 receiptに加え、元の有限延期outcomeから
最終candidate receiptとguard-stopへの結び付きを確認する。
停止記録の所有フォルダIDと3つのファイル名を、元所有receiptと照合する。
一般の保護分類・原保護未復旧・入力の機密性未立証は保持する。

原パスはGitの配布sourceへ含めない。実行物の隣の
`current-input-3089.json`は参照だけを運び、コンパイルした各原パスのhashと
独立証拠の期待hashを変更できない。承認flagや新しいhashを書くだけでは資格を作れない。
元入力は原位置から一度だけbounded readerで読み、検証した同じbytesをparserへ渡す。
別位置へのコピーや原入力を包み直す操作は行わない。

他ToDoの古いフィールド形式は今回の4件の照合から除く。
対象4件の欠落・重複・停止・元仕様差替えは引き続き拒否する。
原レビューの対象headと出力hash、acceptanceの独立receiptとの対応も確認する。
個々のJSONをhelperが生成したという出所や、作成コマンド・厳密時刻の確認は主張しない。

元の実入力の照合は`review_independent_payload`で拒否した。
元レビューenvelopeのpayloadと独立最終レビューの対応は未立証であり、
形式や自己申告hashが合うだけで、その3入力を許可しない。

独立した代替経路は、同じ元仕様・outcome・停止記録・元receiptの照合後に、
3068 receiptの最終レビュー記録と受入receiptから正式helper自身がtyped snapshotを構成する。
この経路は元の3入力を開かず、コピー・再配置・再包装をしない。
生成したplan/review/acceptanceのbytesはRAM内に保持し、hash確認後に同じbytesを
公式parserへ渡す。レビューpayloadは元receiptの記録であり、レビュー応答本文を
読んだと主張しない。新しい本人発言や承認も作らない。

`current-pr-origin-qualify`は構成した入力のhash・目的・原証拠参照hashだけを返す。
`current-pr-origin-inspect`と`current-pr-origin-job`は同じ構成器を使い、
既存の実API・queue・runner・隔離・回収の全条件を通す。
資格の成功は原995の正式CI成功ではない。元939の再取消は行わない。

独立Redで確認したhardlink5件と別headレビューはGreenになった。
公式consumerのmainから未知入力を拒否する検査もGreen。
自己整合する別payload、停止記録の別入力、未知locatorも拒否する。
正式CI・別モデルレビュー・merge・限定配布は別途確認する。
