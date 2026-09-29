package vpctui

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/russross/blackfriday/v2"
)

// ---------------------------------------------------------------------------
// HTML export
//
// exportHTML renders the same content as exportMarkdown into a self-contained
// HTML document: a header naming the VPC, a row of headline counts, the
// architecture diagram with its layer switches, a sticky table of contents
// built from the level-2 headings, and every resource table in its own
// scrollable box with a filter and sortable columns.
//
// The page is one file and needs no network: the stylesheet and the script are
// embedded, the fonts fall back to the system stack, and the tables' search
// and sort are ~100 lines of vanilla JS rather than jQuery + DataTables from a
// CDN. A report mailed to someone, or opened on a laptop with no connection,
// looks and behaves exactly as it did when it was written.
//
// Colours are theme tokens (see assets/report.css), so the document has a real
// dark mode and the SVG diagram — which styles itself from those same tokens —
// follows it.
// ---------------------------------------------------------------------------

//go:embed assets/report.css
var reportCSS string

//go:embed assets/report.js
var reportJS string

type htmlTOCEntry struct {
	Title  string
	Anchor string
	Count  string // row count shown beside the link ("" when the heading has none)
}

// htmlKPI is one headline count in the row under the header.
type htmlKPI struct {
	Label  string
	Value  string
	Tone   string // "", "ok", "warn", "crit"
	Detail string
}

type reportHTMLData struct {
	Title       string
	VPCID       string
	VPCName     string
	Region      string
	Account     string
	CIDR        string
	State       string
	GeneratedAt string
	Status      string // findings summary shown beside the title
	StatusTone  string
	LoadErrors  []string
	KPIs        []htmlKPI
	TOC         []htmlTOCEntry
	Diagram     template.HTML
	Content     template.HTML
	CSS         template.CSS
	JS          template.JS
}

// exportHTML builds the complete HTML report for a VPC.
func exportHTML(data fullExport, findings []Finding, generatedAt time.Time) string {
	md := exportMarkdown(data, findings, generatedAt)
	rendered := blackfriday.Run([]byte(md),
		blackfriday.WithExtensions(blackfriday.CommonExtensions|blackfriday.AutoHeadingIDs))

	crit, warn, info := countBySeverity(findings)
	status, tone := findingsStatus(crit, warn, info)

	// The architecture diagram leads the report; give it its own TOC entry.
	toc := append([]htmlTOCEntry{{Title: "Architecture", Anchor: "architecture"}}, buildTOC(md)...)

	d := reportHTMLData{
		Title:       "VPC report · " + data.VPC.ID,
		VPCID:       data.VPC.ID,
		VPCName:     data.VPC.Name,
		Region:      data.VPC.Region,
		Account:     data.VPC.OwnerId,
		CIDR:        data.VPC.CIDR,
		State:       data.VPC.State,
		GeneratedAt: reportTime(generatedAt),
		Status:      status,
		StatusTone:  tone,
		LoadErrors:  data.LoadErrors,
		KPIs:        reportKPIs(data, crit, warn),
		TOC:         toc,
		Diagram:     template.HTML(vpcDiagramSVG(data)),          //nolint:gosec // generated from our own snapshot
		Content:     template.HTML(sectionize(string(rendered))), //nolint:gosec // generated from our own report
		CSS:         template.CSS(reportCSS),
		JS:          template.JS(reportJS),
	}
	var buf bytes.Buffer
	if err := reportTmpl.Execute(&buf, d); err != nil {
		return string(rendered)
	}
	return buf.String()
}

// findingsStatus summarises the findings for the badge beside the title. It
// names the worst severity present rather than a total, since one critical
// matters more than nine info notes.
func findingsStatus(crit, warn, info int) (label, tone string) {
	switch {
	case crit > 0:
		return plural(crit, "critical finding", "critical findings"), "crit"
	case warn > 0:
		return plural(warn, "warning", "warnings"), "warn"
	case info > 0:
		return plural(info, "info note", "info notes"), ""
	default:
		return "no findings", "ok"
	}
}

// reportKPIs picks the headline counts. They are the shape of the VPC — how
// much is in it and how much of it is exposed — not a repeat of the Summary
// table, which lists every resource type below.
func reportKPIs(data fullExport, crit, warn int) []htmlKPI {
	snap := data.Snap
	azs := map[string]bool{}
	public := 0
	egress := subnetEgress(snap)
	for _, s := range snap.Subnets {
		if s.AZ != "" {
			azs[s.AZ] = true
		}
		if egress[s.ID].kind == "igw" {
			public++
		}
	}
	findingsTone := ""
	switch {
	case crit > 0:
		findingsTone = "crit"
	case warn > 0:
		findingsTone = "warn"
	}
	return []htmlKPI{
		{Label: "Subnets", Value: itoa(len(snap.Subnets)),
			Detail: fmt.Sprintf("%d public · across %d AZ%s", public, len(azs), pluralSuffix(len(azs)))},
		{Label: "Network interfaces", Value: itoa(len(snap.NetworkInterfaces)),
			Detail: "attached ENIs"},
		{Label: "Security groups", Value: itoa(len(snap.SecurityGroups)),
			Detail: fmt.Sprintf("%d NACL%s", len(snap.NetworkACLs), pluralSuffix(len(snap.NetworkACLs)))},
		{Label: "Routing", Value: itoa(len(snap.RouteTables)),
			Detail: fmt.Sprintf("route tables · %d NAT · %d IGW", len(snap.NatGateways), len(snap.InternetGateways))},
		{Label: "External links", Value: itoa(len(snap.Endpoints) + len(snap.Peerings) + len(data.TransitGatewayAttachments) + len(data.VPNConnections)),
			Detail: fmt.Sprintf("%d endpoint · %d peering · %d TGW · %d VPN",
				len(snap.Endpoints), len(snap.Peerings), len(data.TransitGatewayAttachments), len(data.VPNConnections))},
		{Label: "Findings", Value: itoa(crit + warn), Tone: findingsTone,
			Detail: fmt.Sprintf("%d critical · %d warning", crit, warn)},
	}
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// h2Re matches the level-2 headings blackfriday emits, which are where one
// report section ends and the next begins.
var h2Re = regexp.MustCompile(`<h2 id="([^"]*)">(.*?)</h2>`)

// tableRe matches a bare <table>…</table> block.
var tableRe = regexp.MustCompile(`(?s)<table>(.*?)</table>`)

// bodyRowRe counts the rows in a table body.
var bodyRowRe = regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)

// filterMinRows is the point at which a table gets its own search box. Below
// it the whole table is on screen already and the box is just furniture.
const filterMinRows = 8

// sectionize wraps each level-2 heading and the content that follows it in a
// <section>, and puts every table in a scrollable box (with a filter above it
// when the table is long enough to need one). The Markdown pipeline emits a
// flat stream of headings and tables; the page needs the structure to give
// sections their own spacing, scroll anchors and sidebar highlighting.
func sectionize(html string) string {
	locs := h2Re.FindAllStringSubmatchIndex(html, -1)
	if len(locs) == 0 {
		return wrapTables(html)
	}

	var b strings.Builder
	// Anything before the first heading (the partial-data blockquote) stays
	// outside the sections, minus the Markdown's own title and timestamp: the
	// page header already names the VPC and when it was generated, and showing
	// them twice reads like two documents stapled together.
	if head := strings.TrimSpace(dropMarkdownTitle(html[:locs[0][0]])); head != "" {
		b.WriteString(head + "\n")
	}
	for i, loc := range locs {
		end := len(html)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		anchor := html[loc[2]:loc[3]]
		heading := html[loc[0]:loc[1]]
		body := html[loc[1]:end]
		b.WriteString(`<section id="` + anchor + `">` + "\n")
		b.WriteString(heading + "\n")
		b.WriteString(wrapTables(body))
		b.WriteString("</section>\n")
	}
	return b.String()
}

// mdTitleRe matches the report's own <h1> and the italic "Generated …" line
// under it — the two pieces of the Markdown preamble the HTML header repeats.
var mdTitleRe = regexp.MustCompile(`(?s)^\s*<h1[^>]*>.*?</h1>\s*(<p><em>Generated[^<]*</em></p>)?`)

// dropMarkdownTitle removes that duplicated title block, leaving any other
// preamble (notably the partial-data notice) untouched.
func dropMarkdownTitle(html string) string {
	return mdTitleRe.ReplaceAllString(html, "")
}

// wrapTables puts each table in its own scrollable box, with a filter bar when
// the table is long.
func wrapTables(html string) string {
	return tableRe.ReplaceAllStringFunc(html, func(tbl string) string {
		rows := countTableRows(tbl)
		var b strings.Builder
		if rows >= filterMinRows {
			b.WriteString(`<div class="tblbar">`)
			b.WriteString(`<input class="filter" type="search" placeholder="Filter these rows…" aria-label="Filter table rows" hidden>`)
			b.WriteString(`<span class="count">` + plural(rows, "row", "rows") + `</span>`)
			b.WriteString("</div>\n")
		}
		b.WriteString(`<div class="tbl">` + tbl + "</div>")
		return b.String()
	})
}

// countTableRows counts the body rows of one rendered table.
func countTableRows(tbl string) int {
	m := bodyRowRe.FindStringSubmatch(tbl)
	if len(m) < 2 {
		return 0
	}
	return strings.Count(m[1], "<tr>")
}

// headingCountRe pulls the trailing "(n)" off a section heading so the sidebar
// can show the count in its own column instead of in the link text.
var headingCountRe = regexp.MustCompile(`^(.*?)\s*\((\d+)\)$`)

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
		entry := htmlTOCEntry{Title: title, Anchor: sanitizedAnchorName(title)}
		if m := headingCountRe.FindStringSubmatch(title); m != nil {
			entry.Title, entry.Count = m[1], m[2]
		}
		toc = append(toc, entry)
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

const reportHTMLTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="generator" content="aws_explorer">
<title>{{.Title}}</title>
<style>{{.CSS}}</style>
</head>
<body>
<div class="wrap">
  <div class="top">
    <div class="brand"><b>aws_explorer</b> · VPC report · generated {{.GeneratedAt}}</div>
    <div class="topr"><button type="button" class="themebtn" id="themebtn" hidden>Light / dark</button></div>
  </div>

  <header class="head">
    <h1>{{if .VPCName}}{{.VPCName}}{{else}}VPC{{end}} <span class="status {{.StatusTone}}">{{.Status}}</span></h1>
    <div class="idline">
      <span>VPC <b class="mono">{{.VPCID}}</b></span>
      {{if .CIDR}}<span>CIDR <b class="mono">{{.CIDR}}</b></span>{{end}}
      {{if .Region}}<span>Region <b>{{.Region}}</b></span>{{end}}
      {{if .Account}}<span>Account <b class="mono">{{.Account}}</b></span>{{end}}
      {{if .State}}<span>State <b>{{.State}}</b></span>{{end}}
    </div>
    {{if .LoadErrors}}<div class="missing">
      <h3>Partial data — {{len .LoadErrors}} listing(s) failed</h3>
      <p>These resource listings could not be read, so the counts and tables below under-report them: {{range $i, $e := .LoadErrors}}{{if $i}}, {{end}}<b>{{$e}}</b>{{end}}. Everything else was collected normally.</p>
    </div>{{end}}
  </header>

  <div class="kpis">
    {{range .KPIs}}<div class="kpi"><span class="l">{{.Label}}</span><span class="v{{if .Tone}} {{.Tone}}{{end}}">{{.Value}}</span><span class="x">{{.Detail}}</span></div>
    {{end}}
  </div>

  <div class="layout">
    <nav class="toc" aria-label="Sections">
      {{- range .TOC}}
      <a href="#{{.Anchor}}">{{.Title}}{{if .Count}}<span class="n">{{.Count}}</span>{{end}}</a>
      {{- end}}
    </nav>

    <main>
      <section class="arch" id="architecture">
        <h2>Architecture</h2>
        <div class="layer-toggles" role="group" aria-label="Diagram layers">
          <label><input type="checkbox" id="lt-subnets" checked><span>Subnets</span></label>
          <label><input type="checkbox" id="lt-labels" checked><span>Detail labels</span></label>
          <label><input type="checkbox" id="lt-traffic" checked><span>Internet path</span></label>
          <label><input type="checkbox" id="lt-nat" checked><span>NAT</span></label>
          <label><input type="checkbox" id="lt-routing" checked><span>Route tables</span></label>
          <label><input type="checkbox" id="lt-endpoints" checked><span>Endpoints</span></label>
          <label><input type="checkbox" id="lt-peering" checked><span>Peerings &amp; gateways</span></label>
          <label><input type="checkbox" id="lt-nacl"><span>Network ACLs</span></label>
          <label><input type="checkbox" id="lt-sg"><span>Security groups</span></label>
        </div>
        <div class="dgtools" hidden>
          <button type="button" data-act="in" title="Zoom in">Zoom in</button>
          <button type="button" data-act="out" title="Zoom out">Zoom out</button>
          <button type="button" data-act="reset" title="Reset the view">Reset</button>
          <span class="hint">Hover a box to isolate what it connects to · click it to jump to its row · drag to pan</span>
          <span class="sel-name" aria-live="polite"></span>
        </div>
        <div class="diagram">{{.Diagram}}</div>
        <p class="cap">Drawn from this snapshot alone: a subnet's class comes from the default route in the route table associated with it, so "public" here means a 0.0.0.0/0 route to an internet gateway — not that anything in it has a public address. Switch layers off to follow one kind of relationship at a time.</p>
      </section>
      {{.Content}}
    </main>
  </div>

  <footer>
    <span>Generated by aws_explorer · {{.GeneratedAt}}</span>
    <span>Read-only snapshot of {{.VPCID}}{{if .Region}} in {{.Region}}{{end}}</span>
  </footer>
</div>
<script>{{.JS}}</script>
</body>
</html>
`
