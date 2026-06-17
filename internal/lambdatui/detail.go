package lambdatui

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
)

// DetailRow is one line in a resource-detail overlay. Its rendering depends on
// which fields are set (see detailBody):
//   - Section        → a section header
//   - Label (+Value) → an aligned "label   value" line (em dash when empty)
//   - Value only     → an indented bullet/continuation line
//   - all empty      → a blank separator line
type DetailRow struct {
	Label   string
	Value   string
	Section bool
}

// ResourceDetail is a flattened view of a single Lambda resource shown in the
// detail overlay opened with Enter. Secret-looking values are never collected,
// so nothing here needs redacting beyond omitting env-var values.
type ResourceDetail struct {
	Title string
	Rows  []DetailRow
}

// detailBuilder accumulates DetailRows; kv/header/bullet keep each builder terse.
type detailBuilder struct{ rows []DetailRow }

func (d *detailBuilder) kv(label, value string) {
	d.rows = append(d.rows, DetailRow{Label: label, Value: value})
}
func (d *detailBuilder) header(label string) {
	d.rows = append(d.rows, DetailRow{}, DetailRow{Label: label, Section: true})
}
func (d *detailBuilder) bullet(value string) { d.rows = append(d.rows, DetailRow{Value: value}) }

// buildFunctionDetail flattens a GetFunction response (configuration + code +
// concurrency + tags) for the function detail overlay. Environment-variable
// values are deliberately omitted — only the keys are shown.
func buildFunctionDetail(region, name string, out *lambda.GetFunctionOutput) ResourceDetail {
	cfg := out.Configuration
	var d detailBuilder

	d.kv("Runtime", runtimeLabel(string(cfg.Runtime), string(cfg.PackageType)))
	d.kv("Package type", emptyDash(string(cfg.PackageType)))
	d.kv("Handler", aws.ToString(cfg.Handler))
	d.kv("Memory", formatMemory(aws.ToInt32(cfg.MemorySize)))
	d.kv("Timeout", formatTimeout(aws.ToInt32(cfg.Timeout)))
	if cfg.EphemeralStorage != nil {
		d.kv("Ephemeral /tmp", formatMemory(aws.ToInt32(cfg.EphemeralStorage.Size)))
	}
	d.kv("Code size", formatCodeSize(cfg.CodeSize))
	d.kv("Architectures", joinArchitectures(cfg.Architectures))
	d.kv("Role", aws.ToString(cfg.Role))
	d.kv("Description", aws.ToString(cfg.Description))
	d.kv("Last modified", shortTime(parseLambdaTime(aws.ToString(cfg.LastModified))))
	d.kv("State", stateLabel(string(cfg.State)))
	if r := aws.ToString(cfg.StateReason); r != "" {
		d.kv("State reason", r)
	}
	d.kv("Last update", emptyDash(string(cfg.LastUpdateStatus)))
	if r := aws.ToString(cfg.LastUpdateStatusReason); r != "" {
		d.kv("Update reason", r)
	}
	if cfg.TracingConfig != nil {
		d.kv("Tracing", emptyDash(string(cfg.TracingConfig.Mode)))
	}

	// Reserved concurrency: GetFunction-only. Absent means "unreserved" (uses the
	// account pool); 0 means the function is throttled to never run.
	if out.Concurrency != nil && out.Concurrency.ReservedConcurrentExecutions != nil {
		d.kv("Reserved concurrency", fmt.Sprintf("%d", aws.ToInt32(out.Concurrency.ReservedConcurrentExecutions)))
	} else {
		d.kv("Reserved concurrency", "unreserved")
	}

	if dc := cfg.DeadLetterConfig; dc != nil && aws.ToString(dc.TargetArn) != "" {
		d.kv("Dead-letter queue", dlqLabel(aws.ToString(dc.TargetArn)))
	} else {
		d.kv("Dead-letter queue", "none")
	}

	if vc := cfg.VpcConfig; vc != nil && (aws.ToString(vc.VpcId) != "" || len(vc.SubnetIds) > 0) {
		d.header("VPC")
		d.kv("  VPC", aws.ToString(vc.VpcId))
		if len(vc.SubnetIds) > 0 {
			d.kv("  Subnets", strings.Join(vc.SubnetIds, ", "))
		}
		if len(vc.SecurityGroupIds) > 0 {
			d.kv("  Security groups", strings.Join(vc.SecurityGroupIds, ", "))
		}
	}

	if len(cfg.Layers) > 0 {
		d.header("Layers")
		for _, l := range cfg.Layers {
			d.bullet(aws.ToString(l.Arn))
		}
	}

	if cfg.Environment != nil {
		keys := sortedMapKeys(cfg.Environment.Variables)
		if len(keys) > 0 {
			d.header(fmt.Sprintf("Environment variables (%d · keys only)", len(keys)))
			for _, k := range keys {
				d.bullet(k)
			}
		}
	}

	if code := out.Code; code != nil {
		d.header("Code")
		if u := aws.ToString(code.ImageUri); u != "" {
			d.kv("  Image URI", u)
		}
		if rt := aws.ToString(code.RepositoryType); rt != "" {
			d.kv("  Repository", rt)
		}
	}

	if len(out.Tags) > 0 {
		d.header(fmt.Sprintf("Tags (%d)", len(out.Tags)))
		for _, k := range sortedMapKeys(out.Tags) {
			d.bullet(fmt.Sprintf("%s = %s", k, out.Tags[k]))
		}
	}

	return ResourceDetail{Title: "Function — " + name, Rows: d.rows}
}

// buildLayerDetail renders a layer's latest version straight from the loaded
// inventory — ListLayers already returns the LatestMatchingVersion, so no extra
// call is needed.
func buildLayerDetail(l Layer) ResourceDetail {
	var d detailBuilder
	d.kv("Latest version", fmt.Sprintf("%d", l.LatestVersion))
	d.kv("Version ARN", l.LatestVersionARN)
	d.kv("Compatible runtimes", joinOrDash(l.Runtimes))
	d.kv("Compatible archs", joinOrDash(l.Architectures))
	d.kv("Created", l.CreatedDate)
	d.kv("License", l.License)
	d.kv("Description", l.Description)
	return ResourceDetail{Title: "Layer — " + l.Name, Rows: d.rows}
}

// buildEventSourceDetail renders an event-source mapping from the loaded
// inventory.
func buildEventSourceDetail(es EventSource) ResourceDetail {
	var d detailBuilder
	d.kv("Function", es.FunctionName)
	d.kv("Source", es.SourceLabel)
	d.kv("State", stateLabel(es.State))
	d.kv("Batch size", fmt.Sprintf("%d", es.BatchSize))
	d.kv("Last modified", shortTime(es.LastModified))
	d.kv("Last result", emptyDash(es.LastProcessingResult))
	d.kv("UUID", es.UUID)
	return ResourceDetail{Title: "Event source — " + es.FunctionName, Rows: d.rows}
}

func emptyDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func joinOrDash(parts []string) string {
	if len(parts) == 0 {
		return "—"
	}
	return strings.Join(parts, ", ")
}

func joinArchitectures[T ~string](archs []T) string {
	if len(archs) == 0 {
		return "—"
	}
	out := make([]string, 0, len(archs))
	for _, a := range archs {
		out = append(out, string(a))
	}
	return strings.Join(out, ", ")
}
