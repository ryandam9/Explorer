package xref

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	ectypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	rstypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
)

func TestRDSInstanceEdges(t *testing.T) {
	db := rdstypes.DBInstance{
		DBInstanceArn:        aws.String("arn:aws:rds:us-east-1:111:db:orders"),
		DBInstanceIdentifier: aws.String("orders"),
		KmsKeyId:             aws.String("arn:aws:kms:us-east-1:111:key/k"),
		VpcSecurityGroups:    []rdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-1")}},
		DBSubnetGroup: &rdstypes.DBSubnetGroup{
			DBSubnetGroupName: aws.String("db-subnets"),
			Subnets:           []rdstypes.Subnet{{SubnetIdentifier: aws.String("subnet-1")}},
		},
		DBParameterGroups:      []rdstypes.DBParameterGroupStatus{{DBParameterGroupName: aws.String("pg-1")}},
		OptionGroupMemberships: []rdstypes.OptionGroupMembership{{OptionGroupName: aws.String("og-1")}},
		MasterUserSecret:       &rdstypes.MasterUserSecret{SecretArn: aws.String("arn:aws:secretsmanager:us-east-1:111:secret:db")},
		MonitoringRoleArn:      aws.String("arn:aws:iam::111:role/rds-monitoring"),
		DBClusterIdentifier:    aws.String("orders-cluster"),
	}
	got := viaTargets(rdsInstanceEdges(db, "us-east-1"))
	want := map[string]string{
		"storage encryption key":   "arn:aws:kms:us-east-1:111:key/k",
		"DB security group":        "sg-1",
		"DB subnet group":          "db-subnets",
		"subnet":                   "subnet-1",
		"parameter group":          "pg-1",
		"option group":             "og-1",
		"master user secret":       "arn:aws:secretsmanager:us-east-1:111:secret:db",
		"enhanced monitoring role": "arn:aws:iam::111:role/rds-monitoring",
		"member of cluster":        "orders-cluster",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
	if len(got) != len(want) {
		t.Errorf("edge count = %d, want %d (%+v)", len(got), len(want), got)
	}
}

func TestRDSClusterEdges_MemberLink(t *testing.T) {
	c := rdstypes.DBCluster{
		DBClusterArn:        aws.String("arn:aws:rds:us-east-1:111:cluster:orders-cluster"),
		DBClusterIdentifier: aws.String("orders-cluster"),
		KmsKeyId:            aws.String("arn:aws:kms:us-east-1:111:key/k"),
		AssociatedRoles:     []rdstypes.DBClusterRole{{RoleArn: aws.String("arn:aws:iam::111:role/s3-import")}},
		DBClusterMembers:    []rdstypes.DBClusterMember{{DBInstanceIdentifier: aws.String("orders")}},
	}
	got := viaTargets(rdsClusterEdges(c, "us-east-1"))
	if got["associated role"] != "arn:aws:iam::111:role/s3-import" {
		t.Errorf("associated role edge = %+v", got)
	}
	if got["cluster member"] != "orders" {
		t.Errorf("cluster member edge = %+v", got)
	}
}

func TestDynamoTableEdges(t *testing.T) {
	d := &ddbtypes.TableDescription{
		TableName:       aws.String("sessions"),
		TableArn:        aws.String("arn:aws:dynamodb:us-east-1:111:table/sessions"),
		SSEDescription:  &ddbtypes.SSEDescription{KMSMasterKeyArn: aws.String("arn:aws:kms:us-east-1:111:key/ddb")},
		LatestStreamArn: aws.String("arn:aws:dynamodb:us-east-1:111:table/sessions/stream/2026"),
	}
	got := viaTargets(dynamoTableEdges(d, "us-east-1"))
	if got["encryption key"] != "arn:aws:kms:us-east-1:111:key/ddb" {
		t.Errorf("kms edge = %+v", got)
	}
	if got["stream"] != "arn:aws:dynamodb:us-east-1:111:table/sessions/stream/2026" {
		t.Errorf("stream edge = %+v", got)
	}
	if dynamoTableEdges(nil, "us-east-1") != nil {
		t.Errorf("nil table should yield no edges")
	}
}

func TestElastiCacheEdges(t *testing.T) {
	cc := ectypes.CacheCluster{
		CacheClusterId:       aws.String("cache-1"),
		SecurityGroups:       []ectypes.SecurityGroupMembership{{SecurityGroupId: aws.String("sg-c")}},
		CacheSubnetGroupName: aws.String("cache-subnets"),
	}
	ccGot := viaTargets(elastiCacheClusterEdges(cc, "us-east-1"))
	if ccGot["security group"] != "sg-c" || ccGot["cache subnet group"] != "cache-subnets" {
		t.Errorf("cache cluster edges = %+v", ccGot)
	}

	rg := ectypes.ReplicationGroup{
		ReplicationGroupId: aws.String("rg-1"),
		KmsKeyId:           aws.String("arn:aws:kms:us-east-1:111:key/ec"),
		MemberClusters:     []string{"cache-1", "cache-2"},
	}
	rgEdges := elastiCacheReplicationGroupEdges(rg, "us-east-1")
	rgGot := viaTargets(rgEdges)
	if rgGot["encryption key"] != "arn:aws:kms:us-east-1:111:key/ec" {
		t.Errorf("rg kms edge = %+v", rgGot)
	}
	var members int
	for _, e := range rgEdges {
		if e.From.Via == "member cache cluster" {
			members++
		}
	}
	if members != 2 {
		t.Errorf("want 2 member edges, got %d", members)
	}
}

func TestRedshiftClusterEdges(t *testing.T) {
	c := rstypes.Cluster{
		ClusterIdentifier:      aws.String("analytics"),
		KmsKeyId:               aws.String("arn:aws:kms:us-east-1:111:key/rs"),
		IamRoles:               []rstypes.ClusterIamRole{{IamRoleArn: aws.String("arn:aws:iam::111:role/redshift-copy")}},
		VpcSecurityGroups:      []rstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: aws.String("sg-r")}},
		ClusterSubnetGroupName: aws.String("rs-subnets"),
	}
	got := viaTargets(redshiftClusterEdges(c, "us-east-1"))
	want := map[string]string{
		"cluster IAM role":     "arn:aws:iam::111:role/redshift-copy",
		"encryption key":       "arn:aws:kms:us-east-1:111:key/rs",
		"security group":       "sg-r",
		"cluster subnet group": "rs-subnets",
	}
	for via, tgt := range want {
		if got[via] != tgt {
			t.Errorf("via %q = %q, want %q", via, got[via], tgt)
		}
	}
}

func TestCheckedTypes_DatabaseRegistered(t *testing.T) {
	role := CheckedTypes(KindIAMRole)
	kms := CheckedTypes(KindKMSKey)
	sg := CheckedTypes(KindSecurityGroup)
	if !contains(role, "Redshift cluster IAM roles") || !contains(role, "RDS enhanced-monitoring roles") {
		t.Errorf("IAM CheckedTypes missing database roles: %v", role)
	}
	if !contains(kms, "DynamoDB table encryption") || !contains(kms, "Redshift cluster encryption") {
		t.Errorf("KMS CheckedTypes missing database encryption: %v", kms)
	}
	if !contains(sg, "RDS DB security groups") || !contains(sg, "Redshift cluster security groups") {
		t.Errorf("SG CheckedTypes missing database SGs: %v", sg)
	}
}
