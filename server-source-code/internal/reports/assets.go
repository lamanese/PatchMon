package reports

import (
	_ "embed"
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
