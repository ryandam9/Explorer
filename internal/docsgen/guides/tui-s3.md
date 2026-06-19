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

## Bucket detail shortcuts

Press `d` on a bucket to open its detail view — a tabbed summary (Overview,
Access & Security, Data Protection, Operational, Tags) of the bucket's
configuration.

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Switch between detail tabs |
| `p` | View the full bucket policy as pretty-printed JSON |
| `c` | View the full CORS configuration as pretty-printed JSON |
| `r` | Refresh (re-fetch this bucket's details) |
| `Esc` | Back to the bucket list |

`p` and `c` open a scrollable, full-screen JSON viewer; press `y` there to copy
the document to the clipboard. When the bucket has no policy / CORS configuration
(or access is denied) the viewer says so rather than showing an empty pane.

## Table preview shortcuts

Previewing a delimited (CSV/TSV/…), Parquet, or fixed-width object opens a
full-screen table. A second header line shows each column's number — its
original position in the file — so wide files stay navigable.

| Key | Action |
|-----|--------|
| `↑` / `↓` · `PgUp` / `PgDn` | Scroll rows |
| `←` / `→` | Scroll columns (the first column stays pinned) |
| `Enter` | Show the selected row as a vertical record (Col : value) |
| `c` | Cycle the column filter: all columns → only columns with data → only empty columns |
| `w` | Cycle the row window (first/last N rows) |
| `s` / `S` | Auto-detect / type the delimiter (delimited files only) |
| `h` | Set the header row, or `0` for none (delimited files only) |
| `n` | Choose how many rows to read (Parquet only) |
| `L` | Apply a local fixed-width layout file (`name,start,length` per line) |
| `y` | Copy the visible columns and rows as a Markdown table |
| `t` | Toggle between the table and the raw-text preview |
| `Esc` | Close the preview |

The `c` column filter is handy for very wide files (hundreds of columns) where
many columns are entirely empty: it narrows the view to just the populated
columns (or, conversely, just the empty ones) while preserving each surviving
column's original number. A mode that would show no columns is skipped, so the
table never goes blank.
