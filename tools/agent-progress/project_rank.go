package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Rank calculation is independent of the public-name allowlist. Exact policy
// aliases are encoded as SHA-256 so protected aliases are not copied into this
// module or diagnostics. This is not a secrecy guarantee for guessable names.
// Port of cc-job-policy priority and product-identity rules; synthetic behavior fixtures guard this boundary.
// Canonical LF source SHA-256: fd4fd0a71c1438d77f67c7a0c5405495fc856bba34c4171c81cda6f6035472ac.
func rankDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func projectRank(project string) int {
	if r := mcpNamed(project); r.scoped {
		return r.rank
	}
	p := strings.ToLower(project)
	normalized := strings.NewReplacer("α", "alpha", "-", "", "_", "", " ", "", ":", "").Replace(p)
	if rank := normalizedRanks[rankDigest(normalized)]; rank != 0 {
		return rank
	}
	if rank := exactRanks[rankDigest(p)]; rank != 0 {
		return rank
	}
	if strings.HasPrefix(p, "godd-press-") {
		return 61
	}
	if strings.HasPrefix(p, "godd") {
		return 67
	}
	if strings.HasPrefix(p, "cc-") || strings.HasPrefix(p, "opencodex") {
		return 30
	}
	switch p {
	case "tools", "pcツール", "pcの道具", "常駐", "makhmeto", "localai-web-rs", "chatgpt-web", "chrome-bookmark-worker":
		return 30
	}
	for _, part := range []string{"youtube", "health-channel", "adj-daily-shorts", "x-skill", "xブックマーク"} {
		if strings.Contains(p, part) {
			return 40
		}
	}
	return 100
}

var normalizedRanks = map[string]int{
	"eca194cd88616b628d635f9ec7a9a40439fe5a01df6a8ab249db20724009564c": 10,
	"0a9d4ce4a27ee04539c8919e25f8c766b9a46d888edc119a2b8c0504e73e32a9": 10,
	"e78c024b191e29603ad54d0aeb05663a83bb5c68c63a40f6307f5346f7586ea3": 10,
	"65d7963612cff58c2dcecc7af3f0f919e3193c45fac3bc49e2ce0448f2906206": 20,
	"0d83f2331cc4795ec0ffd73ef85abb90319abdc5028150914ab9b9779e8affa3": 20,
	"556ef87a42924d67cc0ea146c9f3c7a74f247507c1588deb9c6ab697a2e38cdf": 20,
	"8ea6fe30ead4deb7365d5316c15da1c7f9d23e97036d3a65455a0627d9eeec71": 20,
	"cd18cef9f9b254c21bbf8efdda783e4ad2ef868c5c7d887e5658216867ad68c7": 20,
	"32c1cf49a2feee269ed74dd860f72644ac71029839b21b1ce0c212efafa9ef07": 50,
	"9c53c074d7ac6a2728b638ac1f376c5fa9eb8f71603017c3ea638c2fd40548df": 50,
	"49734ead28003f469f7af9d6a8cc4d5d91a056953c761571ae986781f419f353": 60,
	"6cbc38b24e26621dd34f465a272e2cf372fcb3ad75485824c1fd891bb00e34fb": 61,
	"43326538e858c5871c842efbeebd2e10167acdfe060aa5332041da46e6cddcc5": 62,
	"2c93d8a4c0ae8e65b66d727513dc4c0eeb11c187d3494472fbb01a9134ecbb1d": 63,
	"809c9becbf00976a8994d3829af08b5625e0054a0c9159c3335be657a23a80ee": 63,
	"d97947b1bf1abbbf087cb8c08914a1663357a3a9ac817d838b1e1ea479190805": 64,
	"aa6f81a2585f0d5aa39c8bb44c996b60a8507aca78a90e626aee2f238707cb9b": 64,
	"a5c66fb045e1970383f02a542178051e9ea2983d813fab199838a6565957c3c3": 65,
	"aeebad4a796fcc2e15dc4c6061b45ed9b373f26adfc798ca7d2d8cc58182718e": 65,
	"d4c0f359eac589fc7c903f368ad23e6962b756f2c9ad039d0a8628dcb74fc1db": 66,
	"8047156f8ae1b4f6f120cd6d6a447a7175e3cb1ca966ccfe59dafbf1167da47f": 66,
	"2c6bcde9c408b188eff88743e9e1f5c4009e020a2e86da2e0fc606888dc23cc6": 30,
	"adba48111b2717c135e4d6b2beddcc5fc38e5a38b2b848ac3d7ea6ddc5342391": 30,
	"46f69732f7871aef9fe7d60db7fa1b3fba31c1f9c9dbc3f5be3915782d443f05": 30,
	"82d52541945b1ace3929b72132861321824ec9eb8ecdebc97c9c20e6ce4de4b0": 30,
}

var exactRanks = map[string]int{
	"0a9d4ce4a27ee04539c8919e25f8c766b9a46d888edc119a2b8c0504e73e32a9": 10,
	"e78c024b191e29603ad54d0aeb05663a83bb5c68c63a40f6307f5346f7586ea3": 10,
	"1e3ec1975099fdb31ed8547d9fd72e0f5afaa4c5c5436860f0062c99b6350dbd": 10,
	"7302b991b2f5d921eea0bd4c1935f6ab7770b4407253f077c5afce27d3d3c3df": 10,
	"65d7963612cff58c2dcecc7af3f0f919e3193c45fac3bc49e2ce0448f2906206": 20,
	"c0fb3c41b8e7f247d6eea53b7d2c746287afc136fc06a7a3fded4563beac796b": 20,
	"556ef87a42924d67cc0ea146c9f3c7a74f247507c1588deb9c6ab697a2e38cdf": 20,
	"8ea6fe30ead4deb7365d5316c15da1c7f9d23e97036d3a65455a0627d9eeec71": 20,
	"32c1cf49a2feee269ed74dd860f72644ac71029839b21b1ce0c212efafa9ef07": 50,
	"9c53c074d7ac6a2728b638ac1f376c5fa9eb8f71603017c3ea638c2fd40548df": 50,
	"8825eba876d7bad46cfce9d457c5118f9d04f1e366594a7626bdd02ee10a0238": 60,
	"8641cfbaaaa29753a1c3d492be37a202967b1122cdb1a4d36707479b27dcdd90": 61,
	"b4c6059c32b63226e9334d67a3fa06b1fbb0f1f22247317da6aebed2c5fff945": 62,
	"809c9becbf00976a8994d3829af08b5625e0054a0c9159c3335be657a23a80ee": 63,
	"1bd5f964a32fac28062af7634cfc17460e06f4604692d6d1a52b70bb7e07c145": 63,
	"5f4af4b8be620e12377c0d13b546fd44d5520895bd1a6980ab32c216f1cc2282": 64,
	"535d8135ab1702820cf0aa9a026b527d59c56d351497267470eda664c5323aa9": 64,
	"c32345ecb42f4769fe7c1b3e3a6795ac9eb7929c5d96e58c2bb78819666be5af": 65,
	"aeebad4a796fcc2e15dc4c6061b45ed9b373f26adfc798ca7d2d8cc58182718e": 65,
	"2adbd9e34ce97ca829b2a17c3b17c56ad83a4f4b2d3781c7e183e432c8671424": 66,
	"8047156f8ae1b4f6f120cd6d6a447a7175e3cb1ca966ccfe59dafbf1167da47f": 66,
}
