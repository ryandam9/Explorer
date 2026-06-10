package config

// Config represents the top-level configuration matching the specification.
type Config struct {
	App      AppConfig                `mapstructure:"app"`
	AWS      AWSConfig                `mapstructure:"aws"`
	Services map[string]ServiceConfig `mapstructure:"services"`
	Filters  FilterConfig             `mapstructure:"filters"`
	Output   OutputConfig             `mapstructure:"output"`
	UI       UIConfig                 `mapstructure:"ui"`
	Display  DisplayConfig            `mapstructure:"display"`
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
