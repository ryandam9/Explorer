package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/user/aws_explorer/internal/model"
)

// PrintResult takes an ExploreResult and formats it according to the given format.
func PrintResult(result model.ExploreResult, format string) {
	if len(result.Errors) > 0 {
		printErrors(os.Stderr, result.Errors)
	}

	if len(result.Resources) == 0 {
		fmt.Println("No resources found.")
		return
	}

	switch strings.ToLower(format) {
	case "json":
		printJSON(result.Resources)
	case "table":
		fallthrough
	default:
		printTable(result.Resources)
	}
}

// StreamOutput reads chunks from the channel and prints results incrementally.
func StreamOutput(chunks <-chan model.ResultChunk, format string) {
	switch strings.ToLower(format) {
	case "json":
		streamJSON(chunks)
	case "table":
		fallthrough
	default:
		streamTable(chunks)
	}
}

func streamTable(chunks <-chan model.ResultChunk) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "SERVICE\tTYPE\tREGION\tID\tNAME\tSTATE")

	anyOutput := false
	for chunk := range chunks {
		if len(chunk.Errors) > 0 {
			printErrors(os.Stderr, chunk.Errors)
		}

		for _, r := range chunk.Resources {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				r.Service, r.Type, r.Region, r.ID, r.Name, r.State)
			anyOutput = true
		}
		w.Flush()
	}

	if !anyOutput {
		fmt.Println("No resources found.")
	}
}

func streamJSON(chunks <-chan model.ResultChunk) {
	bw := bufio.NewWriter(os.Stdout)
	defer bw.Flush()

	first := true
	bw.WriteString("[")
	for chunk := range chunks {
		if len(chunk.Errors) > 0 {
			printErrors(os.Stderr, chunk.Errors)
		}

		for _, r := range chunk.Resources {
			if !first {
				bw.WriteByte(',')
			}
			first = false
			data, err := json.Marshal(r)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to marshal resource: %v\n", err)
				continue
			}
			bw.Write(data)
		}
	}
	bw.WriteString("]\n")
}

func printJSON(resources []model.Resource) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resources); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal JSON: %v\n", err)
	}
}

// printErrors writes errors to w, grouping access-denied errors into a distinct
// "Insufficient Privileges" section so users know exactly what permissions to add.
func printErrors(w io.Writer, errs []model.ExploreError) {
	var authErrs, otherErrs []model.ExploreError
	for _, e := range errs {
		if e.Code == "AccessDenied" {
			authErrs = append(authErrs, e)
		} else {
			otherErrs = append(otherErrs, e)
		}
	}

	if len(authErrs) > 0 {
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "  +--------------------------------------------------------------+")
		fmt.Fprintln(w, "  |              INSUFFICIENT PRIVILEGES                         |")
		fmt.Fprintln(w, "  +--------------------------------------------------------------+")
		for _, e := range authErrs {
			fmt.Fprintf(w, "  | Service : %-50s |\n", e.Service+" ("+e.Region+")")
			// Word-wrap the message at 50 chars so it fits the box
			words := strings.Fields(e.Message)
			line := ""
			for _, word := range words {
				if len(line)+1+len(word) > 50 {
					fmt.Fprintf(w, "  | %-52s |\n", line)
					line = word
				} else {
					if line == "" {
						line = word
					} else {
						line += " " + word
					}
				}
			}
			if line != "" {
				fmt.Fprintf(w, "  | %-52s |\n", line)
			}
			fmt.Fprintln(w, "  +--------------------------------------------------------------+")
		}
		fmt.Fprintln(w, "")
	}

	if len(otherErrs) > 0 {
		fmt.Fprintln(w, "Errors encountered during collection:")
		for _, e := range otherErrs {
			fmt.Fprintf(w, "  [%s|%s] %s: %s\n", e.Service, e.Region, e.Code, e.Message)
		}
		fmt.Fprintln(w, "")
	}
}

func printTable(resources []model.Resource) {
	grouped := make(map[string][]model.Resource)
	for _, r := range resources {
		key := fmt.Sprintf("%s/%s", r.Service, r.Type)
		grouped[key] = append(grouped[key], r)
	}

	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		resList := grouped[key]
		fmt.Printf("\n--- %s ---\n", strings.ToUpper(key))

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

		headers := []string{"REGION", "ID", "NAME", "STATE"}
		fmt.Fprintln(w, strings.Join(headers, "\t"))

		sep := make([]string, len(headers))
		for i, h := range headers {
			sep[i] = strings.Repeat("-", len(h))
		}
		fmt.Fprintln(w, strings.Join(sep, "\t"))

		for _, r := range resList {
			row := []string{
				r.Region,
				r.ID,
				r.Name,
				r.State,
			}
			fmt.Fprintln(w, strings.Join(row, "\t"))
		}

		w.Flush()
	}
}
