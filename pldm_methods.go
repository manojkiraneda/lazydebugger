package main

import (
	"fmt"

	"github.com/Lifailon/lazydebugger/parsers/pldm"
	"github.com/awesome-gocui/gocui"
)

// Handle Space key in logs view for PLDM parsing
func (app *App) handlePldmParse(g *gocui.Gui, v *gocui.View) error {
	if app.selectFilterMode != "pldm_verbose" {
		app.showInterfaceInfo(g, false, "PLDM parser only works in pldm_verbose mode")
		return nil
	}
	
	if len(app.filteredLogLines) == 0 {
		app.showInterfaceInfo(g, true, "No log lines available")
		return nil
	}
	
	currentLineIndex := app.logScrollPos + app.selectedLogLine
	
	if currentLineIndex < 0 || currentLineIndex >= len(app.filteredLogLines) {
		app.showInterfaceInfo(g, true, fmt.Sprintf("Line index out of bounds: %d (total: %d)", currentLineIndex, len(app.filteredLogLines)))
		return nil
	}
	
	currentLine := app.filteredLogLines[currentLineIndex]
	hexBytes := pldm.ExtractHexBytes(currentLine)
	
	dockerView, err := g.View("docker")
	if err != nil {
		return err
	}
	
	return pldm.ParseAndDisplay(dockerView, hexBytes, app.selectedFrameColor, app.selectedTitleColor)
}

// Move selection up in PLDM mode
func (app *App) movePldmSelectionUp(g *gocui.Gui, v *gocui.View) error {
	if app.selectFilterMode != "pldm_verbose" {
		return app.scrollUpLogs(1)
	}

	// selectedLogLine is always 0 in PLDM mode; the selected line is logScrollPos.
	if app.logScrollPos > 0 {
		app.logScrollPos--
		app.autoScroll = false
		if !app.testMode {
			app.updateStatus()
		}
		app.updateLogsView(false)
		app.updatePldmPanel(g)
	}

	return nil
}

// Move selection down in PLDM mode
func (app *App) movePldmSelectionDown(g *gocui.Gui, v *gocui.View) error {
	if app.selectFilterMode != "pldm_verbose" {
		return app.scrollDownLogs(1)
	}

	// selectedLogLine is always 0 in PLDM mode; the selected line is logScrollPos.
	if app.logScrollPos < len(app.filteredLogLines)-1 {
		app.logScrollPos++
		if !app.testMode {
			app.updateStatus()
		}
		app.updateLogsView(false)
		app.updatePldmPanel(g)
	}

	return nil
}

// Scroll the PLDM parser panel up by step lines
func (app *App) scrollPldmPanelUp(g *gocui.Gui, step int) error {
	v, err := g.View("docker")
	if err != nil {
		return nil
	}
	_, oy := v.Origin()
	newOy := oy - step
	if newOy < 0 {
		newOy = 0
	}
	v.SetOrigin(0, newOy) //nolint:errcheck
	return nil
}

// Scroll the PLDM parser panel down by step lines
func (app *App) scrollPldmPanelDown(g *gocui.Gui, step int) error {
	v, err := g.View("docker")
	if err != nil {
		return nil
	}
	_, oy := v.Origin()
	v.SetOrigin(0, oy+step) //nolint:errcheck
	return nil
}

// Update docker panel with current line's PLDM data
func (app *App) updatePldmPanel(g *gocui.Gui) {
	if app.selectFilterMode != "pldm_verbose" {
		return
	}
	
	if len(app.filteredLogLines) == 0 {
		return
	}
	
	currentLineIndex := app.logScrollPos + app.selectedLogLine
	if currentLineIndex < 0 || currentLineIndex >= len(app.filteredLogLines) {
		return
	}
	
	currentLine := app.filteredLogLines[currentLineIndex]
	hexBytes := pldm.ExtractHexBytes(currentLine)
	
	dockerView, err := g.View("docker")
	if err != nil {
		return
	}
	
	pldm.ParseAndDisplay(dockerView, hexBytes, app.selectedFrameColor, app.selectedTitleColor)
}

