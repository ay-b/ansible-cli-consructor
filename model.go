package main

import "sort"

// Variable represents a single Ansible variable extracted from a playbook or role defaults.
type Variable struct {
	Name        string // variable name (e.g. "cluster_name")
	Default     string // default value as string, or "[complex]"/"[vault-encrypted]"
	Description string // from YAML comment
	UserValue   string // user-entered value in TUI
	IsComplex   bool   // true for dicts, lists, vault — non-editable
	Source      string // "playbook" or role name
}

// Playbook represents a discovered playbook with its roles and variables.
type Playbook struct {
	Name      string              // display name (filename without extension)
	Path      string              // relative path (e.g. "playbooks/deploy-k3s-mini-cluster.yml")
	Roles     []string            // deduplicated role names in discovery order
	RoleTags  map[string][]string // role name -> list of tags from roles: section
	Variables []Variable          // deduplicated variables, playbook vars first
}

// AllTags returns a sorted, deduplicated list of all tags in the playbook.
func (pb *Playbook) AllTags() []string {
	seen := make(map[string]bool)
	var tags []string
	for _, roleTags := range pb.RoleTags {
		for _, t := range roleTags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	sort.Strings(tags)
	return tags
}

const (
	PanePlaybooks = 0
	PaneTags      = 1
	PaneVariables = 2
	PaneCommand   = 3
	PaneCount     = 4
)

// AppState holds all runtime state for the TUI.
type AppState struct {
	Playbooks       []Playbook
	ActivePane      int
	SelectedPB      int
	SelectedVar     int
	VarScrollOffset int
	EditMode        bool
	EditBuffer      string
	Notification    string
	SearchMode      bool   // true when search overlay is open
	SearchQuery     string // current search text
	SearchPane      int    // which pane initiated the search

	// Tag selection state
	SelectedTags   map[string]bool // tag name -> selected
	TagPopupOpen   bool            // true when tag selection overlay is visible
	TagPopupCursor int             // cursor position within the tag popup list
}

// currentPlaybook returns the currently selected playbook.
func (s *AppState) currentPlaybook() *Playbook {
	if s.SelectedPB < 0 || s.SelectedPB >= len(s.Playbooks) {
		return nil
	}
	return &s.Playbooks[s.SelectedPB]
}

// FilteredVariables returns the variable list for the current playbook,
// filtered by selected tags. Playbook-level vars (Source == "playbook")
// are always included. If no tags are selected, all variables are returned.
func (s *AppState) FilteredVariables() []Variable {
	pb := s.currentPlaybook()
	if pb == nil {
		return nil
	}
	if len(s.SelectedTags) == 0 {
		return pb.Variables
	}

	// Build set of roles that match at least one selected tag
	matchedRoles := make(map[string]bool)
	for role, roleTags := range pb.RoleTags {
		for _, t := range roleTags {
			if s.SelectedTags[t] {
				matchedRoles[role] = true
				break
			}
		}
	}

	var filtered []Variable
	for _, v := range pb.Variables {
		if v.Source == "playbook" || matchedRoles[v.Source] {
			filtered = append(filtered, v)
		}
	}
	return filtered
}
