package docsgen

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteMarkdown renders the pages as Markdown files under dir (created if
// needed), one "<slug>.md" per page. The home page is additionally written as
// README.md so the directory renders as a landing page on GitHub. Each page
// gets an H1 title and a breadcrumb back to the index. It returns the number
// of files written.
func WriteMarkdown(dir string, pages []Page) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	written := 0
	for _, p := range pages {
		content := markdownPage(p)
		if err := os.WriteFile(filepath.Join(dir, p.Slug+".md"), []byte(content), 0o644); err != nil {
			return written, err
		}
		written++
		if p.Section == SectionHome {
			if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644); err != nil {
				return written, err
			}
			written++
		}
	}
	return written, nil
}

// markdownPage assembles one page's Markdown: an optional breadcrumb, the H1
// title, and the body.
func markdownPage(p Page) string {
	var b strings.Builder
	if p.Section != SectionHome {
		b.WriteString("[← Documentation index](index.md)\n\n")
	}
	fmt.Fprintf(&b, "# %s\n\n", p.Title)
	b.WriteString(p.Body)
	b.WriteString("\n")
	return b.String()
}
