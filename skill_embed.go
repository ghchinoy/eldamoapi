package main

// skill_embed.go — compile-time embedding of SKILL.md prompt files.
//
// The //go:embed directive requires paths relative to this file's directory
// and cannot use ".." to escape it, so the embeds must live in package main
// (the root) where the skills/ markdown directory is directly reachable.
// The embedded strings are passed into skills.Deps when the A2A handler is
// constructed, keeping the skills/ Go package free of embed directives.

import _ "embed"

//go:embed skills/tolkien-translation/SKILL.md
var translateSkillMD string

//go:embed skills/neologism-builder/SKILL.md
var neologismSkillMD string
