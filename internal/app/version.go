package app

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var releaseVersion string

// Revision is set by release builds; local source builds may leave it unknown.
var Revision = "unknown"

func Version() string { return strings.TrimSpace(releaseVersion) }
