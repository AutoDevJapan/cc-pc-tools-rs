package contract

import _ "embed"

// DesktopWorkflow is the only executable workflow admitted by this version.
//go:embed desktop-build.yml
var DesktopWorkflow []byte
