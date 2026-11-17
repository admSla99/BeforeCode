// Package beforecode holds files that ship inside the bfc binary. The
// default templates are this repository's own .beforecode/templates, so
// there is one copy of each (docs/adr/0003-config-file-v1.md).
package beforecode

import "embed"

// Templates holds .beforecode/templates/intent.md and plan.md.
//
//go:embed .beforecode/templates
var Templates embed.FS
