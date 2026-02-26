package main

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
	Name      string     // display name (filename without extension)
	Path      string     // relative path (e.g. "playbooks/deploy-k3s-mini-cluster.yml")
	Roles     []string   // deduplicated role names in discovery order
	Variables []Variable // deduplicated variables, playbook vars first
}

const (
	PanePlaybooks = 0
	PaneVariables = 1
	PaneCommand   = 2
	PaneCount     = 3
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
}
