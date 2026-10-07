//! Stable channel topics; aliases reuse existing log threads during migration.
pub fn canonical(topic: &str) -> Option<&'static str> {
    match topic {
        "麻臭 a.k.a. mash" | "mash-etymology" => Some("麻臭 a.k.a. mash"),
        "XB Bittensor" | "xb-bittensor" => Some("XB Bittensor"),
        "Motocrab" | "motocrab-love" | "【Skills化】motocrab-loveチャンネルの解析" => {
            Some("Motocrab")
        }
        "健康ナビ" | "health-channel" | "【健康チャンネル】動画の作り方と試作" => {
            Some("健康ナビ")
        }
        "EtymoMaster" => Some("EtymoMaster"),
        "AutoDevJapan" | "ADJ Shorts" => Some("AutoDevJapan"),
        _ => None,
    }
}
