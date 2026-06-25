package emrtui

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"

	"github.com/ryandam9/aws_explorer/internal/emrconn"
)

// Effective (merged) configuration browser (Layer 2): reads a Hadoop daemon's
// /conf endpoint over the on-cluster layer. Unlike the declared classifications
// (`emr config`, which come from the EMR API), /conf returns the *effective*
// merged Configuration the daemon is actually running — every property after all
// site files and defaults are applied, tagged with the source file it came from.
// The NameNode is queried because its classpath carries the general Hadoop config
// (core-site, hdfs-site, plus *-default.xml). Parsing is pure and fixture-tested.

// FetchEffectiveConfig fetches the NameNode's merged configuration via the
// on-cluster connection layer, as ConfigRows whose Classification/File is the
// source file each effective value came from. Returns an ErrUnreachable-wrapped
// error when the NameNode can't be reached, so callers render the connect helper.
func FetchEffectiveConfig(ctx context.Context, d *emrconn.Dialer, masterDNS string) ([]ConfigRow, error) {
	if d == nil {
		return nil, emrconn.ErrDisabled
	}
	if masterDNS == "" {
		return nil, fmt.Errorf("%w: cluster has no primary-node DNS (not running?)", emrconn.ErrUnreachable)
	}
	body, err := d.GetRaw(ctx, emrconn.ServiceNameNode, masterDNS, "/conf")
	if err != nil {
		return nil, err
	}
	return parseEffectiveConfig(body)
}

// parseEffectiveConfig decodes a Hadoop /conf payload into ConfigRows. The
// endpoint honours Accept: application/json on most versions but some always
// return XML, so JSON is tried first and XML is the fallback. Pure.
func parseEffectiveConfig(body []byte) ([]ConfigRow, error) {
	trimmed := strings.TrimLeft(string(body), " \t\r\n\ufeff")
	if strings.HasPrefix(trimmed, "<") {
		return parseEffectiveConfigXML(body)
	}
	rows, err := parseEffectiveConfigJSON(body)
	if err != nil {
		// Some daemons mislabel the body; try XML before giving up.
		if xmlRows, xerr := parseEffectiveConfigXML(body); xerr == nil {
			return xmlRows, nil
		}
		return nil, err
	}
	return rows, nil
}

type confJSON struct {
	Properties []struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Resource string `json:"resource"`
	} `json:"properties"`
}

func parseEffectiveConfigJSON(body []byte) ([]ConfigRow, error) {
	var env confJSON
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	rows := make([]ConfigRow, 0, len(env.Properties))
	for _, p := range env.Properties {
		src := effectiveSource(p.Resource)
		rows = append(rows, ConfigRow{Classification: src, File: src, Key: p.Key, Value: p.Value})
	}
	sortConfigRows(rows)
	return rows, nil
}

type confXML struct {
	Properties []struct {
		Name   string   `xml:"name"`
		Value  string   `xml:"value"`
		Source []string `xml:"source"` // Hadoop lists every source; the last wins
	} `xml:"property"`
}

func parseEffectiveConfigXML(body []byte) ([]ConfigRow, error) {
	var env confXML
	if err := xml.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	rows := make([]ConfigRow, 0, len(env.Properties))
	for _, p := range env.Properties {
		src := ""
		if len(p.Source) > 0 {
			src = p.Source[len(p.Source)-1] // last source is the effective one
		}
		src = effectiveSource(src)
		rows = append(rows, ConfigRow{Classification: src, File: src, Key: p.Name, Value: p.Value})
	}
	sortConfigRows(rows)
	return rows, nil
}

// effectiveSource normalizes a /conf "resource"/"source" value to a short file
// name (dropping any directory), defaulting to "(merged)" when absent.
func effectiveSource(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(merged)"
	}
	if i := strings.LastIndexAny(s, "/\\"); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	return s
}

// sortConfigRows orders rows by source/classification then key — the same stable
// order FlattenConfigRows uses, so declared and effective views read alike.
func sortConfigRows(rows []ConfigRow) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Classification != rows[j].Classification {
			return rows[i].Classification < rows[j].Classification
		}
		return rows[i].Key < rows[j].Key
	})
}
