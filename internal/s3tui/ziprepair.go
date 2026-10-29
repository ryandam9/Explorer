package s3tui

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"log/slog"
)

// Reading a member out of a zip whose central directory lies about where its
// entries are.
//
// A zip is read from the index at its end: the end-of-central-directory
// record says how big the central directory is and where it starts, and each
// directory entry says where its member's local header is. Go's reader treats
// a mismatch between the recorded directory offset and the directory's real
// position as a prefix on the archive (a self-extracting stub, say) and
// subtracts the difference from every offset. When the recorded offset is too
// *large* the correction goes negative, and the entry offsets go with it:
//
//	cannot read docs/a.pdf: bytes.Reader.ReadAt: negative offset
//
// The directory itself is still found, because the same correction cancels out
// there — which is why such an archive lists perfectly and then fails to open
// anything in it. Real archives arrive this way: a prefix stripped off, a
// spanned archive reassembled, or a writer that recorded the offset against
// something other than the file start.
//
// The member is still present and intact; only the arithmetic pointing at it
// is wrong. So rather than refuse, find the member the way the arithmetic was
// meant to: scan the archive for the local file header that carries its name.
// Scanning is the one approach that does not trust any offset, and matching on
// the name makes a false hit (the signature does occur inside compressed data)
// effectively impossible — a wrong hit would fail to decompress and be
// reported, not shown as content.
//
// This runs only after the ordinary path has already failed, so a healthy
// archive never reaches it.

// Local file header layout (APPNOTE.TXT 4.3.7), little-endian throughout.
// The sizes at 18 and 22 are zero when a data descriptor follows the body, so
// the central directory's sizes — which the caller already holds — are used
// instead of these.
const (
	zipLocalHeaderLen   = 30
	zipLocalNameLenAt   = 26
	zipLocalExtraLenAt  = 28
	zipMinCompressedCap = 1 << 30 // sanity bound on a single member's body
)

var zipLocalSig = []byte("PK\x03\x04")

// zipMemberByScan reads one member located by its local file header rather
// than by the central directory's offset.
func zipMemberByScan(data []byte, f *zip.File, maxOut int64) (out []byte, truncated bool, err error) {
	body, err := zipMemberBody(data, f)
	if err != nil {
		return nil, false, err
	}
	switch f.Method {
	case zip.Store:
		return readCapped(bytes.NewReader(body), maxOut)
	case zip.Deflate:
		fr := flate.NewReader(bytes.NewReader(body))
		defer fr.Close()
		return readCapped(fr, maxOut)
	default:
		// The same two methods archive/zip itself registers by default;
		// anything else (encrypted, bzip2, zstd) is named rather than
		// shown as an empty file.
		return nil, false, fmt.Errorf("compression method %d is not supported", f.Method)
	}
}

// zipMemberBody returns the member's still-compressed bytes, found by walking
// the archive's local file headers until one names it.
func zipMemberBody(data []byte, f *zip.File) ([]byte, error) {
	name := []byte(f.Name)
	for at := 0; at < len(data); {
		i := bytes.Index(data[at:], zipLocalSig)
		if i < 0 {
			break
		}
		h := at + i
		at = h + len(zipLocalSig) // always advance, even on a false hit

		if h+zipLocalHeaderLen > len(data) {
			break
		}
		nameLen := int(binary.LittleEndian.Uint16(data[h+zipLocalNameLenAt:]))
		extraLen := int(binary.LittleEndian.Uint16(data[h+zipLocalExtraLenAt:]))
		nameAt := h + zipLocalHeaderLen
		if nameLen != len(name) || nameAt+nameLen > len(data) {
			continue
		}
		if !bytes.Equal(data[nameAt:nameAt+nameLen], name) {
			continue
		}

		bodyAt := nameAt + nameLen + extraLen
		if bodyAt > len(data) {
			return nil, fmt.Errorf("%s: local header runs past the end of the archive", f.Name)
		}
		// The body is bounded by the central directory's compressed size.
		// A size that cannot fit in what is left means the archive is cut
		// short — say so rather than decompressing a partial member and
		// presenting it as the whole thing.
		avail := uint64(len(data) - bodyAt)
		if f.CompressedSize64 > avail {
			return nil, fmt.Errorf("%s: needs %d bytes but only %d remain — the archive is incomplete",
				f.Name, f.CompressedSize64, avail)
		}
		if f.CompressedSize64 > zipMinCompressedCap {
			return nil, fmt.Errorf("%s: entry claims %d compressed bytes", f.Name, f.CompressedSize64)
		}
		return data[bodyAt : bodyAt+int(f.CompressedSize64)], nil
	}
	return nil, fmt.Errorf("%s: no local header for this member anywhere in the archive", f.Name)
}

// zipOpenMember reads one member, falling back to the scan when the central
// directory's offset does not lead anywhere readable. The bool reports whether
// the fallback was needed, so the UI can say the archive is malformed instead
// of quietly presenting repaired content as ordinary content (CLAUDE.md §6a).
func zipOpenMember(data []byte, f *zip.File, maxOut int64) (out []byte, truncated, repaired bool, err error) {
	rc, oerr := f.Open()
	if oerr == nil {
		defer rc.Close()
		out, truncated, err = readCapped(rc, maxOut)
		if err == nil {
			return out, truncated, false, nil
		}
		oerr = err
	}

	slog.Warn("zip member unreadable via the central directory; locating it by scan",
		"member", f.Name, "err", oerr)
	out, truncated, serr := zipMemberByScan(data, f, maxOut)
	if serr != nil {
		return nil, false, false, fmt.Errorf("cannot read %s: %w (and it could not be located by scanning: %v)",
			f.Name, oerr, serr)
	}
	return out, truncated, true, nil
}
