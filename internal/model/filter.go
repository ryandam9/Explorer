package model

// Filter represents the filtering criteria for AWS resources.
type Filter struct {
	Regions      []string
	Services     []string
	Types        []string
	States       []string
	Tags         map[string]string
	NameContains string
	IDs          []string
}
