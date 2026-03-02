package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/awesome-gocui/gocui"
)

const (
	viewPlaybooks = "playbooks"
	viewTags      = "tags"
	viewVariables = "variables"
	viewCommand   = "command"
	viewEditor    = "editor"
	viewSearch    = "search"
	viewHelp      = "help"
	viewTagPopup  = "tagpopup"
)

// RunUI starts the gocui main loop.
func RunUI(state *AppState) error {
	g, err := gocui.NewGui(gocui.OutputNormal, true)
	if err != nil {
		return fmt.Errorf("init gui: %w", err)
	}
	defer g.Close()

	g.Highlight = true
	g.SelFgColor = gocui.ColorGreen
	g.SelFrameColor = gocui.ColorGreen
	g.Cursor = false
	g.Mouse = false

	g.SetManagerFunc(func(g *gocui.Gui) error {
		return layout(g, state)
	})

	if err := keybindings(g, state); err != nil {
		return fmt.Errorf("keybindings: %w", err)
	}

	if err := g.MainLoop(); err != nil && !errors.Is(err, gocui.ErrQuit) {
		return err
	}
	return nil
}

// layout is called on every render cycle to position views.
func layout(g *gocui.Gui, state *AppState) error {
	maxX, maxY := g.Size()
	splitX := maxX * 20 / 100
	if splitX < 15 {
		splitX = 15
	}
	cmdY := maxY - 5
	if cmdY < 3 {
		cmdY = 3
	}

	// Tags pane height: frame top + 1 content line + frame bottom = 3 rows
	tagsY1 := 2

	// Left pane: Playbooks
	if v, err := g.SetView(viewPlaybooks, 0, 0, splitX-1, cmdY-1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Highlight = true
		v.SelBgColor = gocui.ColorGreen
		v.SelFgColor = gocui.ColorBlack
		v.Frame = true
		if _, err := g.SetCurrentView(viewPlaybooks); err != nil {
			return err
		}
	}

	// Right top pane: Tags
	if v, err := g.SetView(viewTags, splitX, 0, maxX-1, tagsY1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Frame = true
		v.Wrap = false
	}

	// Right bottom pane: Variables
	if v, err := g.SetView(viewVariables, splitX, tagsY1+1, maxX-1, cmdY-1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Frame = true
		v.Wrap = false
	}

	// Bottom pane: Command
	if v, err := g.SetView(viewCommand, 0, cmdY, maxX-1, maxY-1, 0); err != nil {
		if !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}
		v.Frame = true
		v.Wrap = true
	}

	renderPlaybooks(g, state)
	renderTags(g, state)
	renderVariables(g, state)
	renderCommand(g, state)
	updateTitles(g, state)

	return nil
}

// updateTitles sets pane titles with focus indicators.
func updateTitles(g *gocui.Gui, state *AppState) {
	if v, err := g.View(viewPlaybooks); err == nil {
		if state.ActivePane == PanePlaybooks {
			v.Title = " Playbooks [*] "
			v.FrameColor = gocui.ColorGreen
		} else {
			v.Title = " Playbooks "
			v.FrameColor = gocui.ColorDefault
		}
	}
	if v, err := g.View(viewTags); err == nil {
		pb := state.currentPlaybook()
		tagCount := 0
		if pb != nil {
			tagCount = len(pb.AllTags())
		}
		selectedCount := 0
		for _, sel := range state.SelectedTags {
			if sel {
				selectedCount++
			}
		}
		if state.ActivePane == PaneTags {
			v.Title = fmt.Sprintf(" Tags (%d/%d) [*] ", selectedCount, tagCount)
			v.FrameColor = gocui.ColorGreen
		} else {
			v.Title = fmt.Sprintf(" Tags (%d/%d) ", selectedCount, tagCount)
			v.FrameColor = gocui.ColorDefault
		}
	}
	if v, err := g.View(viewVariables); err == nil {
		vars := state.FilteredVariables()
		varCount := len(vars)
		pb := state.currentPlaybook()
		roleInfo := ""
		if pb != nil && len(pb.Roles) > 0 {
			roleInfo = fmt.Sprintf(" | %d roles", len(pb.Roles))
		}
		if state.ActivePane == PaneVariables {
			v.Title = fmt.Sprintf(" Variables (%d)%s [*] ", varCount, roleInfo)
			v.FrameColor = gocui.ColorGreen
		} else {
			v.Title = fmt.Sprintf(" Variables (%d)%s ", varCount, roleInfo)
			v.FrameColor = gocui.ColorDefault
		}
	}
	if v, err := g.View(viewCommand); err == nil {
		notify := ""
		if state.Notification != "" {
			notify = fmt.Sprintf(" ~ %s ~", state.Notification)
		}
		if state.ActivePane == PaneCommand {
			v.Title = fmt.Sprintf(" Command [Enter/C: copy]%s [*] ", notify)
			v.FrameColor = gocui.ColorGreen
		} else {
			v.Title = fmt.Sprintf(" Command [Enter/C: copy]%s ", notify)
			v.FrameColor = gocui.ColorDefault
		}
	}
}

// renderPlaybooks draws the playbook list.
func renderPlaybooks(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewPlaybooks)
	if err != nil {
		return
	}
	v.Clear()

	for _, pb := range state.Playbooks {
		fmt.Fprintln(v, pb.Name)
	}

	// Vim-style centered scrolling: cursor stays mid-screen,
	// viewport scrolls around it. At list boundaries the cursor
	// moves away from center toward the edge.
	_, viewH := v.Size()
	if viewH <= 0 {
		viewH = 1
	}
	origin, cursor := centeredScroll(state.SelectedPB, len(state.Playbooks), viewH)
	_ = v.SetOrigin(0, origin)
	_ = v.SetCursor(0, cursor)
}

// renderTags draws the tags summary line in the tags pane.
func renderTags(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewTags)
	if err != nil {
		return
	}
	v.Clear()

	pb := state.currentPlaybook()
	if pb == nil || len(pb.AllTags()) == 0 {
		fmt.Fprint(v, "  selected tags: (none available)")
		return
	}

	// Collect selected tags in sorted order
	var selected []string
	allTags := pb.AllTags()
	for _, t := range allTags {
		if state.SelectedTags[t] {
			selected = append(selected, t)
		}
	}

	if len(selected) == 0 {
		fmt.Fprint(v, "  selected tags: none")
	} else {
		line := "  selected tags: " + strings.Join(selected, ", ")
		viewW, _ := v.Size()
		fmt.Fprint(v, truncate(line, viewW))
	}
}

// varSectionCount returns the number of section headers that would be
// rendered for a variable list (one per distinct Source group).
func varSectionCount(vars []Variable) int {
	if len(vars) == 0 {
		return 0
	}
	count := 1
	for i := 1; i < len(vars); i++ {
		if vars[i].Source != vars[i-1].Source {
			count++
		}
	}
	return count
}

// varIndexToDisplayRow maps a variable index to its display row,
// accounting for section header lines that precede it.
func varIndexToDisplayRow(vars []Variable, varIdx int) int {
	row := 1 // first section header
	for i := 1; i <= varIdx; i++ {
		row++ // previous variable
		if vars[i].Source != vars[i-1].Source {
			row++ // section header before this variable
		}
	}
	return row
}

// sectionLabel returns the display label for a variable source.
func sectionLabel(source string) string {
	if source == "playbook" {
		return "playbook vars"
	}
	return "Role: " + source
}

// renderVariables draws the variable list for the selected playbook.
func renderVariables(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewVariables)
	if err != nil {
		return
	}
	v.Clear()

	vars := state.FilteredVariables()
	if len(vars) == 0 {
		if len(state.SelectedTags) > 0 {
			fmt.Fprintln(v, "  (no variables match selected tags)")
		} else {
			fmt.Fprintln(v, "  (no variables)")
		}
		return
	}

	viewW, viewH := v.Size()
	if viewH <= 0 {
		viewH = 1
	}

	totalRows := len(vars) + varSectionCount(vars)
	selectedRow := varIndexToDisplayRow(vars, state.SelectedVar)
	origin, _ := centeredScroll(selectedRow, totalRows, viewH)

	nameW := 30
	valW := 25
	if viewW > 100 {
		nameW = 35
		valW = 30
	}

	// Build display rows and render the visible window
	displayRow := 0
	rendered := 0
	prevSource := ""
	for i := 0; i < len(vars) && rendered < viewH; i++ {
		vr := vars[i]

		// Section header when source changes
		if vr.Source != prevSource {
			if displayRow >= origin && rendered < viewH {
				label := sectionLabel(vr.Source)
				pad := viewW - len(label) - 6 // "  ── " + " ──"
				if pad < 0 {
					pad = 0
				}
				fmt.Fprintf(v, "  \033[36m── %s %s\033[0m\n", label, strings.Repeat("─", pad))
				rendered++
			}
			displayRow++
			prevSource = vr.Source
		}

		if rendered >= viewH {
			break
		}

		// Variable row
		if displayRow >= origin {
			cursor := "  "
			if i == state.SelectedVar && state.ActivePane == PaneVariables {
				cursor = "> "
			}

			displayVal := vr.Default
			if vr.UserValue != "" {
				displayVal = vr.UserValue
			}

			name := truncate(vr.Name, nameW)
			val := truncate(displayVal, valW)
			desc := ""
			if vr.Description != "" {
				remaining := viewW - nameW - valW - 8
				if remaining > 5 {
					desc = truncate(vr.Description, remaining)
				}
			}

			if vr.IsComplex {
				fmt.Fprintf(v, "%s\033[90m%-*s  %-*s  %s\033[0m\n", cursor, nameW, name, valW, val, desc)
			} else if vr.UserValue != "" {
				fmt.Fprintf(v, "%s\033[33m%-*s\033[0m  \033[32m%-*s\033[0m  \033[90m%s\033[0m\n", cursor, nameW, name, valW, val, desc)
			} else {
				fmt.Fprintf(v, "%s%-*s  \033[90m%-*s\033[0m  \033[90m%s\033[0m\n", cursor, nameW, name, valW, val, desc)
			}
			rendered++
		}
		displayRow++
	}
}

// renderCommand draws the generated ansible-playbook command.
func renderCommand(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewCommand)
	if err != nil {
		return
	}
	v.Clear()

	pb := state.currentPlaybook()
	if pb == nil {
		return
	}

	cmd := generateCommand(pb, state.SelectedTags)
	fmt.Fprint(v, cmd)
}

// generateCommand builds the ansible-playbook command string.
func generateCommand(pb *Playbook, selectedTags map[string]bool) string {
	var b strings.Builder
	b.WriteString("ansible-playbook ")
	b.WriteString(pb.Path)

	// Add --tags if any are selected
	if len(selectedTags) > 0 {
		var tags []string
		allTags := pb.AllTags()
		for _, t := range allTags {
			if selectedTags[t] {
				tags = append(tags, t)
			}
		}
		if len(tags) > 0 {
			b.WriteString(" \\\n  --tags ")
			b.WriteString(strings.Join(tags, ","))
		}
	}

	for _, v := range pb.Variables {
		if v.UserValue == "" || v.IsComplex {
			continue
		}
		b.WriteString(" \\\n  -e ")
		b.WriteString(v.Name)
		b.WriteString("=")
		b.WriteString(shellEscape(v.UserValue))
	}

	return b.String()
}

// shellEscape wraps a value in single quotes if it contains special characters.
func shellEscape(s string) string {
	if s == "" {
		return "''"
	}
	needsQuoting := false
	for _, c := range s {
		if !isShellSafe(c) {
			needsQuoting = true
			break
		}
	}
	if !needsQuoting {
		return s
	}
	// Escape single quotes: replace ' with '\''
	escaped := strings.ReplaceAll(s, "'", `'\''`)
	return "'" + escaped + "'"
}

func isShellSafe(c rune) bool {
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	switch c {
	case '-', '_', '.', '/', ':', '@', '+', ',':
		return true
	}
	return false
}

// keybindings sets up all keyboard handlers.
func keybindings(g *gocui.Gui, state *AppState) error {
	// Global: quit
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		return err
	}

	// Global: Tab cycles panes
	if err := g.SetKeybinding("", gocui.KeyTab, gocui.ModNone, func(g *gocui.Gui, v *gocui.View) error {
		if state.EditMode || state.TagPopupOpen {
			return nil
		}
		state.ActivePane = (state.ActivePane + 1) % PaneCount
		return setFocus(g, state)
	}); err != nil {
		return err
	}

	// Playbooks pane: navigation
	for _, spec := range []struct {
		key interface{}
		fn  func(*gocui.Gui, *gocui.View) error
	}{
		{gocui.KeyArrowUp, pbUp(state)},
		{gocui.KeyArrowDown, pbDown(state)},
		{'k', pbUp(state)},
		{'j', pbDown(state)},
		{gocui.KeyPgup, pbPageUp(g, state)},
		{gocui.KeyPgdn, pbPageDown(g, state)},
		{gocui.KeyEnter, func(g *gocui.Gui, v *gocui.View) error {
			state.ActivePane = PaneVariables
			state.SelectedVar = 0
			state.VarScrollOffset = 0
			return setFocus(g, state)
		}},
		{'/', openSearch(g, state, PanePlaybooks)},
		{'n', searchNext(state)},
		{'N', searchPrev(state)},
		{'?', openHelp(g, state)},
		{'q', quit},
	} {
		if err := g.SetKeybinding(viewPlaybooks, spec.key, gocui.ModNone, spec.fn); err != nil {
			return err
		}
	}

	// Tags pane: keybindings
	for _, spec := range []struct {
		key interface{}
		fn  func(*gocui.Gui, *gocui.View) error
	}{
		{gocui.KeyEnter, openTagPopup(g, state)},
		{'?', openHelp(g, state)},
		{'q', quit},
	} {
		if err := g.SetKeybinding(viewTags, spec.key, gocui.ModNone, spec.fn); err != nil {
			return err
		}
	}

	// Variables pane: navigation
	for _, spec := range []struct {
		key interface{}
		fn  func(*gocui.Gui, *gocui.View) error
	}{
		{gocui.KeyArrowUp, varUp(state)},
		{gocui.KeyArrowDown, varDown(state)},
		{'k', varUp(state)},
		{'j', varDown(state)},
		{gocui.KeyPgup, varPageUp(g, state)},
		{gocui.KeyPgdn, varPageDown(g, state)},
		{gocui.KeyEnter, openEditor(g, state)},
		{'d', resetVar(state)},
		{'/', openSearch(g, state, PaneVariables)},
		{'n', searchNext(state)},
		{'N', searchPrev(state)},
		{'?', openHelp(g, state)},
		{'q', quit},
	} {
		if err := g.SetKeybinding(viewVariables, spec.key, gocui.ModNone, spec.fn); err != nil {
			return err
		}
	}

	// Command pane: copy
	for _, key := range []interface{}{gocui.KeyEnter, 'c', 'C'} {
		if err := g.SetKeybinding(viewCommand, key, gocui.ModNone, copyCommand(g, state)); err != nil {
			return err
		}
	}

	// Command pane: help + quit
	if err := g.SetKeybinding(viewCommand, '?', gocui.ModNone, openHelp(g, state)); err != nil {
		return err
	}
	if err := g.SetKeybinding(viewCommand, 'q', gocui.ModNone, quit); err != nil {
		return err
	}

	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func setFocus(g *gocui.Gui, state *AppState) error {
	var name string
	switch state.ActivePane {
	case PanePlaybooks:
		name = viewPlaybooks
	case PaneTags:
		name = viewTags
	case PaneVariables:
		name = viewVariables
	case PaneCommand:
		name = viewCommand
	}
	_, err := g.SetCurrentView(name)
	return err
}

// Playbook navigation handlers
func pbUp(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if state.SelectedPB > 0 {
			state.SelectedPB--
			state.SelectedVar = 0
			state.VarScrollOffset = 0
			state.SelectedTags = make(map[string]bool)
		}
		return nil
	}
}

func pbDown(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if state.SelectedPB < len(state.Playbooks)-1 {
			state.SelectedPB++
			state.SelectedVar = 0
			state.VarScrollOffset = 0
			state.SelectedTags = make(map[string]bool)
		}
		return nil
	}
}

// Variable navigation handlers
func varUp(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if state.SelectedVar > 0 {
			state.SelectedVar--
		}
		return nil
	}
}

func varDown(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		vars := state.FilteredVariables()
		if len(vars) > 0 && state.SelectedVar < len(vars)-1 {
			state.SelectedVar++
		}
		return nil
	}
}

// findOriginalVar finds the variable in pb.Variables that matches the
// given filtered variable by name and source, returning a pointer to it.
func findOriginalVar(pb *Playbook, name, source string) *Variable {
	for i := range pb.Variables {
		if pb.Variables[i].Name == name && pb.Variables[i].Source == source {
			return &pb.Variables[i]
		}
	}
	return nil
}

// openEditor creates an overlay view for editing a variable value.
func openEditor(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		vars := state.FilteredVariables()
		if len(vars) == 0 || state.SelectedVar >= len(vars) {
			return nil
		}
		filteredVar := vars[state.SelectedVar]
		vr := findOriginalVar(pb, filteredVar.Name, filteredVar.Source)
		if vr == nil || vr.IsComplex {
			return nil
		}

		state.EditMode = true

		maxX, maxY := g.Size()
		editorW := maxX * 60 / 100
		if editorW < 40 {
			editorW = 40
		}
		editorH := 2
		x0 := (maxX - editorW) / 2
		y0 := (maxY - editorH) / 2
		x1 := x0 + editorW
		y1 := y0 + editorH

		ev, err := g.SetView(viewEditor, x0, y0, x1, y1, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		ev.Title = fmt.Sprintf(" Edit: %s ", vr.Name)
		ev.Frame = true
		ev.Editable = true
		ev.FrameColor = gocui.ColorYellow
		ev.Clear()
		g.Cursor = true

		// Pre-fill with current value
		prefill := vr.UserValue
		if prefill == "" {
			prefill = vr.Default
		}
		fmt.Fprint(ev, prefill)
		_ = ev.SetCursor(len(prefill), 0)

		if _, err := g.SetCurrentView(viewEditor); err != nil {
			return err
		}

		// Editor keybindings
		_ = g.SetKeybinding(viewEditor, gocui.KeyEnter, gocui.ModNone, confirmEdit(g, state))
		_ = g.SetKeybinding(viewEditor, gocui.KeyEsc, gocui.ModNone, cancelEdit(g, state))

		return nil
	}
}

func confirmEdit(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		val := strings.TrimSpace(v.Buffer())

		pb := state.currentPlaybook()
		if pb != nil {
			vars := state.FilteredVariables()
			if state.SelectedVar < len(vars) {
				fv := vars[state.SelectedVar]
				if orig := findOriginalVar(pb, fv.Name, fv.Source); orig != nil {
					orig.UserValue = val
				}
			}
		}

		return closeEditor(gui, state)
	}
}

func cancelEdit(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		return closeEditor(gui, state)
	}
}

func closeEditor(g *gocui.Gui, state *AppState) error {
	state.EditMode = false
	g.Cursor = false

	g.DeleteKeybindings(viewEditor)
	if err := g.DeleteView(viewEditor); err != nil {
		return err
	}

	state.ActivePane = PaneVariables
	_, err := g.SetCurrentView(viewVariables)
	return err
}

// resetVar clears the user value for the selected variable.
func resetVar(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		vars := state.FilteredVariables()
		if state.SelectedVar < len(vars) {
			fv := vars[state.SelectedVar]
			if orig := findOriginalVar(pb, fv.Name, fv.Source); orig != nil {
				orig.UserValue = ""
			}
		}
		return nil
	}
}

// copyCommand copies the generated command to the clipboard.
func copyCommand(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}

		cmd := generateCommand(pb, state.SelectedTags)
		if err := clipboard.WriteAll(cmd); err != nil {
			state.Notification = fmt.Sprintf("Clipboard error: %v", err)
		} else {
			state.Notification = "Copied to clipboard!"
		}

		// Clear notification after 2 seconds
		go func() {
			time.Sleep(2 * time.Second)
			gui.Update(func(g *gocui.Gui) error {
				state.Notification = ""
				return nil
			})
		}()

		return nil
	}
}

// Page navigation for playbooks pane
func pbPageUp(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		_, viewH := v.Size()
		if viewH <= 0 {
			viewH = 1
		}
		state.SelectedPB -= viewH
		if state.SelectedPB < 0 {
			state.SelectedPB = 0
		}
		state.SelectedVar = 0
		state.VarScrollOffset = 0
		state.SelectedTags = make(map[string]bool)
		return nil
	}
}

func pbPageDown(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		_, viewH := v.Size()
		if viewH <= 0 {
			viewH = 1
		}
		state.SelectedPB += viewH
		if state.SelectedPB >= len(state.Playbooks) {
			state.SelectedPB = len(state.Playbooks) - 1
		}
		state.SelectedVar = 0
		state.VarScrollOffset = 0
		state.SelectedTags = make(map[string]bool)
		return nil
	}
}

// Page navigation for variables pane
func varPageUp(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		_, viewH := v.Size()
		if viewH <= 0 {
			viewH = 1
		}
		state.SelectedVar -= viewH
		if state.SelectedVar < 0 {
			state.SelectedVar = 0
		}
		return nil
	}
}

func varPageDown(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		_, viewH := v.Size()
		if viewH <= 0 {
			viewH = 1
		}
		vars := state.FilteredVariables()
		if len(vars) == 0 {
			return nil
		}
		state.SelectedVar += viewH
		if state.SelectedVar >= len(vars) {
			state.SelectedVar = len(vars) - 1
		}
		return nil
	}
}

// --- Tag Popup Overlay ---

// openTagPopup creates an overlay with a toggleable list of tags.
func openTagPopup(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil || len(pb.AllTags()) == 0 {
			return nil
		}

		state.TagPopupOpen = true
		state.TagPopupCursor = 0

		allTags := pb.AllTags()

		maxX, maxY := gui.Size()
		popupW := 50
		if popupW > maxX-4 {
			popupW = maxX - 4
		}
		popupH := len(allTags) + 1
		if popupH > maxY-4 {
			popupH = maxY - 4
		}
		x0 := (maxX - popupW) / 2
		y0 := (maxY - popupH) / 2

		tv, err := gui.SetView(viewTagPopup, x0, y0, x0+popupW, y0+popupH, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		tv.Title = " Select Tags [Space: toggle, a: all, x: clear] "
		tv.Frame = true
		tv.FrameColor = gocui.ColorYellow

		if _, err := gui.SetCurrentView(viewTagPopup); err != nil {
			return err
		}

		renderTagPopup(gui, state)

		// Keybindings for the popup
		_ = gui.SetKeybinding(viewTagPopup, gocui.KeyArrowUp, gocui.ModNone, tagPopupUp(state))
		_ = gui.SetKeybinding(viewTagPopup, gocui.KeyArrowDown, gocui.ModNone, tagPopupDown(state))
		_ = gui.SetKeybinding(viewTagPopup, 'k', gocui.ModNone, tagPopupUp(state))
		_ = gui.SetKeybinding(viewTagPopup, 'j', gocui.ModNone, tagPopupDown(state))
		_ = gui.SetKeybinding(viewTagPopup, gocui.KeySpace, gocui.ModNone, tagPopupToggle(state))
		_ = gui.SetKeybinding(viewTagPopup, gocui.KeyEnter, gocui.ModNone, tagPopupToggle(state))
		_ = gui.SetKeybinding(viewTagPopup, gocui.KeyEsc, gocui.ModNone, closeTagPopup(gui, state))
		_ = gui.SetKeybinding(viewTagPopup, 'q', gocui.ModNone, closeTagPopup(gui, state))
		_ = gui.SetKeybinding(viewTagPopup, 'a', gocui.ModNone, tagPopupSelectAll(state))
		_ = gui.SetKeybinding(viewTagPopup, 'x', gocui.ModNone, tagPopupClearAll(state))

		return nil
	}
}

// renderTagPopup draws the tag list with toggle checkboxes and role info.
func renderTagPopup(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewTagPopup)
	if err != nil {
		return
	}
	v.Clear()

	pb := state.currentPlaybook()
	if pb == nil {
		return
	}

	allTags := pb.AllTags()
	for i, tag := range allTags {
		cursor := "  "
		if i == state.TagPopupCursor {
			cursor = "> "
		}
		checkbox := "[ ]"
		if state.SelectedTags[tag] {
			checkbox = "[x]"
		}
		// Show which roles have this tag
		var roles []string
		for role, tags := range pb.RoleTags {
			for _, t := range tags {
				if t == tag {
					roles = append(roles, role)
					break
				}
			}
		}
		sort.Strings(roles)
		roleInfo := strings.Join(roles, ", ")
		fmt.Fprintf(v, "%s%s %s  \033[90m(%s)\033[0m\n", cursor, checkbox, tag, roleInfo)
	}
}

func tagPopupUp(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		if state.TagPopupCursor > 0 {
			state.TagPopupCursor--
		}
		renderTagPopup(g, state)
		return nil
	}
}

func tagPopupDown(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		allTags := pb.AllTags()
		if state.TagPopupCursor < len(allTags)-1 {
			state.TagPopupCursor++
		}
		renderTagPopup(g, state)
		return nil
	}
}

func tagPopupToggle(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		allTags := pb.AllTags()
		if state.TagPopupCursor >= len(allTags) {
			return nil
		}
		tag := allTags[state.TagPopupCursor]
		if state.SelectedTags[tag] {
			delete(state.SelectedTags, tag)
		} else {
			if state.SelectedTags == nil {
				state.SelectedTags = make(map[string]bool)
			}
			state.SelectedTags[tag] = true
		}
		// Reset variable selection since filtered list changes
		state.SelectedVar = 0
		state.VarScrollOffset = 0
		renderTagPopup(g, state)
		return nil
	}
}

func tagPopupSelectAll(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		if state.SelectedTags == nil {
			state.SelectedTags = make(map[string]bool)
		}
		for _, t := range pb.AllTags() {
			state.SelectedTags[t] = true
		}
		state.SelectedVar = 0
		state.VarScrollOffset = 0
		renderTagPopup(g, state)
		return nil
	}
}

func tagPopupClearAll(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		state.SelectedTags = make(map[string]bool)
		state.SelectedVar = 0
		state.VarScrollOffset = 0
		renderTagPopup(g, state)
		return nil
	}
}

func closeTagPopup(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		state.TagPopupOpen = false
		gui.DeleteKeybindings(viewTagPopup)
		if err := gui.DeleteView(viewTagPopup); err != nil {
			return err
		}
		state.ActivePane = PaneTags
		_, err := gui.SetCurrentView(viewTags)
		return err
	}
}

// --- Search ---

// openSearch creates a vim-style "/" search overlay.
func openSearch(g *gocui.Gui, state *AppState, pane int) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		state.SearchMode = true
		state.SearchPane = pane

		maxX, maxY := gui.Size()
		searchW := maxX * 50 / 100
		if searchW < 30 {
			searchW = 30
		}
		x0 := (maxX - searchW) / 2
		y0 := maxY/2 - 1
		x1 := x0 + searchW
		y1 := y0 + 2

		sv, err := gui.SetView(viewSearch, x0, y0, x1, y1, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		sv.Title = " / Search "
		sv.Frame = true
		sv.Editable = true
		sv.FrameColor = gocui.ColorCyan
		sv.Clear()
		gui.Cursor = true

		if _, err := gui.SetCurrentView(viewSearch); err != nil {
			return err
		}

		_ = gui.SetKeybinding(viewSearch, gocui.KeyEnter, gocui.ModNone, confirmSearch(gui, state))
		_ = gui.SetKeybinding(viewSearch, gocui.KeyEsc, gocui.ModNone, cancelSearch(gui, state))

		return nil
	}
}

func confirmSearch(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		query := strings.TrimSpace(v.Buffer())
		state.SearchQuery = strings.ToLower(query)

		if err := closeSearch(gui, state); err != nil {
			return err
		}

		if state.SearchQuery != "" {
			doSearch(state, 1)
		}
		return nil
	}
}

func cancelSearch(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		return closeSearch(gui, state)
	}
}

func closeSearch(g *gocui.Gui, state *AppState) error {
	state.SearchMode = false
	g.Cursor = false

	g.DeleteKeybindings(viewSearch)
	if err := g.DeleteView(viewSearch); err != nil {
		return err
	}

	state.ActivePane = state.SearchPane
	return setFocus(g, state)
}

// doSearch finds the next/prev match from the current position.
// direction: +1 forward, -1 backward.
func doSearch(state *AppState, direction int) {
	if state.SearchQuery == "" {
		return
	}

	query := state.SearchQuery

	switch state.ActivePane {
	case PanePlaybooks:
		count := len(state.Playbooks)
		for i := 1; i <= count; i++ {
			idx := (state.SelectedPB + i*direction + count) % count
			if strings.Contains(strings.ToLower(state.Playbooks[idx].Name), query) {
				state.SelectedPB = idx
				state.SelectedVar = 0
				state.VarScrollOffset = 0
				return
			}
		}

	case PaneVariables:
		vars := state.FilteredVariables()
		if len(vars) == 0 {
			return
		}
		count := len(vars)
		for i := 1; i <= count; i++ {
			idx := (state.SelectedVar + i*direction + count) % count
			v := vars[idx]
			target := strings.ToLower(v.Name + " " + v.Description + " " + v.Default)
			if strings.Contains(target, query) {
				state.SelectedVar = idx
				return
			}
		}
	}
}

// searchNext jumps to the next search match (n key).
func searchNext(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		doSearch(state, 1)
		return nil
	}
}

// searchPrev jumps to the previous search match (N key).
func searchPrev(state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		doSearch(state, -1)
		return nil
	}
}

// openHelp shows a help overlay with all keybindings.
func openHelp(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		maxX, maxY := gui.Size()
		w := 56
		h := 42
		if w > maxX-4 {
			w = maxX - 4
		}
		if h > maxY-4 {
			h = maxY - 4
		}
		x0 := (maxX - w) / 2
		y0 := (maxY - h) / 2

		hv, err := gui.SetView(viewHelp, x0, y0, x0+w, y0+h, 0)
		if err != nil && !errors.Is(err, gocui.ErrUnknownView) {
			return err
		}

		hv.Title = " Help "
		hv.Frame = true
		hv.FrameColor = gocui.ColorCyan
		hv.Clear()

		help := `
 ┌──────────────────────────────────────────────────┐
 │ Ansible command constructor                      │
 │ https://github.com/ay-b/ansible-cli-consructor   │
 └──────────────────────────────────────────────────┘

 GLOBAL
   Tab          Cycle panes (playbooks/tags/vars/cmd)
   Ctrl+C       Quit
   q            Quit
   ?            Show this help

 PLAYBOOKS PANE
   j / Down     Next playbook
   k / Up       Previous playbook
   PgDn         Page down
   PgUp         Page up
   Enter        Focus variables pane
   /            Search playbooks
   n            Next search match
   N            Previous search match

 TAGS PANE
   Enter        Open tag selector

 TAG SELECTOR
   j / Down     Next tag
   k / Up       Previous tag
   Space/Enter  Toggle tag on/off
   a            Select all tags
   x            Clear all tags
   Escape/q     Close selector

 VARIABLES PANE
   j / Down     Next variable
   k / Up       Previous variable
   PgDn         Page down
   PgUp         Page up
   Enter        Edit selected variable
   d            Reset variable to default
   /            Search variables
   n            Next search match
   N            Previous search match

 EDIT OVERLAY
   Enter        Confirm value
   Escape       Cancel edit

 COMMAND PANE
   Enter/c/C    Copy command to clipboard`

		fmt.Fprint(hv, help)

		if _, err := gui.SetCurrentView(viewHelp); err != nil {
			return err
		}

		_ = gui.SetKeybinding(viewHelp, gocui.KeyEsc, gocui.ModNone, closeHelp(gui, state))
		_ = gui.SetKeybinding(viewHelp, gocui.KeyEnter, gocui.ModNone, closeHelp(gui, state))
		_ = gui.SetKeybinding(viewHelp, '?', gocui.ModNone, closeHelp(gui, state))
		_ = gui.SetKeybinding(viewHelp, 'q', gocui.ModNone, closeHelp(gui, state))

		return nil
	}
}

func closeHelp(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(gui *gocui.Gui, v *gocui.View) error {
		g.DeleteKeybindings(viewHelp)
		if err := g.DeleteView(viewHelp); err != nil {
			return err
		}
		return setFocus(gui, state)
	}
}

// centeredScroll computes origin (first visible item) and cursor (position
// within the viewport) so the selected item stays vertically centered.
// At the top and bottom of the list the cursor drifts from center to
// allow reaching the first/last items without dead space.
//
//	selected  - zero-based index of the selected item
//	total     - total number of items in the list
//	viewH     - number of visible rows in the viewport
//
// Returns (origin, cursor) where origin is the first visible index and
// cursor is the offset within the viewport for the highlight.
func centeredScroll(selected, total, viewH int) (int, int) {
	if total <= viewH {
		// Everything fits — no scrolling needed
		return 0, selected
	}

	half := viewH / 2

	// Top region: not enough room above to center
	if selected <= half {
		return 0, selected
	}

	// Bottom region: not enough room below to center
	maxOrigin := total - viewH
	if selected >= total-viewH+half {
		origin := maxOrigin
		cursor := selected - origin
		return origin, cursor
	}

	// Middle region: keep selected at center
	origin := selected - half
	return origin, half
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
