# civ6-ci-linux 作業記録（ToDo2964 / 親2962）

## 完了
- 新規 module tools/civ6-ci-linux（Go）の本体: plan / contract 境界 / api 許可リスト / docker 名前空間 / 容量確認 / policy / source / gate / guest / run / main
- 単体テスト: plan, api, policy, gate（go test 通過）

## 残り
- docker・queue・run・source・guest の単体テスト、実コンテナの正常/取消 fixture
- contract_bridge（-tags civ6contract）の go.work 確認
- README、別 workflow（civ6-ci-linux.yml）
- 別モデルレビュー（共有ToDo --spec）、PR、CI、merge
- 親2962 の note へ結果を保存

## 方針
- 親 contract が未着のため default build は fail-closed（unwiredContract）。-tags civ6contract で親 module に接続する。
- 実 source 取得・runner 登録・製品 Cargo・CodeQL・route 変更はしない（親の統合工程）。
