package config

// Config represents the top-level configuration matching the specification.
type Config struct {
	App      AppConfig                `mapstructure:"app"`
	AWS      AWSConfig                `mapstructure:"aws"`
	Accounts []AccountConfig          `mapstructure:"accounts"`
	Services map[string]ServiceConfig `mapstructure:"services"`
	Filters  FilterConfig             `mapstructure:"filters"`
	Output   OutputConfig             `mapstructure:"output"`
	UI       UIConfig                 `mapstructure:"ui"`
	Display  DisplayConfig            `mapstructure:"display"`
	Trail    TrailConfig              `mapstructure:"trail"`
}

// TrailConfig configures the CloudTrail activity feed (the `trail` command and
// its `--tui`).
//
// Example config.yaml:
//
//	trail:
//	  hideEvents:
//	    - AssumeRole
//	    - ConsoleLogin
//	    - Describe*
type TrailConfig struct {
	// HideEvents lists CloudTrail event names to suppress from the feed so the
	// signal isn't drowned out by routine calls. Matching is case-insensitive;
	// a trailing "*" is a prefix wildcard, so "Describe*" hides every describe
	// call. Hidden events are dropped from the CLI output; in the TUI they are
	// hidden by default and can be revealed with the "h" key.
	HideEvents []string `mapstructure:"hideEvents"`
}

// AccountConfig represents configuration for a specific AWS account sweep.
type AccountConfig struct {
	Name    string `mapstructure:"name"`
	Profile string `mapstructure:"profile"`
	RoleARN string `mapstructure:"roleArn"`
}

// DisplayConfig controls which attributes are shown for each resource type.
// Each module (vpc, s3) maps resource-type keys to column/detail field lists.
// When a list is empty the built-in defaults are used.
//
// Example config.yaml:
//
//	display:
//	  vpc:
//	    ec2_instances:
//	      columns: [instance_id, name, state, type, private_ip, az, iam_role]
//	      detail:  [instance_id, name, state, type, platform, private_ip, public_ip,
//	                az, subnet_id, vpc_id, iam_role, ami_id, key_pair, launch_time, tags]
//	  s3:
//	    objects:
//	      columns: [name, size, last_modified, storage_class]
//	    buckets:
//	      columns: [name, region, creation_date]
type DisplayConfig struct {
	VPC map[string]ResourceDisplay `mapstructure:"vpc"`
	S3  S3DisplayConfig            `mapstructure:"s3"`
}

// ResourceDisplay holds the field key lists for one resource type.
type ResourceDisplay struct {
	Columns []string `mapstructure:"columns"`
	Detail  []string `mapstructure:"detail"`
}

// S3DisplayConfig holds display settings for the two S3 views.
type S3DisplayConfig struct {
	Objects ResourceDisplay `mapstructure:"objects"`
	Buckets ResourceDisplay `mapstructure:"buckets"`
}

type AppConfig struct {
	DefaultOutput  string `mapstructure:"defaultOutput"`
	DefaultMode    string `mapstructure:"defaultMode"`
	TimeoutSeconds int    `mapstructure:"timeoutSeconds"`
	MaxConcurrency int    `mapstructure:"maxConcurrency"`
	// DownloadDir is the directory the S3 browser writes downloaded objects to.
	// A leading "~" is expanded to the user's home directory. Empty means the
	// current working directory ("."). The directory is created if missing.
	DownloadDir string `mapstructure:"downloadDir"`
}

type AWSConfig struct {
	Profile    string            `mapstructure:"profile"`
	AuthMethod string            `mapstructure:"authMethod"` // auto, profile, env, static, sts
	Regions    []string          `mapstructure:"regions"`
	AllRegions bool              `mapstructure:"allRegions"`
	STS        STSConfig         `mapstructure:"sts"`
	Static     StaticCredentials `mapstructure:"static"`
	Retry      RetryConfig       `mapstructure:"retry"`
}

// RetryConfig tunes how AWS API calls are retried. Large accounts hitting
// throttling can raise maxAttempts and switch to the adaptive mode, which
// client-side rate-limits to back off automatically.
type RetryConfig struct {
	// MaxAttempts is the total number of attempts per API call (1 = no
	// retries). 0 means use the AWS SDK default (3).
	MaxAttempts int `mapstructure:"maxAttempts"`
	// Mode selects the SDK retry strategy: "standard" (default) or
	// "adaptive" (standard plus client-side rate limiting on throttle).
	Mode string `mapstructure:"mode"`
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
	// Themes holds per-theme color role overrides. The outer map key is the
	// theme name; the inner map maps a color role name to a hex color string
	// (e.g. "tableHeader: '#F5A200'").
	//
	// The full list of role names — and the related role each one falls back
	// to when unset — is the Roles registry in internal/ui/theme.go. Role
	// names are matched case-insensitively, and the roles are intentionally
	// granular ("as minute as possible") so that changing one part of the UI
	// (say, the table header) never alters an unrelated part (say, a panel
	// border). Only the roles you want to change need to be listed.
	Themes map[string]map[string]string `mapstructure:"themes"`
}
