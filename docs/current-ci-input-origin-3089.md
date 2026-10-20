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
実装・検査・レビュー・正式CI・配布・実資格の読返しはまだ完了していない。

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
