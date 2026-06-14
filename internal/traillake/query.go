package traillake

import (
	"fmt"
	"strings"
	"time"
)

// Preset parameterizes the built-in Lake queries. The zero value is "recent
// activity, no filters, default limit".
type Preset struct {
	Since       time.Time
	Limit       int
	Principal   string // substring match on userIdentity.arn
	EventName   string // exact match on eventName
	EventSource string // exact match on eventSource (e.g. s3.amazonaws.com)
	ErrorsOnly  bool   // only events carrying an errorCode
}

// RecentSQL is the default feed: recent events, newest first, with the same
// columns as the LookupEvents feed plus whatever the filters narrow to.
func RecentSQL(edsID string, p Preset) string {
	return fmt.Sprintf(
		"SELECT eventTime, eventName, userIdentity.arn, sourceIPAddress, errorCode "+
			"FROM %s%s ORDER BY eventTime DESC LIMIT %d",
		edsID, whereClause(p), limitOf(p))
}

// TopPrincipalsSQL ranks principals by event count over the window — "who is
// most active" / "who is generating all the denials" (with --errors-only).
func TopPrincipalsSQL(edsID string, p Preset) string {
	return fmt.Sprintf(
		"SELECT userIdentity.arn AS principal, COUNT(*) AS events "+
			"FROM %s%s GROUP BY userIdentity.arn ORDER BY events DESC LIMIT %d",
		edsID, whereClause(p), limitOf(p))
}

// TopEventsSQL ranks API calls by volume over the window.
func TopEventsSQL(edsID string, p Preset) string {
	return fmt.Sprintf(
		"SELECT eventName, COUNT(*) AS events "+
			"FROM %s%s GROUP BY eventName ORDER BY events DESC LIMIT %d",
		edsID, whereClause(p), limitOf(p))
}

// whereClause builds the shared WHERE from the preset filters. Returns "" when
// nothing is set. String values are single-quote escaped so an operator-typed
// principal or event name cannot break (or inject into) the query.
func whereClause(p Preset) string {
	var conds []string
	if !p.Since.IsZero() {
		conds = append(conds, "eventTime > "+sqlLit(p.Since.UTC().Format("2006-01-02 15:04:05")))
	}
	if p.Principal != "" {
		conds = append(conds, "userIdentity.arn LIKE "+sqlLit("%"+p.Principal+"%"))
	}
	if p.EventName != "" {
		conds = append(conds, "eventName = "+sqlLit(p.EventName))
	}
	if p.EventSource != "" {
		conds = append(conds, "eventSource = "+sqlLit(p.EventSource))
	}
	if p.ErrorsOnly {
		conds = append(conds, "errorCode IS NOT NULL")
	}
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

func sqlLit(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func limitOf(p Preset) int {
	if p.Limit <= 0 {
		return 50
	}
	return p.Limit
}
