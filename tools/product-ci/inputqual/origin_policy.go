package inputqual

import (
	"os"
	"path/filepath"
	"strings"
)

// Locators carry no authority: every location AND each independent evidence
// digest is compiled from the original retained records and direct task scope.
// No raw original/private path is included in the distributed source.
var originalLocations = map[string]struct{ pathSHA, contentSHA string }{
	"root":         {"9cc3eb629c049b403bdfa45924389c1de5c4ebd11cf5fda903abb14529f55782", ""},
	"db":           {"e1d9e17271e5e7d32dce7484a28d1517cb7079a6d75289e2a7cc2fc74405b2d5", ""},
	"pin":          {"85995f1f8606fe2bdf81134d232233309b654d7b97dcffe5adce2ee7d0a24b4d", ""},
	"review":       {"5611171c05a97bd0c4cab26fba6e6726ed4cbe61b6373ab4cbd1f6338cdd7446", ""},
	"acceptance":   {"e04d1010434d65172845c2fd5363e2c55d5eb5db5336d7691cfa874301696999", ""},
	"scope":        {"5886e69ae01dd745c7dfacc68ef2abe57cdd6c16d8ee93b9b274ff2beca0a98e", "b53f01349b384cabe0500c678689ea8e451d4a01f9e1b62d2c46dd756a1b4847"},
	"parent_scope": {"593c34d4fd107b3f0368b98c963eb5eaa2298ac1cb2a473328626391cc11f33d", "62c85a9d2fd3f005a93457b596c3047e249da49377c071af3d1fa05a1dd6bea7"},
	"origin_scope": {"fa44cb2e6d997e122d97bcb45d33d2c76b9f0e8bb5ac515b0e5cae86fabf7ba9", "cd7655c1ecdb196042d9b8be22f9d8b2d73c90dbe61432bb73241b8519ec8223"},
	"predecessor":  {"a66ef0356cccd17f41d54e5f2e57c57f38851edb155b00f2415ffae14ded2454", "e1f5b29043fda809d51354951cbe0762c35997e08cadf94c066f6da1d66b095e"},
	"original":     {"3e4748085e47610f9fdfec3d33f5c31e53ab6a6f7e6f53e5be4b02385f981a38", "b0ed7c7f6729447cd7e52ad4774a046ba7142d7d9fcd1900acad3ab793c9ea0d"},
	"owned":        {"be22d31a725e6cd5ec7be1577da6057102c07be32b5edf25efe390c5a85301d7", "528f87f0f2d0db7785f5149cf66c312e1bf7d14631ba575ab1f061d23f3252fa"},
	"outcome":      {"2a99b796773e60846a3142d81d290498d4edca3f8e2bc26ff0b5b7fc8199579b", "0e6aaed46f2776d444b3f043f508e8c489f2c69c90e935041d25cdfcdf4186a5"},
	"final":        {"64cdeccaa69ccaf5cad2f9f919ddaba6748322b9573d6453cb5b64c7a50d3a1c", "be60102c022501652e767564efa390bf40069b9a7115fb6b477dfd3653c08ae9"},
	"stop":         {"7e05693e72ff745131dd776d7b49f3f1f227b0d21fa59370a8547c5da39c96db", "623b261e4b34e41ef0dfcd9726b11cd133f15d512aebc7767a121575f1902943"},
}

func locationDigest(path string) string {
	return hash([]byte(strings.ToLower(filepath.ToSlash(filepath.Clean(path)))))
}

func productionPolicy() (policy, error) {
	exe, err := os.Executable()
	if err != nil {
		return policy{}, rejected("locator_unavailable")
	}
	return loadPolicy(filepath.Join(filepath.Dir(exe), "current-input-3089.json"))
}

func loadPolicy(locator string) (policy, error) {
	data, err := readExact(locator, 32<<10)
	if err != nil {
		return policy{}, rejected("locator_unavailable")
	}
	var paths map[string]string
	if decode(data, &paths, true) != nil || len(paths) != len(originalLocations) {
		return policy{}, rejected("locator_schema")
	}
	for label, expected := range originalLocations {
		path, ok := paths[label]
		if !ok || !canonical(path) || locationDigest(path) != expected.pathSHA {
			return policy{}, rejected("locator_location")
		}
	}
	a := func(label string) anchor { return anchor{paths[label], originalLocations[label].contentSHA, 1 << 20} }
	return policy{root: paths["root"], db: paths["db"], pin: paths["pin"], review: paths["review"], acceptance: paths["acceptance"], scope: a("scope"), parentSpec: a("parent_scope"), originSpec: a("origin_scope"), predecessor: a("predecessor"), original: a("original"), owned: a("owned"), outcome: a("outcome"), final: a("final"), stop: a("stop"), reviewSHA: originalReviewSHA, origin: verifiedOrigin}, nil
}
