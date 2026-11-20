// Package beforecode holds files that ship inside the bfc binary. The
// defaults are this repository's own .beforecode/templates and
// .beforecode/skills, so there is one copy of each
// (docs/adr/0003-config-file-v1.md).
package beforecode

import "embed"

// Templates holds .beforecode/templates/intent.md and plan.md.
//
//go:embed .beforecode/templates
var Templates embed.FS

// SkillsDir is where a repository keeps its skills, one directory with a
// SKILL.md each. The repository's copy of a skill wins over the default.
const SkillsDir = ".beforecode/skills"

// Skills holds the default skills, SkillsDir/<name>/SKILL.md.
//
//go:embed .beforecode/skills
var Skills embed.FS
