package reports

import (
	_ "embed"

	"codeberg.org/go-pdf/fpdf"
)

// Embedded assets for the PDF renderer. Fonts: Noto Sans (OFL 1.1, see
// assets/fonts/README.md). Logo: vendor default used when no PNG/JPEG logo is
// uploaded.

//go:embed assets/fonts/NotoSans-Regular.ttf
var fontRegular []byte

//go:embed assets/fonts/NotoSans-Bold.ttf
var fontBold []byte

//go:embed assets/logo_default.png
var defaultLogoPNG []byte

// TODO(task 3): remove once fpdf is used by the PDF renderer. Keeps `go mod
// tidy` from dropping the dependency before it has a real import site.
// fpdf.Version does not exist in v0.12.0, so this anchors on a stable
// exported constant instead.
var _ = fpdf.OrientationPortrait
