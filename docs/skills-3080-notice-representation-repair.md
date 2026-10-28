# 元3686：個別noticeと改変表示の有限修復

元3080/Issue283の固定PUBLIC `74f4eff6a10b5bf235e4028ada75232a5f348bb6`、
manifest `5ab55cfca8778374e86862905e9c39d5bfe11bea2ae6e3e33ec82f82e7607f89` に結ぶ。
旧150件と成功済み21出力は保持し、中央の作成は親3080だけが行う。

6mileの固定commit `920714f17d81e0a95c7a9bc51faf0a7921cd982d` にある
`skills/skywyze/rlm-controller/LICENCE.md` は、取得indexの `support_text` を変更しない。
同じrepository/commit/path/role/10752 bytes/SHA256/Git blob/content pathが全て一致する
一記録だけを個別noticeとして照合する。明示licenseと同packageの暗黙noticeに同じ判定を
用い、catalog/index/tree/commitの全体pinと、出力内のnotice全byte保持を引き続き要求する。
別のsupport_textや名前だけのnoticeには適用しない。
テスト用noticeは親の固定公開manifestから同一byteで保存し、SHAとGit blobも確認する。
親が直接読まなかった2行の意味や独立著者確認を、この機械照合で読了扱いにしない。

参照には依頼で指定したASCIIの改変表示1行とLF空行だけを任意で先頭へ付けられる。
照合時にその完全一致prefixを一度だけ分け、同じpayloadに従来の原行または同位置の
WITHHELD marker、元行数、末尾LF、UTF-8/NUL/CR、実可視行と新たな伏字の双方という条件を
適用する。改変・未知・重複prefix、新本文、marker移動、notice自体の伏字は認めない。
旧prefixなしの発行と現在bufferの読戻し契約を変更しない。

Redでは正しい個別noticeが `source_notice_or_catalog`、正しい注記付き参照が原行照合で
拒否されることを確認した。現在のPR610配布物も既存両updaterのbaselineから欠落していた。
親の候補生成前観測と、このwriterの同handleによる現物観測は、
`9be9a066053183a099e78133571d394ecbb2845372e3cf603e292c74dac1a8d3` /
source `deb430b8b089fde2391dbd243c01db48de48d493` / 13,315,072 bytes /
Windows amd64 / clean buildinfoで一致した。この既受入softwareの一組だけを両比較へ保持し、
未知SHA・別revisionは拒否する。候補から旧baselineを申告させない。

関連Goテストは固定tuple各項目の不一致、同名偽notice、暗黙package noticeと全byte保持、
旧正常系、固定prefixとencoding/末尾LF/marker/本文の負例、両software比較を対象にする。
この修復のためにsourceを追加取得したり、中央本文や647件の取得保留を開かない。

配布には同3686の未凍結の既存reviewを別々のselfとラバーダックとして通常schedulerで使う。
原originと旧spec/job/nativeを保持し、今回の実作者・実launch親を分けて照合する。
成功後凍結した3712/3896と親の4129/4083を編集・再起動しない。
両実Luna/max・両origin owner_request・同最終head・原native/cwd/時刻・全指摘0と、同headの
CI成功後だけ通常マージする。親の4184/4115はsoftware配布のレビューへ流用しない。

受入済みmerged sourceの全guardビルド入力を照合し、cleanなWindows CLIを一度生成する。
実writer3686の `--verify-skills3080-update` が正常に通った同候補だけを、既存のCC-AdminBroker
と同じソースでレビューしたハンドル保持の置換手順で更新する。現在SHAのCAS、旧実backup、owner/group/DACL、配置後の
hash/buildinfoと実CLIを独立して読み戻す。SACLや歴史的物理連続性は証明しない。
不明な現物を上書き・force rollbackせず、失敗時は同じ原operation記録と実backupを照合する。

source/レビュー/CI/mergeとWindows実配布・実CLIは別々の実結果として親へ返す。
親自身による新4案のCreator-bound全文レビュー、28buffer/ID、不在、通常CreateNew、
発見と現在指示の明示適用は別の受入工程であり、software修復だけで元16資料全体や
中央配置の完了とは報告しない。旧source、作成時ID/hash、原spec/log/native、backupを保持する。

## Windows置換の競合を同じ範囲で解消する

ラバーダックが指摘した、旧SHA確認とFile.Replaceの間の競合は、合成Windowsファイルに
別byteを差し込むRedで実再現した。旧v3 scriptと原レビューは変更せず保持する。
新しいdeploy3686は通常のverifyが受け入れた同merged sourceから生成し、同じreviewed
全guard入力へ含める。新しいデータの資格確認や任意ファイルの置換には使わない。

v4 wrapperは実writer・既存の正常verify・候補byte・helperの実hash/sourceを照合する。
Go helperは固定scrub391 target、そのnonceのstage/backup、同writer、同clean sourceと
作業場所内のoperationだけに限定する。旧現物とstageをREAD+DELETE、書込み/削除共有なしで
開き、全物理親を保持し、同じhandleのSHA・owner/group/DACL・実IDを照合する。
初回renameの前に実IDをnative journalへCreateNew+Flushして残す。

同じ旧handleをbackupへno-replace renameし、同じstage handleをtargetへno-replace rename
する。公開の隙間で第三者がtargetを作ると衝突として失敗し、そのtargetを上書きせず、
既知の旧backupとstageを保持する。正常時のbackup IDは元ID、新target IDはstage IDと一致する。
異常時の復旧も同じhandleのno-replace renameだけで行い、PS側のpathでhash確認してから
backupを動かす処理は廃止する。物理的に一度も不在にならない置換とは主張しない。

独立したwrite/rename/親renameの拒否、公開直前のforeign CreateNew、old移動後・新公開後の
注入失敗と同じ元IDへの復旧、未知current/stage、既存backup/journal、hardlink、先行readerを
実Windows上で確認した。broker期限でprocessが終了した場合は自動で成功扱いせず、
原operation/native ID journalと現物・backup・stageを同担当が照合して復旧する。
未知の現物をforce rollbackしない。元16資料の取得・中央書込み・一般guardへ広げない。