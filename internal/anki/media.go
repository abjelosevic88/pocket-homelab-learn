package anki

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/klauspost/compress/zstd"
)

// zstdMagic starts every zstd frame.
var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

// maxCollection caps a decompressed collection (a zip bomb guard).
const maxCollection = 2 << 30

func isZstd(b []byte) bool { return bytes.HasPrefix(b, zstdMagic) }

func newZstdReader(r io.Reader) (*zstd.Decoder, error) {
	return zstd.NewReader(r, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxCollection))
}

// parseMediaMap reads the package's "media" entry: zip entry name → file
// name. Legacy packages use JSON {"0":"cat.jpg"}; newer ones a
// zstd-compressed protobuf MediaEntries list.
func parseMediaMap(raw []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(bytes.TrimSpace(raw)) == 0 {
		return out, nil
	}
	if isZstd(raw) {
		d, err := newZstdReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		defer d.Close()
		dec, err := io.ReadAll(io.LimitReader(d, 256<<20))
		if err != nil {
			return nil, fmt.Errorf("media list: %w", err)
		}
		return parseMediaEntries(dec)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		// Some newer packages store the protobuf uncompressed.
		if pm, perr := parseMediaEntries(raw); perr == nil {
			return pm, nil
		}
		return nil, fmt.Errorf("media list: %w", err)
	}
	return m, nil
}

// parseMediaEntries decodes MediaEntries { repeated MediaEntry entries = 1; }
// MediaEntry { string name = 1; uint32 size = 2; bytes sha1 = 3;
// optional uint32 legacy_zip_filename = 255; }. Entry i lives in the zip as
// str(i) unless legacy_zip_filename is set.
func parseMediaEntries(b []byte) (map[string]string, error) {
	out := map[string]string{}
	i := 0
	err := protoFields(b, func(num int, wire int, v uint64, data []byte) error {
		if num != 1 || wire != 2 {
			return nil
		}
		name := ""
		zipName := strconv.Itoa(i)
		i++
		if err := protoFields(data, func(n, w int, v uint64, d []byte) error {
			switch {
			case n == 1 && w == 2:
				name = string(d)
			case n == 255 && w == 0:
				zipName = strconv.FormatUint(v, 10)
			}
			return nil
		}); err != nil {
			return err
		}
		if name != "" {
			out[zipName] = name
		}
		return nil
	})
	return out, err
}

var errProto = errors.New("malformed protobuf")

func varint(b []byte) (uint64, int) {
	var v uint64
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i] < 0x80 {
			return v, i + 1
		}
	}
	return 0, 0
}

// protoFields walks the top-level fields of a protobuf message: varints
// (wire 0) arrive in v, length-delimited fields (wire 2) in data; fixed
// 32/64-bit fields are skipped.
func protoFields(b []byte, fn func(num, wire int, v uint64, data []byte) error) error {
	for len(b) > 0 {
		key, n := varint(b)
		if n == 0 {
			return errProto
		}
		b = b[n:]
		num, wire := int(key>>3), int(key&7)
		switch wire {
		case 0:
			v, n := varint(b)
			if n == 0 {
				return errProto
			}
			b = b[n:]
			if err := fn(num, wire, v, nil); err != nil {
				return err
			}
		case 1:
			if len(b) < 8 {
				return errProto
			}
			b = b[8:]
		case 2:
			l, n := varint(b)
			if n == 0 || uint64(len(b)-n) < l {
				return errProto
			}
			data := b[n : n+int(l)]
			b = b[n+int(l):]
			if err := fn(num, wire, 0, data); err != nil {
				return err
			}
		case 5:
			if len(b) < 4 {
				return errProto
			}
			b = b[4:]
		default:
			return errProto
		}
	}
	return nil
}

// protoString returns the first string field num of a message ("" if absent).
func protoString(b []byte, num int) string {
	out := ""
	_ = protoFields(b, func(n, w int, v uint64, d []byte) error {
		if n == num && w == 2 && out == "" {
			out = string(d)
		}
		return nil
	})
	return out
}

// protoVarint returns varint field num of a message (0 if absent).
func protoVarint(b []byte, num int) uint64 {
	var out uint64
	_ = protoFields(b, func(n, w int, v uint64, d []byte) error {
		if n == num && w == 0 {
			out = v
		}
		return nil
	})
	return out
}

// sortedNames returns the map's values sorted.
func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// zstdReadCloser decompresses a zip entry and closes both.
type zstdReadCloser struct {
	d   *zstd.Decoder
	src io.Closer
}

func (z *zstdReadCloser) Read(p []byte) (int, error) { return z.d.Read(p) }
func (z *zstdReadCloser) Close() error {
	z.d.Close()
	return z.src.Close()
}
