package util

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

const agentVersionRe = `(?i)(?:PatchMon Agent v|patchmon-agent v|version )?([0-9]+\.[0-9]+\.[0-9]+)`

// GetAgentsDir returns the agents binary directory from env (AGENT_BINARIES_DIR, AGENTS_DIR) or "agents".
func GetAgentsDir() string {
	if d := os.Getenv("AGENT_BINARIES_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("AGENTS_DIR"); d != "" {
		return d
	}
	return "agents"
}

// getServerGoArch maps runtime.GOARCH to Go binary naming (matches handler).
func getServerGoArch() string {
	archMap := map[string]string{
		"amd64": "amd64",
		"386":   "386",
		"arm64": "arm64",
		"arm":   "arm",
	}
	if a, ok := archMap[runtime.GOARCH]; ok {
		return a
	}
	return runtime.GOARCH
}

// GetCurrentAgentVersionFromBinary finds the Linux agent binary for server arch, executes it, and returns the version.
// Returns empty string if binary not found or version cannot be parsed.
func GetCurrentAgentVersionFromBinary(ctx context.Context, agentsDir string) string {
	serverGoArch := getServerGoArch()
	possiblePaths := []string{
		filepath.Join(agentsDir, "patchmon-agent-linux-"+serverGoArch),
		filepath.Join(agentsDir, "patchmon-agent-linux-amd64"),
		filepath.Join(agentsDir, "patchmon-agent"),
	}

	var agentPath string
	for _, p := range possiblePaths {
		if _, err := os.Stat(p); err == nil {
			agentPath = p
			break
		}
	}
	if agentPath == "" {
		return ""
	}

	versionRe := regexp.MustCompile(agentVersionRe)
	versionCommands := []string{"--version", "version", "--help"}

	for _, cmd := range versionCommands {
		runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		out, err := exec.CommandContext(runCtx, agentPath, cmd).CombinedOutput()
		cancel()
		if err != nil {
			continue
		}
		if m := versionRe.FindStringSubmatch(string(out)); len(m) >= 2 {
			return m[1]
		}
	}
	return ""
}

// GetVersionFromBinaryPath gets version from a binary at the given path.
// Tries executing the binary if it matches server platform (linux/linux, freebsd/freebsd), else reads the embedded version banner.
// Callers must pass binaryPath validated with SafePathUnderBase(baseDir, binaryName) to prevent command injection.
func GetVersionFromBinaryPath(ctx context.Context, binaryPath string) string {
	versionRe := regexp.MustCompile(agentVersionRe)
	serverOS := runtime.GOOS
	binaryOS := "linux"
	if strings.Contains(binaryPath, "freebsd") {
		binaryOS = "freebsd"
	} else if strings.Contains(binaryPath, "windows") || strings.HasSuffix(binaryPath, ".exe") {
		binaryOS = "windows"
	}

	// Try exec if same platform (Windows .exe cannot be executed on Linux)
	if (serverOS == "linux" && binaryOS == "linux") || (serverOS == "freebsd" && binaryOS == "freebsd") {
		for _, cmd := range []string{"--version", "version", "--help"} {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			out, err := exec.CommandContext(runCtx, binaryPath, cmd).CombinedOutput()
			cancel()
			if err != nil {
				continue
			}
			if m := versionRe.FindStringSubmatch(string(out)); len(m) >= 2 {
				return m[1]
			}
		}
	}

	// Fallback for binaries this server cannot execute (Windows, FreeBSD,
	// foreign CPU architectures): read the version from the agent's embedded
	// banner. Never take the first X.Y.Z in the file - Go binaries contain
	// unrelated matches such as "VersionEnumPageFilesWv0.0.0" long before the
	// real version, which made those hosts permanently "up to date".
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		return ""
	}
	return extractEmbeddedAgentVersion(data)
}

// embeddedAgentVersionRe matches the banner compiled into every agent build
// (cobra Long text in the agent's root command: "PatchMon Agent v<version>\n").
var embeddedAgentVersionRe = regexp.MustCompile(`PatchMon Agent v([0-9]+\.[0-9]+\.[0-9]+)`)

// extractEmbeddedAgentVersion returns the agent version embedded in raw binary
// data, or "" when the banner is absent.
func extractEmbeddedAgentVersion(data []byte) string {
	if m := embeddedAgentVersionRe.FindSubmatch(data); len(m) >= 2 {
		return string(m[1])
	}
	return ""
}
