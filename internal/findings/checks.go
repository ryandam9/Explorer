package findings

// CheckMeta describes one check: its stable ID, a PascalCase rule name (used
// as the SARIF rule name), a one-line summary of what it detects, and the
// severity it reports at. The registry is the source of truth for "--ignore"
// validation and SARIF rule metadata; every check a linter can emit must be
// registered here.
type CheckMeta struct {
	ID       string
	Name     string
	Summary  string
	Severity Severity
}

var checkRegistry = []CheckMeta{
	{CheckUnattachedVolume, "UnattachedEBSVolume",
		"EBS volume not attached to any instance but still billing for its provisioned size", SevWarning},
	{CheckGP2Volume, "GP2VolumeCouldBeGP3",
		"gp2 EBS volume that could migrate online to the ~20% cheaper gp3 type", SevInfo},
	{CheckUnassociatedEIP, "UnassociatedElasticIP",
		"Elastic IP not associated with any resource, billing hourly", SevWarning},
	{CheckIdleNATGateway, "IdleNATGateway",
		"available NAT gateway no route table routes through, billing hourly with no traffic", SevWarning},
	{CheckLBNoHealthyTarget, "LoadBalancerNoHealthyTargets",
		"load balancer whose target groups have no healthy targets, billing while serving nothing", SevWarning},
	{CheckLBIdle, "LoadBalancerIdle",
		"load balancer with zero requests/flows over the 14-day lookback window", SevWarning},
	{CheckStoppedWithEBS, "StoppedInstanceWithEBS",
		"stopped EC2 instance whose attached EBS volumes keep billing", SevInfo},
	{CheckOldSnapshot, "OldUnreferencedSnapshot",
		"EBS snapshot older than 180 days and not referenced by any AMI in the account", SevInfo},
	{CheckUnusedAMI, "UnusedAMI",
		"AMI older than 180 days that no instance uses, whose backing snapshots keep billing", SevInfo},
	{CheckDDBOverProvision, "DynamoDBOverProvisioned",
		"provisioned DynamoDB table consuming under 10% of its provisioned capacity", SevWarning},

	// Security category (AXE-008).
	{CheckS3Public, "PublicS3Bucket",
		"S3 bucket whose policy status reports it as public", SevCritical},
	{CheckS3PABOff, "S3PublicAccessBlockOff",
		"S3 bucket without all four Public Access Block settings enabled", SevCritical},
	{CheckS3EncryptionOff, "S3DefaultEncryptionOff",
		"S3 bucket with no default encryption configuration", SevWarning},
	{CheckEBSUnencrypted, "UnencryptedEBSVolumeSecurity",
		"EBS volume without encryption", SevWarning},
	{CheckEBSDefaultEncOff, "EBSDefaultEncryptionOff",
		"region where EBS encryption-by-default is disabled", SevWarning},
	{CheckPublicEBSSnapshot, "PublicEBSSnapshot",
		"EBS snapshot restorable by any AWS account", SevCritical},
	{CheckRDSPublic, "RDSPubliclyAccessible",
		"RDS instance with PubliclyAccessible enabled", SevCritical},
	{CheckRDSUnencrypted, "RDSStorageUnencrypted",
		"RDS instance without storage encryption", SevWarning},
	{CheckPublicRDSSnapshot, "PublicRDSSnapshot",
		"RDS snapshot restorable by any AWS account", SevCritical},
	{CheckIMDSv1, "IMDSv1Allowed",
		"EC2 instance whose metadata service does not require IMDSv2 tokens", SevWarning},
	{CheckSGOpenPort, "SecurityGroupSensitivePortOpen",
		"security group exposing a sensitive port (SSH, RDP, databases, …) to the internet", SevCritical},
	{CheckLambdaURLNoAuth, "LambdaFunctionURLNoAuth",
		"Lambda function URL with AuthType NONE, invocable by anyone", SevCritical},
	{CheckSQSOpenPolicy, "SQSPolicyAllowsEveryone",
		"SQS queue policy with an unconditioned Allow for Principal \"*\"", SevCritical},
	{CheckSNSOpenPolicy, "SNSPolicyAllowsEveryone",
		"SNS topic policy with an unconditioned Allow for Principal \"*\"", SevCritical},
	{CheckAlarmNoData, "AlarmInsufficientData",
		"CloudWatch alarm stuck in INSUFFICIENT_DATA for over 7 days", SevInfo},

	// IAM hygiene category (AXE-003).
	{CheckOldAccessKey, "AccessKeyOlderThan90Days",
		"access key not rotated in over 90 days", SevWarning},
	{CheckUnusedAccessKey, "ActiveAccessKeyUnused90Days",
		"active access key unused for over 90 days (standing credential nobody needs)", SevCritical},
	{CheckUserNoMFA, "ConsoleUserWithoutMFA",
		"IAM user with console password but no MFA device", SevCritical},
	{CheckRootAccessKey, "RootAccessKeyExists",
		"active access key on the root account", SevCritical},
	{CheckUnusedRole, "RoleUnused90Days",
		"role not assumed in over 90 days (per RoleLastUsed)", SevInfo},
	{CheckWildcardPolicy, "CustomerPolicyFullAdmin",
		"customer-managed policy granting Action \"*\" on Resource \"*\"", SevCritical},
	{CheckUserAttachedPolicy, "PolicyAttachedDirectlyToUsers",
		"policy attached directly to users instead of groups/roles", SevInfo},
	{CheckOpenTrustPolicy, "TrustPolicyAllowsAnyPrincipal",
		"role trust policy allowing \"AWS\": \"*\" without conditions", SevCritical},

	// Messaging plumbing category (AXE-018).
	{CheckQueueNoConsumers, "QueueFillingNoConsumers",
		"SQS queue accumulating messages with no receive activity in 24h", SevWarning},
	{CheckRedriveDangling, "RedrivePolicyTargetMissing",
		"SQS redrive policy whose dead-letter target queue does not exist", SevCritical},
	{CheckDLQNotEmpty, "DeadLetterQueueNotEmpty",
		"dead-letter queue holding failed messages awaiting attention", SevWarning},
	{CheckSubPending, "SubscriptionPendingConfirmation",
		"SNS subscription stuck in PendingConfirmation, delivering nothing", SevWarning},
	{CheckTopicNoSubs, "TopicZeroSubscriptions",
		"SNS topic with no subscriptions — published messages are discarded", SevInfo},

	// CloudTrail configuration category (AXE-019).
	{CheckTrailNotLogging, "NoMultiRegionTrailLogging",
		"no multi-region CloudTrail is actively logging — the account has no audit trail", SevCritical},
	{CheckTrailLogValidationOff, "TrailLogValidationDisabled",
		"trail without log file validation — delivered logs could be tampered with undetected", SevWarning},
	{CheckTrailNotKMSEncrypted, "TrailLogsNotKMSEncrypted",
		"trail logs encrypted with SSE-S3 only, not a customer-managed KMS key", SevWarning},
	{CheckTrailNoCloudWatchLogs, "TrailNoCloudWatchLogs",
		"trail not delivering to CloudWatch Logs — no metric filters or alarms on its events", SevInfo},
	{CheckTrailMgmtEventsPartial, "TrailManagementEventsIncomplete",
		"trail not recording all management read/write events, leaving gaps in the audit record", SevWarning},
}

// Checks returns the registry of every known check, in declaration order.
func Checks() []CheckMeta {
	out := make([]CheckMeta, len(checkRegistry))
	copy(out, checkRegistry)
	return out
}

// CheckByID looks up a check's metadata by its stable ID.
func CheckByID(id string) (CheckMeta, bool) {
	for _, c := range checkRegistry {
		if c.ID == id {
			return c, true
		}
	}
	return CheckMeta{}, false
}
