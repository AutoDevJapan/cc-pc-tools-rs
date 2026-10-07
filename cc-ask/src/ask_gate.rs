//! Deterministic permission guard. No model output can authorize a question.
use regex::Regex;
use std::sync::LazyLock;

pub const BLOCKED_EXIT: i32 = 78;
pub const BLOCKED_MESSAGE: &str = "聞かずに進めること: 作業に当然必要な操作の許可は質問しません。オーナーだけが決める事項がある場合は --owner-only money|publish|rights|customer|direction|irreversible で理由の種類を指定してください。";

#[derive(Clone, Copy, Debug, PartialEq, Eq, clap::ValueEnum)]
pub enum OwnerOnly {
    Money,
    Publish,
    Rights,
    Customer,
    Direction,
    Irreversible,
}

impl OwnerOnly {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Money => "money",
            Self::Publish => "publish",
            Self::Rights => "rights",
            Self::Customer => "customer",
            Self::Direction => "direction",
            Self::Irreversible => "irreversible",
        }
    }
}

static LABEL: LazyLock<Regex> =
    LazyLock::new(|| Regex::new(r"【[^】]*】|\[(?:Codex|Claude|Scheduler)[^\]]*\]").unwrap());
static PERMISSION: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
    r"許可|承認|(?:して|使って|進めて|続けて|調べて|任せて|消して|行って|試して|止めて|測って|選んで|入れて|開いて)[も]?(?:よい|いい|良い)|よろしい|構いませんか|構わないか|問題ないか|大丈夫(?:です)?か|permission|may i|can i|approve"
).unwrap()
});
static ROUTINE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
    r"タブ|chrome|ブラウザ|画面|計測|測る|測定|確認|ログイン|サインイン|検査|検証|テスト|試験|ビルド|cargo|一時|仮の|片付け|片づけ|掃除|共有枠|利用枠|サブスク枠|再試行|再実行|リトライ|続行|継続|再開|進め|実行|起動|操作|コマンド|修正|注記|実装|モデル|調べ|調査|civ6|docker|ドライバー|test|build|login|retry|continue|temporary|cleanup|inspect|measure"
).unwrap()
});

// Match an owner decision, not just its topic word ("公開サイトでChromeの検査"
// and "所有権の試験" are routine). Negative statements do not grant exemptions.
static OWNER_DECISION: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(concat!(
    r"(?:支払|課金|購入|契約).{0,16}(?:して|許可|承認|同意|決め|変更)",
    r"|(?:有料プラン|追加料金|月額|料金案|価格案|料金体系|販売条件|返金|請求設定|費用の上限|費用・保全範囲|予算)",
    r"|(?:円|ドル|費用|お金|料金).{0,25}(?:使って|払って|かけて|負担して|決め|認め|承認)",
    r"|(?:一般公開|公開範囲|公開するか|公開して|公開の許可|公開の承認|外部公開|外部.{0,12}配布|配布.{0,12}外部|投稿先|投稿するチャンネル|直接メール送付|送信して|送付して|リリースして)",
    r"|(?:著作権|肖像権|利用許諾|利用条件|写真の許諾|ライセンス.{0,16}(?:同意|契約|購入|利用)|規約.{0,16}同意|権利者)",
    r"|(?:顧客|クライアント).{0,25}(?:連絡|約束|契約|納期|請求|承諾|送付|送信)",
    r"|(?:製品|商品|デザイン|画面案|提供案|販売|実況画面|アバター).{0,25}(?:方向|方針|選ぶ|選ん|採用|決め|扱い|変更)",
    r"|(?:方向性|提供開始|料金体系|買い切りプラン|有料提供)",
    r"|(?:取り消せない|取り消しできない|元に戻せない|復元できない|不可逆|永久削除|完全削除|本番db.{0,12}(?:削除|初期化)|本番データ.{0,12}(?:削除|消去)|アカウント.{0,12}(?:削除|解約))"
)).unwrap()
});

/// Unclear/non-permission questions keep the existing behavior. An explicit
/// owner-only reason is an intentional escape hatch, recorded by cmd_ask.
pub fn blocked(title: &str, body: &str, reason: Option<OwnerOnly>) -> bool {
    if reason.is_some() {
        return false;
    }
    let title = LABEL.replace_all(title, "").to_lowercase();
    let body = LABEL.replace_all(body, "").to_lowercase();
    let text = format!("{title}\n{body}");
    if !PERMISSION.is_match(&text) || !ROUTINE.is_match(&text) {
        return false;
    }
    !text
        .split(['。', '！', '？', '\n', '.', '!', '?'])
        .any(|sentence| {
            let sentence = sentence.trim();
            // An explanation that these decisions are absent must not open the gate.
            let negative = [
                "費用なし",
                "費用はなし",
                "追加費用はありません",
                "追加料金なし",
                "課金なし",
                "課金しない",
                "購入しない",
                "公開しない",
                "公開せず",
                "公開は不要",
                "権利の変更なし",
                "顧客への連絡は不要",
                "取り消せない操作はありません",
            ];
            !negative.iter().any(|v| sentence.contains(v)) && OWNER_DECISION.is_match(sentence)
        })
}

pub fn enforce(title: &str, body: &str, reason: Option<OwnerOnly>) -> crate::core::Result<()> {
    if blocked(title, body, reason) {
        Err(BLOCKED_MESSAGE.into())
    } else {
        Ok(())
    }
}
