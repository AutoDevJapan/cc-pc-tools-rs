# 元3686：発行済みCreateNewのパス照合

元3080の固定manifest39ea・request79f・native call64138で作成済みの新4件28ファイルを、
再作成せず正常consumeするための修復である。原入力450バイト/d843と、親が公開した
837cb35fの仕様・ソフトウェア観測・失敗記録はそのまま保持する。

`skillsCreationCall` は単一await・単一結果の小さい呼び出し言語とstrict JSONを保つ。
同じ実行ファイル、フラグ、引用、全6オプションを照合し、引用されたrequest絶対パスと
workdirだけで連続するWindowsの `\` を1文字として比較する。
大文字小文字・スラッシュの種類は変えず、dot segment、末尾の点/空白、ADS、device、
UNC、相対/drive-relative、混在した連続区切りは新たに許可しない。
ファイルを開くpath、原入力・requestのhash、writer、thread/cwd、時間窓、元の実native、
作成時producer、object ID、現在の同bufferの検証は従来の処理に結んだままにする。
元450バイトの拒否を修正前のproductionで再現し、修正後は同じbyteで成功を確認した。
正規rendererの成功、異なる作用・option・対象の拒否、元証拠のbindingもテストする。
セッションのheaderも再帰的な重複キー検査を通し、type/id/cwd/payload/timestampと
入れ子の重複について、修正前の拒否不足と修正後の拒否を確認する。
完全一致でも同じパス文法を検証し、rendererも同じ文法で読戻せる呼び出しだけを返す。
dot segmentや末尾の点/空白、device、UNCを含む同一cmdの拒否を、実際のcall入口で確認する。

新しいWindows版の正式セルフレビュー4203とラバーダック4204を、同じ最終sourceで
Luna/max・両origin owner_requestとして正常受領する。成功済み4198/4199は凍結し、
元Eab/0345/reviewed4284のreadonly読戻しだけへ保持する。新apply/recoverには使わない。
独立して公開済みのPR624の同じSHA/source一組を両既存updaterに保持し、別sourceや
未知SHAを拒否する。一般guardや原保護台帳、未知中央、647件の取得条件は変更しない。

関連Go raceテスト・vet、正式2役割の全修正指摘解消、最終headのCI、通常mergeを経て、
cleanな同merged sourceの全raw入力をGit blobと前後照合してWindows版を一度生成する。
生成前に、PR627の実MERGED状態、reviewed head、実merge commit、必須14チェックの
SUCCESSと他のチェックの終了・失敗なしを照合して保存する。通常merge後も全build入力が
reviewed headと同じであることを別に検証し、同じソースのCIをmerge SHAだけのために重ねない。
正常verify/applyと既存AdminBrokerで元Eabの実backup・ID/hash・owner/group/DACLを
保持し、配置後の同buffer/CLIを照合する。SACLや過去の物理的連続性を証明とはしない。
親が元request79f/native call64138の正常consumeで同じ28出力のID/hash/bufferを再確認し、
発見と現在の指示の実適用へ進む。これらの実結果が揃う前に完了とは報告しない。
全16資料、次28評価のmain受入、新4件の実利用全体は親3080の残作業と区別する。
