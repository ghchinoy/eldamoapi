package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Const identifiers from Agent Plugins Specification 1.0.0
const (
	expectedPluginSchema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
	expectedMcpSchema    = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
)

// validatePluginName validates plugin name constraints according to §5.5 of agent-plugins-spec
func validatePluginName(name string) error {
	if len(name) < 1 || len(name) > 64 {
		return fmt.Errorf("length must be between 1 and 64 characters, got %d", len(name))
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("must not contain consecutive hyphens '--'")
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("must not contain consecutive periods '..'")
	}

	validChars := regexp.MustCompile(`^[a-z0-9.-]+$`)
	if !validChars.MatchString(name) {
		return fmt.Errorf("must contain only lowercase alphanumeric characters, hyphens, and periods")
	}

	first := name[0]
	last := name[len(name)-1]
	isAlphaNum := func(b byte) bool {
		return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
	}
	if !isAlphaNum(first) || !isAlphaNum(last) {
		return fmt.Errorf("first and last characters must be alphanumeric")
	}

	return nil
}

func TestPluginManifest_Valid(t *testing.T) {
	data, err := os.ReadFile("plugin.json")
	if err != nil {
		t.Fatalf("Failed to read plugin.json: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("plugin.json is not valid JSON: %v", err)
	}

	// 1. $schema check
	schemaVal, ok := raw["$schema"].(string)
	if !ok || schemaVal != expectedPluginSchema {
		t.Errorf("Expected $schema '%s', got '%v'", expectedPluginSchema, raw["$schema"])
	}

	// 2. Name check
	nameVal, ok := raw["name"].(string)
	if !ok || nameVal == "" {
		t.Fatalf("plugin.json missing required string field 'name'")
	}
	if err := validatePluginName(nameVal); err != nil {
		t.Errorf("plugin name '%s' violates naming constraints: %v", nameVal, err)
	}

	// 3. Closed schema properties check (§5.2)
	allowedKeys := map[string]bool{
		"$schema":     true,
		"name":        true,
		"version":     true,
		"description": true,
		"author":      true,
		"homepage":    true,
		"repository":  true,
		"license":     true,
		"keywords":    true,
		"extensions":  true,
	}

	for key := range raw {
		if !allowedKeys[key] {
			t.Errorf("plugin.json contains forbidden top-level field '%s'", key)
		}
	}

	// 4. Author object check if present
	if authorRaw, exists := raw["author"]; exists {
		authorObj, ok := authorRaw.(map[string]any)
		if !ok {
			t.Errorf("'author' field must be an object")
		} else {
			authorAllowed := map[string]bool{"name": true, "email": true, "url": true}
			for k := range authorObj {
				if !authorAllowed[k] {
					t.Errorf("'author' object contains unknown key '%s'", k)
				}
			}
		}
	}
}

func TestMcpManifest_Valid(t *testing.T) {
	data, err := os.ReadFile("mcp.json")
	if err != nil {
		t.Fatalf("Failed to read mcp.json: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("mcp.json is not valid JSON: %v", err)
	}

	// 1. $schema check
	schemaVal, ok := raw["$schema"].(string)
	if !ok || schemaVal != expectedMcpSchema {
		t.Errorf("Expected $schema '%s', got '%v'", expectedMcpSchema, raw["$schema"])
	}

	// 2. Closed top-level properties check
	allowedKeys := map[string]bool{"$schema": true, "mcpServers": true}
	for key := range raw {
		if !allowedKeys[key] {
			t.Errorf("mcp.json contains forbidden top-level field '%s'", key)
		}
	}

	// 3. mcpServers object check
	serversRaw, ok := raw["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcp.json 'mcpServers' must be an object")
	}

	for srvName, srvRaw := range serversRaw {
		srvMap, ok := srvRaw.(map[string]any)
		if !ok {
			t.Errorf("mcpServers entry '%s' must be an object", srvName)
			continue
		}

		typeVal, ok := srvMap["type"].(string)
		if !ok || (typeVal != "stdio" && typeVal != "streamable-http" && typeVal != "sse") {
			t.Errorf("Server '%s' has invalid type '%v'", srvName, srvMap["type"])
			continue
		}

		if typeVal == "streamable-http" || typeVal == "sse" {
			urlVal, ok := srvMap["url"].(string)
			if !ok || urlVal == "" {
				t.Errorf("Server '%s' of type '%s' missing required 'url'", srvName, typeVal)
				continue
			}

			parsedURL, err := url.Parse(urlVal)
			if err != nil || !parsedURL.IsAbs() {
				t.Errorf("Server '%s' url '%s' must be a valid absolute URL", srvName, urlVal)
			}
			if parsedURL.User != nil {
				t.Errorf("Server '%s' url must not contain user credentials", srvName)
			}
			if parsedURL.Fragment != "" {
				t.Errorf("Server '%s' url must not contain a fragment", srvName)
			}
		}
	}
}

func TestSkillsDiscovery_Valid(t *testing.T) {
	entries, err := os.ReadDir("skills")
	if err != nil {
		t.Fatalf("Failed to read skills directory: %v", err)
	}

	skillCount := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDirName := entry.Name()
		skillMDPath := filepath.Join("skills", skillDirName, "SKILL.md")
		data, err := os.ReadFile(skillMDPath)
		if err != nil {
			t.Errorf("Expected SKILL.md in directory 'skills/%s', read error: %v", skillDirName, err)
			continue
		}

		skillCount++
		content := string(data)

		// Simple frontmatter parsing
		if !strings.HasPrefix(content, "---") {
			t.Errorf("skills/%s/SKILL.md missing YAML frontmatter opening '---'", skillDirName)
			continue
		}

		parts := strings.SplitN(content, "---", 3)
		if len(parts) < 3 {
			t.Errorf("skills/%s/SKILL.md invalid YAML frontmatter delimiters", skillDirName)
			continue
		}

		frontmatter := parts[1]
		var name, description string
		for _, line := range strings.Split(frontmatter, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "name:") {
				name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			} else if strings.HasPrefix(line, "description:") {
				description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			}
		}

		if name == "" {
			t.Errorf("skills/%s/SKILL.md missing 'name' in frontmatter", skillDirName)
		} else if name != skillDirName {
			t.Errorf("skills/%s/SKILL.md frontmatter name '%s' does not match directory name '%s'", skillDirName, name, skillDirName)
		}

		if description == "" {
			t.Errorf("skills/%s/SKILL.md missing 'description' in frontmatter", skillDirName)
		} else if len(description) > 1024 {
			t.Errorf("skills/%s/SKILL.md description length (%d) exceeds 1024 char limit", skillDirName, len(description))
		}
	}

	if skillCount < 3 {
		t.Errorf("Expected at least 3 skills in skills/, discovered %d", skillCount)
	}
}

func TestEmbeddedSkills_NonEmpty(t *testing.T) {
	if translateSkillMD == "" {
		t.Error("Embedded translateSkillMD is empty")
	}
	if neologismSkillMD == "" {
		t.Error("Embedded neologismSkillMD is empty")
	}
	if nameGenSkillMD == "" {
		t.Error("Embedded nameGenSkillMD is empty")
	}
}
