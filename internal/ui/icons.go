package ui

import (
	"strings"
	"sync/atomic"
)

// Icons (ui.nerdFont). With a Nerd Font (https://www.nerdfonts.com) the TUIs
// prefix service names, tab bars and panel titles with glyphs, the way
// superfile does for files. Nerd Font glyphs live in Unicode's private-use
// area, so without such a font they render as empty boxes — which is why this
// is off by default and every icon has a plain fallback. Service icons fall
// back to nothing (so the default look is unchanged); status markers fall
// back to the Unicode symbols the UI has always used.
//
// Icons are for titles, tab bars, badges and list prefixes — never inside a
// fixed-width table column or header, where a glyph drawn two cells wide by a
// non-"Mono" Nerd Font would shift every column after it (CLAUDE.md §12).

var nerdFont atomic.Bool

// SetNerdFont switches icons between Nerd Font glyphs and plain fallbacks.
func SetNerdFont(on bool) { nerdFont.Store(on) }

// NerdFontEnabled reports whether Nerd Font glyphs are in use.
func NerdFontEnabled() bool { return nerdFont.Load() }

type iconSpec struct {
	nerd  string // Nerd Font glyph
	plain string // fallback without a Nerd Font ("" = no icon)
}

// icons maps an icon name (a service key as the collectors name it, a
// command, or a status) to its glyphs. The Nerd Font codepoints are Font
// Awesome ones (nf-fa-*) unless noted.
var icons = map[string]iconSpec{
	// Services (collector keys) and commands.
	"all":            {"", ""},          // th-large
	"triage":         {"", ""},          // stethoscope
	"acm":            {"", ""},          // certificate
	"apigateway":     {"", ""},          // link
	"athena":         {"", ""},          // table
	"cloudformation": {"", ""},          // cube
	"cloudfront":     {"", ""},          // cloud
	"cloudtrail":     {"", ""},          // history
	"cloudwatch":     {"", ""},          // line-chart
	"dynamodb":       {"", ""},          // database
	"ec2":            {"", ""},          // server
	"ecr":            {"", ""},          // cube
	"ecs":            {"", ""},          // cubes
	"efs":            {"", ""},          // folder
	"eks":            {"", ""},          // cubes
	"elasticache":    {"", ""},          // database
	"elbv2":          {"", ""},          // exchange
	"emr":            {"", ""},          // cogs
	"eventbridge":    {"", ""},          // bolt
	"glue":           {"", ""},          // cogs
	"iam":            {"", ""},          // shield
	"kinesis":        {"", ""},          // exchange
	"kms":            {"", ""},          // key
	"lambda":         {"\U000f0627", ""}, // nf-md-lambda
	"rds":            {"", ""},          // database
	"redshift":       {"", ""},          // database
	"route53":        {"", ""},          // globe
	"s3":             {"", ""},          // archive
	"secretsmanager": {"", ""},          // lock
	"sns":            {"", ""},          // bullhorn
	"sqs":            {"", ""},          // envelope
	"stepfunctions":  {"", ""},          // tasks
	"vpc":            {"", ""},          // sitemap
	"bill":           {"", ""},          // dollar
	"audit":          {"", ""},          // shield
	"tags":           {"", ""},          // tags
	"related":        {"", ""},          // link
	"lake":           {"", ""},          // database
	"code":           {"", ""},          // code
	"findings":       {"", ""},          // stethoscope

	// Status and navigation markers. These keep a plain fallback — they carry
	// meaning, so they are never dropped.
	"region": {"", "◉"}, // globe
	"cursor": {"", "▶"}, // chevron-right
	"ok":     {"", "✓"}, // check-circle
	"error":  {"", "✗"}, // times-circle
	"warn":   {"", "⚠"}, // warning
	"info":   {"", "ℹ"}, // info-circle
	"folder": {"", ""},  // folder
	"file":   {"", ""},  // file
	"search": {"", "/"}, // search
	"asc":    {"", "▲"}, // sort-asc
	"desc":   {"", "▼"}, // sort-desc
	"clock":  {"", ""},  // clock-o
}

// Glyph returns the named icon in the current mode ("" when it has none).
// Names are matched case-insensitively.
func Glyph(name string) string {
	spec, ok := icons[strings.ToLower(name)]
	if !ok {
		return ""
	}
	if nerdFont.Load() {
		return spec.nerd
	}
	return spec.plain
}

// Icon is Glyph followed by a space, for prefixing a label — or "" when the
// icon has nothing to show, so `Icon(x) + label` is always safe.
func Icon(name string) string {
	if g := Glyph(name); g != "" {
		return g + " "
	}
	return ""
}
