package docsgen

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"regexp"

	"github.com/russross/blackfriday/v2"
)

// WriteHTML renders the pages as a self-contained static HTML site under dir
// (created if needed): one "<slug>.html" per page plus "index.html" for the
// home page. Each page embeds the same CSS and a sidebar navigation listing
// every page, so the site works offline straight from disk. It returns the
// number of files written.
func WriteHTML(dir string, pages []Page) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	nav := buildNav(pages)
	written := 0
	for _, p := range pages {
		out, err := htmlPage(p, nav)
		if err != nil {
			return written, err
		}
		if err := os.WriteFile(filepath.Join(dir, p.Slug+".html"), []byte(out), 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

// navGroup is one labelled cluster of nav links in sidebar order.
type navGroup struct {
	Section Section
	Links   []navLink
}

type navLink struct {
	Slug, Title string
}

// buildNav groups the pages into sidebar sections, preserving page order.
func buildNav(pages []Page) []navGroup {
	order := []Section{SectionHome, SectionGuides, SectionCommands}
	bySection := map[Section][]navLink{}
	for _, p := range pages {
		bySection[p.Section] = append(bySection[p.Section], navLink{p.Slug, p.Title})
	}
	var groups []navGroup
	for _, s := range order {
		if links := bySection[s]; len(links) > 0 {
			groups = append(groups, navGroup{Section: s, Links: links})
		}
	}
	return groups
}

// mdLinkRe matches relative Markdown links ("foo.md", "foo.md#frag") so the
// HTML output can point at the ".html" siblings instead.
var mdLinkRe = regexp.MustCompile(`\]\(([a-zA-Z0-9_-]+)\.md(#[a-zA-Z0-9_-]+)?\)`)

// htmlPage renders one page to a full HTML document.
func htmlPage(p Page, nav []navGroup) (string, error) {
	body := mdLinkRe.ReplaceAllString(p.Body, "](${1}.html${2})")
	rendered := blackfriday.Run([]byte(body),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs))

	data := htmlData{
		Title:    p.Title,
		Slug:     p.Slug,
		Content:  template.HTML(rendered), //nolint:gosec // content is generated from our own pages
		Nav:      nav,
		NavTitle: navTitle(nav),
	}
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// navTitle returns the site title shown above the sidebar (the home page's
// title), falling back to a generic label.
func navTitle(nav []navGroup) string {
	for _, g := range nav {
		if g.Section == SectionHome && len(g.Links) > 0 {
			return g.Links[0].Title
		}
	}
	return "Documentation"
}

type htmlData struct {
	Title    string
	Slug     string
	Content  template.HTML
	Nav      []navGroup
	NavTitle string
}

var pageTmpl = template.Must(template.New("page").Parse(htmlTemplate))

// htmlTemplate is a self-contained document: inline CSS, a sticky sidebar with
// the grouped navigation, and the rendered Markdown as the main column.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · {{.NavTitle}}</title>
<style>
:root { --bg:#0d1117; --panel:#161b22; --border:#30363d; --text:#c9d1d9; --muted:#8b949e; --link:#58a6ff; --accent:#3fb950; --code:#1f2630; }
* { box-sizing: border-box; }
body { margin:0; font:16px/1.6 -apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif; color:var(--text); background:var(--bg); }
.layout { display:flex; align-items:flex-start; max-width:1200px; margin:0 auto; }
nav.sidebar { position:sticky; top:0; width:270px; flex:0 0 270px; height:100vh; overflow-y:auto; padding:1.5rem 1rem; border-right:1px solid var(--border); background:var(--panel); }
nav.sidebar .site { font-weight:700; font-size:1.05rem; margin:0 0 1rem; color:var(--text); text-decoration:none; display:block; }
nav.sidebar h3 { text-transform:uppercase; font-size:.72rem; letter-spacing:.06em; color:var(--muted); margin:1.2rem 0 .4rem; }
nav.sidebar a { display:block; padding:.18rem .5rem; color:var(--muted); text-decoration:none; border-radius:5px; font-size:.92rem; }
nav.sidebar a:hover { color:var(--text); background:rgba(110,118,129,.12); }
nav.sidebar a.active { color:var(--text); background:rgba(88,166,255,.15); font-weight:600; }
nav.sidebar code { font-size:.85rem; }
main { flex:1 1 auto; min-width:0; padding:2rem 2.5rem 4rem; }
main h1 { margin-top:0; border-bottom:1px solid var(--border); padding-bottom:.4rem; }
main h2 { margin-top:2rem; border-bottom:1px solid var(--border); padding-bottom:.3rem; }
a { color:var(--link); }
code { background:var(--code); padding:.12em .35em; border-radius:5px; font-size:.88em; font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace; }
pre { background:var(--code); padding:1rem; border-radius:8px; overflow-x:auto; border:1px solid var(--border); }
pre code { background:none; padding:0; }
table { border-collapse:collapse; width:100%; margin:1rem 0; display:block; overflow-x:auto; }
th, td { border:1px solid var(--border); padding:.5rem .7rem; text-align:left; vertical-align:top; }
th { background:rgba(110,118,129,.1); }
blockquote { margin:1rem 0; padding:.4rem 1rem; border-left:4px solid var(--accent); background:rgba(63,185,80,.08); color:var(--text); }
hr { border:none; border-top:1px solid var(--border); margin:2rem 0; }
@media (max-width:800px){ .layout{display:block;} nav.sidebar{position:static;width:auto;height:auto;border-right:none;border-bottom:1px solid var(--border);} main{padding:1.5rem;} }
</style>
</head>
<body>
<div class="layout">
<nav class="sidebar">
<a class="site" href="index.html">{{.NavTitle}}</a>
{{- $active := .Slug}}
{{- range .Nav}}
<h3>{{.Section}}</h3>
{{- range .Links}}
<a href="{{.Slug}}.html"{{if eq .Slug $active}} class="active" aria-current="page"{{end}}>{{.Title}}</a>
{{- end}}
{{- end}}
</nav>
<main>
{{.Content}}
</main>
</div>
</body>
</html>
`
