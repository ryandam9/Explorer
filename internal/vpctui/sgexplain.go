package vpctui

import (
	"strings"
)

// ---------------------------------------------------------------------------
// Security Group rule explanations
//
// Security Group rules are fully structured (protocol, port range, source,
// direction), so each one can be translated into a plain-English sentence with
// static lookup tables — no AI required. We also flag rules that expose risky
// ports to the public internet.
// ---------------------------------------------------------------------------

// portServices maps well-known TCP/UDP ports to a short service name shown in
// explanations, e.g. 443 -> "HTTPS".
var portServices = map[string]string{
	"20":    "FTP-data",
	"21":    "FTP",
	"22":    "SSH",
	"23":    "Telnet",
	"25":    "SMTP",
	"53":    "DNS",
	"67":    "DHCP",
	"68":    "DHCP",
	"69":    "TFTP",
	"80":    "HTTP",
	"110":   "POP3",
	"123":   "NTP",
	"135":   "MS-RPC",
	"137":   "NetBIOS",
	"138":   "NetBIOS",
	"139":   "NetBIOS",
	"143":   "IMAP",
	"161":   "SNMP",
	"389":   "LDAP",
	"443":   "HTTPS",
	"445":   "SMB",
	"465":   "SMTPS",
	"514":   "Syslog",
	"587":   "SMTP",
	"636":   "LDAPS",
	"993":   "IMAPS",
	"995":   "POP3S",
	"1433":  "MS SQL Server",
	"1521":  "Oracle DB",
	"2049":  "NFS",
	"2375":  "Docker",
	"2376":  "Docker (TLS)",
	"3000":  "Grafana/dev",
	"3306":  "MySQL/Aurora",
	"3389":  "RDP",
	"4789":  "VXLAN",
	"5432":  "PostgreSQL",
	"5439":  "Redshift",
	"5601":  "Kibana",
	"5672":  "AMQP/RabbitMQ",
	"5900":  "VNC",
	"5984":  "CouchDB",
	"6379":  "Redis",
	"6443":  "Kubernetes API",
	"8080":  "HTTP (alt)",
	"8443":  "HTTPS (alt)",
	"8500":  "Consul",
	"9000":  "SonarQube/MinIO",
	"9092":  "Kafka",
	"9200":  "Elasticsearch",
	"9300":  "Elasticsearch",
	"11211": "Memcached",
	"15672": "RabbitMQ admin",
	"27017": "MongoDB",
}

// adminPorts are remote-administration ports that are dangerous to expose to
// the public internet.
var adminPorts = map[string]bool{
	"22":   true, // SSH
	"23":   true, // Telnet
	"3389": true, // RDP
	"5900": true, // VNC
}

// dataPorts are database / cache / search ports that should almost never be
// reachable from the public internet.
var dataPorts = map[string]bool{
	"1433": true, "1521": true, "3306": true, "5432": true, "5439": true,
	"6379": true, "9200": true, "9300": true, "11211": true, "27017": true,
	"5984": true, "9092": true,
}

// explainSGRule renders a single rule as a plain-English sentence, with a
// trailing risk note when the rule exposes a sensitive port to the internet.
func explainSGRule(r SGRule) string {
	verb, prep := "Allow inbound", "from"
	if strings.EqualFold(r.Direction, "Outbound") {
		verb, prep = "Allow outbound", "to"
	}

	sentence := verb + " " + describeProtoPorts(r.Protocol, r.PortRange) + " " + prep + " " + describeSource(r.Source)
	if note := exposureRisk(r.Protocol, r.PortRange, r.Source); note != "" {
		sentence += "  " + note
	}
	return sentence
}

// describeProtoPorts turns the protocol + port range into a readable phrase such
// as "HTTPS (TCP 443)", "all TCP ports", or "all traffic".
func describeProtoPorts(protocol, portRange string) string {
	proto := strings.TrimSpace(protocol)
	if proto == "" || strings.EqualFold(proto, "All") {
		return "all traffic"
	}
	ports := strings.TrimSpace(portRange)
	if ports == "" || strings.EqualFold(ports, "All") {
		return "all " + proto + " ports"
	}
	if !strings.Contains(ports, "-") {
		if svc, ok := portServices[ports]; ok {
			return svc + " (" + proto + " " + ports + ")"
		}
		return proto + " port " + ports
	}
	return proto + " ports " + ports
}

// describeSource explains a rule's source/destination: CIDRs, the all-internet
// ranges, single hosts, private networks, security-group and prefix-list refs.
func describeSource(src string) string {
	src = strings.TrimSpace(src)
	switch {
	case src == "" || src == "-":
		return "any source"
	case src == "0.0.0.0/0":
		return "anywhere on the internet (0.0.0.0/0)"
	case src == "::/0":
		return "anywhere on the internet over IPv6 (::/0)"
	case strings.HasPrefix(src, "sg-"):
		return "resources in security group " + src
	case strings.HasPrefix(src, "pl-"):
		return "the prefix list " + src
	case strings.HasSuffix(src, "/32"):
		return "the single host " + strings.TrimSuffix(src, "/32")
	case strings.HasSuffix(src, "/128"):
		return "the single host " + strings.TrimSuffix(src, "/128")
	case isPrivateCIDR(src):
		return "the private network " + src
	default:
		return src
	}
}

// exposureRisk returns a warning when a rule allows a sensitive port (or all
// ports) from the public internet. It returns "" for low-risk rules. It is
// shared by the Security Group and Network ACL explanations.
func exposureRisk(protocol, portRange, source string) string {
	if !isPublicSource(source) {
		return ""
	}
	proto := strings.TrimSpace(protocol)
	ports := strings.TrimSpace(portRange)

	if proto == "" || strings.EqualFold(proto, "All") || strings.EqualFold(ports, "All") {
		return "⚠ ALL ports open to the entire internet"
	}
	if !strings.Contains(ports, "-") {
		switch {
		case adminPorts[ports]:
			return "⚠ remote admin access open to the entire internet"
		case dataPorts[ports]:
			return "⚠ database/cache port exposed to the entire internet"
		}
	} else if rangeIncludesSensitivePort(ports) {
		return "⚠ port range exposes sensitive ports to the entire internet"
	}
	// Other public ports (e.g. HTTP/HTTPS) are normal for internet-facing
	// services, so they are not flagged to avoid alert fatigue.
	return ""
}

func isPublicSource(src string) bool {
	src = strings.TrimSpace(src)
	return src == "0.0.0.0/0" || src == "::/0"
}

// isPrivateCIDR reports whether an IPv4 CIDR falls entirely within the RFC 1918
// private ranges (10/8, 172.16/12, 192.168/16).
func isPrivateCIDR(cidr string) bool {
	ip := cidr
	if i := strings.IndexByte(cidr, '/'); i >= 0 {
		ip = cidr[:i]
	}
	switch {
	case strings.HasPrefix(ip, "10."):
		return true
	case strings.HasPrefix(ip, "192.168."):
		return true
	case strings.HasPrefix(ip, "172."):
		// 172.16.0.0 – 172.31.255.255
		rest := strings.TrimPrefix(ip, "172.")
		dot := strings.IndexByte(rest, '.')
		if dot < 0 {
			return false
		}
		return secondOctetIn16to31(rest[:dot])
	default:
		return false
	}
}

func secondOctetIn16to31(s string) bool {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	return n >= 16 && n <= 31
}

// rangeIncludesSensitivePort reports whether a "from-to" port range covers any
// known admin or data port.
func rangeIncludesSensitivePort(rng string) bool {
	from, to, ok := parsePortRange(rng)
	if !ok {
		return false
	}
	for port := range adminPorts {
		if p, ok := atoiPort(port); ok && p >= from && p <= to {
			return true
		}
	}
	for port := range dataPorts {
		if p, ok := atoiPort(port); ok && p >= from && p <= to {
			return true
		}
	}
	return false
}

func parsePortRange(rng string) (from, to int, ok bool) {
	parts := strings.SplitN(rng, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	f, okF := atoiPort(strings.TrimSpace(parts[0]))
	t, okT := atoiPort(strings.TrimSpace(parts[1]))
	if !okF || !okT {
		return 0, 0, false
	}
	return f, t, true
}

func atoiPort(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
