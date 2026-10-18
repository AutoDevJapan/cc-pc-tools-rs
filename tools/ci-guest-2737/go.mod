module cc/ci-guest-2737

go 1.27.0

require cc/ci-source-2737 v0.0.0

require cc/product-ci v0.0.0

require (
	github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737 v0.0.0
	github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737 v0.0.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace cc/ci-source-2737 => ../ci-source-2737

replace cc/product-ci => ../product-ci

replace github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-runner-2737 => ../windows-ci-runner-2737

replace github.com/autodevjapan/cc-pc-tools-rs/tools/windows-ci-sdk-2737 => ../windows-ci-sdk-2737
