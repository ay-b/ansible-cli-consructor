package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: %s [options]\n\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "Ansible playbook variable constructor TUI.\n")
	fmt.Fprintf(os.Stderr, "Discovers playbooks, extracts variables from roles, and builds\n")
	fmt.Fprintf(os.Stderr, "ansible-playbook commands interactively.\n\n")
	fmt.Fprintf(os.Stderr, "Options:\n")
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  %s                          # use ./playbooks/ (default)\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s -d /path/to/playbooks    # custom playbooks directory\n", os.Args[0])
	fmt.Fprintf(os.Stderr, "  %s --playbooks-dir ../other  # same, long form\n", os.Args[0])
}

func main() {
	playbooksDir := flag.String("d", "playbooks", "path to playbooks directory")
	flag.StringVar(playbooksDir, "playbooks-dir", "playbooks", "path to playbooks directory")
	rolesFlag := flag.String("r", "roles", "path to roles directory")
	flag.StringVar(rolesFlag, "roles-dir", "roles", "path to roles directory")
	flag.Usage = usage
	flag.Parse()

	rolesDir := *rolesFlag

	if _, err := os.Stat(*playbooksDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: %s/ directory not found.\n\n", *playbooksDir)
		usage()
		os.Exit(1)
	}

	paths, err := DiscoverPlaybooks(*playbooksDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering playbooks: %v\n", err)
		os.Exit(1)
	}

	if len(paths) == 0 {
		fmt.Fprintf(os.Stderr, "No playbooks found in %s/\n", *playbooksDir)
		os.Exit(1)
	}

	var playbooks []Playbook
	for _, p := range paths {
		roles, roleTags, pbVars, err := ParsePlaybook(p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: %v\n", p, err)
			continue
		}

		var roleVarSets [][]Variable
		for _, roleName := range roles {
			roleVars, err := ParseRoleDefaults(rolesDir, roleName)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: role %s defaults: %v\n", roleName, err)
				continue
			}
			roleVarSets = append(roleVarSets, roleVars)
		}

		variables := DeduplicateVariables(pbVars, roleVarSets)

		name := filepath.Base(p)
		name = strings.TrimSuffix(name, filepath.Ext(name))

		playbooks = append(playbooks, Playbook{
			Name:      name,
			Path:      p,
			Roles:     roles,
			RoleTags:  roleTags,
			Variables: variables,
		})
	}

	if len(playbooks) == 0 {
		fmt.Fprintf(os.Stderr, "No valid playbooks parsed.\n")
		os.Exit(1)
	}

	state := &AppState{
		Playbooks:    playbooks,
		ActivePane:   PanePlaybooks,
		SelectedTags: make(map[string]bool),
	}

	if err := RunUI(state); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
