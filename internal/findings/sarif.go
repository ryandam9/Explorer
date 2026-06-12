package findings

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// SARIF 2.1.0 output (AXE-023): serializes findings in the OASIS Static
// Analysis Results Interchange Format so they can be uploaded to GitHub code
// scanning (or any SARIF consumer). Check IDs map to rules; findings map to
// results. Spec: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html
//
// Only the fields consumers actually use are emitted; the structs below are
// for serialization, not a general SARIF model.

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name            string      `json:"name"`
	InformationURI  string      `json:"informationUri"`
	SemanticVersion string      `json:"semanticVersion,omitempty"`
	Rules           []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name,omitempty"`
	ShortDescription     sarifText         `json:"shortDescription"`
	DefaultConfiguration sarifConfig       `json:"defaultConfiguration"`
	Properties           map[string]string `json:"properties,omitempty"`
}

type sarifConfig struct {
	Level string `json:"level"`
}

type sarifText struct {
	Text string `json:"text"`
}

type sarifResult struct {
	RuleID     string          `json:"ruleId"`
	RuleIndex  int             `json:"ruleIndex"`
	Level      string          `json:"level"`
	Message    sarifText       `json:"message"`
	Locations  []sarifLocation `json:"locations"`
	Properties map[string]any  `json:"properties,omitempty"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
	LogicalLocations []sarifLogical        `json:"logicalLocations,omitempty"`
}

// sarifPhysicalLocation is required by GitHub code scanning even though cloud
// resources have no source file; the artifact URI carries the resource
// identifier, the convention cloud scanners use.
type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifLogical struct {
	FullyQualifiedName string `json:"fullyQualifiedName"`
}

// sarifLevel maps a finding severity onto SARIF's result levels.
func sarifLevel(s Severity) string {
	switch s {
	case SevCritical:
		return "error"
	case SevWarning:
		return "warning"
	default:
		return "note"
	}
}

// RenderSARIF writes the findings as a SARIF 2.1.0 document. toolVersion is
// the aws_explorer version string (may be empty). Zero findings produce a
// valid document with an empty results array.
func RenderSARIF(w io.Writer, fs []Finding, toolVersion string) error {
	rules, ruleIndex := sarifRules(fs)

	results := make([]sarifResult, 0, len(fs))
	for _, f := range fs {
		msg := f.Title
		if f.Detail != "" {
			msg += " — " + f.Detail
		}
		if f.Fix != "" {
			msg += " Fix: " + f.Fix
		}

		loc := sarifLocation{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifact{URI: artifactURI(f)},
				Region:           sarifRegion{StartLine: 1},
			},
		}
		if f.ARN != "" {
			loc.LogicalLocations = []sarifLogical{{FullyQualifiedName: f.ARN}}
		}

		results = append(results, sarifResult{
			RuleID:    f.ID,
			RuleIndex: ruleIndex[f.ID],
			Level:     sarifLevel(f.Severity),
			Message:   sarifText{Text: msg},
			Locations: []sarifLocation{loc},
			Properties: map[string]any{
				"service":       f.Service,
				"region":        f.Region,
				"resource":      f.Resource,
				"estMonthlyUSD": f.EstMonthlyUSD,
			},
		})
	}

	log := sarifLog{
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:            "aws_explorer",
				InformationURI:  "https://github.com/ryandam9/aws_explorer",
				SemanticVersion: toolVersion,
				Rules:           rules,
			}},
			Results: results,
		}},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}

// sarifRules builds the rule array for the check IDs present in fs (registry
// metadata where known, synthesized from the finding otherwise) and an
// ID → index map for results to reference.
func sarifRules(fs []Finding) ([]sarifRule, map[string]int) {
	var rules []sarifRule
	index := map[string]int{}
	for _, f := range fs {
		if _, seen := index[f.ID]; seen {
			continue
		}
		rule := sarifRule{
			ID:                   f.ID,
			ShortDescription:     sarifText{Text: f.Title},
			DefaultConfiguration: sarifConfig{Level: sarifLevel(f.Severity)},
		}
		if meta, ok := CheckByID(f.ID); ok {
			rule.Name = meta.Name
			rule.ShortDescription = sarifText{Text: meta.Summary}
			rule.DefaultConfiguration = sarifConfig{Level: sarifLevel(meta.Severity)}
		}
		index[f.ID] = len(rules)
		rules = append(rules, rule)
	}
	if rules == nil {
		rules = []sarifRule{}
	}
	return rules, index
}

// artifactURI derives a URI-safe identifier for the finding's resource. SARIF
// requires a physical location; resource display names may carry decorations
// ("nat-01 (spare)"), so only the leading token is kept and remaining unsafe
// characters are replaced.
func artifactURI(f Finding) string {
	res := f.Resource
	if i := strings.IndexAny(res, " \t"); i > 0 {
		res = res[:i]
	}
	res = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_', r == '.', r == '/', r == ':':
			return r
		default:
			return '_'
		}
	}, res)
	if res == "" {
		res = "unknown"
	}
	return fmt.Sprintf("aws/%s/%s/%s", f.Service, f.Region, res)
}
