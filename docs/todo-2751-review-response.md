# レビュー2778への対応

固定0377dd0に対するSonnetの原応答を受領し、5件のMustに対応した。修正コードは`bdd9a61c30e09601e2dfe77ecc3e8b407d6a9b50`、module treeは`cc35a1abd029cde20692487783fdda66d5d101a6`。追加レビューの結論はまだ受領していない。

最後に、timeoutを読むためのworkflow取得も、数値repo IDの照合後に限る追加確認を入れた。再レビューの最新固定headは`c1635c9ce37cb07524335130e5e2b040f549fa92`、module treeは`252685436e69fe57bab8e29a682c82e1719b381c`。元5Must修正からの追加差分は、製品実行前のscope照合順とそのunitだけで、実fixtureの実行経路は変えていない。

原応答のSHA256は`CDF80D55E329CFE9B1BC33365E6B39C3EDAB648AA94DA346CD20833CC62D0B01`。実jobは20261017-104232-todo-2778、実モデルはanthropic/claude-sonnet-5-5、succeeded/exit0。原応答と受領情報を`.ci-work/review2778`に保存した。レビュー担当はworktreeの15ファイルを読んだが、gitの起動失敗により固定blobと全差分を照合できなかった。実装側では変更前のworktreeと0377dd0のmodule一致を確認した。これを全差分レビュー済みとは扱わず、次のレビューへ固定差分と全headファイルを直接供給する。

## Mustへの対応

- M1: previewの上限でpipeを閉じる処理を廃止した。全出力をprivate logへ書き、全streamのbytes/SHAを記録する。previewは32MiBで切り詰めても読み続ける。下流の書き込み失敗時もreaderを排出する。preview切り詰めは元Cargo exitと分け、wrapperはexit126で失敗する。33MiB超の実出力でも正常に群を排出でき、完全な原logを保持した。
- M2: 固定workflowのworkflow/job/stepのliteral env、defaults/runとstepのworking-directoryを継承する。環境・場所を再現できない表現やshell内での変更は、製品取得前に拒否する。host側はcallerのenv/pathを受け付けない。元の直列指定を維持し、共有gateより大きい並列数は抑える。`RUSTFLAGS=-D warnings`等の継承、式/保護設定/上限外pathの拒否をunitで確認した。
- M3: Landlock ABIを問い合わせ、ABI3以上でREFERとTRUNCATEを扱う。書き込み可能な範囲だけに両権限を付け、ディレクトリをまたぐmv/lnを実測した。元のRustupコマンドを新しいhomeで実行する検証では、最初にSDKのreadonlyなRustup設置先でself-updateが失敗した。その失敗を保持し、新tmpfsのCARGO_HOME/binへSDKのRustupをコピーしてから、原commandを変更せずに1.95.0とrustfmtの導入・exit0を確認した。imageは変更していない。
- M4: gate-childのdrain記録が無い経路では、そのnamespaceをbrokenとして以後のcallbackを拒否し、mutexを解放する前にowned namespaceを停止する。実fixtureは、kernelで群不在を確認した後に記録だけを欠落させ、安全にmetadata-lossの回収を検証した。任意のhost強制終了に対する原子的なpermit保証を検証したものではない。
- M5: 元jobのtimeout-minutesをliteralで読み、job開始後の全callbackが同じ期限を使う。host全体には準備と排出の猶予を加え、25/26/30分の独自制限で元jobを短縮しない。75分のfixtureで元値の継承を確認した。

## Suggestion等への対応と限界

- S1: namespace停止が確認不能な場合のgate保持は維持した。明示的な拒否の可能性を残したまま停止操作を無限再送しない。これは正常な常駐運用や成功ではなく、記録したnamespaceの不在を確認する復旧保持である。新しいserviceやPC再起動を前提にしない。
- S2: hookは[公式設計](https://github.com/actions/runner/blob/main/docs/adrs/1751-runner-job-hooks.md)でdefault環境を受け、[公開実装のallowlist](https://github.com/actions/runner/blob/main/src/Runner.Worker/GitHubContext.cs)にも必須項目がある。今回の子工程では公式jobを登録していないため、SDK実版での実hook成功は未確認。APIでのrun/job/割当照合は維持し、安易に条件を外していない。
- S3: start時に、固定したjob ID/run/attemptが合い、queuedでrunner名だけがまだ見えない場合に限り、最大5回の有限再確認を行う。別runnerや別sourceの拒否は再試行しない。
- S4: 同じLandlock/seccompで公式Runner.Listener --versionを実測した。初回は祖先の`/`の一覧が拒否され起動しなかった。immutable SDKのディレクトリ一覧だけを許し、fileのread/exec/writeを広げず、起動exit0を確認した。registration/job成功とは扱わない。
- S5: setsid/setpgid等を使う製品テストがHostedと異なる失敗になる可能性は残る。群からの離脱を許すと共有gate外にCargoを残せるため、禁止を維持する。親はこの制限による失敗と製品不具合を区別する。旧製品テストの再実行や合格転記は行っていない。
- S6: stdinで検証・展開した新guestのGitに、認証情報のない固定originを設定した。actions/checkoutの原stepは保持する。後でcheckoutが実行したsourceのHEAD/treeと認証不在は、引き続きhookとhostで照合する。
- S7: payloadの無いUTC無署名commitのbytes再現をunitで確認した。親がAPIで作る新commitはUTCを明示する。任意時差のcommitを別bytesで置換して受け入れない。
- S8: 新containerの8GiB上限と、tmpfsもその使用量へ含まれる点は維持した。今回の小さなfixtureとSDK導入の成功は、製品workspace全件のRAM収容を証明しない。親が正式jobで確認する。未確認を隠すためのskipや予算増額は行わない。
- S9: 原実行receiptへguest ELFとPowerShell assetのSHAを記録した。正式実行の前にsafe-deleteのplanで、実際の所有IDとreceipt rootが一致することを検証する。planは削除を実行しない。
- S10: 原workflowにworkflow_dispatchの定義が無ければ、計画生成時に拒否する。その他の原bytesは変更しない。
- hook pathとRustup引数の取り違え、魔法のYAML kind値、main.goの書式を修正した。Git置換objectを使わない指定も追加した。

[Landlockの公式仕様](https://docs.kernel.org/userspace-api/landlock.html)では、REFERはABI2から、TRUNCATEはABI3から扱える。今回は両方が必要なのでABI3以上を条件にした。同じUIDの任意の悪意あるコードへの完全な防壁を実証したとは扱わない。

## bdd9a61の実結果

Go unitはsubtest込み125 pass / 4 skip / failure0、vet exit0。Windows/Linux buildはともにexit0。4種の新実HTTP fixture（正常、取消し、33MiB超、drain記録欠落）と、元Rustup commandによる新homeへの1.95.0導入は全pass。最終の実fixture合計31.325秒。正式runner登録、製品source、workflow書込、dispatch、製品テストは0。

Windows artifact SHA256: `049B1CEB710698A272AA9D358D620B2409EA608C9C28FEED9BD416BE7557F2A2`。
Linux artifact SHA256: `B97BE2E219A86F26BA1E9E8C799576FB3010E8C087E432A07E75215B64CCC6B0`。

元SDK/Listenerの初回失敗とRustup readonly失敗も保管する。検証担当が自分でこれらを再実行したとは書かない。追加レビューでMustが残らないことと全変更の読了を確認してから、親2736へ最終配送する。

## c1635c9の追加検査

scope照合の拒否がworkflow読取より先であることをunitで確認した。最終Go unitはsubtest込み126 pass / 4 skip / failure0、vet0、Windows/Linux build0。実行経路を変えていない5種の実fixtureは上記bdd9a61の結果を保持し、scope照合の追加だけを理由に再実行していない。

最新Windows artifact SHA256: `C2119F8737FD728423A7AAAFBC9F183DC1498DF1422EAE64A28EE399F7BCF688`。
最新Linux artifact SHA256: `2BA32E68899F05902DA117D022F3FD43A520E51DFB25B478C498A82E310AB94D`。
