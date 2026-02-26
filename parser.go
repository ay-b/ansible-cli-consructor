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

// ParsePlaybook reads a playbook YAML file and extracts roles and playbook-level variables.
func ParsePlaybook(path string) (roles []string, vars []Variable, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil, nil
	}

	root := doc.Content[0]
	if root.Kind != yaml.SequenceNode {
		return nil, nil, nil
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
				names := extractRoleNamesFromRolesSection(valNode)
				for _, name := range names {
					if !seenRoles[name] {
						seenRoles[name] = true
						roles = append(roles, name)
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

	return roles, vars, nil
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

// extractRoleNamesFromRolesSection extracts role names from a `roles:` sequence node.
func extractRoleNamesFromRolesSection(node *yaml.Node) []string {
	node = resolveAlias(node)
	if node.Kind != yaml.SequenceNode {
		return nil
	}

	var names []string
	for _, item := range node.Content {
		item = resolveAlias(item)
		switch item.Kind {
		case yaml.ScalarNode:
			// Bare role name: `- first_run_init`
			names = append(names, item.Value)
		case yaml.MappingNode:
			// Role object: `- role: deploy_longhorn` or `- { role: deploy_longhorn, tags: [...] }`
			for j := 0; j < len(item.Content)-1; j += 2 {
				if item.Content[j].Value == "role" {
					names = append(names, resolveAlias(item.Content[j+1]).Value)
					break
				}
			}
		}
	}
	return names
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
