package reports

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"mime"
	"strings"

	"golang.org/x/image/draw"
)

const (
	LogoSourceUploaded = "uploaded"
	LogoSourceDefault  = "default"
	MaxLogoPixels      = 4_000_000
	MaxLogoWidth       = 600
)

// selectLogo picks the header logo for a PDF: the uploaded light logo when it
// is a sane PNG/JPEG, otherwise the embedded vendor default. It never fails.
//
// An accepted upload is always fully decoded by the Go image decoders and
// re-encoded as an 8-bit, non-interlaced PNG (scaled to MaxLogoWidth when
// wider). The raw upload never reaches fpdf: its own PNG parser rejects
// interlaced files, panics on truncated data, mangles 16-bit images and
// inflates IDAT without a bound. The 4 MP limit is checked on the header
// before any pixel is decoded.
func selectLogo(b Branding) ([]byte, string, string) {
	def := func() ([]byte, string, string) { return defaultLogoPNG, "png", LogoSourceDefault }
	if len(b.LogoData) == 0 {
		return def()
	}
	mt, _, err := mime.ParseMediaType(strings.ToLower(strings.TrimSpace(b.LogoContentType)))
	if err != nil {
		return def()
	}
	var want string
	switch mt {
	case "image/png":
		want = "png"
	case "image/jpeg", "image/jpg":
		want = "jpeg"
	default:
		return def()
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b.LogoData))
	if err != nil || format != want || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > MaxLogoPixels {
		return def()
	}
	src, format, err := image.Decode(bytes.NewReader(b.LogoData))
	if err != nil || format != want {
		return def()
	}
	sb := src.Bounds()
	if sb.Dx() != cfg.Width || sb.Dy() != cfg.Height {
		return def()
	}
	w, h := cfg.Width, cfg.Height
	if w > MaxLogoWidth {
		h = max(cfg.Height*MaxLogoWidth/cfg.Width, 1)
		w = MaxLogoWidth
	}
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	if w == cfg.Width && h == cfg.Height {
		draw.Draw(dst, dst.Bounds(), src, sb.Min, draw.Src)
	} else {
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Src, nil)
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return def()
	}
	return out.Bytes(), "png", LogoSourceUploaded
}

// keep the decoders linked for image.DecodeConfig / image.Decode
var _ = jpeg.Decode
