package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/awesome-gocui/gocui"
)

const (
	viewPlaybooks = "playbooks"
	viewVariables = "variables"
	viewCommand   = "command"
	viewEditor    = "editor"
	viewSearch    = "search"
	viewHelp      = "help"
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

	// Right pane: Variables
	if v, err := g.SetView(viewVariables, splitX, 0, maxX-1, cmdY-1, 0); err != nil {
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
	if v, err := g.View(viewVariables); err == nil {
		pb := state.currentPlaybook()
		roleInfo := ""
		if pb != nil && len(pb.Roles) > 0 {
			roleInfo = fmt.Sprintf(" | %d roles", len(pb.Roles))
		}
		varCount := 0
		if pb != nil {
			varCount = len(pb.Variables)
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
		if state.ActivePane == PaneCommand {
			v.Title = " Command [Enter/C: copy] [*] "
			v.FrameColor = gocui.ColorGreen
		} else {
			v.Title = " Command [Enter/C: copy] "
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

// renderVariables draws the variable list for the selected playbook.
func renderVariables(g *gocui.Gui, state *AppState) {
	v, err := g.View(viewVariables)
	if err != nil {
		return
	}
	v.Clear()

	pb := state.currentPlaybook()
	if pb == nil || len(pb.Variables) == 0 {
		fmt.Fprintln(v, "  (no variables)")
		return
	}

	viewW, viewH := v.Size()
	if viewH <= 0 {
		viewH = 1
	}

	// Vim-style centered scrolling
	origin, _ := centeredScroll(state.SelectedVar, len(pb.Variables), viewH)
	state.VarScrollOffset = origin
	nameW := 30
	valW := 25
	if viewW > 100 {
		nameW = 35
		valW = 30
	}

	for i := state.VarScrollOffset; i < len(pb.Variables) && i < state.VarScrollOffset+viewH; i++ {
		vr := pb.Variables[i]
		cursor := "  "
		if i == state.SelectedVar && state.ActivePane == PaneVariables {
			cursor = "> "
		}

		displayVal := vr.Default
		if vr.UserValue != "" {
			displayVal = vr.UserValue
		}

		// Truncate fields
		name := truncate(vr.Name, nameW)
		val := truncate(displayVal, valW)
		desc := ""
		if vr.Description != "" {
			remaining := viewW - nameW - valW - 8 // cursor + separators
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

	cmd := generateCommand(pb)
	if state.Notification != "" {
		fmt.Fprintf(v, "%s\n\n  \033[32m%s\033[0m", cmd, state.Notification)
	} else {
		fmt.Fprint(v, cmd)
	}
}

// generateCommand builds the ansible-playbook command string.
func generateCommand(pb *Playbook) string {
	var b strings.Builder
	b.WriteString("ansible-playbook ")
	b.WriteString(pb.Path)

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
		if state.EditMode {
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
		pb := state.currentPlaybook()
		if pb != nil && state.SelectedVar < len(pb.Variables)-1 {
			state.SelectedVar++
		}
		return nil
	}
}

// openEditor creates an overlay view for editing a variable value.
func openEditor(g *gocui.Gui, state *AppState) func(*gocui.Gui, *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		pb := state.currentPlaybook()
		if pb == nil || len(pb.Variables) == 0 {
			return nil
		}
		vr := &pb.Variables[state.SelectedVar]
		if vr.IsComplex {
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
		if pb != nil && state.SelectedVar < len(pb.Variables) {
			pb.Variables[state.SelectedVar].UserValue = val
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
		if pb != nil && state.SelectedVar < len(pb.Variables) {
			pb.Variables[state.SelectedVar].UserValue = ""
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

		cmd := generateCommand(pb)
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

// currentPlaybook returns the currently selected playbook.
func (s *AppState) currentPlaybook() *Playbook {
	if s.SelectedPB < 0 || s.SelectedPB >= len(s.Playbooks) {
		return nil
	}
	return &s.Playbooks[s.SelectedPB]
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
		pb := state.currentPlaybook()
		if pb == nil {
			return nil
		}
		state.SelectedVar += viewH
		if state.SelectedVar >= len(pb.Variables) {
			state.SelectedVar = len(pb.Variables) - 1
		}
		return nil
	}
}

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
		pb := state.currentPlaybook()
		if pb == nil {
			return
		}
		count := len(pb.Variables)
		for i := 1; i <= count; i++ {
			idx := (state.SelectedVar + i*direction + count) % count
			v := pb.Variables[idx]
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
		h := 26
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
 GLOBAL
   Tab          Cycle panes (playbooks/vars/cmd)
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
