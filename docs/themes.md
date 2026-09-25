# Themes

The TUI ships 20 built-in color themes: 12 named after Australian birds and 8
popular editor/terminal color schemes. The bird themes' colors come straight
from the [feathers](https://github.com/shandiya/feathers) palettes (the same
data rendered at
[ryandam.net/demos/feathers_palettes](https://ryandam.net/demos/feathers_palettes/index.html)).
Set the active theme in `config.yaml` under `ui.theme` or with the `--theme`
flag on any TUI command.

| Theme Name | Palette feel |
|------------|--------------|
| `spotted-pardalote` | Warm yellow, orange and red |
| `plains-wanderer` | Cream, tan and golden brown |
| `bee-eater` | Cyan, blue and amber |
| `rose-crowned-fruit-dove` | Magenta, coral and green |
| `eastern-rosella` | Yellow, lime and red |
| `oriole` | Gold, salmon and lavender |
| `princess-parrot` | Green, blue and pink (default) |
| `superb-fairy-wren` | Rust, tan and cream |
| `cassowary` | Teal, gold and pink |
| `yellow-robin` | Bright yellow, slate and amber |
| `galah` | Pink, blush and slate |
| `blue-winged-kookaburra` | Light cyan, teal and orange |

**Color schemes.** These follow well-known palettes, mapped onto the roles
below from the theme files of [superfile](https://github.com/yorukot/superfile)
(MIT). Each scheme is designed around its own background, so they look their
best with the [painted background](#painted-background) on.

| Theme Name | Scheme |
|------------|--------|
| `catppuccin-mocha` | [Catppuccin](https://catppuccin.com) Mocha — pastel blue & mauve on deep navy |
| `tokyo-night` | [Tokyo Night](https://github.com/enkia/tokyo-night-vscode-theme) — blue & violet on ink |
| `nord` | [Nord](https://www.nordtheme.com) — frost blues on polar grey |
| `gruvbox` | [Gruvbox](https://github.com/morhetz/gruvbox) — warm retro yellows & oranges |
| `dracula` | [Dracula](https://draculatheme.com) — purple, pink & green on charcoal |
| `rose-pine` | [Rosé Pine](https://rosepinetheme.com) — muted rose, gold & iris |
| `one-dark` | One Dark (Atom) — blue, green & soft red on slate |
| `everforest` | [Everforest](https://github.com/sainnhe/everforest) dark — green & sand on forest |

Their selected-row and body text are checked to read at ≥4.5:1 contrast
(WCAG AA) on their backgrounds, and a test holds them to it.

### Color roles

Each theme configures granular color roles so that changing one part of the UI
never bleeds into another. Set only the roles you want to change — any role you
leave out falls back to a sensible related role (noted below).

**General**

| Role | Used for | Fallback |
|------|----------|----------|
| `heading` | Titles and section headers | — |
| `text` | Body / foreground text | — |
| `background` | Panel backgrounds (empty = terminal default) | — |
| `canvas` | Full-screen background, painted only when `ui.paintBackground` is on | `background` |
| `muted` | De-emphasised / secondary text | — |
| `accent` | Decorative rails, input prompts and cursors | `heading` |
| `border` | Borders of unfocused panels | — |
| `borderFocus` | Border of the focused panel | `heading` |
| `highlight` | Selected item background (lists, menus) | — |
| `highlightText` | Text on the selected item | — |

**Tables** (every table in the app shares these, so all tables look identical)

| Role | Used for | Fallback |
|------|----------|----------|
| `tableHeader` | Table column header text | `muted` |
| `tableHeaderBg` | Table column header background | `background` |
| `tableHeaderLine` | Rule drawn under table headers | `border` |
| `tableText` | Table cell text | `text` |
| `tableBorder` | Border drawn around table panels | `border` |
| `tableSelectedBg` | Selected table-row background | `highlight` |
| `tableSelectedText` | Text on the selected table row | `highlightText` |
| `tableRowAltBg` | Zebra background for alternate rows (CSV/TSV table) | `tableBorder` |

Every built-in theme sets `tableRowAltBg` explicitly to a dark, low-intensity
tint of its palette, so the zebra stripe stays subtle under the cell text. The
`tableBorder` fallback only applies if a config override clears the value. If
the stripe looks too dark or too bright in your terminal, override it per theme
in `config.yaml` (`ui.themes.<name>.tableRowAltBg`) or from the settings panel.

**Status bar & shortcut hints**

| Role | Used for | Fallback |
|------|----------|----------|
| `statusBarBg` | Status bar background | `highlight` |
| `statusBarText` | Status bar text | `highlightText` |
| `hintKey` | Shortcut keys (e.g. `Enter`) in the status bar hints | `statusBarText` |
| `hintText` | Shortcut descriptions (e.g. *open*) in the hints | `statusBarText` |

**Alerts**

| Role | Used for | Fallback |
|------|----------|----------|
| `error` | Error messages and indicators | — |
| `warning` | Warning messages and indicators | — |
| `success` | Success / confirmation messages (e.g. *reachable*, *no issues*) | `accent` |
| `info` | Informational messages and indicators | `muted` |

(The authoritative list lives in the `Roles` registry in
`internal/ui/theme.go`; role names in `config.yaml` are matched
case-insensitively.)

Override any role in `config.yaml` — for example, to recolor just the table
header of the `oriole` theme without touching anything else:

```yaml
ui:
  theme: oriole
  themes:
    oriole:
      tableHeader: "#34E0A1"   # only the table header changes
      error: "#FF0000"         # override just this role
```

### The theme console

The in-app settings panel — **Appearance** — opens with **`Ctrl+T` on any
screen of any TUI** (`lambda`, `s3`, `emr`, `cw`, `bill`, …; the dashboard, S3
and VPC browsers also keep their `S` key). It is styled as a sci-fi mission
console.
It **floats over the live app** (the UI stays visible around it), it has a
**fixed size** that never changes with the terminal, tab or mode, and every
row is a control: `↑`/`↓` selects a row, `←`/`→` changes its value —
**instantly**.

- **Theme selector** — the top row. With it selected, `←`/`→` cycles the 20
  built-in themes and the whole app restyles in real time around the console.
- **Icons** — the row under the theme. `←`/`→` (or `Space`/`Enter`) switches
  between Nerd Font glyphs and plain symbols, live. A program can't change the
  terminal's font, so the panel shows sample glyphs: if they render as boxes,
  set a [Nerd Font](https://www.nerdfonts.com) in your terminal's settings.
- **Background** — `terminal` (your terminal's own background) or `painted`
  (the theme's canvas color fills the screen), live.
- **Subsystem tabs** — the roles are grouped into segmented `GENERAL` /
  `TABLES` / `STATUS BAR` / `ALERTS` tabs (`Tab` or `1`–`4` to switch).
- **Slider rows** — every role renders as a fader: the knob position is the
  color's hue, the track glows in the color itself, and the hex value and a
  swatch sit at the end of the row. Roles on `auto` show a dimmed dashed
  track.
- **Quick palette** — with a role selected, `←`/`→` steps it through a swatch
  ring (the theme's own colors, a hue wheel and a gray ramp), applied
  immediately — changing a color is one keystroke. `a` resets it to `auto`.
- **HUE / SAT / LUM tuner** — `Enter` opens three knobs for fine control
  (`↑`/`↓` picks a knob, `←`/`→` turns it, `Shift+←/→` turns it coarsely),
  plus a `HEX` field for typing an exact value. `Enter` applies, `Esc`
  cancels.
- **Signal monitor** — a live preview strip (mini header, table row, status
  bar and alert glyphs) that follows every knob turn *before* you apply.

All changes apply live to the running app — tables included, in every TUI —
and `Ctrl+S` persists the theme, the icon and background switches and every
role edit back to `config.yaml`. `Esc` (or `Ctrl+T` again) closes the panel
without saving; what you changed stays in effect until you quit. While it is
open, the screen underneath keeps updating (scans and log streams carry on).

## Look & feel

Two display options sit beside the theme, both **off by default** so a plain
terminal looks exactly as it always has. Switch them while the app runs in the
Appearance panel (`Ctrl+T`, then `↑` to the Icons / Background rows) and save
with `Ctrl+S` — or set them in `config.yaml`, or pass a flag for one run:

```yaml
ui:
  theme: catppuccin-mocha
  paintBackground: true   # --paint-background
  nerdFont: true          # --nerd-font
```

### Painted background

Normally the terminal's own background shows through every screen. With
`paintBackground` on, the whole screen is filled with the theme's `canvas`
color — the scheme's own background for the color-scheme themes, a dark tint of
the palette for the bird themes — for the solid, designed look of tools like
superfile and btop. Styles that carry their own background (the status bar,
the selected row, buttons) keep it. It is applied once to each finished frame,
so it covers every TUI, and does nothing on a terminal without color.

### Nerd Font icons

With a [Nerd Font](https://www.nerdfonts.com) set as your terminal font,
`nerdFont` adds icons: a glyph per service in the dashboard sidebar, and in tab
bars, page headers and panel titles (Lambda, S3, EMR, Glue, bill, audit,
CloudTrail, tags…), plus Nerd Font versions of the region, cursor and status
markers. Without a Nerd Font those glyphs render as empty boxes, which is why
it is opt-in; when it is off, service icons are simply absent and the markers
use the usual `◉ ▶ ✓ ✗ ⚠` symbols. Icons are never put inside table columns,
where a glyph drawn two cells wide would push the columns out of line.

### Panel frames

These need no setting — every TUI uses them:

- **Table panels** carry their title in the top border and, in the bottom
  border, the row position (`3/120`) and — when columns are scrolled out of
  view — `◀ 2 more cols ▶`. That used to be an extra line under each table;
  the line now goes to the table.

  ```
  ╭─┤ Functions (42) ├──────────────────────────╮
  │ NAME           RUNTIME      MEMORY  TIMEOUT │
  │ copy-object    python3.12   256 MB  30s     │
  ╰───────────────────┤ 1 more cols ▶ ├─┤ 1/42 ├─╯
  ```

- **Section dividers** split a panel into labelled parts (`├─ Aliases ───┤`),
  e.g. the Lambda detail page's versions and permissions panels, and the
  dashboard sidebar opens with a `Services ─────` heading.
- **Confirmation buttons** — prompts that gate a download, a peek, a full
  scan or a delete end in a filled confirm button and a quieter cancel one,
  each naming its key (`y  Download` / `Esc  Cancel`; a delete's is red).
- **Gradient progress bars** run from the theme's heading color to its accent:
  S3 downloads, multi-region scans (audit, CloudTrail, VPC) and the Lambda
  activity log scan, whose bar shows how much of the day has been read.

