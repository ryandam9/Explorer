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
	Summary  SummaryConfig            `mapstructure:"summary"`
	EMR      EMRConfig                `mapstructure:"emr"`
}

// EMRConfig configures the `emr` dashboard's optional on-cluster features
// (AXE-039) — reaching the YARN / HBase / Oozie REST daemons that run on a
// cluster's primary node and have no AWS API.
type EMRConfig struct {
	OnCluster OnClusterConfig `mapstructure:"onCluster"`
}

// OnClusterConfig controls how (and whether) the tool reaches a cluster's
// on-cluster REST daemons. It is OFF by default: the live browsers stay dark
// until the user opts in, because this is the one place the tool reaches outside
// the AWS API surface into a private VPC.
//
// Example config.yaml:
//
//	emr:
//	  onCluster:
//	    mode: socks            # off | direct | socks
//	    socksProxy: 127.0.0.1:8157
//	    timeoutSeconds: 5
//	    ports:
//	      yarn:  8088
//	      hbase: 8080
//	      oozie: 11000
type OnClusterConfig struct {
	// Mode selects how the daemons are reached: "off" (default — features
	// disabled), "direct" (tool runs inside the VPC; plain HTTP to the primary
	// node), "socks" (route through an existing SOCKS5 proxy, e.g. an
	// `ssh -D 8157` dynamic tunnel — the pattern AWS documents for the web UIs),
	// or "tunnel" (the tool opens its own SSH connection to the primary node and
	// dials the daemon through it, using the ssh settings below).
	Mode string `mapstructure:"mode"`
	// SocksProxy is the host:port of the SOCKS5 proxy used in "socks" mode.
	SocksProxy string `mapstructure:"socksProxy"`
	// SSH holds the credentials used in "tunnel" mode.
	SSH OnClusterSSH `mapstructure:"ssh"`
	// TimeoutSeconds bounds each on-cluster HTTP request. 0 uses the built-in
	// default (5s).
	TimeoutSeconds int `mapstructure:"timeoutSeconds"`
	// Ports overrides the default daemon ports (yarn 8088, hbase 8080,
	// oozie 11000); unset entries use the defaults.
	Ports OnClusterPorts `mapstructure:"ports"`
}

// OnClusterSSH holds the SSH login used by "tunnel" mode to reach the primary
// node. The tool connects to the cluster's primary DNS on the SSH port with this
// user and private key, then dials the daemon through that session.
type OnClusterSSH struct {
	// User is the SSH login (EMR's default is "hadoop").
	User string `mapstructure:"user"`
	// KeyFile is the path to the private key; a leading "~" is expanded.
	KeyFile string `mapstructure:"keyFile"`
	// Port is the SSH port (0 = 22).
	Port int `mapstructure:"port"`
}

// OnClusterPorts holds the per-daemon ports (0 = use the EMR default).
type OnClusterPorts struct {
	YARN     int `mapstructure:"yarn"`
	HBase    int `mapstructure:"hbase"`
	Oozie    int `mapstructure:"oozie"`
	NameNode int `mapstructure:"namenode"` // HDFS NameNode HTTP (default 9870)
}

// SummaryConfig configures the `summary` command.
type SummaryConfig struct {
	// CommonServices extends the built-in list of services that the coverage
	// advisory checks for (the "services with nothing shown" list). The map key
	// is the AWS service as it appears in ARNs / collector names (e.g.
	// "route53", "apprunner"), and the value is the friendly label shown to the
	// user (e.g. "Route 53", "App Runner"). Entries are merged on top of the
	// built-in catalog; an empty map leaves the defaults unchanged.
	CommonServices map[string]string `mapstructure:"commonServices"`

	// HideServices removes services from the coverage list by key, so a
	// built-in entry (or an added one) can be suppressed when it is just noise
	// for your account. Unknown keys are ignored.
	HideServices []string `mapstructure:"hideServices"`
}

// TrailConfig configures the CloudTrail activity feed (the `trail` command and
// its `--tui`).
//
// Example config.yaml:
//
//	trail:
//	  hideEvents:
//	    - Get*
//	    - Describe*
//	    - List*
//	  maxEvents: 200
type TrailConfig struct {
	// HideEvents lists CloudTrail event names to suppress from the feed so the
	// signal isn't drowned out by routine calls (read-only Get*/Describe*/List*
	// calls, ConsoleLogin, AssumeRole, …). Matching is case-insensitive; a
	// trailing "*" is a prefix wildcard, so "Describe*" hides every describe
	// call. The filter is applied server-side, so a hidden event never counts
	// against MaxEvents. An explicit `--event <Name>` lookup is never hidden.
	HideEvents []string `mapstructure:"hideEvents"`
	// MaxEvents caps how many events the `--tui` feed keeps after HideEvents is
	// applied. 0 uses the built-in default (200). `--limit` overrides it.
	MaxEvents int `mapstructure:"maxEvents"`
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
	// PreviewMaxSize caps how much of a text/XML/JSON object the S3 browser reads
	// when previewing it with "p". Accepts a human-readable size such as "10MB",
	// "512KB", or a plain byte count; empty uses the built-in default. The value
	// is clamped to a sane range — previews are always bounded.
	PreviewMaxSize string `mapstructure:"previewMaxSize"`
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
