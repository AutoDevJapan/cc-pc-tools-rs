# 指定16資料の調査と反映

Issue: https://github.com/AutoDevJapan/cc-pc-tools-rs/issues/258 。確認日: 2026-10-20。調査範囲は各資料のREADME・許諾・構成・明示した代表本文。収録物すべての監査や実行の成功は主張しない。実読パス・固定commit・取得時刻・内容ハッシュは [出典記録](skills-3048-sources.json) に記す。

|資料|扱い|反映先|理由と境界|
|---|---|---|---|
|[Nutlope/hallmark](https://github.com/Nutlope/hallmark)|既存統合|ui-design-styles|ページ構造と部品の範囲を統合。固定テーマ・嗜好スコア・強制全状態は採らない。|
|[MengTo/Skills](https://github.com/MengTo/Skills)|既存統合|web-design-process|独自性を双方の証拠で比較する方法を統合。既存と重なるため別名Skillを作らない。|
|[x1xhlol/system-prompts-and-models-of-ai-tools](https://github.com/x1xhlol/system-prompts-and-models-of-ai-tools)|参考のみ||README、root GPL-3.0、構成を確認。収録promptの真正性・個別権利・技能形式を保証できず、本文を運用へ採らない。|
|[hirokaji/jp-ui-contracts](https://github.com/hirokaji/jp-ui-contracts)|既存統合|ui-accessibility|設計条件→fixture→表示計測→原因層の修正を統合。Python validatorやブラウザ依存を導入しない。|
|[openai/skills](https://github.com/openai/skills)|採用・既存統合|coding-agent-tools, skill-maintenance|既存cli-creator資料と同用途のためcoding-agent-toolsへ統合。Go・OAuth・Windowsへ適合し、個別Apache-2.0とdeprecated注記を保持。|
|[mattpocock/skills](https://github.com/mattpocock/skills)|採用・既存統合|domain-modeling, spec-atdd-tdd-issue-workflow|概念と用語集の境界を追加し、振る舞いのTDDを既存へ統合。毎回の検査境界承認は採らない。|
|[VoltAgent/awesome-design-md](https://github.com/VoltAgent/awesome-design-md)|参考・既存統合|web-design-process|Airbnb例の欠測と第三者分析の位置づけを統合。ブランド固有tokensや素材は転載しない。|
|[obra/superpowers](https://github.com/obra/superpowers)|既存統合|skill-maintenance|適用正例/負例と観測を使う。毎ターン全件再検査、無条件の全体工程、他Skill必読は導入しない。|
|[kepano/obsidian-skills](https://github.com/kepano/obsidian-skills)|採用|obsidian-markdown, obsidian-bases, json-canvas|異なる3ファイル形式を個別の狭い引き金で追加。CLI・プラグインは導入せず、MITを保持。|
|[6mile/skills](https://github.com/6mile/skills)|参考のみ|knowledge-to-skills|歴史的バックアップで悪意ある収録物への注意あり。代表OWASP候補は既存と重なり、個別出所の保証もないため転用しない。|
|[cloudflare/security-audit-skill](https://github.com/cloudflare/security-audit-skill)|既存統合|security-review|候補/実証/追加検証/反証と独立確認を統合。全段階の一律義務化と未依頼の本番通信は採らない。|
|[mizchi/skills](https://github.com/mizchi/skills)|参考のみ||dep-lib-reviewとREADMEの未記載時MITの説明を確認。個別許諾ファイルなし。既存と重複し、dev依存をignoreする表等も一律採用に適さないため転用なし。|
|[docker/skills](https://github.com/docker/skills)|既存統合|docker-operations|BuildKitの秘密、cache境界、Compose準備、config --quietを統合。MCP/agent/sandbox配布は導入しない。|
|[Tencent/teamai-cli](https://github.com/Tencent/teamai-cli)|参考・既存統合|agent-system-design|LICENSE本文のMITを確認。既存GTOを別同期で置換せず、正本・所有・実出力の確認へ反映。CLI未導入。|
|[nateherkai/AIS-OS](https://github.com/nateherkai/AIS-OS)|既存統合|agent-system-design|LICENSE本文のMITと商標留保を確認。新セッションの経路・鮮度・稼働証拠を採り、100点評価や定期実行を追加しない。|
|[https://agent-skills.jp/category](https://agent-skills.jp/category)|参考・既存統合|knowledge-to-skills|分類は探索の入口。各候補の原本と個別ライセンスを確認する経路へ統合。自動分類や件数を品質としない|
