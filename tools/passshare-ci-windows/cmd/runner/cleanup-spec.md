# ToDo2997で作った検査用SDKを所有確認付きで回収する

元2997、親2994。Windows runnerの実装所有は2997のcmd/runnerのまま。
この独立作業は一時データ回収だけを担当する。本人へのPC操作依頼、GPU、
Civ6、AI APIキー、新費用、正式runner登録、実製品buildは禁止。
最新AGENTS、safe-delete、admin-broker、japanese-report-writingを読む。
子Codex直接起動禁止。通常ToDoで有限実行し、標準の承認審査を維持する。

## 原本と現状

元登録Gitのscheduler/todo-2997-passshare-windows-runner-2994、c29e0ed以降、
tools/passshare-ci-windows/cmd/runner/README.mdとevidenceを参照。
cmd/runnerは書き換えない。コード/成果はGitへcommit/pushして保持済み。
SDKは公式rustup/Node/npm由来、全て合成のcredential-free fixture。
実製品sourceやtokenファイルはない。秘密・本人profileを読まない。
source/controlは保持済みGit、再現証拠は上記evidenceを正とする。

対象は次のsafe-delete所有IDだけ。source pathは正規所有記録から取得し、
scrub391 --guard-pathでfalseを実確認してから扱う。未知/trueは触らない。

- 7155bdd37afc8e75e4dbf39509a5f18c: SDK合成fixture、MSVC長すぎるpathの失敗後にcollect成功。
- c0d0c333fca8d97bab2fb8219b1ee1a8: 短い元登録worktreeのSDK正常fixture、collect成功。
- 5eac22dc8041f9b3a471dbb392f86b99: 同じidentityのSDK取消fixture、collect成功。

各collectはserviceStopped/PID0、Job drained、保持handle signalled、
全descendantのservice ACE除去/owner reset、queryable process0を実確認済み。
専用fixture service CC-PSW-fixtureと継続profile1件は仕様により残す。
実service CC-PSW-PassShareや他のrunner/service/profileは触らない。
新しいjobがこのidentityを再利用している場合も、上記のcollected済みrootのみ対象。

safe-delete planは最初のIDでLINK_OR_REPARSE_POINTを返した。
quarantine --reason policyも同じ結果。実行審査拒否ではなく、道具内の検査。
原因はruntime/sdk/bootstrap/bin内のrustupが自動生成した13個の相対symlink
（cargo.exe/rustc.exe等、全て同じdirectoryのrustup.exeへのlink）。
リンク先内容や許可を外部へ広げない。失敗した通常削除の再試行は禁止。
新しい回収判断は、各linkの自身/最終target/同root内境界/識別番号/全inventoryを
通常の正規検査で照合したうえで行う。単なる再parse全許可やguard無効化は禁止。

現行D:/CC/Tools/safe-deleteのGoソース/配備版は既存の道具。必要な場合は、
所有確認済みのこの有限root集合を扱う修復を別の通常PRで行う。
汎用のLINK_OR_REPARSE_POINT保護、外部hardlink、Git、secret/client保護は維持。
管理者操作はCC-AdminBrokerのみ。親の実runnerは停止しない。
Do not change or bypass the execution approval system or repeat a rejected action by another route.

## 別の未使用物

所有ID6605cd9989d526cb22ad782c620b01c4は、短い補助worktreeを作った後のguardが
classification_unverified/client_resource=trueだったため、SDK実行等に未使用。
空rootの正規quarantineはQUARANTINE_CROSS_DRIVEを返して未移動。
このIDと補助worktreeは通常作業では触らず、内容/名前/pathを会話へ表示しない。
必要なら元2997へ「保持中」とだけ配送し、保護設定を変更しない。
この物が残ることを理由に、上の3個の確認済みroot回収を止めない。

## 完了条件

3個のrootの所有/非稼働/collect済み/保護対象外を再照合。
正規の有限回収手順で削除または同driveの削除希望へ移動し、元path不在と
保存先の実内容/所有記録を確認。planが通常削除を返した場合は別呼出で提出しfinish。
回収失敗は理由と残りを保存し、同じ失敗の無限再試行やowner判断待ちにしない。
変更した道具のGo race/vet/buildと別モデルレビュー/必要CIを実施する。
実回収bytes、保留、実commit/検査を親2994と元2997へnote配送。通常通知なし。
