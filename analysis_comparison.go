package main

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// stripANSI removes ANSI escape codes so that column widths are based on
// visible characters, not on invisible escape sequences.
func stripANSI(s string) string {
	out := strings.Builder{}
	i := 0
	for i < len(s) {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			i = j + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

// vmiComparison aggregates results from three sources:
//
//	0 – Redfish
//	1 – D-Bus  (hypervisor-network-manager)
//	2 – BIOS attributes (pldmtool)
type vmiComparison struct {
	out           io.Writer
	values        [3][]biosDisplayValue
	stage, active int
	complete      [3]bool
}

func (v *vmiComparison) Write(p []byte) (int, error) { return len(p), nil }
func (v *vmiComparison) WriteReport(string) error    { return nil }
func (v *vmiComparison) WriteBIOSValues(values []biosDisplayValue, active int) {
	v.publish(2, values, active)
}
func (v *vmiComparison) publish(stage int, values []biosDisplayValue, active int) {
	v.stage, v.active = stage, active
	// The query worker continues updating its slice between publications.
	// Keep a snapshot so another source can render safely in the meantime.
	v.values[stage] = append([]biosDisplayValue(nil), values...)
	if out, ok := v.out.(interface{ WriteReport(string) error }); ok {
		out.WriteReport(v.format())
	}
}

// colourValue applies ANSI colours to a cell value for display in the
// comparison table.  ERROR is red, pending/querying is dimmed/yellow,
// real values are green, dashes and "Not reported" are grey.
func colourValue(text string) string {
	switch {
	case text == "ERROR":
		return ansiBoldRed + text + ansiReset
	case text == "Pending":
		return ansiDim + text + ansiReset
	case text == "Querying…":
		return ansiYellow + text + ansiReset
	case text == "—" || text == "Not reported":
		return ansiGray + text + ansiReset
	case text == "(empty)":
		return ansiGray + text + ansiReset
	default:
		return ansiGreen + text + ansiReset
	}
}

const numSources = 3

func (v *vmiComparison) format() string {
	var out strings.Builder
	// Coloured source-column headers (kept plain for jump-key matching).
	colourNames := [numSources]string{
		ansiBoldBlue + "1. REDFISH" + ansiReset,
		ansiBoldCyan + "2. D-BUS" + ansiReset,
		ansiBoldYellow + "3. BIOS ATTRIBUTES" + ansiReset,
	}
	plainNames := [numSources]string{"1. REDFISH", "2. D-BUS", "3. BIOS ATTRIBUTES"}

	// ── Title banner ─────────────────────────────────────────────────────────
	fmt.Fprintf(&out, "%sVMI COMPARISON%s  %sLeft/Right: scroll columns | Up/Down: scroll rows%s\n",
		ansiBoldCyan, ansiReset, ansiGray, ansiReset)
	allDone := true
	for _, complete := range v.complete {
		allDone = allDone && complete
	}
	if allDone {
		fmt.Fprintf(&out, "%s✔ Checks complete%s\n", ansiGreen, ansiReset)
	} else {
		fmt.Fprintf(&out, "%sChecking: %s%s%s\n", ansiYellow, ansiBold, plainNames[v.stage], ansiReset)
	}

	// ── Build rows ────────────────────────────────────────────────────────
	type row [numSources + 1]string // [label, src0..src3]
	var rawRows []row
	var details []string

	for iface := 0; iface < 2; iface++ {
		for _, version := range []int{4, 6} {
			prefix := fmt.Sprintf("vmi_if%d_ipv%d_", iface, version)
			groupLabel := fmt.Sprintf("Interface %d / IPV%d", iface, version)
			rawRows = append(rawRows, row{groupLabel})

			for _, field := range []struct{ key, label string }{
				{"ipaddr", "IP address"},
				{"gateway", "Gateway"},
				{"method", "Method"},
				{"prefix_length", "Prefix length"},
				{"enabled", "Enabled"},
			} {
				r := row{"  " + field.label, "—", "—", "—"}
				for source := range v.values {
					key := prefix + field.key
					if source == 0 && field.key == "enabled" {
						key = prefix + "interface_enabled"
					}
					// "Enabled" is not reported by BIOS (source 2).
					supported := field.key != "enabled" || source != 2
					if supported {
						r[source+1] = "Pending"
						if source < v.stage || v.complete[source] {
							r[source+1] = "Not reported"
						}
					}
					for index, value := range v.values[source] {
						if value.attribute != key {
							continue
						}
						text := "Pending"
						if source == v.stage && index == v.active {
							text = "Querying…"
						}
						if value.done {
							text = value.value
							if text == "" {
								text = "(empty)"
							}
						}
						if value.err != "" {
							text = "ERROR"
						}
						r[source+1] = text
					}
				}
				rawRows = append(rawRows, r)
			}
		}
	}

	// ── Compute column widths ─────────────────────────────────────────────
	widths := [numSources + 1]int{22, 17, 17, 19}
	for _, r := range rawRows {
		for col, text := range r {
			if n := utf8.RuneCountInString(stripANSI(text)); n > widths[col] {
				widths[col] = n
			}
		}
	}
	totalWidth := widths[0]
	for i := 1; i <= numSources; i++ {
		totalWidth += 3 + widths[i]
	}

	// ── Helpers ───────────────────────────────────────────────────────────
	pad := func(text string, width int) string {
		visible := utf8.RuneCountInString(stripANSI(text))
		if width > visible {
			return text + strings.Repeat(" ", width-visible)
		}
		return text
	}
	writeSep := func() {
		out.WriteString(ansiBoldBlue)
		for col := 0; col <= numSources; col++ {
			if col > 0 {
				out.WriteString("─┼─")
			}
			out.WriteString(strings.Repeat("─", widths[col]))
		}
		out.WriteString(ansiReset)
		out.WriteByte('\n')
	}
	writeRow := func(r row, isGroup bool) {
		if isGroup {
			label := r[0]
			padding := totalWidth - utf8.RuneCountInString(stripANSI(label))
			leftPad := padding / 2
			rightPad := padding - leftPad
			out.WriteString(ansiBoldMagenta)
			out.WriteString(strings.Repeat(" ", leftPad))
			out.WriteString(ansiBold + label + ansiReset + ansiBoldMagenta)
			out.WriteString(strings.Repeat(" ", rightPad))
			out.WriteString(ansiReset)
			out.WriteByte('\n')
			return
		}
		for col, text := range r {
			if col > 0 {
				out.WriteString(" │ ")
			}
			out.WriteString(pad(text, widths[col]))
		}
		out.WriteByte('\n')
	}

	// ── Header row ────────────────────────────────────────────────────────
	// "PROPERTY" must start the line (test uses HasPrefix).
	out.WriteString(pad("PROPERTY", widths[0]))
	for i := 0; i < numSources; i++ {
		out.WriteString(" │ ")
		out.WriteString(pad(colourNames[i], widths[i+1]))
	}
	out.WriteByte('\n')
	writeSep()

	// ── Data rows ─────────────────────────────────────────────────────────
	for i, r := range rawRows {
		isGroup := i == 0 || rawRows[i][0][0] != ' '
		writeRow(r, isGroup)
	}
	writeSep()

	// ── Footer note ───────────────────────────────────────────────────────
	fmt.Fprintf(&out, "%sEnabled: Redfish = interface; D-Bus = address. — = not provided by source.%s\n",
		ansiGray, ansiReset)

	// ── Error / extra details ─────────────────────────────────────────────
	for source, values := range v.values {
		for _, value := range values {
			if value.err != "" {
				details = append(details, fmt.Sprintf("%s%s / %s:%s %s%s%s",
					ansiBold, plainNames[source], value.attribute, ansiReset,
					ansiRed, value.err, ansiReset))
			}
			if source == 0 && (strings.HasSuffix(value.attribute, "_dhcp_enabled") || strings.HasSuffix(value.attribute, "_dhcp_mode") || strings.HasSuffix(value.attribute, "_autoconfig")) {
				details = append(details, fmt.Sprintf("%sRedfish %s:%s %s%s%s",
					ansiCyan, value.attribute, ansiReset,
					ansiGreen, value.value, ansiReset))
			}
		}
	}
	if len(details) > 0 {
		fmt.Fprintf(&out, "\n%sDetails%s\n", ansiBoldCyan, ansiReset)
		fmt.Fprintln(&out, strings.Join(details, "\n"))
	}
	return out.String()
}
