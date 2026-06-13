package docsgen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// commandPage renders one cobra command to a documentation Page: its
// description, usage line, examples, a flags table, and links to any
// subcommands.
func commandPage(c *cobra.Command) Page {
	var b strings.Builder

	if short := c.Short; short != "" {
		fmt.Fprintf(&b, "%s\n\n", short)
	}
	if long := strings.TrimSpace(c.Long); long != "" && long != strings.TrimSpace(c.Short) {
		fmt.Fprintf(&b, "%s\n\n", long)
	}

	b.WriteString("## Usage\n\n")
	fmt.Fprintf(&b, "```\n%s\n```\n\n", c.UseLine())

	if ex := dedent(c.Example); ex != "" {
		b.WriteString("## Examples\n\n")
		fmt.Fprintf(&b, "```bash\n%s\n```\n\n", ex)
	}

	writeFlagsTable(&b, "Flags", localFlags(c))
	writeFlagsTable(&b, "Global flags", c.InheritedFlags())

	if children := visibleChildren(c); len(children) > 0 {
		b.WriteString("## Subcommands\n\n")
		for _, child := range children {
			short := child.Short
			fmt.Fprintf(&b, "- [`%s`](%s.md) — %s\n", child.CommandPath(), commandSlug(child), short)
		}
		b.WriteString("\n")
	}

	if c.HasParent() {
		fmt.Fprintf(&b, "---\n\n_Part of [`%s`](%s.md)._\n", c.Root().Name(), commandSlug(c.Root()))
	}

	title := c.CommandPath()
	return Page{
		Slug:    commandSlug(c),
		Title:   title,
		Section: SectionCommands,
		Body:    strings.TrimRight(b.String(), "\n"),
	}
}

// localFlags returns the command's own flag set (not inherited ones).
func localFlags(c *cobra.Command) *pflag.FlagSet {
	// LocalFlags() includes persistent flags defined on this command; for the
	// root that is exactly its global flags, which we render separately under
	// "Global flags" for children. For the root there are no inherited flags,
	// so its persistent flags show once under "Flags" — which reads correctly.
	return c.LocalFlags()
}

// writeFlagsTable appends a Markdown table for the non-hidden flags in set,
// skipping the auto-added `help` flag. Nothing is written when the set is empty.
func writeFlagsTable(b *strings.Builder, heading string, set *pflag.FlagSet) {
	type row struct{ flag, def, usage string }
	var rows []row
	set.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		name := "`--" + f.Name + "`"
		if f.Shorthand != "" {
			name += " / `-" + f.Shorthand + "`"
		}
		def := f.DefValue
		if def == "" || def == "[]" || def == "false" {
			def = "—"
		}
		rows = append(rows, row{name, def, escapePipes(f.Usage)})
	})
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].flag < rows[j].flag })

	fmt.Fprintf(b, "## %s\n\n", heading)
	b.WriteString("| Flag | Default | Description |\n|------|---------|-------------|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %s | %s | %s |\n", r.flag, r.def, r.usage)
	}
	b.WriteString("\n")
}

// visibleChildren returns the documentable subcommands of c, sorted by name.
func visibleChildren(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, child := range c.Commands() {
		if skipCommand(child) {
			continue
		}
		out = append(out, child)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}

// escapePipes keeps a flag description from breaking the Markdown table by
// escaping the cell separator.
func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// dedent trims surrounding blank lines and removes the common leading-space
// indentation shared by every non-blank line. Cobra's Example fields are
// conventionally indented two spaces; this restores a flush-left code block.
func dedent(s string) string {
	lines := strings.Split(strings.Trim(s, "\n"), "\n")
	indent := -1
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		n := len(l) - len(strings.TrimLeft(l, " "))
		if indent == -1 || n < indent {
			indent = n
		}
	}
	if indent <= 0 {
		return strings.TrimRight(strings.Join(lines, "\n"), " \n")
	}
	for i, l := range lines {
		if len(l) >= indent {
			lines[i] = l[indent:]
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), " \n")
}
