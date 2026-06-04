package model

import "time"

// Resource represents a normalized AWS resource.
type Resource struct {
	Service   string                 `json:"service"`
	Type      string                 `json:"type"`
	Region    string                 `json:"region"`
	AccountID string                 `json:"accountId,omitempty"`
	ID        string                 `json:"id"`
	Name      string                 `json:"name,omitempty"`
	ARN       string                 `json:"arn,omitempty"`
	State     string                 `json:"state,omitempty"`
	CreatedAt *time.Time             `json:"createdAt,omitempty"`
	Tags      map[string]string      `json:"tags,omitempty"`
	Summary   map[string]string      `json:"summary,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}
