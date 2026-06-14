// Package traillake queries CloudTrail Lake event data stores with SQL. Unlike
// LookupEvents (90 days, management events only, in internal/trail), a Lake
// event data store can hold years of history and data events (S3 object access,
// Lambda invokes, …) and supports aggregation — at the cost of needing a store
// to be configured first. This package lists the available stores, runs a query
// to completion (StartQuery → poll GetQueryResults), and returns generic
// columnar results, plus SQL builders for the common questions.
package traillake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
)

// DataStore is one CloudTrail Lake event data store.
type DataStore struct {
	ARN  string
	ID   string // the ARN's final segment, used in a query's FROM clause
	Name string
}

// Result is the generic columnar output of a Lake query.
type Result struct {
	QueryID string
	Columns []string
	Rows    [][]string
	// BytesScanned is what the query read (and is billed for); Total is the
	// total number of matching rows the query produced.
	BytesScanned int64
	Total        int
}

// QueryOptions tunes RunQuery. The zero value polls up to defaultMaxWait and
// returns every result row.
type QueryOptions struct {
	MaxWait time.Duration // overall budget for the query to finish; 0 = default
	MaxRows int           // cap on returned rows; <=0 = no cap
}

const (
	defaultMaxWait = 60 * time.Second
	pollInterval   = 1 * time.Second
	resultPageSize = 1000
)

// ListDataStores returns the Lake event data stores visible in the config's
// region (CloudTrail Lake is regional; a multi-region store is queried from its
// home region).
func ListDataStores(ctx context.Context, cfg aws.Config) ([]DataStore, error) {
	client := cloudtrail.NewFromConfig(cfg)
	var out []DataStore
	pager := cloudtrail.NewListEventDataStoresPaginator(client, &cloudtrail.ListEventDataStoresInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, eds := range page.EventDataStores {
			arn := aws.ToString(eds.EventDataStoreArn)
			out = append(out, DataStore{ARN: arn, ID: storeID(arn), Name: aws.ToString(eds.Name)})
		}
	}
	return out, nil
}

// storeID extracts the event data store ID (the FROM-clause identifier) from
// its ARN: arn:aws:cloudtrail:region:account:eventdatastore/<id> → <id>.
func storeID(arn string) string {
	if i := strings.LastIndexByte(arn, '/'); i >= 0 {
		return arn[i+1:]
	}
	return arn
}

// RunQuery submits the SQL, polls until the query finishes (or the budget runs
// out), and returns the collected rows. A FAILED/CANCELLED/TIMED_OUT query
// returns an error carrying the service's message.
func RunQuery(ctx context.Context, cfg aws.Config, sql string, opts QueryOptions) (Result, error) {
	client := cloudtrail.NewFromConfig(cfg)
	start, err := client.StartQuery(ctx, &cloudtrail.StartQueryInput{QueryStatement: aws.String(sql)})
	if err != nil {
		return Result{}, err
	}
	qid := aws.ToString(start.QueryId)
	res := Result{QueryID: qid}

	maxWait := opts.MaxWait
	if maxWait <= 0 {
		maxWait = defaultMaxWait
	}
	deadline := time.Now().Add(maxWait)

	var nextToken *string
	for {
		out, err := client.GetQueryResults(ctx, &cloudtrail.GetQueryResultsInput{
			QueryId:         aws.String(qid),
			NextToken:       nextToken,
			MaxQueryResults: aws.Int32(resultPageSize),
		})
		if err != nil {
			return res, err
		}

		switch out.QueryStatus {
		case types.QueryStatusFinished:
			// fall through to ingest below
		case types.QueryStatusQueued, types.QueryStatusRunning:
			if time.Now().After(deadline) {
				return res, fmt.Errorf("query %s did not finish within %s (status %s)", qid, maxWait, out.QueryStatus)
			}
			select {
			case <-ctx.Done():
				return res, ctx.Err()
			case <-time.After(pollInterval):
			}
			continue // re-poll the first page until the query finishes
		default: // FAILED, CANCELLED, TIMED_OUT
			return res, fmt.Errorf("query %s %s: %s", qid, out.QueryStatus, describeQueryError(ctx, client, qid))
		}

		cols, rows := parseResultRows(out.QueryResultRows)
		if res.Columns == nil {
			res.Columns = cols
		}
		res.Rows = append(res.Rows, rows...)
		if out.QueryStatistics != nil {
			res.BytesScanned = aws.ToInt64(out.QueryStatistics.BytesScanned)
		}

		if opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
			res.Rows = res.Rows[:opts.MaxRows]
			break
		}
		if out.NextToken == nil || *out.NextToken == "" {
			break
		}
		nextToken = out.NextToken
	}
	res.Total = len(res.Rows)
	return res, nil
}

// describeQueryError returns the service's failure message for a query, or a
// generic note if it cannot be retrieved.
func describeQueryError(ctx context.Context, client *cloudtrail.Client, qid string) string {
	out, err := client.DescribeQuery(ctx, &cloudtrail.DescribeQueryInput{QueryId: aws.String(qid)})
	if err != nil || out.ErrorMessage == nil {
		return "query failed (no detail available)"
	}
	return aws.ToString(out.ErrorMessage)
}

// parseResultRows flattens CloudTrail Lake's [][]single-key-map result shape
// into ordered column names and string rows. Column order is taken from the
// first row. Pure: fixture-testable without AWS.
func parseResultRows(rows [][]map[string]string) (columns []string, out [][]string) {
	for _, row := range rows {
		if columns == nil {
			for _, cell := range row {
				for k := range cell {
					columns = append(columns, k)
				}
			}
		}
		vals := make([]string, 0, len(row))
		for _, cell := range row {
			for _, v := range cell {
				vals = append(vals, v)
			}
		}
		out = append(out, vals)
	}
	return columns, out
}
