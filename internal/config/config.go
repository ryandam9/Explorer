package config

// Config represents the top-level configuration matching the specification.
type Config struct {
	App      AppConfig                `mapstructure:"app"`
	AWS      AWSConfig                `mapstructure:"aws"`
	Services map[string]ServiceConfig `mapstructure:"services"`
	Filters  FilterConfig             `mapstructure:"filters"`
	Output   OutputConfig             `mapstructure:"output"`
}

type AppConfig struct {
	DefaultOutput  string `mapstructure:"defaultOutput"`
	DefaultMode    string `mapstructure:"defaultMode"`
	TimeoutSeconds int    `mapstructure:"timeoutSeconds"`
	MaxConcurrency int    `mapstructure:"maxConcurrency"`
}

type AWSConfig struct {
	Profile    string           `mapstructure:"profile"`
	AuthMethod string           `mapstructure:"authMethod"` // auto, profile, env, static, sts
	Regions    []string         `mapstructure:"regions"`
	AllRegions bool             `mapstructure:"allRegions"`
	STS        STSConfig        `mapstructure:"sts"`
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
