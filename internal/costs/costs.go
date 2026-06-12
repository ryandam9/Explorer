// Package costs holds the static price table used by the cost/waste linter
// (AXE-004) to attach an order-of-magnitude monthly estimate to each finding.
//
// Prices are us-east-1 on-demand rates, hand-maintained from the public AWS
// pricing pages. They are deliberately approximate: the linter's job is to
// rank waste and justify action ("this idle NAT gateway costs ~$33/mo"), not
// to reproduce a bill. Regional price differences are out of scope.
package costs

import "fmt"

// HoursPerMonth is the convention AWS pricing pages use (365.25*24/12 ≈ 730).
const HoursPerMonth = 730.0

// EBS storage, $/GiB-month.
// Source: https://aws.amazon.com/ebs/pricing/ (us-east-1).
const (
	EBSGP2PerGiBMonth      = 0.10
	EBSGP3PerGiBMonth      = 0.08
	EBSIO1PerGiBMonth      = 0.125
	EBSIO2PerGiBMonth      = 0.125
	EBSST1PerGiBMonth      = 0.045
	EBSSC1PerGiBMonth      = 0.015
	EBSMagneticPerGiBMonth = 0.05 // "standard" (previous generation)
)

// EBSSnapshotPerGiBMonth is the standard-tier snapshot price, $/GiB-month.
// Snapshots are incremental, so size-based estimates are an upper bound.
// Source: https://aws.amazon.com/ebs/pricing/.
const EBSSnapshotPerGiBMonth = 0.05

// ElasticIPMonth: an idle (unassociated) public IPv4 address bills $0.005/hr.
// Source: https://aws.amazon.com/vpc/pricing/ (Public IPv4 addresses).
const ElasticIPMonth = 0.005 * HoursPerMonth

// NATGatewayMonth is the hourly charge alone ($0.045/hr); data processing
// ($0.045/GB) comes on top and is not estimated.
// Source: https://aws.amazon.com/vpc/pricing/.
const NATGatewayMonth = 0.045 * HoursPerMonth

// Load balancer hourly charges, excluding LCU/NLCU usage.
// Source: https://aws.amazon.com/elasticloadbalancing/pricing/.
const (
	ALBMonth  = 0.0225 * HoursPerMonth
	NLBMonth  = 0.0225 * HoursPerMonth
	GWLBMonth = 0.0125 * HoursPerMonth
)

// DynamoDB provisioned-capacity prices per unit-month.
// Source: https://aws.amazon.com/dynamodb/pricing/provisioned/
// ($0.00013 per RCU-hour, $0.00065 per WCU-hour).
const (
	DynamoDBRCUMonth = 0.00013 * HoursPerMonth
	DynamoDBWCUMonth = 0.00065 * HoursPerMonth
)

// EBSPerGiBMonth returns the monthly storage price for an EBS volume type.
// Unknown/future types fall back to the gp3 rate, the cheapest general
// purpose tier, so estimates stay conservative.
func EBSPerGiBMonth(volumeType string) float64 {
	switch volumeType {
	case "gp2":
		return EBSGP2PerGiBMonth
	case "gp3":
		return EBSGP3PerGiBMonth
	case "io1":
		return EBSIO1PerGiBMonth
	case "io2":
		return EBSIO2PerGiBMonth
	case "st1":
		return EBSST1PerGiBMonth
	case "sc1":
		return EBSSC1PerGiBMonth
	case "standard":
		return EBSMagneticPerGiBMonth
	default:
		return EBSGP3PerGiBMonth
	}
}

// EBSVolumeMonth estimates the monthly storage cost of a volume.
func EBSVolumeMonth(volumeType string, sizeGiB int32) float64 {
	if sizeGiB <= 0 {
		return 0
	}
	return float64(sizeGiB) * EBSPerGiBMonth(volumeType)
}

// GP2ToGP3SavingsMonth estimates the monthly saving from migrating a gp2
// volume to gp3 at the same size (gp3 baseline performance covers typical
// gp2 workloads).
func GP2ToGP3SavingsMonth(sizeGiB int32) float64 {
	if sizeGiB <= 0 {
		return 0
	}
	return float64(sizeGiB) * (EBSGP2PerGiBMonth - EBSGP3PerGiBMonth)
}

// SnapshotMonth estimates the monthly cost of a snapshot from its source
// volume size (an upper bound — snapshots are incremental).
func SnapshotMonth(sizeGiB int32) float64 {
	if sizeGiB <= 0 {
		return 0
	}
	return float64(sizeGiB) * EBSSnapshotPerGiBMonth
}

// LoadBalancerMonth returns the monthly hourly-charge cost for an ELBv2 load
// balancer type ("application", "network", "gateway").
func LoadBalancerMonth(lbType string) float64 {
	switch lbType {
	case "network":
		return NLBMonth
	case "gateway":
		return GWLBMonth
	default:
		return ALBMonth
	}
}

// DynamoDBProvisionedMonth estimates the monthly cost of provisioned table
// capacity.
func DynamoDBProvisionedMonth(rcu, wcu float64) float64 {
	if rcu < 0 {
		rcu = 0
	}
	if wcu < 0 {
		wcu = 0
	}
	return rcu*DynamoDBRCUMonth + wcu*DynamoDBWCUMonth
}

// Dollars formats a USD amount for display ("$32.85"). Zero renders as "-"
// so unknown estimates don't read as "free".
func Dollars(usd float64) string {
	if usd <= 0 {
		return "-"
	}
	return fmt.Sprintf("$%.2f", usd)
}
