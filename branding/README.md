# Branding sources

Vector sources of the amanIT logo used by the fork, converted from the
InDesign master (`Logo_Amanit_rgb.idml`, not in the repository) with
`idml2svg.py`. Colors are InDesign's RGB export of the CMYK swatches
(black, anthracite #2a3441 with 80 % #434e5f and 50 % #727d8f tints); the
dark variants use white/#c9d3e0 text and lightened facets for dark backgrounds.

| File | Use |
|---|---|
| `logo_full_light.svg` / `logo_full_dark.svg` | wordmark + claim "Security und Consulting" |
| `logo_wordmark_light.svg` / `logo_wordmark_dark.svg` | wordmark without claim: source of `frontend/public/assets/logo_light_default.png` / `logo_dark_default.png` (500x150, transparent) and of `server-source-code/internal/reports/assets/logo_default.png` (PDF/report header, light) |
| `logo_icon_square.svg` | diamond in a square viewBox: `frontend/public/assets/logo_square_default.svg` and `favicon.svg` |

Regenerating the PNGs: render the SVG with headless Chrome on a transparent
canvas at 2x (`--default-background-color=00000000 --screenshot`), then
downscale with Pillow (LANCZOS). Keep the wordmarks at 500x150 so the
sidebar `<Logo>` (h-10) and the Branding tab previews are unchanged.

The logo is a trademark of amanIT GmbH and is not covered by the AGPL
license of the code.
