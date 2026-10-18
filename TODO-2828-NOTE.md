Windows接続の実装と関連検査を終え、別モデルのレビューを待っています。

実装候補はbec99388b45319bfed7e3db67f1cbd24422883fdです。
treeは6c5c18608b0d7940178baaa97938e6dd0a8fb5cdです。
作業ブランチへcommit/pushし、GitHubの同HEADを確認しました。
共有schedulerのcheckpointも実HEADで成功しました。

新しいGoモジュール、SDKの最小callback、固定seed、通信手順、stock runnerの
設定と開始/完了hook、Job Objectによる取消し・子孫終了確認を実装しました。
Windows上でrace/vet、仮sourceの物理照合、実TLS・pipe・テスト子プロセスを確認しました。
元SDKの準備や版確認、原source取得、実VM/runner起動、重Cargoは実行していません。

別モデルレビューはToDo2830へ通常登録しました。指定モデルはgpt-6-lunaです。
まだ受付待ちで、レビュー完了やMust0は未確認です。
古い8858544ではなく、上記bec99388の固定HEADをレビューするspecへ更新し、
保存済みの原specと更新specを読み返しました。子Codexの直接起動は行っていません。

2830の終了後に同じ2828を自動再開し、実際のモデル・対象版・読んだ範囲と原応答を
照合します。Mustを直し、関連検査の後に最終pinと引き継ぎを親2737へ返します。
親2737の完了は待ちません。独立PR/mergeは作りません。

公開型、各段階、戻り値、offline staging、service名、COM/TLS、固定環境pathは
tools/windows-ci-runner-2737/README.mdとHANDOFF.mdに保存しました。
親にはrunとroleを含むlabelへの接続、新clone、実公式runner、原13工程と
公式completion、VM/登録/host lock回収が残ります。合成検査を実受入とは扱いません。

再開記録はTODO-2828-CHECKPOINT.mdです。
検査用の所有一時領域は通常削除後にfinishで不在を確認しました。
本人のPC操作や技術的な判断は不要です。
