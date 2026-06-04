package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/user/aws_explorer/internal/model"
)

// PrintResult takes an ExploreResult and formats it according to the given format.
func PrintResult(result model.ExploreResult, format string) {
	if len(result.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "Errors encountered during collection:\n")
		for _, err := range result.Errors {
			fmt.Fprintf(os.Stderr, "  [%s|%s] %s: %s\n", err.Service, err.Region, err.Code, err.Message)
		}
		fmt.Fprintln(os.Stderr)
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
		for _, err := range chunk.Errors {
			fmt.Fprintf(os.Stderr, "[%s|%s] %s: %s\n", err.Service, err.Region, err.Code, err.Message)
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
		for _, err := range chunk.Errors {
			fmt.Fprintf(os.Stderr, "[%s|%s] %s: %s\n", err.Service, err.Region, err.Code, err.Message)
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

func printTable(resources []model.Resource) {
	grouped := make(map[string][]model.Resource)
	for _, r := range resources {
		key := fmt.Sprintf("%s/%s", r.Service, r.Type)
		grouped[key] = append(grouped[key], r)
	}

	for key, resList := range grouped {
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
