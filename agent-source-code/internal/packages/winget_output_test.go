package packages

import "testing"

func TestSanitizeWinGetOutput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain output passes through",
			in:   "Name  Id  Version\nFoo   Foo.Foo  1.0\n1 upgrade available.",
			want: "Name  Id  Version\nFoo   Foo.Foo  1.0\n1 upgrade available.",
		},
		{
			name: "spinner redraw collapses to final state",
			in:   "-\r\\\r|\r/\rFound Foo [Foo.Foo] Version 2.0",
			want: "Found Foo [Foo.Foo] Version 2.0",
		},
		{
			name: "lone spinner frame line dropped",
			in:   "Starting\n- \nDone",
			want: "Starting\nDone",
		},
		{
			name: "progress bar lines dropped",
			in:   "Downloading https://example.com/foo.msi\n  ██████████▒▒▒▒▒▒▒▒▒▒  1.2 MB / 3.4 MB\n  ████████████████████  3.4 MB / 3.4 MB\nSuccessfully installed",
			want: "Downloading https://example.com/foo.msi\nSuccessfully installed",
		},
		{
			name: "table separator dashes survive",
			in:   "Name  Id\n-----------------------------------------\nFoo   Foo.Foo",
			want: "Name  Id\n-----------------------------------------\nFoo   Foo.Foo",
		},
		{
			name: "ansi escape sequences stripped",
			in:   "\x1b[?25lInstalling\x1b[0m Foo\x1b[?25h",
			want: "Installing Foo",
		},
		{
			name: "crlf normalized and umlauts preserved",
			in:   "Es wurden keine Aktualisierungen für Anwendungen gefunden.\r\nFertig.",
			want: "Es wurden keine Aktualisierungen für Anwendungen gefunden.\nFertig.",
		},
		{
			name: "blank line runs collapse",
			in:   "A\n\n\n\nB",
			want: "A\n\nB",
		},
		{
			name: "progress bar redraw within one line via cr",
			in:   "  █▒▒▒  0.1 MB / 3.4 MB\r  ███▒  2.0 MB / 3.4 MB\r  ████  3.4 MB / 3.4 MB\nInstalled.",
			want: "Installed.",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeWinGetOutput(tt.in); got != tt.want {
				t.Errorf("sanitizeWinGetOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}
