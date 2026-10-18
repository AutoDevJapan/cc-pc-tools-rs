package windowsrunner

import (
	"encoding/json"
	"errors"
	sdk "github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737"
)

// PublicSeeds returns seed.json and runner-seed.json bytes for offline staging.
// Private keys stay in identity; only its certificate HASH is serialized.
func PublicSeeds(seed Seed, identity sdk.Identity) (bootstrap, runner []byte, err error) {
	if seed.Validate() != nil || sdk.CertificateHash(identity.Leaf) != seed.ClientSHA256 {
		return nil, nil, errors.New("public seed identity rejected")
	}
	bootstrap, err = sdk.BuildPublicSeed(seed.Plan.Instance, identity)
	if err != nil {
		return nil, nil, err
	}
	runner, err = json.Marshal(seed)
	return bootstrap, runner, err
}
