`aws_explorer s3` is a dedicated S3 browser: bucket listing, object
navigation, metadata and version viewing, preview, download, presigned URLs,
and optional guarded delete. It can also point at S3-compatible endpoints
(LocalStack, MinIO) with `--endpoint-url`.

```bash
# Browse all buckets
aws_explorer s3

# Jump straight into a bucket and prefix
aws_explorer s3 --bucket my-bucket --prefix logs/2026/

# Enable deletion (guarded by a typed confirmation)
aws_explorer s3 --bucket my-bucket --allow-delete

# Point at LocalStack or MinIO
aws_explorer s3 --endpoint-url http://localhost:4566
```

See the [`s3` command reference](s3.md) for every flag.

## Bucket list shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate buckets |
| `Enter` | Open the selected bucket |
| `d` | Bucket details |
| `/` | Search buckets |
| `o` | Open the bucket in the AWS console |
| `r` | Refresh |
| `S` | Theme / settings |
| `?` | Help · `q` Quit |

## Object list shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate objects and prefixes |
| `Enter` | Open a prefix (folder) or object |
| `p` | Preview the selected object |
| `/` | Go to a prefix |
| `D` | Download the selected object (to `app.downloaddir`) |
| `L` | Load more (next page of a large listing) |
| `y` | Copy the object's S3 URI |
| `o` | Open the selection in the AWS console |
| `g` | Generate a presigned URL |
| `s` | Sort |
| `C` | Export the listing to CSV |
| `f` | Toggle flat view (recurse prefixes) |
| `x` | Delete the object (only with `--allow-delete`; type `delete` to confirm) |
| `r` | Refresh · `Esc` Back · `?` Help |

Press `o` anywhere to open the current selection (bucket, bucket+prefix, or
object) in the AWS console — the URL is copied, and a browser opens when the
session is local.
