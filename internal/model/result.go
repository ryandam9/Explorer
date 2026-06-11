package model

// ExploreResult represents the outcome of an exploration run.
type ExploreResult struct {
	Resources []Resource
	Errors    []ExploreError
}

// ResultChunk is a partial result emitted during a streaming run.
type ResultChunk struct {
	Resources []Resource
	Errors    []ExploreError
	// Progress, when non-nil, reports that one collection task (a service ×
	// region pair) finished — successfully or not, with or without resources —
	// so consumers can show real scan progress instead of a spinner.
	Progress *TaskProgress
}

// TaskProgress identifies a finished collection task.
type TaskProgress struct {
	Service string
	Region  string
}

// ExploreError represents an error that occurred during exploration.
type ExploreError struct {
	Service string `json:"service"`
	Region  string `json:"region"`
	Code    string `json:"code"`
	Message string `json:"message"`
	// Partial is true when the failing collector still returned some
	// resources (collected before the failure) and those were kept.
	Partial bool `json:"partial,omitempty"`
}
