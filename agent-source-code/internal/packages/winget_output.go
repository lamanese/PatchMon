package packages

import (
	"regexp"
	"strings"
)

// ansiEscapePattern matches ANSI/VT escape sequences (CSI sequences, OSC
// sequences and single-character escapes) that winget emits when it believes
// it is writing to a terminal.
var ansiEscapePattern = regexp.MustCompile(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

// sanitizeWinGetOutput strips terminal animation noise from captured winget
// output: spinner frames (- \ | /) and block progress bars redrawn via
// carriage returns. winget produces these even with --disable-interactivity,
// and the captured \r redraws would otherwise explode into one line each in
// the patch run view. Kept in a file without a build tag so it is compiled
// and unit-tested on every platform.
func sanitizeWinGetOutput(raw string) string {
	s := ansiEscapePattern.ReplaceAllString(raw, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")

	var out []string
	blanks := 0
	for _, line := range strings.Split(s, "\n") {
		// A \r redraw sequence within one line: only the final state matters.
		if i := strings.LastIndexByte(line, '\r'); i >= 0 {
			line = line[i+1:]
		}
		if isWinGetNoiseLine(line) {
			continue
		}
		line = strings.TrimRight(line, " \t")
		if line == "" {
			// Collapse runs of blank lines left behind by removed noise.
			blanks++
			if blanks > 1 {
				continue
			}
		} else {
			blanks = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// isWinGetNoiseLine reports whether a line is pure terminal animation:
// a single leftover spinner frame or a block progress bar. Table separator
// rows (long runs of dashes) must survive - only a lone spinner character
// counts as noise.
func isWinGetNoiseLine(line string) bool {
	t := strings.TrimSpace(line)
	switch t {
	case "-", `\`, "|", "/":
		return true
	}
	// Progress bars are the only place winget uses block/shade characters,
	// e.g. "██████████▒▒▒▒▒  1.2 MB / 3.4 MB".
	return strings.ContainsAny(t, "█▓▒░")
}
