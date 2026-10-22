module github.com/AutoDevJapan/cc-pc-tools-rs/repair-guard

go 1.26

require cc/civ6-ci-contract v0.0.0

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace cc/civ6-ci-contract => ../tools/civ6-ci-contract
