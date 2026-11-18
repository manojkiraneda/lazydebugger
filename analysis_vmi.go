package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

func queryVMI(ctx context.Context, options []string, command string) ([]byte, error) {
	return queryVMIInput(ctx, options, command, nil)
}
func queryVMIInput(ctx context.Context, options []string, command string, input []byte) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "ssh", analysisSSHArgs(options, command)...)
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = bytes.NewReader(input)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if commandCtx.Err() != nil {
		return nil, commandCtx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, terminalSafeText(stderr.String(), 800))
	}
	return stdout.Bytes(), nil
}

func runVMINetwork(ctx context.Context, options []string, plugin analysisPlugin, out io.Writer) error {
	stage := &vmiComparison{out: out, active: -1}
	var mu sync.Mutex // guards stage.values and failures during parallel stages
	var failures []error

	addFailure := func(err error) {
		mu.Lock()
		failures = append(failures, err)
		mu.Unlock()
	}

	// ── Stage 0: Redfish ─────────────────────────────────────────────────────
	var redfishResults []biosDisplayValue
	stage.publish(0, nil, -1)
	session, loginErr := createRedfishSession(ctx, options, plugin.auth)
	if loginErr != nil {
		redfishResults = append(redfishResults, biosDisplayValue{attribute: "Redfish session login", done: true, err: loginErr.Error()})
		addFailure(loginErr)
	} else {
		for iface, path := range plugin.RedfishPaths {
			data, err := session.request(ctx, options, "GET", path)
			if ctx.Err() != nil {
				break
			}
			var values []biosDisplayValue
			if err == nil {
				values, err = parseRedfishVMI(data, iface)
			}
			if err != nil {
				redfishResults = append(redfishResults, biosDisplayValue{attribute: fmt.Sprintf("eth%d Redfish GET", iface), done: true, err: err.Error()})
				addFailure(err)
			} else {
				redfishResults = append(redfishResults, values...)
			}
			stage.publish(0, redfishResults, -1)
		}
		logoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, logoutErr := session.request(logoutCtx, options, "DELETE", session.location)
		cancel()
		session.token = ""
		if logoutErr != nil {
			redfishResults = append(redfishResults, biosDisplayValue{attribute: "Redfish session logout", done: true, err: logoutErr.Error()})
			addFailure(logoutErr)
		}
	}
	stage.complete[0] = true
	stage.publish(0, redfishResults, -1)
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// ── Stages 1+2: D-Bus and BIOS run in parallel ───────────────────────────
	var wg sync.WaitGroup
	wg.Add(2)

	// Stage 1 – D-Bus
	go func() {
		defer wg.Done()
		type query struct{ command, attribute string }
		var queries []query
		var results []biosDisplayValue
		for iface := 0; iface < 2; iface++ {
			for _, version := range []int{4, 6} {
				object := fmt.Sprintf("/xyz/openbmc_project/network/hypervisor/eth%d/ipv%d/addr0", iface, version)
				for _, property := range []struct{ iface, name, field string }{
					{"xyz.openbmc_project.Network.IP", "Address", "ipaddr"},
					{"xyz.openbmc_project.Network.IP", "Gateway", "gateway"},
					{"xyz.openbmc_project.Network.IP", "Origin", "method"},
					{"xyz.openbmc_project.Network.IP", "PrefixLength", "prefix_length"},
					{"xyz.openbmc_project.Object.Enable", "Enabled", "enabled"},
				} {
					attribute := fmt.Sprintf("vmi_if%d_ipv%d_%s", iface, version, property.field)
					command := fmt.Sprintf("busctl get-property xyz.openbmc_project.Network.Hypervisor %s %s %s", object, property.iface, property.name)
					queries = append(queries, query{command, attribute})
					results = append(results, biosDisplayValue{attribute: attribute})
				}
			}
		}
		mu.Lock()
		stage.publish(1, results, 0)
		mu.Unlock()
		for i, q := range queries {
			data, err := queryVMI(ctx, options, q.command)
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			if err == nil {
				results[i].value, err = parseBusctlValue(string(data))
				if strings.HasSuffix(q.attribute, "_method") && err == nil {
					parts := strings.Split(results[i].value, ".")
					results[i].value = parts[len(parts)-1]
				}
			}
			results[i].done = true
			if err != nil {
				results[i].err = err.Error()
				failures = append(failures, err)
			}
			stage.publish(1, results, -1)
			mu.Unlock()
		}
		mu.Lock()
		stage.complete[1] = true
		stage.publish(1, results, -1)
		mu.Unlock()
	}()

	// Stage 2 – BIOS (pldmtool)
	go func() {
		defer wg.Done()
		biosStage := &biosForwarder{cmp: stage, mu: &mu}
		if err := runBIOSValueCommands(ctx, options, plugin.Commands, biosStage); err != nil {
			addFailure(err)
		}
		mu.Lock()
		stage.complete[2] = true
		stage.publish(2, stage.values[2], -1)
		mu.Unlock()
	}()

	wg.Wait()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if _, ok := out.(interface{ WriteReport(string) error }); !ok {
		fmt.Fprint(out, stage.format())
	}
	return errors.Join(failures...)
}

// biosForwarder is an io.Writer adapter that routes WriteBIOSValues calls from
// runBIOSValueCommands into the shared vmiComparison under the given mutex.
type biosForwarder struct {
	cmp *vmiComparison
	mu  *sync.Mutex
}

func (b *biosForwarder) Write(p []byte) (int, error) { return len(p), nil }
func (b *biosForwarder) WriteReport(string) error    { return nil }
func (b *biosForwarder) WriteBIOSValues(values []biosDisplayValue, active int) {
	b.mu.Lock()
	b.cmp.publish(2, values, active)
	b.mu.Unlock()
}

func parseBusctlValue(output string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(output), " ", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid busctl response: %q", terminalSafeText(output, 200))
	}
	value := strings.TrimSpace(parts[1])
	switch parts[0] {
	case "s":
		var text string
		if err := json.Unmarshal([]byte(value), &text); err != nil {
			return "", err
		}
		return terminalSafeText(text, 4096), nil
	case "y":
		number, err := strconv.ParseUint(value, 10, 8)
		return strconv.FormatUint(number, 10), err
	case "b":
		flag, err := strconv.ParseBool(value)
		return strconv.FormatBool(flag), err
	default:
		return "", fmt.Errorf("unsupported busctl type %q", parts[0])
	}
}

// Redfish may omit an IP family on firmware that does not expose it. Display
// absent properties explicitly, preserving false and numeric zero values.
func parseRedfishVMI(data []byte, iface int) ([]biosDisplayValue, error) {
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	if _, ok := document["error"]; ok {
		return nil, fmt.Errorf("Redfish returned an error response")
	}
	if _, ok := document["Id"]; !ok {
		if _, ipv4 := document["IPv4Addresses"]; !ipv4 {
			if _, ipv6 := document["IPv6Addresses"]; !ipv6 {
				return nil, fmt.Errorf("response is not an EthernetInterface resource")
			}
		}
	}
	var results []biosDisplayValue
	for _, version := range []int{4, 6} {
		var addresses []map[string]json.RawMessage
		key := fmt.Sprintf("IPv%dAddresses", version)
		raw := document[key]
		if len(raw) == 0 {
			raw = document[fmt.Sprintf("IPv%dStaticAddresses", version)]
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &addresses); err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
		}
		address := map[string]json.RawMessage{}
		if len(addresses) > 0 {
			address = addresses[0]
		}
		fields := []struct{ key, label string }{{"Address", "ipaddr"}, {"Gateway", "gateway"}, {"AddressOrigin", "method"}, {"PrefixLength", "prefix_length"}}
		for _, field := range fields {
			value := "Not reported"
			raw := address[field.key]
			if version == 6 && field.key == "Gateway" && len(raw) == 0 {
				raw = document["IPv6DefaultGateway"]
			}
			if len(raw) > 0 && string(raw) != "null" {
				var text string
				if json.Unmarshal(raw, &text) == nil {
					value = terminalSafeText(text, 4096)
				} else {
					value = string(raw)
				}
			} else if version == 4 && field.key == "PrefixLength" {
				var mask string
				if json.Unmarshal(address["SubnetMask"], &mask) == nil {
					if ip := net.ParseIP(mask).To4(); ip != nil {
						ones, bits := net.IPMask(ip).Size()
						if bits == 32 {
							value = strconv.Itoa(ones)
						}
					}
				}
			}
			results = append(results, biosDisplayValue{attribute: fmt.Sprintf("vmi_if%d_ipv%d_%s", iface, version, field.label), value: value, done: true})
		}

		settings := []struct{ object, property, label string }{{"DHCPv4", "DHCPEnabled", "dhcp_enabled"}}
		if version == 6 {
			settings = []struct{ object, property, label string }{
				{"DHCPv6", "OperatingMode", "dhcp_mode"},
				{"StatelessAddressAutoConfig", "IPv6AutoConfigEnabled", "autoconfig"},
			}
		}
		for _, setting := range settings {
			var properties map[string]json.RawMessage
			if raw := document[setting.object]; len(raw) > 0 {
				if err := json.Unmarshal(raw, &properties); err != nil {
					return nil, fmt.Errorf("%s: %w", setting.object, err)
				}
			}
			value := "Not reported"
			if raw := properties[setting.property]; len(raw) > 0 && string(raw) != "null" {
				var text string
				if json.Unmarshal(raw, &text) == nil {
					value = terminalSafeText(text, 4096)
				} else {
					value = string(raw)
				}
			}
			results = append(results, biosDisplayValue{attribute: fmt.Sprintf("vmi_if%d_ipv%d_%s", iface, version, setting.label), value: value, done: true})
		}
		if raw := document["InterfaceEnabled"]; len(raw) > 0 {
			results = append(results, biosDisplayValue{attribute: fmt.Sprintf("vmi_if%d_ipv%d_interface_enabled", iface, version), value: string(raw), done: true})
		}
	}
	return results, nil
}
