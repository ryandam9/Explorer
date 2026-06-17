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
	themeCSS, themes := themeStyles()
	written := 0
	for _, p := range pages {
		out, err := htmlPage(p, nav, themeCSS, themes)
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

// htmlPage renders one page to a full HTML document. themeCSS is the shared,
// pre-rendered palette stylesheet and themes is the switcher option list (both
// the same for every page).
func htmlPage(p Page, nav []navGroup, themeCSS string, themes []docThemeOption) (string, error) {
	body := mdLinkRe.ReplaceAllString(p.Body, "](${1}.html${2})")
	rendered := blackfriday.Run([]byte(body),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs))

	data := htmlData{
		Title:     p.Title,
		Slug:      p.Slug,
		Section:   string(p.Section),
		Content:   template.HTML(rendered), //nolint:gosec // content is generated from our own pages
		Nav:       nav,
		NavTitle:  navTitle(nav),
		ThemeCSS:  template.CSS(themeCSS), //nolint:gosec // generated from our own palettes
		Themes:    themes,
		DefaultID: defaultDocTheme,
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
	Title     string
	Slug      string
	Section   string
	Content   template.HTML
	Nav       []navGroup
	NavTitle  string
	ThemeCSS  template.CSS
	Themes    []docThemeOption
	DefaultID string
}

var pageTmpl = template.Must(template.New("page").Parse(htmlTemplate))

// htmlTemplate is a self-contained, offline-ready document: inline CSS, a
// per-theme palette sheet (generated from the app's "feathers" themes), a
// sticky sidebar with the grouped navigation and a theme switcher, a page
// header, and the rendered Markdown as the main column. No external fonts,
// scripts or stylesheets are referenced, so it works straight from disk.
const htmlTemplate = `<!DOCTYPE html>
<html lang="en" data-theme="{{.DefaultID}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<title>{{.Title}} · {{.NavTitle}}</title>
<style>
{{.ThemeCSS}}
*,*::before,*::after { box-sizing:border-box; }
html { scroll-behavior:smooth; }
body {
  margin:0;
  font-family:"Inter",system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;
  font-size:16px; line-height:1.65;
  color:var(--text); background:var(--bg);
  -webkit-font-smoothing:antialiased; text-rendering:optimizeLegibility;
}
::selection { background:var(--accent); color:var(--bg); }
.layout { display:flex; align-items:flex-start; max-width:1240px; margin:0 auto; }

/* Sidebar */
nav.sidebar {
  position:sticky; top:0; width:280px; flex:0 0 280px; height:100vh;
  overflow-y:auto; padding:1.4rem 1.1rem 2rem;
  border-right:1px solid var(--border); background:var(--panel);
}
nav.sidebar .brand { display:flex; align-items:center; gap:.55rem; text-decoration:none; margin-bottom:.2rem; }
nav.sidebar .brand .mark {
  width:30px; height:30px; flex:0 0 30px; border-radius:8px;
  background:linear-gradient(135deg,var(--heading),var(--accent));
  box-shadow:0 0 0 1px var(--border), 0 2px 8px rgba(0,0,0,.35);
}
nav.sidebar .brand .site { font-weight:700; font-size:1.05rem; color:var(--text); line-height:1.2; }
nav.sidebar .tagline { color:var(--muted); font-size:.74rem; margin:.1rem 0 1rem; }
nav.sidebar h3 { text-transform:uppercase; font-size:.7rem; letter-spacing:.08em; color:var(--muted); margin:1.3rem 0 .45rem; font-weight:700; }
nav.sidebar a.nav { display:block; padding:.26rem .6rem; color:var(--muted); text-decoration:none; border-radius:7px; font-size:.92rem; border-left:2px solid transparent; }
nav.sidebar a.nav:hover { color:var(--text); background:color-mix(in srgb, var(--accent) 14%, transparent); }
nav.sidebar a.nav.active { color:var(--heading); background:color-mix(in srgb, var(--heading) 16%, transparent); border-left-color:var(--heading); font-weight:600; }
nav.sidebar code { font-size:.85rem; }

/* Theme switcher */
.theme-switch { margin:.4rem 0 .2rem; }
.theme-switch label { display:block; text-transform:uppercase; font-size:.66rem; letter-spacing:.08em; color:var(--muted); margin-bottom:.3rem; font-weight:700; }
.theme-switch select {
  width:100%; padding:.4rem .55rem; border-radius:8px; cursor:pointer;
  color:var(--text); background:var(--bg); border:1px solid var(--border);
  font:inherit; font-size:.86rem;
}
.theme-switch select:focus { outline:none; border-color:var(--focus); box-shadow:0 0 0 2px color-mix(in srgb, var(--focus) 40%, transparent); }
.swatches { display:flex; gap:4px; margin-top:.5rem; }
.swatches span { flex:1; height:6px; border-radius:3px; }
.swatches .s1 { background:var(--heading); }
.swatches .s2 { background:var(--accent); }
.swatches .s3 { background:var(--status-bg); }
.swatches .s4 { background:var(--th-text); }

/* Main column */
main { flex:1 1 auto; min-width:0; padding:0 0 4rem; }
.page-header {
  padding:2.2rem 2.6rem 1.5rem; border-bottom:1px solid var(--border);
  background:
    radial-gradient(120% 140% at 0% 0%, color-mix(in srgb, var(--heading) 12%, transparent), transparent 60%),
    var(--panel);
}
.page-header .eyebrow { text-transform:uppercase; letter-spacing:.1em; font-size:.7rem; font-weight:700; color:var(--accent); margin:0 0 .35rem; }
.page-header h1 { margin:0; font-size:2rem; line-height:1.2; color:var(--heading); letter-spacing:-.01em; }
.rail { height:4px; margin-top:1.1rem; border-radius:3px; background:linear-gradient(90deg,var(--heading),var(--accent),var(--status-bg)); }
.content { padding:1.8rem 2.6rem; }

.content h2 { margin-top:2.2rem; padding-bottom:.35rem; border-bottom:1px solid var(--border); color:var(--text); font-size:1.4rem; }
.content h3 { margin-top:1.8rem; color:var(--heading); font-size:1.12rem; }
.content h2 a.anchor, .content h3 a.anchor { color:inherit; text-decoration:none; }
.content p { margin:.85rem 0; }
a { color:var(--link); text-decoration:none; }
a:hover { color:var(--link-hover); text-decoration:underline; }

code { background:var(--code); padding:.14em .4em; border-radius:6px; font-size:.86em; font-family:ui-monospace,"SF Mono",SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace; border:1px solid color-mix(in srgb, var(--border) 60%, transparent); }
pre { background:var(--code); padding:1rem 1.1rem; border-radius:10px; overflow-x:auto; border:1px solid var(--border); box-shadow:inset 0 0 0 1px rgba(255,255,255,.02); }
pre code { background:none; padding:0; border:none; font-size:.85em; }

table { border-collapse:collapse; width:100%; margin:1.2rem 0; display:block; overflow-x:auto; border:1px solid var(--th-line); border-radius:10px; }
th, td { border-bottom:1px solid var(--border); padding:.55rem .8rem; text-align:left; vertical-align:top; }
th { background:var(--th-bg); color:var(--th-text); font-weight:700; border-bottom:2px solid var(--th-line); }
tbody tr:nth-child(even) { background:color-mix(in srgb, var(--panel) 55%, transparent); }
tbody tr:last-child td { border-bottom:none; }

blockquote { margin:1.2rem 0; padding:.6rem 1.1rem; border-left:4px solid var(--accent); background:color-mix(in srgb, var(--accent) 10%, transparent); border-radius:0 8px 8px 0; color:var(--text); }
blockquote p { margin:.3rem 0; }
hr { border:none; border-top:1px solid var(--border); margin:2.2rem 0; }
ul, ol { padding-left:1.4rem; }
li { margin:.3rem 0; }

footer { padding:1.4rem 2.6rem 0; margin-top:2rem; border-top:1px solid var(--border); color:var(--muted); font-size:.84rem; }
footer .birds { color:var(--accent); }

@media (max-width:820px){
  .layout{display:block;}
  nav.sidebar{position:static;width:auto;height:auto;border-right:none;border-bottom:1px solid var(--border);}
  .page-header{padding:1.6rem 1.4rem 1.1rem;} .content{padding:1.4rem;} footer{padding:1.4rem;}
}
</style>
</head>
<body>
<div class="layout">
<nav class="sidebar">
<a class="brand" href="index.html"><span class="mark" aria-hidden="true"></span><span class="site">{{.NavTitle}}</span></a>
<p class="tagline">Read-only AWS explorer · auditor · cost lens</p>
<div class="theme-switch">
<label for="theme-select">Theme</label>
<select id="theme-select" aria-label="Documentation color theme">
{{- range .Themes}}
<option value="{{.Name}}">{{.Label}}</option>
{{- end}}
</select>
<div class="swatches" aria-hidden="true"><span class="s1"></span><span class="s2"></span><span class="s3"></span><span class="s4"></span></div>
</div>
{{- $active := .Slug}}
{{- range .Nav}}
<h3>{{.Section}}</h3>
{{- range .Links}}
<a class="nav{{if eq .Slug $active}} active{{end}}" href="{{.Slug}}.html"{{if eq .Slug $active}} aria-current="page"{{end}}>{{.Title}}</a>
{{- end}}
{{- end}}
</nav>
<main>
<header class="page-header">
<p class="eyebrow">{{.Section}}</p>
<h1>{{.Title}}</h1>
<div class="rail" aria-hidden="true"></div>
</header>
<div class="content">
{{.Content}}
</div>
<footer>
Generated by <code>aws_explorer docs</code> · themed with the
<span class="birds">feathers</span> palettes (named after Australian birds) — pick yours above.
</footer>
</main>
</div>
<script>
(function(){
  var KEY="awsx-docs-theme", root=document.documentElement, sel=document.getElementById("theme-select");
  var saved=null; try{ saved=localStorage.getItem(KEY); }catch(e){}
  var initial=saved||root.getAttribute("data-theme");
  root.setAttribute("data-theme",initial);
  if(sel){
    sel.value=initial;
    sel.addEventListener("change",function(){
      root.setAttribute("data-theme",sel.value);
      try{ localStorage.setItem(KEY,sel.value); }catch(e){}
    });
  }
})();
</script>
</body>
</html>
`
