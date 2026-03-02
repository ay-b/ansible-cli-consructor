package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DiscoverPlaybooks finds all .yml/.yaml files in the playbooks/ directory.
func DiscoverPlaybooks(playbooksDir string) ([]string, error) {
	var paths []string
	for _, ext := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(playbooksDir, ext))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", ext, err)
		}
		paths = append(paths, matches...)
	}
	sort.Strings(paths)
	return paths, nil
}

// RoleEntry holds a parsed role name and its associated tags.
type RoleEntry struct {
	Name string
	Tags []string
}

// ParsePlaybook reads a playbook YAML file and extracts roles, role tags, and playbook-level variables.
func ParsePlaybook(path string) (roles []string, roleTags map[string][]string, vars []Variable, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, nil, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode {
		return nil, nil, nil, nil
	}

	seenRoles := make(map[string]bool)
	seenVars := make(map[string]bool)

	// Iterate over plays in the playbook
	for _, play := range root.Content {
		playNode := resolveAlias(play)
		if playNode.Kind != yaml.MappingNode {
			continue
		}

		for i := 0; i < len(playNode.Content)-1; i += 2 {
			keyNode := playNode.Content[i]
			valNode := resolveAlias(playNode.Content[i+1])

			switch keyNode.Value {
			case "vars":
				extracted := extractVarsFromMapping(valNode, "playbook")
				for _, v := range extracted {
					if !seenVars[v.Name] {
						seenVars[v.Name] = true
						vars = append(vars, v)
					}
				}

			case "roles":
				entries := extractRolesFromRolesSection(valNode)
				for _, entry := range entries {
					if !seenRoles[entry.Name] {
						seenRoles[entry.Name] = true
						roles = append(roles, entry.Name)
					}
					if len(entry.Tags) > 0 {
						if roleTags == nil {
							roleTags = make(map[string][]string)
						}
						existing := roleTags[entry.Name]
						for _, t := range entry.Tags {
							if !containsStr(existing, t) {
								existing = append(existing, t)
							}
						}
						roleTags[entry.Name] = existing
					}
				}

			case "tasks", "pre_tasks", "post_tasks":
				names := extractRoleNamesFromTasks(valNode)
				for _, name := range names {
					if !seenRoles[name] {
						seenRoles[name] = true
						roles = append(roles, name)
					}
				}
			}
		}
	}

	return roles, roleTags, vars, nil
}

// ParseRoleDefaults reads a role's defaults/main.yml and extracts variables.
func ParseRoleDefaults(rolesDir, roleName string) ([]Variable, error) {
	path := filepath.Join(rolesDir, roleName, "defaults", "main.yml")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil
	}

	root := doc.Content[0]
	return extractVarsFromMapping(root, roleName), nil
}

// extractVarsFromMapping extracts variables from a YAML mapping node, preserving comments.
func extractVarsFromMapping(node *yaml.Node, source string) []Variable {
	node = resolveAlias(node)
	if node.Kind != yaml.MappingNode {
		return nil
	}

	var vars []Variable
	for i := 0; i < len(node.Content)-1; i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]

		name := keyNode.Value
		desc := extractComment(keyNode, valNode)
		defVal, isComplex := extractDefaultValue(valNode)

		vars = append(vars, Variable{
			Name:        name,
			Default:     defVal,
			Description: desc,
			IsComplex:   isComplex,
			Source:      source,
		})
	}
	return vars
}

// extractComment gets the description from YAML comments on key or value nodes.
func extractComment(keyNode, valNode *yaml.Node) string {
	// Prefer line comment on the key, then head comment on the key,
	// then line comment on the value
	comment := keyNode.LineComment
	if comment == "" {
		comment = valNode.LineComment
	}
	if comment == "" {
		comment = keyNode.HeadComment
	}

	if comment == "" {
		return ""
	}

	// Clean up comment: strip leading # and whitespace per line, take first meaningful line
	lines := strings.Split(comment, "\n")
	var cleaned []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "#")
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "===") {
			cleaned = append(cleaned, line)
		}
	}
	if len(cleaned) == 0 {
		return ""
	}
	return strings.Join(cleaned, " ")
}

// extractDefaultValue returns the string representation and whether it's a complex type.
func extractDefaultValue(node *yaml.Node) (string, bool) {
	node = resolveAlias(node)

	// Vault-encrypted values
	if node.Tag == "!vault" {
		return "[vault-encrypted]", true
	}

	switch node.Kind {
	case yaml.MappingNode:
		return "[complex:map]", true
	case yaml.SequenceNode:
		// Empty sequences are simple (shown as "[]")
		if len(node.Content) == 0 {
			return "[]", false
		}
		return "[complex:list]", true
	case yaml.ScalarNode:
		return node.Value, false
	default:
		return "", false
	}
}

// extractRolesFromRolesSection extracts role names and tags from a `roles:` sequence node.
func extractRolesFromRolesSection(node *yaml.Node) []RoleEntry {
	node = resolveAlias(node)
	if node.Kind != yaml.SequenceNode {
		return nil
	}

	var entries []RoleEntry
	for _, item := range node.Content {
		item = resolveAlias(item)
		switch item.Kind {
		case yaml.ScalarNode:
			// Bare role name: `- first_run_init`
			entries = append(entries, RoleEntry{Name: item.Value})
		case yaml.MappingNode:
			// Role object: `- { role: deploy_longhorn, tags: [storage, longhorn] }`
			var name string
			var tags []string
			for j := 0; j < len(item.Content)-1; j += 2 {
				key := item.Content[j].Value
				val := resolveAlias(item.Content[j+1])
				switch key {
				case "role":
					name = val.Value
				case "tags":
					tags = extractTagsList(val)
				}
			}
			if name != "" {
				entries = append(entries, RoleEntry{Name: name, Tags: tags})
			}
		}
	}
	return entries
}

// extractTagsList extracts tag strings from a tags value node.
// Handles both scalar (single tag) and sequence (list of tags).
func extractTagsList(node *yaml.Node) []string {
	node = resolveAlias(node)
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "" {
			return []string{node.Value}
		}
	case yaml.SequenceNode:
		var tags []string
		for _, item := range node.Content {
			item = resolveAlias(item)
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				tags = append(tags, item.Value)
			}
		}
		return tags
	}
	return nil
}

// containsStr checks if a string slice contains a given string.
func containsStr(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// extractRoleNamesFromTasks scans a tasks sequence for include_role/import_role references.
func extractRoleNamesFromTasks(node *yaml.Node) []string {
	node = resolveAlias(node)
	if node.Kind != yaml.SequenceNode {
		return nil
	}

	var names []string
	for _, task := range node.Content {
		task = resolveAlias(task)
		if task.Kind != yaml.MappingNode {
			continue
		}

		for i := 0; i < len(task.Content)-1; i += 2 {
			key := task.Content[i].Value
			if key == "include_role" || key == "import_role" {
				valNode := resolveAlias(task.Content[i+1])
				if valNode.Kind == yaml.MappingNode {
					for j := 0; j < len(valNode.Content)-1; j += 2 {
						if valNode.Content[j].Value == "name" {
							names = append(names, resolveAlias(valNode.Content[j+1]).Value)
							break
						}
					}
				}
				break
			}
		}
	}
	return names
}

// DeduplicateVariables merges playbook vars with role defaults, playbook vars taking precedence.
func DeduplicateVariables(playbookVars []Variable, roleVarSets [][]Variable) []Variable {
	seen := make(map[string]bool)
	var result []Variable

	// Playbook-level vars first (highest priority)
	for _, v := range playbookVars {
		if !seen[v.Name] {
			seen[v.Name] = true
			result = append(result, v)
		}
	}

	// Then role defaults in order
	for _, roleVars := range roleVarSets {
		for _, v := range roleVars {
			if !seen[v.Name] {
				seen[v.Name] = true
				result = append(result, v)
			}
		}
	}

	return result
}

// resolveAlias follows YAML alias nodes to their anchor target.
func resolveAlias(node *yaml.Node) *yaml.Node {
	for node != nil && node.Kind == yaml.AliasNode {
		node = node.Alias
	}
	return node
}
