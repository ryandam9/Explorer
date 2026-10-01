package xref

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"charm.land/lipgloss/v2/tree"
)

// Tree output for the two commands whose answers are shaped like trees.
//
// `related --depth 2` walks outward in hops, and `whereused` returns a flat
// list of references that nearly always groups by service. A table renders
// both as rows of equal weight: the hop a link was found at is a column, and
// twelve references across four services look like twelve unrelated rows. The
// shape is in the data and the table throws it away.
//
// This is a separate format (`-o tree`) rather than a new default. The table
// is what scripts and habits expect, and the two graph formats already here —
// dot and mermaid — set the precedent for "another way to look at the same
// answer".
//
// Nothing here is styled. The output goes to stdout, which is as often piped
// into a file or a pager as read directly, so the tree is drawn with the
// box-drawing characters alone and carries no colour.

// relatedTree renders a bidirectional related-resources result as two trees:
// what the target depends on, and what depends on it.
func relatedTree(w io.Writer, res RelatedResult, showUses, showUsedBy, partial bool) error {
	label := fmt.Sprintf("%s  (%s)", targetLabel(res.Target), targetKindLabel(res.Target))
	root := tree.Root(label)

	if showUses {
		root.Child(directionBranch("Depends on →", res.Uses, res.Depth, res.AllPaths, partial))
	}
	if showUsedBy {
		root.Child(directionBranch("Used by ←", res.UsedBy, res.Depth, res.AllPaths, partial))
	}

	if _, err := fmt.Fprintln(w, root.String()); err != nil {
		return err
	}
	if showUsedBy && len(res.CheckedTypes) > 0 {
		fmt.Fprintln(w, "\nFor \"Used by\", the tool searched for these kinds of links:")
		for _, ct := range res.CheckedTypes {
			fmt.Fprintf(w, "  • %s\n", ct)
		}
	}
	fmt.Fprintf(w, "\n%s\n", relatedCaveat)
	return nil
}

// directionBranch builds one direction's subtree. Within it, links are grouped
// by hop when the walk went further than one — which is the whole reason this
// view exists, since a flat table can only put the hop in a column.
func directionBranch(title string, links []Link, maxDepth int, showPath, partial bool) *tree.Tree {
	branch := tree.Root(fmt.Sprintf("%s  (%d)", title, len(links)))
	if len(links) == 0 {
		if partial {
			branch.Child("(nothing found — the scan hit errors, so this may be incomplete)")
		} else {
			branch.Child("(nothing found)")
		}
		return branch
	}

	if maxDepth <= 1 {
		for _, l := range links {
			branch.Child(linkLine(l, showPath))
		}
		return branch
	}

	byDepth := map[int][]Link{}
	for _, l := range links {
		byDepth[l.Depth] = append(byDepth[l.Depth], l)
	}
	depths := make([]int, 0, len(byDepth))
	for d := range byDepth {
		depths = append(depths, d)
	}
	sort.Ints(depths)
	for _, d := range depths {
		hop := tree.Root(fmt.Sprintf("%s  (%d)", hopLabel(d), len(byDepth[d])))
		for _, l := range byDepth[d] {
			hop.Child(linkLine(l, showPath))
		}
		branch.Child(hop)
	}
	return branch
}

// hopLabel names a hop group in words. The table can print a bare "2" under a
// HOP column header; a branch of a tree cannot — "2  (1)" says nothing.
func hopLabel(d int) string {
	if d == 1 {
		return "1 hop away"
	}
	return fmt.Sprintf("%d hops away", d)
}

// linkLine is one resource as a single line: what it is, where it is, and the
// relationship that put it in the list.
func linkLine(l Link, showPath bool) string {
	rel := l.Via
	if showPath && l.Path != "" {
		rel = l.Path
	}
	line := fmt.Sprintf("%s %s  %s", l.Service, l.Type, refName(l.Reference))
	if l.Region != "" {
		line += "  [" + l.Region + "]"
	}
	if rel != "" {
		line += "  — " + rel
	}
	return line
}

// whereUsedTree renders a where-used result grouped by service. The flat list
// is what the data is; the grouping is what the question "can I delete this?"
// is actually asking — which parts of the estate would notice.
func whereUsedTree(w io.Writer, res Result) error {
	root := tree.Root(fmt.Sprintf("%s  (%s)", targetLabel(res.Target), res.Target.Kind))

	if len(res.References) == 0 {
		root.Child("Not referenced by anything this tool checked.")
		_, err := fmt.Fprintln(w, root.String())
		return err
	}

	byService := map[string][]Reference{}
	for _, r := range res.References {
		byService[r.Service] = append(byService[r.Service], r)
	}
	services := make([]string, 0, len(byService))
	for s := range byService {
		services = append(services, s)
	}
	sort.Strings(services)

	for _, svc := range services {
		refs := byService[svc]
		branch := tree.Root(fmt.Sprintf("%s  (%d)", svc, len(refs)))
		for _, r := range refs {
			branch.Child(referenceLine(r))
		}
		root.Child(branch)
	}

	if _, err := fmt.Fprintln(w, root.String()); err != nil {
		return err
	}
	if len(res.CheckedTypes) > 0 {
		fmt.Fprintln(w, "\nThe tool searched for these kinds of links:")
		for _, ct := range res.CheckedTypes {
			fmt.Fprintf(w, "  • %s\n", ct)
		}
	}
	return nil
}

// referenceLine is one referencing resource, minus the service that is already
// its parent in the tree.
func referenceLine(r Reference) string {
	line := fmt.Sprintf("%s  %s", r.Type, refName(r))
	if r.Region != "" {
		line += "  [" + r.Region + "]"
	}
	if r.Via != "" {
		line += "  — " + r.Via
	}
	return strings.TrimSpace(line)
}
