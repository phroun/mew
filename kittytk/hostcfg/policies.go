package hostcfg

// The settings a user changes while the desktop is running, and where they are
// kept between runs.
//
// kittytk.ini is written by a person and read whole. `current` is written by
// the desktop -- one line per setting the user has changed in the interface --
// and read straight after it, so it overlays the ini without replacing it. It
// deliberately has no extension: an editable-looking name would invite hand
// edits that the next change overwrites.
//
// The environment (and any switch) sits on top of both, and STARTS a session
// rather than governing it: it decides the value the desktop comes up with,
// and is never written down. The first time the user throws that switch, the
// change is kept in `current` and their choice is what answers from then on.
//
//	built in  ->  kittytk.ini  ->  current  ->  the environment
//
// Which layer answered is kept alongside the value and shown beside the switch,
// so a setting nobody in this session chose can still say where it came from.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// CurrentName is the overlay's basename, and CurrentSection the ini section
// the connection policies are written under.
const (
	CurrentName    = "current"
	CurrentSection = "service"
)

// The policies this file keeps, named as the display service names them so
// there is one spelling for the ini key, the overlay key, and the change the
// Connections window reports.
const (
	PolicyPreTrustedOnly = display.PolicyPreTrustedOnly
	PolicyPromptLocal    = display.PolicyPromptLocal
)

// The words shown for where a value came from. A file names itself; the
// environment names the variable, which is what the reader has to unset.
const (
	OriginBuiltIn = "built in"
	OriginCurrent = CurrentName
)

// policyEnv is the variable that starts each policy.
var policyEnv = map[string]string{
	PolicyPreTrustedOnly: display.PreTrustedOnlyEnv,
	PolicyPromptLocal:    display.PromptLocalEnv,
}

// CurrentPath is where the overlay lives: the user config dir only, never
// beside the executable or in the working directory. It is this machine's
// record of what this user did, not something to ship with a program.
func CurrentPath() string {
	return filepath.Join(client.ConfigDir(), CurrentName)
}

// notePolicy records that the layer being read set this policy.
func (c *Config) notePolicy(name string) {
	if c.policyOrigins == nil {
		c.policyOrigins = map[string]string{}
	}
	layer := c.layer
	if layer == "" {
		layer = OriginBuiltIn
	}
	c.policyOrigins[name] = layer
}

// ResolvePreTrustedOnly and ResolvePromptLocal are the values to start with:
// the environment if it says anything, else what the files left.
func (c Config) ResolvePreTrustedOnly() bool {
	if v, ok := envBool(display.PreTrustedOnlyEnv); ok {
		return v
	}
	return c.PreTrustedOnly
}

func (c Config) ResolvePromptLocal() bool {
	if v, ok := envBool(display.PromptLocalEnv); ok {
		return v
	}
	return c.PromptLocal
}

// PolicyOrigins says where each policy's starting value came from, with the
// environment named where it is what answered.
func (c Config) PolicyOrigins() map[string]string {
	out := map[string]string{}
	for _, name := range []string{PolicyPreTrustedOnly, PolicyPromptLocal} {
		origin := c.policyOrigins[name]
		if origin == "" {
			origin = OriginBuiltIn
		}
		if _, ok := envBool(policyEnv[name]); ok {
			origin = policyEnv[name]
		}
		out[name] = origin
	}
	return out
}

// ApplyPolicies hands a display config what this configuration resolved, and
// the way back: a policy the user changes in the Connections window is written
// to `current`, and what comes back is what the window shows beside it.
func (c Config) ApplyPolicies(dcfg *display.Config) {
	dcfg.PreTrustedOnly = c.ResolvePreTrustedOnly()
	dcfg.PromptLocal = c.ResolvePromptLocal()
	dcfg.PolicyOrigins = c.PolicyOrigins()
	dcfg.OnPolicyChanged = KeepPolicy
}

// KeepPolicy writes a policy the user changed, and names where it went. A
// write that fails says nothing was kept rather than claiming otherwise -- the
// window then tells the user the change lasts this session only.
func KeepPolicy(name string, on bool) string {
	word := "false"
	if on {
		word = "true"
	}
	if err := SaveCurrent(CurrentSection, name, word); err != nil {
		return ""
	}
	return OriginCurrent
}

// SaveCurrent sets one key in the overlay, or removes it when the value is
// empty, and rewrites the file. Every other key it holds is kept.
//
// The file is rebuilt rather than patched: it is one short list of settings a
// program wrote, so there is nothing in it worth preserving byte for byte, and
// rebuilding is what makes removing a key as ordinary as setting one.
func SaveCurrent(section, key, value string) error {
	section, key = strings.ToLower(strings.TrimSpace(section)), strings.ToLower(strings.TrimSpace(key))
	if section == "" || key == "" {
		return fmt.Errorf("current: a setting needs a section and a key")
	}
	path := CurrentPath()
	held := readCurrent(path)
	if held[section] == nil {
		held[section] = map[string]string{}
	}
	if strings.TrimSpace(value) == "" {
		delete(held[section], key)
	} else {
		held[section][key] = value
	}

	var sb strings.Builder
	sb.WriteString("; Written by the KittyTK desktop as settings are changed in it.\n")
	sb.WriteString("; It overlays " + IniName + ", which is the one to edit by hand:\n")
	sb.WriteString("; anything written here is replaced the next time that setting\n")
	sb.WriteString("; is changed in the interface.\n")
	for _, name := range sortedKeys(held) {
		keys := sortedKeys(held[name])
		if len(keys) == 0 {
			continue
		}
		fmt.Fprintf(&sb, "\n[%s]\n", name)
		for _, k := range keys {
			fmt.Fprintf(&sb, "%s = %s\n", k, held[name][k])
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o600)
}

// readCurrent reads the overlay as sections of keys. A file that cannot be
// read is an empty one: the desktop has simply not been used to change
// anything yet.
func readCurrent(path string) map[string]map[string]string {
	out := map[string]map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			if end := strings.IndexByte(line, ']'); end > 0 {
				section = strings.ToLower(strings.TrimSpace(line[1:end]))
			}
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(line[:eq]))
		if key == "" {
			continue
		}
		if out[section] == nil {
			out[section] = map[string]string{}
		}
		out[section][key] = strings.TrimSpace(line[eq+1:])
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// envBool reads a variable that is only an answer when it is set: unset leaves
// the files to say, which is not the same as a variable set to false.
func envBool(name string) (value, set bool) {
	v, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(v) == "" {
		return false, false
	}
	return parseBool(v), true
}

// Serve starts the display service for a host, which is the same act in the
// terminal host, the graphical one, and mew when it runs as a host: the
// endpoint and token this configuration resolved, the desktop's own approval
// prompt, and the connection policies with the way back to `current`.
//
// It is one function because it is one arrangement: a host that assembled it
// by hand would be the host whose switches quietly stopped being remembered.
func Serve(desktop *trinkets.Desktop, cfg Config) (*display.Server, error) {
	dcfg := display.DefaultConfig(desktop, cfg.ResolveEndpoint())
	if dcfg.Token == "" {
		dcfg.Token = cfg.ResolveToken()
	}
	cfg.ApplyPolicies(&dcfg)
	return display.ServeConfig(desktop, dcfg)
}
