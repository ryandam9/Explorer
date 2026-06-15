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
<style>
:root {
  --bg:#f6f8fa; --panel:#ffffff; --border:#d0d7de; --text:#1f2328; --muted:#656d76;
  --link:#0969da; --accent:#0969da; --head:#0d1b2a; --th:#eef2f6; --rowhover:#f3f6f9;
}
* { box-sizing:border-box; }
html { scroll-behavior:smooth; }
body { margin:0; font:15px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif; color:var(--text); background:var(--bg); }
.banner { background:linear-gradient(135deg,#0d1b2a,#1b3a5b); color:#fff; padding:1.6rem 2rem; }
.banner h1 { margin:0 0 .35rem; font-size:1.5rem; }
.banner .meta { font-size:.9rem; opacity:.85; display:flex; gap:1.2rem; flex-wrap:wrap; }
.banner .badge { display:inline-block; background:rgba(255,255,255,.15); border:1px solid rgba(255,255,255,.25); padding:.05rem .5rem; border-radius:999px; }
.layout { display:flex; align-items:flex-start; max-width:1400px; margin:0 auto; }
nav.toc { position:sticky; top:0; width:260px; flex:0 0 260px; height:100vh; overflow-y:auto; padding:1.4rem 1rem; border-right:1px solid var(--border); background:var(--panel); }
nav.toc h2 { text-transform:uppercase; font-size:.72rem; letter-spacing:.06em; color:var(--muted); margin:0 0 .6rem; }
nav.toc a { display:block; padding:.2rem .55rem; color:var(--muted); text-decoration:none; border-radius:6px; font-size:.88rem; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
nav.toc a:hover { color:var(--text); background:var(--rowhover); }
main { flex:1 1 auto; min-width:0; padding:1.8rem 2.4rem 4rem; }
main h1 { display:none; }
main h2 { margin:2.2rem 0 .8rem; padding-bottom:.3rem; border-bottom:2px solid var(--border); color:var(--head); font-size:1.25rem; scroll-margin-top:1rem; }
main h2:first-of-type { margin-top:.4rem; }
main h3 { margin:1.4rem 0 .5rem; color:var(--head); font-size:1.02rem; }
a { color:var(--link); }
em { color:var(--muted); font-style:normal; }
code { background:#eaeef2; padding:.12em .35em; border-radius:5px; font-size:.86em; font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
ul { padding-left:1.2rem; }
table { border-collapse:collapse; width:100%; margin:.6rem 0 1.2rem; display:block; overflow-x:auto; font-size:.88rem; box-shadow:0 1px 0 var(--border); }
th, td { border:1px solid var(--border); padding:.45rem .65rem; text-align:left; vertical-align:top; white-space:nowrap; }
td { white-space:normal; }
th { background:var(--th); position:sticky; top:0; font-weight:600; }
tbody tr:nth-child(even) { background:#fbfcfd; }
tbody tr:hover { background:var(--rowhover); }
blockquote { margin:1rem 0; padding:.4rem 1rem; border-left:4px solid var(--accent); background:#eaf2fb; }
hr { border:none; border-top:1px solid var(--border); margin:2rem 0; }
@media (max-width:900px){ .layout{display:block;} nav.toc{position:static;width:auto;height:auto;border-right:none;border-bottom:1px solid var(--border);} main{padding:1.4rem;} }
@media print { nav.toc{display:none;} .banner{background:#0d1b2a;-webkit-print-color-adjust:exact;print-color-adjust:exact;} }
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
      stateSave: false
    });
  });
});
</script>
</body>
</html>
`
