package static

import "embed"

// FS holds the embedded web frontend.
//
//go:embed web/*
var FS embed.FS
