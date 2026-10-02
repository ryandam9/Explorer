# PDF preview fixtures

Small, hand-built PDFs. Each is a complete document with a correct xref table,
because the preview refuses a truncated PDF on purpose — a prefix of one is
unreadable, not partial — so a half-written fixture tests the wrong path.

| File | What it is for |
| --- | --- |
| `sample.pdf` | Two pages of ordinary text. The happy path: page count, page markers, page text. |
| `blank.pdf` | A page with no text at all, like a scan. Checks that "no text" reads differently from "could not read it". |
| `layout.pdf` | Every word placed at an absolute position (`Tm`), with **no space character anywhere**, plus a paragraph gap and a two-cell table row. This is the shape of document that the library's flat text walk returns as one unbroken run, so it is what the layout reconstruction in `pdfview.go` is measured against. |

`layout.pdf` uses `/Helvetica` with an explicit `Widths` array: the glyph
advances are what let a gap be told from a word break, and a font without them
exercises the other branch (see `TestPDFLayoutWithoutWidths`, which builds its
glyphs in Go rather than from a file).
