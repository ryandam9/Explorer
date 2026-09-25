package lambdatui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/lambda"

	"github.com/ryandam9/aws_explorer/internal/ui"
)

// section is one titled block of a detail view. Body is the panel's text
// (label/value lines styled by dkv). Empty, when set, is the one-line reason the
// section has nothing to show ("not attached to a VPC"): such sections collapse
// into a single "Not configured" card instead of each taking a panel.
type section struct {
	Title string
	Body  string
	Empty string
}

// FunctionDetail is the flattened GetFunction response (configuration + code +
// concurrency + tags) the function detail grid renders. Environment-variable
// values are deliberately not captured — only the keys — so a secret passed as
// an env var can never reach the screen.
type FunctionDetail struct {
	Name         string
	Region       string
	ARN          string
	Runtime      string
	PackageType  string
	Handler      string
	Description  string
	Version      string
	LastModified time.Time
	Role         string

	MemoryMB            int32
	TimeoutSec          int32
	EphemeralMB         int32
	CodeSize            int64
	Architectures       []string
	ReservedConcurrency *int32 // nil = unreserved (uses the account pool)
	TracingMode         string

	State                  string
	StateReason            string
	LastUpdateStatus       string
	LastUpdateStatusReason string

	VpcID            string
	SubnetIDs        []string
	SecurityGroupIDs []string

	Layers     []string
	EnvKeys    []string // keys only — values are never collected
	EnvError   string
	DLQTarget  string
	KMSKeyArn  string
	SigningJob string
	SigningPro string

	RepositoryType string
	ImageURI       string
	CodeLocation   string // presigned S3 URL to download the Zip deployment package

	// Resource-based policy (who may invoke the function), from lambda:GetPolicy.
	// ResourcePolicy is the JSON document ("" when the function has none).
	// ResourcePolicyErr is set when the policy could not be read (e.g. access
	// denied), keeping "unknown" distinct from "no policy".
	ResourcePolicy    string
	ResourcePolicyErr string

	// How it is invoked (detail_invoke.go). Each *Err is set when that read
	// was denied or failed, so "unknown" never reads as "none".
	Versions       []VersionInfo
	VersionsErr    string
	Aliases        []AliasInfo
	AliasesErr     string
	URLs           []URLInfo
	URLsErr        string
	Provisioned    []ProvisionedInfo
	ProvisionedErr string
	Async          *AsyncInfo
	AsyncErr       string

	// Triggers is filled by the model from the loaded inventory: the event-
	// source mappings that feed this function.
	Triggers []EventSource

	Tags map[string]string
}

// flattenFunction reduces a GetFunction response to FunctionDetail. Pure over the
// SDK output so the section builder stays trivially testable.
func flattenFunction(region, name string, out *lambda.GetFunctionOutput) FunctionDetail {
	cfg := out.Configuration
	d := FunctionDetail{
		Name:                   name,
		Region:                 region,
		ARN:                    aws.ToString(cfg.FunctionArn),
		Runtime:                string(cfg.Runtime),
		PackageType:            string(cfg.PackageType),
		Handler:                aws.ToString(cfg.Handler),
		Description:            aws.ToString(cfg.Description),
		Version:                aws.ToString(cfg.Version),
		LastModified:           parseLambdaTime(aws.ToString(cfg.LastModified)),
		Role:                   aws.ToString(cfg.Role),
		MemoryMB:               aws.ToInt32(cfg.MemorySize),
		TimeoutSec:             aws.ToInt32(cfg.Timeout),
		CodeSize:               cfg.CodeSize,
		State:                  string(cfg.State),
		StateReason:            aws.ToString(cfg.StateReason),
		LastUpdateStatus:       string(cfg.LastUpdateStatus),
		LastUpdateStatusReason: aws.ToString(cfg.LastUpdateStatusReason),
		KMSKeyArn:              aws.ToString(cfg.KMSKeyArn),
		SigningJob:             aws.ToString(cfg.SigningJobArn),
		SigningPro:             aws.ToString(cfg.SigningProfileVersionArn),
	}
	for _, a := range cfg.Architectures {
		d.Architectures = append(d.Architectures, string(a))
	}
	if cfg.EphemeralStorage != nil {
		d.EphemeralMB = aws.ToInt32(cfg.EphemeralStorage.Size)
	}
	if cfg.TracingConfig != nil {
		d.TracingMode = string(cfg.TracingConfig.Mode)
	}
	for _, l := range cfg.Layers {
		d.Layers = append(d.Layers, aws.ToString(l.Arn))
	}
	if vc := cfg.VpcConfig; vc != nil {
		d.VpcID = aws.ToString(vc.VpcId)
		d.SubnetIDs = vc.SubnetIds
		d.SecurityGroupIDs = vc.SecurityGroupIds
	}
	if cfg.Environment != nil {
		d.EnvKeys = sortedMapKeys(cfg.Environment.Variables)
		if cfg.Environment.Error != nil {
			d.EnvError = aws.ToString(cfg.Environment.Error.Message)
		}
	}
	if cfg.DeadLetterConfig != nil {
		d.DLQTarget = aws.ToString(cfg.DeadLetterConfig.TargetArn)
	}
	if out.Concurrency != nil {
		d.ReservedConcurrency = out.Concurrency.ReservedConcurrentExecutions
	}
	if code := out.Code; code != nil {
		d.RepositoryType = aws.ToString(code.RepositoryType)
		d.ImageURI = aws.ToString(code.ImageUri)
		d.CodeLocation = aws.ToString(code.Location)
	}
	if len(out.Tags) > 0 {
		d.Tags = out.Tags
	}
	return d
}

// dkv renders a "label value" line with a stable label column: the label
// muted, the value in the text colour. An empty value reads as a muted em dash.
func dkv(label, value string) string {
	return dkvStyled(label, value, ui.ColorText())
}

// dkvWarn is dkv with the value in the warning colour — a setting worth a
// second look (throttled to zero, a URL anyone can call).
func dkvWarn(label, value string) string {
	return dkvStyled(label, value, ui.ColorWarning())
}

func dkvStyled(label, value, color string) string {
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted()))
	v := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(value)
	if strings.TrimSpace(value) == "" {
		v = muted.Render("—")
	}
	return "  " + muted.Render(fmt.Sprintf("%-18s", label)) + " " + v
}

// sections builds the function's detail panels, most-used first:
// configuration, what invokes it (triggers, versions & aliases, URL, async),
// permissions, networking, environment, layers, code and tags. State and
// usage live in the page's summary cards above them. Pure over the
// FunctionDetail so it is fixture-tested.
func (d FunctionDetail) sections() []section {
	var out []section

	// Configuration.
	var cf strings.Builder
	cf.WriteString(dkv("Handler", d.Handler) + "\n")
	cf.WriteString(dkv("Runtime", runtimeLabel(d.Runtime, d.PackageType)) + "\n")
	cf.WriteString(dkv("Architecture", joinOrDash(d.Architectures)) + "\n")
	cf.WriteString(dkv("Memory", formatMemory(d.MemoryMB)) + "\n")
	cf.WriteString(dkv("Timeout", formatTimeout(d.TimeoutSec)) + "\n")
	cf.WriteString(dkv("Ephemeral /tmp", formatMemory(d.EphemeralMB)) + "\n")
	if d.ReservedConcurrency != nil && *d.ReservedConcurrency == 0 {
		cf.WriteString(dkvWarn("Reserved conc.", reservedConcurrencyLabel(d.ReservedConcurrency)) + "\n")
	} else {
		cf.WriteString(dkv("Reserved conc.", reservedConcurrencyLabel(d.ReservedConcurrency)) + "\n")
	}
	cf.WriteString(dkv("Tracing", emptyDash(d.TracingMode)) + "\n")
	cf.WriteString(dkv("Version", d.Version) + "\n")
	cf.WriteString(dkv("Last modified", shortTime(d.LastModified)))
	out = append(out, section{Title: "Configuration", Body: cf.String()})

	// What invokes it, and how.
	out = append(out, section{Title: "Triggers", Body: triggersBody(d)})
	vs := section{Title: "Versions & aliases", Body: versionsBody(d)}
	if len(d.Aliases) == 0 && len(d.Versions) == 0 && len(d.Provisioned) == 0 &&
		d.AliasesErr == "" && d.VersionsErr == "" && d.ProvisionedErr == "" {
		vs.Empty = "only $LATEST — no published versions or aliases"
	}
	out = append(out, vs)
	url := section{Title: "Function URL", Body: urlBody(d)}
	if len(d.URLs) == 0 && d.URLsErr == "" {
		url.Empty = "no function URL"
	}
	out = append(out, url)
	out = append(out, section{Title: "Async invocation", Body: asyncBody(d)})

	// Permissions: the role it runs as, and who may invoke it.
	out = append(out, section{Title: "Permissions", Body: dkv("Execution role", d.Role) + "\n" +
		ui.Divider("Resource policy · who may invoke it") + "\n" + resourcePolicyBody(d)})

	net := section{Title: "Networking", Body: vpcBody(d)}
	if d.VpcID == "" && len(d.SubnetIDs) == 0 && len(d.SecurityGroupIDs) == 0 {
		net.Empty = "not attached to a VPC (Lambda-managed network)"
	}
	out = append(out, net)

	env := section{Title: envTitle(d), Body: envBody(d)}
	if len(d.EnvKeys) == 0 && d.EnvError == "" {
		env.Empty = "no environment variables"
	}
	out = append(out, env)

	layers := section{Title: fmt.Sprintf("Layers (%d)", len(d.Layers)), Body: layersBody(d.Layers)}
	if len(d.Layers) == 0 {
		layers.Empty = "no layers"
	}
	out = append(out, layers)

	out = append(out, section{Title: "Code package", Body: codeBody(d)})

	tags := section{Title: tagsTitle(d.Tags), Body: tagsBody(d.Tags)}
	if len(d.Tags) == 0 {
		tags.Empty = "no tags"
	}
	out = append(out, tags)
	return out
}

// reservedConcurrencyLabel distinguishes "unreserved" (no reservation, uses the
// account pool) from a numeric reservation, flagging the throttled-to-zero case.
func reservedConcurrencyLabel(n *int32) string {
	if n == nil {
		return "unreserved (account pool)"
	}
	if *n == 0 {
		return "0 (throttled — cannot execute)"
	}
	return fmt.Sprintf("%d", *n)
}

func vpcBody(d FunctionDetail) string {
	if d.VpcID == "" && len(d.SubnetIDs) == 0 && len(d.SecurityGroupIDs) == 0 {
		return "  Not attached to a VPC (runs in the Lambda-managed network)."
	}
	var b strings.Builder
	b.WriteString(dkv("VPC", d.VpcID) + "\n")
	b.WriteString(dkv("Subnets", joinOrDash(d.SubnetIDs)) + "\n")
	b.WriteString(dkv("Security groups", joinOrDash(d.SecurityGroupIDs)))
	return b.String()
}

func envTitle(d FunctionDetail) string {
	return fmt.Sprintf("Environment (%d)", len(d.EnvKeys))
}

func envBody(d FunctionDetail) string {
	var b strings.Builder
	if d.EnvError != "" {
		b.WriteString("  ⚠ " + d.EnvError + "\n")
	}
	if len(d.EnvKeys) == 0 {
		if d.EnvError != "" {
			return strings.TrimRight(b.String(), "\n")
		}
		return "  (no environment variables)"
	}
	b.WriteString("  keys only — values are never read:\n")
	for _, k := range d.EnvKeys {
		b.WriteString("    " + k + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func layersBody(layers []string) string {
	if len(layers) == 0 {
		return "  (no layers)"
	}
	var b strings.Builder
	for i, l := range layers {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("  " + l)
	}
	return b.String()
}

func codeBody(d FunctionDetail) string {
	var b strings.Builder
	b.WriteString(dkv("Package type", d.PackageType) + "\n")
	if d.PackageType == "Image" || d.ImageURI != "" {
		b.WriteString(dkv("Repository", emptyDash(d.RepositoryType)) + "\n")
		b.WriteString(dkv("Image URI", d.ImageURI) + "\n")
	} else {
		b.WriteString(dkv("Code size", formatCodeSize(d.CodeSize)) + "\n")
		if d.CodeLocation != "" {
			b.WriteString(dkv("Source", "press v to download & browse") + "\n")
		}
	}
	b.WriteString(dkv("KMS key", d.KMSKeyArn) + "\n")
	b.WriteString(dkv("Signing profile", d.SigningPro) + "\n")
	b.WriteString(dkv("Signing job", d.SigningJob))
	return b.String()
}

// resourcePolicyBody renders the function's resource-based policy as
// pretty-printed JSON, or an explanatory line when there is none / it could not
// be read (so "access denied" is never mistaken for "no policy").
func resourcePolicyBody(d FunctionDetail) string {
	if d.ResourcePolicyErr != "" {
		return "  " + d.ResourcePolicyErr
	}
	if strings.TrimSpace(d.ResourcePolicy) == "" {
		return "  (no resource-based policy — only the account's own principals can invoke it)"
	}
	return indentJSON(d.ResourcePolicy)
}

// indentJSON pretty-prints a JSON document with a two-space left margin so it
// lines up with the other detail bodies. Non-JSON input is shown as-is (trimmed)
// rather than dropped.
func indentJSON(s string) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return "  " + strings.TrimSpace(s)
	}
	// Syntax-highlight the JSON, then add a two-space margin to each line so it
	// lines up with the other detail bodies (JSON tokens never span lines, so
	// prefixing per line keeps the colour spans intact).
	lines := strings.Split(ui.HighlightLang(buf.String(), "json"), "\n")
	for i, ln := range lines {
		lines[i] = "  " + ln
	}
	return strings.Join(lines, "\n")
}

func tagsTitle(tags map[string]string) string {
	return fmt.Sprintf("Tags (%d)", len(tags))
}

func tagsBody(tags map[string]string) string {
	if len(tags) == 0 {
		return "  (no tags)"
	}
	var b strings.Builder
	for i, k := range sortedMapKeys(tags) {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("  %s = %s", k, tags[k]))
	}
	return b.String()
}

// layerSections builds the per-panel detail for a layer from the loaded
// inventory (ListLayers already returns the latest version), so no extra call is
// needed.
func layerSections(l Layer) []section {
	var ov strings.Builder
	ov.WriteString(dkv("Name", l.Name) + "\n")
	ov.WriteString(dkv("Latest version", fmt.Sprintf("%d", l.LatestVersion)) + "\n")
	ov.WriteString(dkv("Created", l.CreatedDate) + "\n")
	ov.WriteString(dkv("License", l.License) + "\n")
	ov.WriteString(dkv("ARN", l.LatestVersionARN))

	var cp strings.Builder
	cp.WriteString(dkv("Runtimes", joinOrDash(l.Runtimes)) + "\n")
	cp.WriteString(dkv("Architectures", joinOrDash(l.Architectures)))

	return []section{
		{Title: "Overview", Body: ov.String()},
		{Title: "Compatibility", Body: cp.String()},
		{Title: "Description", Body: "  " + emptyDash(l.Description)},
	}
}

// eventSourceSections builds the per-panel detail for an event-source mapping
// from the loaded inventory.
func eventSourceSections(es EventSource) []section {
	var ov strings.Builder
	ov.WriteString(dkv("Function", es.FunctionName) + "\n")
	ov.WriteString(dkv("Source", es.SourceLabel) + "\n")
	ov.WriteString(dkv("State", stateLabel(es.State)) + "\n")
	ov.WriteString(dkv("UUID", es.UUID))

	var pr strings.Builder
	pr.WriteString(dkv("Batch size", fmt.Sprintf("%d", es.BatchSize)) + "\n")
	pr.WriteString(dkv("Last modified", shortTime(es.LastModified)) + "\n")
	pr.WriteString(dkv("Last result", emptyDash(es.LastProcessingResult)))

	return []section{
		{Title: "Overview", Body: ov.String()},
		{Title: "Processing", Body: pr.String()},
	}
}
