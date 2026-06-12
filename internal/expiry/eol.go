package expiry

import "time"

// Static end-of-life tables. These are the one piece of data in the tool
// that can go stale: they reflect AWS's published schedules as of the
// binary's release (lastReviewed: 2026-01) and are reviewed each release.
// A runtime/version missing from a table simply produces no item — the
// report under-warns rather than mis-warns.

func d(y int, m time.Month, day int) time.Time {
	return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
}

// lambdaRuntimeDeprecation maps a Lambda runtime identifier to its published
// deprecation date (when function updates start being blocked).
// Source: https://docs.aws.amazon.com/lambda/latest/dg/lambda-runtimes.html
var lambdaRuntimeDeprecation = map[string]time.Time{
	// Python
	"python2.7": d(2021, time.July, 15),
	"python3.6": d(2022, time.July, 18),
	"python3.7": d(2023, time.December, 4),
	"python3.8": d(2024, time.October, 14),
	"python3.9": d(2025, time.December, 15),
	// Node.js
	"nodejs10.x": d(2021, time.July, 30),
	"nodejs12.x": d(2023, time.March, 31),
	"nodejs14.x": d(2023, time.December, 4),
	"nodejs16.x": d(2024, time.June, 12),
	"nodejs18.x": d(2025, time.September, 1),
	// Ruby
	"ruby2.5": d(2021, time.July, 30),
	"ruby2.7": d(2023, time.December, 7),
	"ruby3.2": d(2026, time.March, 31),
	// Go / custom (Amazon Linux 1)
	"go1.x":    d(2024, time.January, 8),
	"provided": d(2024, time.January, 8),
	// Java
	"java8": d(2024, time.January, 8),
	// .NET
	"dotnetcore2.1": d(2022, time.January, 5),
	"dotnetcore3.1": d(2023, time.April, 3),
	"dotnet6":       d(2024, time.December, 20),
}

// eksEndOfStandardSupport maps a Kubernetes minor version to the date EKS
// standard support ends (extended support continues at extra cost).
// Source: https://docs.aws.amazon.com/eks/latest/userguide/kubernetes-versions.html
var eksEndOfStandardSupport = map[string]time.Time{
	"1.23": d(2023, time.October, 11),
	"1.24": d(2024, time.January, 31),
	"1.25": d(2024, time.May, 1),
	"1.26": d(2024, time.June, 11),
	"1.27": d(2024, time.July, 24),
	"1.28": d(2024, time.November, 26),
	"1.29": d(2025, time.March, 23),
	"1.30": d(2025, time.July, 23),
	"1.31": d(2025, time.November, 26),
	"1.32": d(2026, time.March, 23),
	"1.33": d(2026, time.July, 29),
}

// rdsCAExpiry maps retired RDS certificate-authority identifiers to their
// expiry date. Instances still pinned to one of these fail TLS verification.
// Source: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.SSL.html
var rdsCAExpiry = map[string]time.Time{
	"rds-ca-2015": d(2020, time.March, 5),
	"rds-ca-2019": d(2024, time.August, 22),
}
