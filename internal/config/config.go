package config

// Config represents the top-level configuration matching the specification.
type Config struct {
	App      AppConfig                `mapstructure:"app"`
	AWS      AWSConfig                `mapstructure:"aws"`
	Services map[string]ServiceConfig `mapstructure:"services"`
	Filters  FilterConfig             `mapstructure:"filters"`
	Output   OutputConfig             `mapstructure:"output"`
	UI       UIConfig                 `mapstructure:"ui"`
}

type AppConfig struct {
	DefaultOutput  string `mapstructure:"defaultOutput"`
	DefaultMode    string `mapstructure:"defaultMode"`
	TimeoutSeconds int    `mapstructure:"timeoutSeconds"`
	MaxConcurrency int    `mapstructure:"maxConcurrency"`
}

type AWSConfig struct {
	Profile    string            `mapstructure:"profile"`
	AuthMethod string            `mapstructure:"authMethod"` // auto, profile, env, static, sts
	Regions    []string          `mapstructure:"regions"`
	AllRegions bool              `mapstructure:"allRegions"`
	STS        STSConfig         `mapstructure:"sts"`
	Static     StaticCredentials `mapstructure:"static"`
}

// STSConfig holds settings for the "sts" auth method (AssumeRole).
type STSConfig struct {
	RoleARN     string `mapstructure:"roleArn"`
	SessionName string `mapstructure:"sessionName"`
	ExternalID  string `mapstructure:"externalId"`
	MFASerial   string `mapstructure:"mfaSerial"`
	Duration    int    `mapstructure:"durationSeconds"` // 0 means use AWS default (1 hour)
}

// StaticCredentials holds plaintext credentials for the "static" auth method.
type StaticCredentials struct {
	AccessKeyID     string `mapstructure:"accessKeyId"`
	SecretAccessKey string `mapstructure:"secretAccessKey"`
	SessionToken    string `mapstructure:"sessionToken"`
}

type ServiceConfig struct {
	Enabled   bool                             `mapstructure:"enabled"`
	Resources map[string]ResourceServiceConfig `mapstructure:"resources"`
}

type ResourceServiceConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	DetailLevel string `mapstructure:"detailLevel"`
}

type FilterConfig struct {
	Regions []string          `mapstructure:"regions"`
	Tags    map[string]string `mapstructure:"tags"`
	States  []string          `mapstructure:"states"`
}

type OutputConfig struct {
	Format         string   `mapstructure:"format"`
	IncludeDetails bool     `mapstructure:"includeDetails"`
	Fields         []string `mapstructure:"fields"`
}

// UIConfig holds all UI/theme configuration.
type UIConfig struct {
	// Theme is the name of the active theme.
	Theme string `mapstructure:"theme"`
	// Themes holds per-theme color role overrides. The map key is the theme name.
	// Any roles not specified fall back to the built-in defaults.
	Themes map[string]ThemeColorConfig `mapstructure:"themes"`
}

// ThemeColorConfig holds the full color palette for a single theme.
// Each field is a hex color string (e.g. "#FF5555") or empty to use the default.
//
// The roles are intentionally granular so that changing one part of the UI
// (say, the table header) never alters an unrelated part (say, a panel border).
// Roles that are left empty fall back to a sensible related role at render time
// (e.g. an empty borderFocus falls back to heading), so you only need to set the
// knobs you actually want to change.
type ThemeColorConfig struct {
	// Heading is used for titles and section headers.
	Heading string `mapstructure:"heading"`
	// Text is used for body / foreground text.
	Text string `mapstructure:"text"`
	// Background is used for panel backgrounds (empty = terminal default).
	Background string `mapstructure:"background"`
	// Border is used for the borders of unfocused panels.
	Border string `mapstructure:"border"`
	// BorderFocus is used for the border of the currently focused panel.
	// Empty falls back to Heading.
	BorderFocus string `mapstructure:"borderFocus"`
	// Highlight is the background color for the selected / highlighted table row.
	Highlight string `mapstructure:"highlight"`
	// HighlightText is the foreground color on the selected / highlighted row.
	HighlightText string `mapstructure:"highlightText"`
	// Muted is used for de-emphasised / secondary text.
	Muted string `mapstructure:"muted"`
	// TableHeader is used for table column header text. Empty falls back to Muted.
	TableHeader string `mapstructure:"tableHeader"`
	// TableHeaderLine is used for the rule drawn under table headers.
	// Empty falls back to Border.
	TableHeaderLine string `mapstructure:"tableHeaderLine"`
	// StatusBarBg is the background color of the bottom status bar.
	// Empty falls back to Highlight.
	StatusBarBg string `mapstructure:"statusBarBg"`
	// StatusBarText is the text color of the bottom status bar.
	// Empty falls back to HighlightText.
	StatusBarText string `mapstructure:"statusBarText"`
	// Accent is used for decorative rails, input prompts and cursors.
	// Empty falls back to Heading.
	Accent string `mapstructure:"accent"`
	// Error is used for error messages and indicators.
	Error string `mapstructure:"error"`
	// Warning is used for warning messages and indicators.
	Warning string `mapstructure:"warning"`
}
