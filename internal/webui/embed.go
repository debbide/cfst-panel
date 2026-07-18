package webui

import "embed"

// FS contains the built-in web panel assets.
//
//go:embed all:static
var FS embed.FS
