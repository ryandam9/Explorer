package emrtui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/emr"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
)

// appUIOption is one persistent application UI the user can open for a cluster.
// uiType is the EMR PersistentAppUIType (SHS / YTS / TEZ).
type appUIOption struct {
	Label  string
	UIType string
}

// appUIOptions are the persistent UIs offered, in menu order.
var appUIOptions = []appUIOption{
	{"Spark History Server", string(emrtypes.PersistentAppUITypeShs)},
	{"YARN Timeline Server", string(emrtypes.PersistentAppUITypeYts)},
	{"Tez UI", string(emrtypes.PersistentAppUITypeTez)},
}

// appUIReadyAttempts / appUIPollInterval bound how long PersistentAppUIURL waits
// for the off-cluster UI to attach and its presigned URL to become ready.
const (
	appUIReadyAttempts = 20
	appUIPollInterval  = 1 * time.Second
)

// PersistentAppUIURL provisions (or reuses) the off-cluster persistent
// application UI for a cluster and returns a presigned URL to it (AXE-037). The
// UI is hosted off-cluster, so the link needs no SSH tunnel and survives 30 days
// past application termination.
//
// It drives the three-call AWS flow — CreatePersistentAppUI →
// DescribePersistentAppUI (poll until ATTACHED) → GetPersistentAppUIPresignedURL
// (poll until ready) — bounded by appUIReadyAttempts so it never hangs.
func (c *Client) PersistentAppUIURL(ctx context.Context, region, targetARN, uiType string) (string, error) {
	if targetARN == "" {
		return "", fmt.Errorf("cluster has no ARN to attach a persistent UI to")
	}
	cl := c.clientFor(region)

	created, err := cl.CreatePersistentAppUI(ctx, &emr.CreatePersistentAppUIInput{
		TargetResourceArn: aws.String(targetARN),
	})
	if err != nil {
		return "", fmt.Errorf("create persistent app UI: %w", err)
	}
	id := aws.ToString(created.PersistentAppUIId)
	if id == "" {
		return "", fmt.Errorf("create persistent app UI returned no ID")
	}

	// Wait for the UI to attach.
	if err := c.waitAppUIAttached(ctx, cl, id); err != nil {
		return "", err
	}

	// Wait for the presigned URL to be ready.
	for i := 0; i < appUIReadyAttempts; i++ {
		out, err := cl.GetPersistentAppUIPresignedURL(ctx, &emr.GetPersistentAppUIPresignedURLInput{
			PersistentAppUIId:   aws.String(id),
			PersistentAppUIType: emrtypes.PersistentAppUIType(uiType),
		})
		if err != nil {
			return "", fmt.Errorf("get presigned URL: %w", err)
		}
		if aws.ToBool(out.PresignedURLReady) && aws.ToString(out.PresignedURL) != "" {
			return aws.ToString(out.PresignedURL), nil
		}
		if err := sleepCtx(ctx, appUIPollInterval); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("persistent app UI URL not ready after %s", time.Duration(appUIReadyAttempts)*appUIPollInterval)
}

func (c *Client) waitAppUIAttached(ctx context.Context, cl *emr.Client, id string) error {
	for i := 0; i < appUIReadyAttempts; i++ {
		d, err := cl.DescribePersistentAppUI(ctx, &emr.DescribePersistentAppUIInput{
			PersistentAppUIId: aws.String(id),
		})
		if err != nil {
			return fmt.Errorf("describe persistent app UI: %w", err)
		}
		status := ""
		if d.PersistentAppUI != nil {
			status = aws.ToString(d.PersistentAppUI.PersistentAppUIStatus)
		}
		switch strings.ToUpper(status) {
		case "ATTACHED":
			return nil
		case "FAILED":
			return fmt.Errorf("persistent app UI failed to attach")
		}
		if err := sleepCtx(ctx, appUIPollInterval); err != nil {
			return err
		}
	}
	return fmt.Errorf("persistent app UI did not attach after %s", time.Duration(appUIReadyAttempts)*appUIPollInterval)
}

// sleepCtx sleeps for d unless ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
