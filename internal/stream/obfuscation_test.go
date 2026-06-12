package stream

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
)

func TestDetectObfuscatedMatroska(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		matched bool
		size    int64
	}{
		{
			name:    "png signature followed by ebml",
			data:    append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x1a, 0x45, 0xdf, 0xa3}, []byte("payload")...),
			matched: true,
			size:    int64(len("payload") + 4),
		},
		{
			name:    "ordinary png is unchanged",
			data:    append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, []byte("not-ebml")...),
			matched: false,
			size:    int64(len([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}) + len("not-ebml")),
		},
		{
			name:    "ordinary matroska is unchanged",
			data:    append([]byte{0x1a, 0x45, 0xdf, 0xa3}, []byte("matroska")...),
			matched: false,
			size:    int64(len([]byte{0x1a, 0x45, 0xdf, 0xa3}) + len("matroska")),
		},
		{
			name:    "short file is unchanged",
			data:    []byte("short"),
			matched: false,
			size:    int64(len("short")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := newBytesRangeReader(tt.data)
			got := DetectObfuscatedMatroska(context.Background(), rr, int64(len(tt.data)))

			if got.Matched != tt.matched {
				t.Fatalf("Matched = %v, want %v", got.Matched, tt.matched)
			}
			if got.Size != tt.size {
				t.Fatalf("Size = %d, want %d", got.Size, tt.size)
			}
			if tt.matched && got.ContentType != ObfuscatedMatroskaContentType {
				t.Fatalf("ContentType = %q, want %q", got.ContentType, ObfuscatedMatroskaContentType)
			}
		})
	}
}

func TestObfuscatedRangeReaderMapsClientRangeToUpstreamOffset(t *testing.T) {
	data := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x1a, 0x45, 0xdf, 0xa3}, []byte("payload")...)
	rr := newBytesRangeReader(data)
	detected := DetectObfuscatedMatroska(context.Background(), rr, int64(len(data)))
	if !detected.Matched {
		t.Fatal("expected obfuscated matroska detection")
	}

	rc, err := detected.RangeReader.RangeRead(context.Background(), http_range.Range{Start: 0, Length: 4})
	if err != nil {
		t.Fatalf("RangeRead returned error: %v", err)
	}
	defer rc.Close()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	want := []byte{0x1a, 0x45, 0xdf, 0xa3}
	if !bytes.Equal(got, want) {
		t.Fatalf("RangeRead bytes = % x, want % x", got, want)
	}
}

type bytesRangeReader struct {
	data []byte
}

func newBytesRangeReader(data []byte) *bytesRangeReader {
	return &bytesRangeReader{data: data}
}

func (r *bytesRangeReader) RangeRead(_ context.Context, hr http_range.Range) (io.ReadCloser, error) {
	if hr.Start > int64(len(r.data)) {
		return io.NopCloser(bytes.NewReader(nil)), nil
	}
	end := int64(len(r.data))
	if hr.Length >= 0 && hr.Start+hr.Length < end {
		end = hr.Start + hr.Length
	}
	return io.NopCloser(bytes.NewReader(r.data[hr.Start:end])), nil
}
