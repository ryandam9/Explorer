package vpctui

import (
	"bytes"
	"html/template"
	"strings"
	"time"
	"unicode"

	"github.com/russross/blackfriday/v2"
)

// ---------------------------------------------------------------------------
// HTML export
//
// exportHTML renders the same content as exportMarkdown into a styled HTML
// document: a sticky table-of-contents sidebar (built from the level-2 section
// headings), a header banner, and the Markdown converted to HTML. Resource
// tables are turned into interactive DataTables (per-table search + column
// sorting) via a CDN; opened offline they degrade to plain, horizontally
// scrolling tables. The page's own CSS is embedded.
// ---------------------------------------------------------------------------

type htmlTOCEntry struct {
	Title  string
	Anchor string
}

type reportHTMLData struct {
	Title       string
	VPCID       string
	Region      string
	GeneratedAt string
	TOC         []htmlTOCEntry
	Content     template.HTML
}

// exportHTML builds the complete HTML report for a VPC.
func exportHTML(data fullExport, findings []Finding, generatedAt time.Time) string {
	md := exportMarkdown(data, findings, generatedAt)
	rendered := blackfriday.Run([]byte(md),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs))

	d := reportHTMLData{
		Title:       "VPC Report: " + data.VPC.ID,
		VPCID:       data.VPC.ID,
		Region:      data.VPC.Region,
		GeneratedAt: generatedAt.UTC().Format("2006-01-02 15:04:05 UTC"),
		TOC:         buildTOC(md),
		Content:     template.HTML(rendered), //nolint:gosec // generated from our own report
	}
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, d); err != nil {
		return string(rendered)
	}
	return buf.String()
}

// buildTOC extracts the level-2 ("## ") section headings from the Markdown and
// pairs each with the anchor blackfriday's AutoHeadingIDs assigns, so the
// sidebar links jump to the right section.
func buildTOC(md string) []htmlTOCEntry {
	var toc []htmlTOCEntry
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		toc = append(toc, htmlTOCEntry{Title: title, Anchor: sanitizedAnchorName(title)})
	}
	return toc
}

// sanitizedAnchorName mirrors blackfriday/v2's heading-ID algorithm so the TOC
// links match the generated anchors exactly.
func sanitizedAnchorName(text string) string {
	var anchor []rune
	futureDash := false
	for _, r := range text {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			if futureDash && len(anchor) > 0 {
				anchor = append(anchor, '-')
			}
			futureDash = false
			anchor = append(anchor, unicode.ToLower(r))
		default:
			futureDash = true
		}
	}
	return string(anchor)
}

var reportTmpl = template.Must(template.New("report").Parse(reportHTMLTemplate))

const reportHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="https://cdn.datatables.net/2.1.8/css/dataTables.dataTables.min.css">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link href="https://fonts.googleapis.com/css2?family=Archivo+Black&family=Space+Grotesk:wght@400;500;600;700&display=swap" rel="stylesheet">
<style>
/* Neo-brutalism: flat bright blocks, thick black borders, hard offset shadows. */
:root {
  --bg:#fff8e7; --panel:#ffffff; --ink:#111111;
  --yellow:#ffe500; --pink:#ff6b9d; --blue:#00d4ff; --lime:#b8ff3c;
  --shadow:6px 6px 0 var(--ink); --shadow-sm:3px 3px 0 var(--ink);
  --body:"Space Grotesk",ui-sans-serif,system-ui,-apple-system,"Segoe UI",sans-serif;
  --display:"Archivo Black","Space Grotesk",sans-serif;
}
* { box-sizing:border-box; }
html { scroll-behavior:smooth; }
body { margin:0; font:16px/1.55 var(--body); color:var(--ink); background:var(--bg); }
/* Banner */
.banner { background:var(--yellow); border-bottom:5px solid var(--ink); padding:1.7rem 2rem; }
.banner h1 { margin:0 0 .6rem; font-family:var(--display); font-size:2rem; text-transform:uppercase; letter-spacing:-.01em; }
.banner .meta { display:flex; gap:.7rem; flex-wrap:wrap; align-items:center; font-weight:600; font-size:.9rem; }
.banner .meta span { background:var(--panel); border:3px solid var(--ink); box-shadow:var(--shadow-sm); padding:.15rem .65rem; }
.banner .badge { background:var(--pink); font-weight:700; text-transform:uppercase; }
/* Layout */
.layout { display:flex; align-items:flex-start; gap:1.5rem; max-width:1500px; margin:0 auto; padding:1.5rem; }
nav.toc { position:sticky; top:1rem; width:250px; flex:0 0 250px; max-height:calc(100vh - 2rem); overflow-y:auto; background:var(--panel); border:4px solid var(--ink); box-shadow:var(--shadow); padding:1rem; }
nav.toc h2 { margin:0 0 .8rem; font-family:var(--display); font-size:.85rem; text-transform:uppercase; background:var(--blue); border:3px solid var(--ink); box-shadow:var(--shadow-sm); padding:.35rem .55rem; }
nav.toc a { display:block; margin:.45rem 0; padding:.35rem .55rem; color:var(--ink); text-decoration:none; font-weight:600; font-size:.84rem; background:var(--panel); border:2px solid var(--ink); box-shadow:var(--shadow-sm); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; transition:transform .05s ease,box-shadow .05s ease; }
nav.toc a:hover { background:var(--yellow); transform:translate(-2px,-2px); box-shadow:5px 5px 0 var(--ink); }
/* Main column */
main { flex:1 1 auto; min-width:0; }
main h1 { display:none; }
main h2 { display:inline-block; margin:2rem 0 1rem; font-family:var(--display); font-size:1.15rem; text-transform:uppercase; background:var(--lime); border:4px solid var(--ink); box-shadow:var(--shadow); padding:.4rem .85rem; scroll-margin-top:1rem; }
main h2:first-of-type { margin-top:0; }
main h3 { margin:1.5rem 0 .5rem; font-family:var(--display); font-size:1rem; text-transform:uppercase; }
main > p em { display:inline-block; background:var(--panel); border:2px solid var(--ink); padding:.1rem .45rem; font-style:normal; font-weight:600; }
a { color:var(--ink); font-weight:700; text-decoration:underline; text-decoration-thickness:2px; }
code { background:var(--lime); border:2px solid var(--ink); padding:.05em .3em; font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; font-size:.85em; }
ul { padding-left:1.2rem; }
li { margin:.25rem 0; }
blockquote { margin:1rem 0; padding:.6rem 1rem; background:var(--panel); border:3px solid var(--ink); border-left:9px solid var(--pink); box-shadow:var(--shadow-sm); }
hr { border:none; border-top:3px solid var(--ink); margin:2rem 0; }
/* Tables */
table { border-collapse:separate; border-spacing:0; width:100%; margin:.4rem 0 1.4rem; display:block; overflow-x:auto; background:var(--panel); border:3px solid var(--ink); box-shadow:var(--shadow); font-size:.86rem; }
th, td { border-right:2px solid var(--ink); border-bottom:2px solid var(--ink); padding:.5rem .7rem; text-align:left; vertical-align:top; }
th:last-child, td:last-child { border-right:none; }
th { background:var(--blue); font-weight:700; text-transform:uppercase; font-size:.76rem; letter-spacing:.02em; white-space:nowrap; }
td { white-space:normal; }
tbody tr:nth-child(even) { background:#fff7d6; }
tbody tr:hover { background:var(--yellow); }
/* DataTables controls, restyled to match */
.dt-container { margin-bottom:1.4rem; }
.dt-container .dt-layout-row { display:flex; flex-wrap:wrap; gap:.6rem; align-items:center; justify-content:space-between; margin:.5rem 0; }
.dt-search input, .dt-length select { border:3px solid var(--ink) !important; border-radius:0 !important; box-shadow:var(--shadow-sm); padding:.3rem .5rem !important; font-family:inherit; font-weight:600; background:var(--panel); }
.dt-search input:focus, .dt-length select:focus { outline:none; background:var(--yellow); }
.dt-info { font-weight:700; }
.dt-paging .dt-paging-button { border:3px solid var(--ink) !important; border-radius:0 !important; background:var(--panel) !important; box-shadow:var(--shadow-sm); margin:0 .15rem; padding:.25rem .6rem !important; font-weight:700; color:var(--ink) !important; }
.dt-paging .dt-paging-button.current, .dt-paging .dt-paging-button:hover:not(.disabled) { background:var(--pink) !important; }
@media (max-width:900px){ .layout{display:block;} nav.toc{position:static;width:auto;max-height:none;margin-bottom:1rem;} }
@media print { nav.toc{display:none;} .banner{-webkit-print-color-adjust:exact;print-color-adjust:exact;} }
</style>
</head>
<body>
<header class="banner">
<h1>VPC Report · {{.VPCID}}</h1>
<div class="meta">
{{if .Region}}<span class="badge">{{.Region}}</span>{{end}}
<span>Generated {{.GeneratedAt}}</span>
</div>
</header>
<div class="layout">
<nav class="toc">
<h2>Contents</h2>
{{- range .TOC}}
<a href="#{{.Anchor}}">{{.Title}}</a>
{{- end}}
</nav>
<main>
{{.Content}}
</main>
</div>
<script src="https://code.jquery.com/jquery-3.7.1.min.js"></script>
<script src="https://cdn.datatables.net/2.1.8/js/dataTables.min.js"></script>
<script>
// Turn each resource table into a searchable, sortable DataTable. The small
// two-column Summary and VPC tables are left as plain tables. If the CDN can't
// load (offline), the tables still render as styled HTML.
document.addEventListener('DOMContentLoaded', function () {
  if (typeof DataTable === 'undefined') { return; }
  document.querySelectorAll('main table').forEach(function (t) {
    if (t.querySelectorAll('thead th').length <= 2) { return; }
    new DataTable(t, {
      paging: true,
      pageLength: -1,
      lengthMenu: [[25, 50, 100, -1], [25, 50, 100, 'All']],
      order: [],
      autoWidth: false,
      stateSave: false
    });
  });
});
</script>
</body>
</html>
`
