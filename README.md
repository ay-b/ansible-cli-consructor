# cli-constructor

An interactive terminal UI for building `ansible-playbook` commands. Discovers playbooks, extracts variables from roles, and lets you fill in values — then copies the ready-to-run command to your clipboard.

```
┌─ Playbooks [*] ─────┬─ Variables (12) | 3 roles ───────────────────────────┐
│ deploy-k3s           │    cluster_name              k3s-prod    Name of ... │
│ setup-monitoring     │  > admin_user                admin       Admin us... │
│ backup-etcd          │    node_count                3           Number o... │
│ rotate-certs         │    enable_traefik            true        Enable T... │
│                      │    registry_mirror           [complex]   Container.. │
│                      │    vault_password            [vault-encrypted]       │
│                      │    log_level                 info        Logging ... │
├──────────────────────┴──────────────────────────────────────────────────────┤
│ ansible-playbook playbooks/deploy-k3s.yml \                                │
│   -e cluster_name=k3s-prod \                                               │
│   -e admin_user=admin                              Copied to clipboard!    │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Features

- **Playbook discovery** -- automatically finds all `.yml`/`.yaml` files in your playbooks directory
- **Variable extraction** -- parses `vars:` from playbooks and `defaults/main.yml` from every referenced role (including `include_role`/`import_role` in tasks)
- **Deduplication** -- merges variables across roles with playbook-level vars taking priority
- **Comment-aware** -- pulls variable descriptions from YAML comments
- **Smart types** -- vault-encrypted values, maps, and lists are shown but marked non-editable; simple scalars and empty lists are editable
- **Live command preview** -- bottom pane updates in real time as you fill in values
- **Clipboard copy** -- one keypress to copy the command
- **Vim navigation** -- `j`/`k`, `/` search with `n`/`N`, page up/down
- **Shell-safe output** -- values are properly escaped for direct shell use

## Requirements

- Go 1.21+
- A terminal that supports ANSI colors

## Installation

```bash
go install github.com/YOUR_USERNAME/cli-constructor@latest
```

Or build from source:

```bash
git clone https://github.com/YOUR_USERNAME/cli-constructor.git
cd cli-constructor
go build -o cli-constructor .
```

## Usage

Run from a directory that contains `playbooks/` and `roles/` subdirectories (standard Ansible layout):

```
your-project/
├── playbooks/
│   ├── deploy-k3s.yml
│   ├── setup-monitoring.yml
│   └── backup-etcd.yml
└── roles/
    ├── k3s_server/
    │   └── defaults/
    │       └── main.yml
    ├── monitoring/
    │   └── defaults/
    │       └── main.yml
    └── etcd/
        └── defaults/
            └── main.yml
```

```bash
# Default: looks for ./playbooks/ and ./roles/
cli-constructor

# Custom paths
cli-constructor -d /path/to/playbooks -r /path/to/roles

# Long-form flags
cli-constructor --playbooks-dir ../ansible/playbooks --roles-dir ../ansible/roles
```

## Keybindings

### Global

| Key | Action |
|-----|--------|
| `Tab` | Cycle panes (playbooks -> variables -> command) |
| `Ctrl+C` | Quit |
| `q` | Quit |
| `?` | Show help overlay |

### Playbooks pane

| Key | Action |
|-----|--------|
| `j` / `Down` | Next playbook |
| `k` / `Up` | Previous playbook |
| `PgDn` / `PgUp` | Page down / up |
| `Enter` | Focus variables pane |
| `/` | Search playbooks |
| `n` / `N` | Next / previous search match |

### Variables pane

| Key | Action |
|-----|--------|
| `j` / `Down` | Next variable |
| `k` / `Up` | Previous variable |
| `PgDn` / `PgUp` | Page down / up |
| `Enter` | Edit selected variable |
| `d` | Reset variable to default |
| `/` | Search variables (name, description, default) |
| `n` / `N` | Next / previous search match |

### Edit overlay

| Key | Action |
|-----|--------|
| `Enter` | Confirm value |
| `Escape` | Cancel |

### Command pane

| Key | Action |
|-----|--------|
| `Enter` / `c` / `C` | Copy command to clipboard |

## How it works

1. **Discovery** -- scans the playbooks directory for `.yml`/`.yaml` files
2. **Parsing** -- for each playbook, extracts roles from `roles:`, `include_role`, and `import_role` directives, plus playbook-level `vars:`
3. **Role inspection** -- reads `defaults/main.yml` from each role to collect variables with their defaults and descriptions
4. **Deduplication** -- merges all variables, with playbook-level definitions taking precedence over role defaults
5. **Interactive editing** -- presents everything in a three-pane TUI where you select a playbook, fill in variable values, and see the resulting command update live
6. **Output** -- generates a properly escaped `ansible-playbook` command with `-e key=value` flags for every variable you've set

## Variable display

| Color | Meaning |
|-------|---------|
| White | Variable name (default value not overridden) |
| Yellow | Variable name with a user-set value |
| Green | User-entered value |
| Gray | Default values, descriptions, complex/vault variables |

Complex types (maps, non-empty lists, vault-encrypted values) are displayed for reference but cannot be edited in the TUI.

## License

MIT
