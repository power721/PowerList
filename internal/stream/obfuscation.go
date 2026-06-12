package stream

import (
	"bytes"
	"context"
	"io"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
)

const (
	ObfuscatedMatroskaSkip        int64 = 8
	ObfuscatedMatroskaContentType       = "video/x-matroska"
	obfuscatedMatroskaProbeLength int64 = 12
)

var (
	pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	ebmlHeader   = []byte{0x1a, 0x45, 0xdf, 0xa3}
)

type ObfuscationResult struct {
	RangeReader model.RangeReaderIF
	Size        int64
	ContentType string
	Matched     bool
}

func DetectObfuscatedMatroska(ctx context.Context, rr model.RangeReaderIF, size int64) ObfuscationResult {
	result := ObfuscationResult{RangeReader: rr, Size: size}
	if rr == nil || size < obfuscatedMatroskaProbeLength {
		return result
	}

	rc, err := rr.RangeRead(ctx, http_range.Range{Start: 0, Length: obfuscatedMatroskaProbeLength})
	if err != nil {
		return result
	}
	defer rc.Close()

	probe, err := io.ReadAll(io.LimitReader(rc, obfuscatedMatroskaProbeLength))
	if err != nil || int64(len(probe)) < obfuscatedMatroskaProbeLength {
		return result
	}
	if !bytes.Equal(probe[:len(pngSignature)], pngSignature) {
		return result
	}
	if !bytes.Equal(probe[ObfuscatedMatroskaSkip:obfuscatedMatroskaProbeLength], ebmlHeader) {
		return result
	}

	return ObfuscationResult{
		RangeReader: &offsetRangeReader{
			RangeReader: rr,
			Offset:      ObfuscatedMatroskaSkip,
		},
		Size:        size - ObfuscatedMatroskaSkip,
		ContentType: ObfuscatedMatroskaContentType,
		Matched:     true,
	}
}

type offsetRangeReader struct {
	RangeReader model.RangeReaderIF
	Offset      int64
}

func (r *offsetRangeReader) RangeRead(ctx context.Context, hr http_range.Range) (io.ReadCloser, error) {
	hr.Start += r.Offset
	return r.RangeReader.RangeRead(ctx, hr)
}
