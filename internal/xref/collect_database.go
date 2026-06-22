package xref

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsddb "github.com/aws/aws-sdk-go-v2/service/dynamodb"
	ddbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	awsec "github.com/aws/aws-sdk-go-v2/service/elasticache"
	ectypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	awsrds "github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	awsrs "github.com/aws/aws-sdk-go-v2/service/redshift"
	rstypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
)

// Database edge extractors (#340): RDS/Aurora, DynamoDB, ElastiCache, Redshift.
// Per-resource mapping is pure and fixture-tested; the wrappers page and
// delegate.

// --- RDS / Aurora -------------------------------------------------------------

// rdsInstanceEdges maps a DB instance to its KMS key, security groups, subnet
// group + subnets, parameter/option groups, master-user secret, enhanced-
// monitoring role, and (Aurora) the cluster it belongs to.
func rdsInstanceEdges(db rdstypes.DBInstance, region string) []Edge {
	from := Reference{Service: "rds", Type: "db-instance", Region: region,
		ID:   orDefault(aws.ToString(db.DBInstanceArn), aws.ToString(db.DBInstanceIdentifier)),
		Name: aws.ToString(db.DBInstanceIdentifier)}
	var edges []Edge

	if key := aws.ToString(db.KmsKeyId); key != "" {
		edges = append(edges, Edge{From: withVia(from, "storage encryption key"), Target: key})
	}
	for _, sg := range db.VpcSecurityGroups {
		if g := aws.ToString(sg.VpcSecurityGroupId); g != "" {
			edges = append(edges, Edge{From: withVia(from, "DB security group"), Target: g})
		}
	}
	if sn := db.DBSubnetGroup; sn != nil {
		if n := aws.ToString(sn.DBSubnetGroupName); n != "" {
			edges = append(edges, Edge{From: withVia(from, "DB subnet group"), Target: n})
		}
		for _, s := range sn.Subnets {
			if id := aws.ToString(s.SubnetIdentifier); id != "" {
				edges = append(edges, Edge{From: withVia(from, "subnet"), Target: id})
			}
		}
	}
	for _, pg := range db.DBParameterGroups {
		if n := aws.ToString(pg.DBParameterGroupName); n != "" {
			edges = append(edges, Edge{From: withVia(from, "parameter group"), Target: n})
		}
	}
	for _, og := range db.OptionGroupMemberships {
		if n := aws.ToString(og.OptionGroupName); n != "" {
			edges = append(edges, Edge{From: withVia(from, "option group"), Target: n})
		}
	}
	if ms := db.MasterUserSecret; ms != nil {
		if s := aws.ToString(ms.SecretArn); s != "" {
			edges = append(edges, Edge{From: withVia(from, "master user secret"), Target: s})
		}
	}
	if role := aws.ToString(db.MonitoringRoleArn); role != "" {
		edges = append(edges, Edge{From: withVia(from, "enhanced monitoring role"), Target: role})
	}
	if c := aws.ToString(db.DBClusterIdentifier); c != "" {
		edges = append(edges, Edge{From: withVia(from, "member of cluster"), Target: c})
	}
	return edges
}

// rdsClusterEdges maps an Aurora/RDS cluster to its KMS key, security groups,
// subnet group, master-user secret, associated roles, and member instances.
func rdsClusterEdges(c rdstypes.DBCluster, region string) []Edge {
	from := Reference{Service: "rds", Type: "db-cluster", Region: region,
		ID:   orDefault(aws.ToString(c.DBClusterArn), aws.ToString(c.DBClusterIdentifier)),
		Name: aws.ToString(c.DBClusterIdentifier)}
	var edges []Edge

	if key := aws.ToString(c.KmsKeyId); key != "" {
		edges = append(edges, Edge{From: withVia(from, "storage encryption key"), Target: key})
	}
	for _, sg := range c.VpcSecurityGroups {
		if g := aws.ToString(sg.VpcSecurityGroupId); g != "" {
			edges = append(edges, Edge{From: withVia(from, "DB security group"), Target: g})
		}
	}
	if n := aws.ToString(c.DBSubnetGroup); n != "" {
		edges = append(edges, Edge{From: withVia(from, "DB subnet group"), Target: n})
	}
	if ms := c.MasterUserSecret; ms != nil {
		if s := aws.ToString(ms.SecretArn); s != "" {
			edges = append(edges, Edge{From: withVia(from, "master user secret"), Target: s})
		}
	}
	for _, r := range c.AssociatedRoles {
		if role := aws.ToString(r.RoleArn); role != "" {
			edges = append(edges, Edge{From: withVia(from, "associated role"), Target: role})
		}
	}
	for _, m := range c.DBClusterMembers {
		if id := aws.ToString(m.DBInstanceIdentifier); id != "" {
			edges = append(edges, Edge{From: withVia(from, "cluster member"), Target: id})
		}
	}
	return edges
}

func rdsEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsrds.NewFromConfig(cfg)
	var edges []Edge

	ip := awsrds.NewDescribeDBInstancesPaginator(client, &awsrds.DescribeDBInstancesInput{})
	for ip.HasMorePages() {
		page, err := ip.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, db := range page.DBInstances {
			edges = append(edges, rdsInstanceEdges(db, region)...)
		}
	}

	// Aurora/RDS clusters are a separate API (the #121 "Aurora invisible" gap).
	cp := awsrds.NewDescribeDBClustersPaginator(client, &awsrds.DescribeDBClustersInput{})
	for cp.HasMorePages() {
		page, err := cp.NextPage(ctx)
		if err != nil {
			rec.record("rds", err)
			break
		}
		for _, c := range page.DBClusters {
			edges = append(edges, rdsClusterEdges(c, region)...)
		}
	}
	return edges
}

// --- DynamoDB -----------------------------------------------------------------

// dynamoTableEdges maps a table to its encryption key and stream (the stream's
// Lambda consumers are linked separately via event-source mappings).
func dynamoTableEdges(d *ddbtypes.TableDescription, region string) []Edge {
	if d == nil {
		return nil
	}
	from := Reference{Service: "dynamodb", Type: "table", Region: region,
		ID:   orDefault(aws.ToString(d.TableArn), aws.ToString(d.TableName)),
		Name: aws.ToString(d.TableName)}
	var edges []Edge
	if d.SSEDescription != nil {
		if key := aws.ToString(d.SSEDescription.KMSMasterKeyArn); key != "" {
			edges = append(edges, Edge{From: withVia(from, "encryption key"), Target: key})
		}
	}
	if s := aws.ToString(d.LatestStreamArn); s != "" {
		edges = append(edges, Edge{From: withVia(from, "stream"), Target: s})
	}
	return edges
}

func dynamodbEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsddb.NewFromConfig(cfg)
	var edges []Edge
	p := awsddb.NewListTablesPaginator(client, &awsddb.ListTablesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("dynamodb", err)
			break
		}
		for _, name := range page.TableNames {
			out, err := client.DescribeTable(ctx, &awsddb.DescribeTableInput{TableName: aws.String(name)})
			if err != nil {
				rec.record("dynamodb", err)
				continue
			}
			edges = append(edges, dynamoTableEdges(out.Table, region)...)
		}
	}
	return edges
}

// --- ElastiCache --------------------------------------------------------------

// elastiCacheClusterEdges maps a cache cluster to its security groups and
// subnet group.
func elastiCacheClusterEdges(cc ectypes.CacheCluster, region string) []Edge {
	from := Reference{Service: "elasticache", Type: "cache-cluster", Region: region,
		ID: aws.ToString(cc.CacheClusterId), Name: aws.ToString(cc.CacheClusterId)}
	var edges []Edge
	for _, sg := range cc.SecurityGroups {
		if g := aws.ToString(sg.SecurityGroupId); g != "" {
			edges = append(edges, Edge{From: withVia(from, "security group"), Target: g})
		}
	}
	if n := aws.ToString(cc.CacheSubnetGroupName); n != "" {
		edges = append(edges, Edge{From: withVia(from, "cache subnet group"), Target: n})
	}
	return edges
}

// elastiCacheReplicationGroupEdges maps a replication group to its KMS key and
// member cache clusters.
func elastiCacheReplicationGroupEdges(rg ectypes.ReplicationGroup, region string) []Edge {
	from := Reference{Service: "elasticache", Type: "replication-group", Region: region,
		ID: aws.ToString(rg.ReplicationGroupId), Name: aws.ToString(rg.ReplicationGroupId)}
	var edges []Edge
	if key := aws.ToString(rg.KmsKeyId); key != "" {
		edges = append(edges, Edge{From: withVia(from, "encryption key"), Target: key})
	}
	for _, m := range rg.MemberClusters {
		if m != "" {
			edges = append(edges, Edge{From: withVia(from, "member cache cluster"), Target: m})
		}
	}
	return edges
}

func elastiCacheEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsec.NewFromConfig(cfg)
	var edges []Edge

	cp := awsec.NewDescribeCacheClustersPaginator(client, &awsec.DescribeCacheClustersInput{})
	for cp.HasMorePages() {
		page, err := cp.NextPage(ctx)
		if err != nil {
			rec.record("elasticache", err)
			break
		}
		for _, cc := range page.CacheClusters {
			edges = append(edges, elastiCacheClusterEdges(cc, region)...)
		}
	}

	rp := awsec.NewDescribeReplicationGroupsPaginator(client, &awsec.DescribeReplicationGroupsInput{})
	for rp.HasMorePages() {
		page, err := rp.NextPage(ctx)
		if err != nil {
			rec.record("elasticache", err)
			break
		}
		for _, rg := range page.ReplicationGroups {
			edges = append(edges, elastiCacheReplicationGroupEdges(rg, region)...)
		}
	}
	return edges
}

// --- Redshift -----------------------------------------------------------------

// redshiftClusterEdges maps a cluster to its IAM roles, KMS key, security
// groups, and subnet group.
func redshiftClusterEdges(c rstypes.Cluster, region string) []Edge {
	from := Reference{Service: "redshift", Type: "cluster", Region: region,
		ID: aws.ToString(c.ClusterIdentifier), Name: aws.ToString(c.ClusterIdentifier)}
	var edges []Edge
	for _, r := range c.IamRoles {
		if role := aws.ToString(r.IamRoleArn); role != "" {
			edges = append(edges, Edge{From: withVia(from, "cluster IAM role"), Target: role})
		}
	}
	if key := aws.ToString(c.KmsKeyId); key != "" {
		edges = append(edges, Edge{From: withVia(from, "encryption key"), Target: key})
	}
	for _, sg := range c.VpcSecurityGroups {
		if g := aws.ToString(sg.VpcSecurityGroupId); g != "" {
			edges = append(edges, Edge{From: withVia(from, "security group"), Target: g})
		}
	}
	if n := aws.ToString(c.ClusterSubnetGroupName); n != "" {
		edges = append(edges, Edge{From: withVia(from, "cluster subnet group"), Target: n})
	}
	return edges
}

func redshiftEdges(ctx context.Context, cfg aws.Config, region string, rec *recorder) []Edge {
	client := awsrs.NewFromConfig(cfg)
	var edges []Edge
	p := awsrs.NewDescribeClustersPaginator(client, &awsrs.DescribeClustersInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			rec.record("redshift", err)
			break
		}
		for _, c := range page.Clusters {
			edges = append(edges, redshiftClusterEdges(c, region)...)
		}
	}
	return edges
}
