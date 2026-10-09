package browser

import (
	"cmp"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Supported agent-browser CLI releases. MinCLIVersion is the oldest release
// with everything this server relies on (snapshot --delta, --human pointer
// movement, the lifecycle field in JSON output); TestedCLIVersion is the
// release the integration suite last passed against.
const (
	MinCLIVersion    = "0.38.0"
	TestedCLIVersion = "0.38.2"
)

const upgradeHint = "upgrade with `npm install -g agent-browser@latest`"

var versionPattern = regexp.MustCompile(`\d+\.\d+\.\d+`)

// CLIVersion is the installed agent-browser version and whether it is supported.
type CLIVersion struct {
	Version string // e.g. "0.38.2"; empty when it could not be read
	Warning string // why tools may fail with this CLI; empty when supported
}

// String describes the installed version against the supported range.
func (v CLIVersion) String() string {
	s := fmt.Sprintf("agent-browser %s (supported: %s or newer; tested with %s)",
		cmp.Or(v.Version, "unknown"), MinCLIVersion, TestedCLIVersion)
	if v.Warning != "" {
		s += "\nWarning: " + v.Warning
	}
	return s
}

// CheckVersion runs `agent-browser --version` once and reports whether the
// installed CLI is supported; later calls return the first answer.
func (m *Manager) CheckVersion(ctx context.Context) CLIVersion {
	m.versionOnce.Do(func() { m.version = m.checkVersion(ctx) })
	return m.version
}

func (m *Manager) checkVersion(ctx context.Context) CLIVersion {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, m.cfg.AgentBrowserPath, "--version").Output()
	if err != nil {
		return CLIVersion{Warning: fmt.Sprintf("cannot run %s --version: %v; %s", m.cfg.AgentBrowserPath, err, upgradeHint)}
	}
	v := versionPattern.FindString(string(out))
	switch {
	case v == "":
		return CLIVersion{Warning: fmt.Sprintf("cannot read a version from %q; %s", strings.TrimSpace(string(out)), upgradeHint)}
	case compareVersions(v, MinCLIVersion) < 0:
		return CLIVersion{Version: v, Warning: fmt.Sprintf("agent-browser %s is older than %s, the oldest supported version; %s", v, MinCLIVersion, upgradeHint)}
	}
	return CLIVersion{Version: v}
}

// compareVersions compares dotted version numbers part by part.
func compareVersions(a, b string) int {
	return slices.Compare(versionParts(a), versionParts(b))
}

func versionParts(v string) []int {
	var parts []int
	for p := range strings.SplitSeq(v, ".") {
		n, _ := strconv.Atoi(p)
		parts = append(parts, n)
	}
	return parts
}
