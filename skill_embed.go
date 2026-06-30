package main

// skill_embed.go — compile-time embedding of Agent Skill prompt files.
//
// Agent Skills (agent-skills/) are markdown workflow documents for external
// LLM agents (OpenCode, Claude) using the MCP interface — see agentskills.io.
// They are ALSO used as Gemini system instructions for the A2A translate and
// neologism skills, so they are embedded here and injected into skills.Deps.
//
// The //go:embed directive cannot use ".." so the embeds must live in package
// main (the repo root) where agent-skills/ is directly reachable.

import _ "embed"

//go:embed agent-skills/tolkien-translation/SKILL.md
var translateSkillMD string

//go:embed agent-skills/neologism-builder/SKILL.md
var neologismSkillMD string
