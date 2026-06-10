package display

// S3ObjectFields are the fields available for the S3 object table and detail panel.
var S3ObjectFields = []FieldMeta{
	{Key: "name", Title: "Name", Width: 40, DefaultCol: true, DefaultDetail: true},
	{Key: "size", Title: "Size", Width: 10, DefaultCol: true, DefaultDetail: true},
	{Key: "last_modified", Title: "Last Modified", Width: 20, DefaultCol: true, DefaultDetail: true},
	{Key: "storage_class", Title: "Storage Class", Width: 15, DefaultCol: true, DefaultDetail: true},
	{Key: "etag", Title: "ETag", Width: 34, DefaultCol: true, DefaultDetail: true},
	{Key: "content_type", Title: "Content-Type", Width: 30, DefaultCol: false, DefaultDetail: true},
	{Key: "sse", Title: "SSE", Width: 20, DefaultCol: false, DefaultDetail: true},
	{Key: "version_id", Title: "Version ID", Width: 30, DefaultCol: false, DefaultDetail: true},
	{Key: "content_enc", Title: "Content-Enc", Width: 20, DefaultCol: false, DefaultDetail: true},
	{Key: "cache_control", Title: "Cache-Control", Width: 20, DefaultCol: false, DefaultDetail: true},
	{Key: "kms_key", Title: "KMS Key", Width: 40, DefaultCol: false, DefaultDetail: true},
}

// S3BucketFields are the fields available for the S3 bucket table.
var S3BucketFields = []FieldMeta{
	{Key: "name", Title: "Name", Width: 40, DefaultCol: true, DefaultDetail: true},
	{Key: "region", Title: "Region", Width: 20, DefaultCol: true, DefaultDetail: true},
	{Key: "creation_date", Title: "Creation Date", Width: 25, DefaultCol: true, DefaultDetail: true},
}
