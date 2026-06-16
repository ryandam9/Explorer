package s3tui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"path"
	"strings"
)

// Size caps for fetching and decompressing compressed objects. Previews are
// bounded so a multi-gigabyte log archive can't exhaust memory; the UI shows a
// "truncated" note when a cap is hit.
const (
	gzCompressedCap    = 4 << 20  // bytes fetched for a plain .gz preview
	gzDecompressedCap  = 4 << 20  // decompressed bytes shown for a plain .gz
	tarCompressedCap   = 32 << 20 // bytes fetched for a .tar(.gz) archive
	tarDecompressedCap = 96 << 20
	memberPreviewCap   = 4 << 20 // decompressed bytes shown for one archive member
)

// looksLikeGzip reports whether key is a plain gzip stream (a single compressed
// file), as opposed to a gzipped tar archive.
func looksLikeGzip(key string) bool {
	lower := strings.ToLower(key)
	if !strings.HasSuffix(lower, ".gz") {
		return false
	}
	return !isTarArchive(lower)
}

// looksLikeTar reports whether key is a tar archive, optionally gzip-compressed.
func looksLikeTar(key string) bool {
	return isTarArchive(strings.ToLower(key))
}

func isTarArchive(lower string) bool {
	return strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz")
}

// innerName returns the logical filename inside a single-file compressor: it
// strips a trailing .gz so the content can be routed by its real extension
// (e.g. access.csv.gz → access.csv → CSV table).
func innerName(key string) string {
	base := path.Base(key)
	if strings.HasSuffix(strings.ToLower(base), ".gz") {
		return base[:len(base)-len(".gz")]
	}
	return base
}

// isGzipCompressed reports whether key (or a tar archive) is gzip-wrapped.
func isGzipCompressed(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasSuffix(lower, ".gz") || strings.HasSuffix(lower, ".tgz")
}

// gunzip decompresses up to maxOut bytes of a gzip stream. A truncated input
// (the preview only fetched the first chunk) is tolerated: the bytes decoded so
// far are returned with truncated=true rather than an error.
func gunzip(data []byte, maxOut int64) (out []byte, truncated bool, err error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, false, err
	}
	defer zr.Close()
	zr.Multistream(true)

	buf, truncated, err := readCapped(zr, maxOut)
	if err != nil && !isTruncationError(err) {
		return buf, truncated, err
	}
	return buf, truncated, nil
}

// readCapped reads up to maxOut bytes from r. It returns truncated=true when r
// still had data at the cap.
func readCapped(r io.Reader, maxOut int64) (data []byte, truncated bool, err error) {
	limited := io.LimitReader(r, maxOut)
	data, err = io.ReadAll(limited)
	if err != nil {
		return data, false, err
	}
	if int64(len(data)) == maxOut {
		// Peek one more byte to see whether more content exists.
		var one [1]byte
		if n, _ := r.Read(one[:]); n > 0 {
			data = append(data, one[0])
			truncated = true
		}
	}
	return data, truncated, nil
}

// isTruncationError reports whether err is the expected end-of-stream condition
// for a deliberately truncated compressed input.
func isTruncationError(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// isTruncationErrorTar treats a short read of a deliberately truncated archive
// as a normal stop, not a failure.
func isTruncationErrorTar(err error) bool {
	return errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF)
}

// archiveMember is one entry in a tar archive listing.
type archiveMember struct {
	Name string
	Size int64
	Dir  bool
}

// tarMembers lists the regular files in a tar archive (raw tar bytes, already
// gunzipped if needed). Directories and special entries are skipped. A
// truncated archive yields the members read before the cut-off.
func tarMembers(data []byte) ([]archiveMember, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var members []archiveMember
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			if isTruncationErrorTar(err) {
				break // keep what we listed before the truncated tail
			}
			return members, err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			members = append(members, archiveMember{Name: hdr.Name, Dir: true})
		case tar.TypeReg, tar.TypeRegA:
			members = append(members, archiveMember{Name: hdr.Name, Size: hdr.Size})
		}
	}
	return members, nil
}

// tarMemberContent extracts one member's content (up to maxOut bytes) from raw
// tar bytes.
func tarMemberContent(data []byte, name string, maxOut int64) (out []byte, truncated bool, err error) {
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, false, errors.New("member not found in archive")
		}
		if err != nil {
			if isTruncationErrorTar(err) {
				return nil, false, errors.New("member not found before the archive was truncated")
			}
			return nil, false, err
		}
		if hdr.Name == name {
			return readCapped(tr, maxOut)
		}
	}
}
