Windows接続の初回レビューを受領し、提案2件の修正と関連検査を終えました。

実装候補はb60e85e7f66abb3329264bfc74895a3d3c2be10fです。
treeはe192d0a34d9c546e146575681d3d9365c6185ee0です。
作業ブランチへcommit/pushし、GitHubの同HEADを確認しました。
共有schedulerのcheckpointも実HEADで成功しました。

新しいGoモジュール、SDKの最小callback、固定seed、通信手順、stock runnerの
設定と開始/完了hook、Job Objectによる取消し・子孫終了確認を実装しました。
Windows上でrace/vet、仮sourceの物理照合、実TLS・pipe・テスト子プロセスを確認しました。
元SDKの準備や版確認、原source取得、実VM/runner起動、重Cargoは実行していません。

ToDo2830は固定bec99388の全23ファイルを読み、Must0でした。
原応答3件とhashを保存し、初回の未読と後2件の完了を区別しました。
実行記録でも実装側gpt-6.1-solとレビュー側gpt-6-lunaの相違を確認しました。
原応答に残るモデル未確認という記述は改変せず、受領側の補足を記録しました。

提案された診断探索の総件数・深さと、失敗報告のrunnerID・排出状態を修正しました。
追加回帰が成功し、変更モジュールのrace検査4.280秒とvetも成功しました。
変更していない共通モジュールの有効な検査結果は再利用しています。
追加差分6ファイルだけの確認をToDo2833へ通常登録し、保存したspecも読み返しました。
現在はそのレビューの受付待ちです。子Codexを直接起動していません。

2833の終了後に同じ2828を自動再開し、実際のモデル・対象版・読んだ範囲と原応答を
照合します。Mustを直し、関連検査の後に最終pinと引き継ぎを親2737へ返します。
親2737の完了は待ちません。独立PR/mergeは作りません。

公開型、各段階、戻り値、offline staging、service名、COM/TLS、固定環境pathは
tools/windows-ci-runner-2737/README.mdとHANDOFF.mdに保存しました。
親にはrunとroleを含むlabelへの接続、新clone、実公式runner、原13工程と
公式completion、VM/登録/host lock回収が残ります。合成検査を実受入とは扱いません。

再開記録はTODO-2828-CHECKPOINT.mdです。
検査用の所有一時領域は通常削除後にfinishで不在を確認しました。
本人のPC操作や技術的な判断は不要です。
