package emrconn

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func toLowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func decodeJSON(body []byte, v any) error {
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: bad JSON response: %v", ErrUnreachable, err)
	}
	return nil
}

// IsUnreachable reports whether err is (or wraps) ErrUnreachable.
func IsUnreachable(err error) bool { return errors.Is(err, ErrUnreachable) }

// ConnectHelp returns the multi-line "how to connect" text shown when a live
// browser can't reach its daemon. It explains the opt-in connection layer and,
// when the cluster's primary DNS is known, the exact SSH dynamic-tunnel command
// that AWS documents for the web UIs.
func ConnectHelp(masterDNS string, port int) string {
	var b strings.Builder
	b.WriteString("On-cluster access is not available.\n\n")
	b.WriteString("YARN, HBase and Oozie run as REST daemons on the cluster's primary node and\n")
	b.WriteString("have no AWS API — they are reachable only from inside the cluster's VPC. This\n")
	b.WriteString("feature is opt-in; enable it in config.yaml:\n\n")
	b.WriteString("  emr:\n")
	b.WriteString("    onCluster:\n")
	b.WriteString("      mode: socks            # or 'direct' when running inside the VPC\n")
	b.WriteString("      socksProxy: 127.0.0.1:8157\n\n")
	if masterDNS != "" {
		b.WriteString("Then open an SSH dynamic tunnel to the primary node (separate terminal):\n\n")
		b.WriteString(fmt.Sprintf("  ssh -i <key.pem> -N -D 8157 hadoop@%s\n\n", masterDNS))
		if port > 0 {
			b.WriteString(fmt.Sprintf("The security group must allow the daemon port (%d) from your tunnel.\n", port))
		}
	} else {
		b.WriteString("The cluster's primary DNS is unknown (cluster not running?), so no tunnel\n")
		b.WriteString("command can be shown.\n")
	}
	return b.String()
}
