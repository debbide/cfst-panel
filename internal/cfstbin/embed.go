package cfstbin

import "embed"

// Bundled CloudflareST assets for Linux.
//
//go:embed all:linux_amd64 all:linux_arm64
var FS embed.FS
