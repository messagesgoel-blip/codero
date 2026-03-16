package dashboard

import "embed"

// Static holds the embedded dashboard web assets served under /dashboard/.
//
//go:embed static
var Static embed.FS
