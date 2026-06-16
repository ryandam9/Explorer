# Themes

The TUI supports 12 built-in color themes, all named after Australian birds.
Their colors come straight from the [feathers](https://github.com/shandiya/feathers)
palettes (the same data rendered at
[ryandam.net/demos/feathers_palettes](https://ryandam.net/demos/feathers_palettes/index.html)).
Set the active theme in `config.yaml` under `ui.theme` or with the `--theme`
flag on the S3 subcommand.

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

The in-app settings panel (press `S`) is styled as a sci-fi mission console.
It **floats over the live app** (the UI stays visible around it), it has a
**fixed size** that never changes with the terminal, tab or mode, and every
row is a control: `↑`/`↓` selects a row, `←`/`→` changes its value —
**instantly**.

- **Theme selector** — the top row. With it selected, `←`/`→` cycles the 12
  built-in themes and the whole app restyles in real time around the console.
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

All changes apply live to the running app; `Ctrl+S` persists the theme and
every role edit back to `config.yaml`.
