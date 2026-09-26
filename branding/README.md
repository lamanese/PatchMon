# Branding sources

## PatMan product mark (since 2.0.2-am.15)

The product logo is a bat silhouette whose body is the amanIT diamond
(the diamond stands for amanIT, the bat for PatMan). Hand-built SVG, no
outside artwork; the diamond paths are the ones from `logo_icon_square.svg`,
scaled into a 100x100 box, and the wings/head are cut out around it with an
SVG mask so the diamond's dark facets stay readable.

| File | Use |
|---|---|
| `patman_icon_square.svg` / `patman_icon_square_dark.svg` | square mark: `frontend/public/assets/logo_square_default.svg`, `favicon.svg` and `logo_square_default_dark.svg` (login page) |
| `patman_wordmark_light.svg` / `patman_wordmark_dark.svg` | mark + "PatMan" / "by amanIT" in Noto Sans (the fonts embedded in `server-source-code/internal/reports/assets/fonts`): source of `frontend/public/assets/logo_light_default.png` / `logo_dark_default.png` (500x150) and `server-source-code/internal/reports/assets/logo_default.png` (1000x300, PDF header) |

Regenerate the PNGs like the amanIT wordmarks below (headless Chrome on a
transparent canvas at 1000x300 with the two Noto Sans faces inlined as
`@font-face`, then Pillow LANCZOS to 500x150). The bat is an original
design of amanIT GmbH; it deliberately uses no emblem, shield or lettering
of any third-party character.

## amanIT vendor logo

Vector sources of the amanIT logo used by the fork, converted from the
InDesign master (`Logo_Amanit_rgb.idml`, not in the repository) with
`idml2svg.py`. Colors are InDesign's RGB export of the CMYK swatches
(black, anthracite #2a3441 with 80 % #434e5f and 50 % #727d8f tints); the
dark variants use white/#c9d3e0 text and lightened facets for dark backgrounds.

| File | Use |
|---|---|
| `logo_full_light.svg` / `logo_full_dark.svg` | wordmark + claim "Security und Consulting" |
| `logo_wordmark_light.svg` / `logo_wordmark_dark.svg` | wordmark without claim (was the default UI/PDF logo up to am.14; kept as vendor artwork) |
| `logo_icon_square.svg` / `logo_icon_square_dark.svg` | diamond in a square viewBox (was favicon/login icon up to am.14; now the body of the PatMan mark) |

Regenerating the PNGs: render the SVG with headless Chrome on a transparent
canvas at 2x (`--default-background-color=00000000 --screenshot`), then
downscale with Pillow (LANCZOS). Keep the wordmarks at 500x150 so the
sidebar `<Logo>` (h-10) and the Branding tab previews are unchanged.

The logos are trademarks of amanIT GmbH and is not covered by the AGPL
license of the code.
