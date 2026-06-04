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
}

// ExploreError represents an error that occurred during exploration.
type ExploreError struct {
	Service string `json:"service"`
	Region  string `json:"region"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
