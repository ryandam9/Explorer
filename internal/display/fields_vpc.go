package display

// VPCFields maps resource-type config keys to their field registries.
var VPCFields = map[string][]FieldMeta{
	"ec2_instances":      EC2InstanceFields,
	"subnets":            SubnetFields,
	"security_groups":    SGFields,
	"route_tables":       RouteTableFields,
	"internet_gateways":  IGWFields,
	"nat_gateways":       NatGWFields,
	"endpoints":          EndpointFields,
	"network_acls":       NACLFields,
	"peering":            PeeringFields,
	"flow_logs":          FlowLogFields,
	"network_interfaces": ENIFields,
	"lambda":             LambdaFields,
	"rds":                RDSFields,
	"load_balancers":     LBFields,
}

var ENIFields = []FieldMeta{
	{Key: "eni_id", Title: "ENI ID", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "type", Title: "Type", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "status", Title: "Status", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "private_ip", Title: "Private IP", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "public_ip", Title: "Public IP", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "attached_to", Title: "Attached To", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "subnet_id", Title: "Subnet ID", Width: 24, DefaultCol: false, DefaultDetail: true},
	{Key: "az", Title: "AZ", Width: 14, DefaultCol: false, DefaultDetail: true},
	{Key: "security_groups", Title: "Security Groups", Width: 30, DefaultCol: false, DefaultDetail: true},
	{Key: "source_dest_check", Title: "Src/Dst Check", Width: 13, DefaultCol: false, DefaultDetail: true},
	{Key: "description", Title: "Description", Width: 40, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var EC2InstanceFields = []FieldMeta{
	{Key: "instance_id", Title: "Instance ID", Width: 20, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 18, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "type", Title: "Type", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "private_ip", Title: "Private IP", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "public_ip", Title: "Public IP", Width: 16, DefaultCol: false, DefaultDetail: true},
	{Key: "az", Title: "AZ", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "platform", Title: "Platform", Width: 12, DefaultCol: false, DefaultDetail: true},
	{Key: "subnet_id", Title: "Subnet ID", Width: 24, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "iam_role", Title: "IAM Role", Width: 50, DefaultCol: false, DefaultDetail: true},
	{Key: "ami_id", Title: "AMI ID", Width: 24, DefaultCol: false, DefaultDetail: true},
	{Key: "key_pair", Title: "Key Pair", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "launch_time", Title: "Launch Time", Width: 18, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var SubnetFields = []FieldMeta{
	{Key: "name", Title: "Name", Width: 18, DefaultCol: true, DefaultDetail: true},
	{Key: "cidr", Title: "CIDR", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "az", Title: "AZ", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "available_ips", Title: "Avail IPs", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "public", Title: "Public", Width: 7, DefaultCol: true, DefaultDetail: true},
	{Key: "subnet_id", Title: "Subnet ID", Width: 24, DefaultCol: false, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 10, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "default_for_az", Title: "Default for AZ", Width: 14, DefaultCol: false, DefaultDetail: true},
	{Key: "map_public_ip", Title: "Map Public IP", Width: 14, DefaultCol: false, DefaultDetail: true},
	{Key: "ipv6_cidrs", Title: "IPv6 CIDRs", Width: 30, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var SGFields = []FieldMeta{
	{Key: "sg_id", Title: "SG ID", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "inbound", Title: "In", Width: 5, DefaultCol: true, DefaultDetail: true},
	{Key: "outbound", Title: "Out", Width: 5, DefaultCol: true, DefaultDetail: true},
	{Key: "description", Title: "Description", Width: 36, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "rules", Title: "Rules", Width: 0, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var RouteTableFields = []FieldMeta{
	{Key: "rt_id", Title: "RT ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 18, DefaultCol: true, DefaultDetail: true},
	{Key: "routes", Title: "Routes", Width: 7, DefaultCol: true, DefaultDetail: true},
	{Key: "subnets", Title: "Subnets", Width: 7, DefaultCol: true, DefaultDetail: true},
	{Key: "main", Title: "Main", Width: 6, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "route_list", Title: "Route Details", Width: 0, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var IGWFields = []FieldMeta{
	{Key: "igw_id", Title: "IGW ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var NatGWFields = []FieldMeta{
	{Key: "nat_id", Title: "NAT ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 18, DefaultCol: true, DefaultDetail: true},
	{Key: "nat_type", Title: "Type", Width: 8, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "public_ip", Title: "Public IP", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "subnet_id", Title: "Subnet", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "private_ip", Title: "Private IP", Width: 16, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var EndpointFields = []FieldMeta{
	{Key: "endpoint_id", Title: "Endpoint ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "service", Title: "Service", Width: 40, DefaultCol: true, DefaultDetail: true},
	{Key: "ep_type", Title: "Type", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var NACLFields = []FieldMeta{
	{Key: "nacl_id", Title: "NACL ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "name", Title: "Name", Width: 18, DefaultCol: true, DefaultDetail: true},
	{Key: "rule_count", Title: "Rules", Width: 6, DefaultCol: true, DefaultDetail: false},
	{Key: "subnet_count", Title: "Subnets", Width: 7, DefaultCol: true, DefaultDetail: false},
	{Key: "is_default", Title: "Default", Width: 8, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "rule_list", Title: "Rules", Width: 0, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var PeeringFields = []FieldMeta{
	{Key: "peering_id", Title: "Peering ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "status", Title: "Status", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "requester_vpc", Title: "Requester VPC", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "accepter_vpc", Title: "Accepter VPC", Width: 22, DefaultCol: true, DefaultDetail: true},
	{Key: "requester_region", Title: "Req Region", Width: 15, DefaultCol: false, DefaultDetail: true},
	{Key: "requester_cidr", Title: "Req CIDR", Width: 18, DefaultCol: false, DefaultDetail: true},
	{Key: "accepter_region", Title: "Acc Region", Width: 15, DefaultCol: false, DefaultDetail: true},
	{Key: "accepter_cidr", Title: "Acc CIDR", Width: 18, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var FlowLogFields = []FieldMeta{
	{Key: "log_id", Title: "Log ID", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "traffic", Title: "Traffic", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "status", Title: "Status", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "destination", Title: "Destination", Width: 40, DefaultCol: true, DefaultDetail: true},
	{Key: "resource_id", Title: "Resource ID", Width: 24, DefaultCol: false, DefaultDetail: true},
	{Key: "log_format", Title: "Log Format", Width: 0, DefaultCol: false, DefaultDetail: true},
	{Key: "tags", Title: "Tags", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var LambdaFields = []FieldMeta{
	{Key: "name", Title: "Function Name", Width: 30, DefaultCol: true, DefaultDetail: true},
	{Key: "runtime", Title: "Runtime", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "memory", Title: "Memory", Width: 8, DefaultCol: true, DefaultDetail: true},
	{Key: "timeout", Title: "Timeout", Width: 9, DefaultCol: true, DefaultDetail: true},
	{Key: "handler", Title: "Handler", Width: 30, DefaultCol: false, DefaultDetail: true},
	{Key: "last_modified", Title: "Last Modified", Width: 20, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "subnets", Title: "Subnets", Width: 0, DefaultCol: false, DefaultDetail: true},
	{Key: "security_groups", Title: "Security Groups", Width: 0, DefaultCol: false, DefaultDetail: true},
}

var RDSFields = []FieldMeta{
	{Key: "db_id", Title: "DB ID", Width: 28, DefaultCol: true, DefaultDetail: true},
	{Key: "engine", Title: "Engine", Width: 20, DefaultCol: true, DefaultDetail: true},
	{Key: "class", Title: "Class", Width: 16, DefaultCol: true, DefaultDetail: true},
	{Key: "status", Title: "Status", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "az", Title: "AZ", Width: 14, DefaultCol: true, DefaultDetail: true},
	{Key: "multi_az", Title: "Multi-AZ", Width: 9, DefaultCol: true, DefaultDetail: true},
	{Key: "storage", Title: "Storage (GB)", Width: 12, DefaultCol: false, DefaultDetail: true},
	{Key: "endpoint", Title: "Endpoint", Width: 40, DefaultCol: false, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
}

var LBFields = []FieldMeta{
	{Key: "name", Title: "Name", Width: 24, DefaultCol: true, DefaultDetail: true},
	{Key: "lb_type", Title: "Type", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "scheme", Title: "Scheme", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "state", Title: "State", Width: 12, DefaultCol: true, DefaultDetail: true},
	{Key: "dns_name", Title: "DNS Name", Width: 40, DefaultCol: true, DefaultDetail: true},
	{Key: "vpc_id", Title: "VPC ID", Width: 22, DefaultCol: false, DefaultDetail: true},
	{Key: "created_at", Title: "Created", Width: 20, DefaultCol: false, DefaultDetail: true},
	{Key: "arn", Title: "ARN", Width: 0, DefaultCol: false, DefaultDetail: true},
}
