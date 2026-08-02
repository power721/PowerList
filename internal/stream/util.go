package stream

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/net"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
	"github.com/OpenListTeam/OpenList/v4/pkg/pool"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/rclone/rclone/lib/mmap"
	log "github.com/sirupsen/logrus"
)

type RangeReaderFunc func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error)

func (f RangeReaderFunc) RangeRead(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
	return f(ctx, httpRange)
}

func GetRangeReaderFromLink(size int64, link *model.Link) (model.RangeReaderIF, error) {
	if len(link.MultiSource) > 1 && (link.Concurrency > 0 || link.PartSize > 0) {
		return newMultiSourceRangeReader(size, link), nil
	}
	if link.RangeReader != nil {
		if link.Concurrency < 1 && link.PartSize < 1 {
			return link.RangeReader, nil
		}
		down := net.NewDownloader(func(d *net.Downloader) {
			d.Concurrency = link.Concurrency
			d.PartSize = link.PartSize
			d.HttpClient = net.GetRangeReaderHttpRequestFunc(link.RangeReader)
		})
		rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			return down.Download(ctx, &net.HttpRequestParams{
				Range: httpRange,
				Size:  size,
			})
		}
		// RangeReader只能在驱动限速
		return RangeReaderFunc(rangeReader), nil
	}

	if len(link.URL) == 0 {
		return nil, errors.New("invalid link: must have at least one of URL or RangeReader")
	}

	if link.Concurrency > 0 || link.PartSize > 0 {
		down := net.NewDownloader(func(d *net.Downloader) {
			d.Concurrency = link.Concurrency
			d.PartSize = link.PartSize
			d.HttpClient = func(ctx context.Context, params *net.HttpRequestParams) (*http.Response, error) {
				if ServerDownloadLimit == nil {
					return net.DefaultHttpRequestFunc(ctx, params)
				}
				resp, err := net.DefaultHttpRequestFunc(ctx, params)
				if err == nil && resp.Body != nil {
					resp.Body = &RateLimitReader{
						Ctx:     ctx,
						Reader:  resp.Body,
						Limiter: ServerDownloadLimit,
					}
				}
				return resp, err
			}
		})
		rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			requestHeader, _ := ctx.Value(conf.RequestHeaderKey).(http.Header)
			header := net.ProcessHeader(requestHeader, link.Header)
			return down.Download(ctx, &net.HttpRequestParams{
				Range:     httpRange,
				Size:      size,
				URL:       link.URL,
				HeaderRef: header,
			})
		}
		return RangeReaderFunc(rangeReader), nil
	}

	rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
		if httpRange.Length < 0 || httpRange.Start+httpRange.Length > size {
			httpRange.Length = size - httpRange.Start
		}
		requestHeader, _ := ctx.Value(conf.RequestHeaderKey).(http.Header)
		header := net.ProcessHeader(requestHeader, link.Header)
		header = http_range.ApplyRangeToHttpHeader(httpRange, header)

		response, err := net.RequestHttp(ctx, "GET", header, link.URL)
		if err != nil {
			if _, ok := errs.UnwrapOrSelf(err).(net.HttpStatusCodeError); ok {
				return nil, err
			}
			return nil, fmt.Errorf("http request failure, err:%w", err)
		}
		if ServerDownloadLimit != nil {
			response.Body = &RateLimitReader{
				Ctx:     ctx,
				Reader:  response.Body,
				Limiter: ServerDownloadLimit,
			}
		}
		if httpRange.Start == 0 && httpRange.Length == size ||
			response.StatusCode == http.StatusPartialContent ||
			checkContentRange(&response.Header, httpRange.Start) {
			return response.Body, nil
		} else if response.StatusCode == http.StatusOK {
			log.Warnf("remote http server not supporting range request, expect low perfromace!")
			readCloser, err := net.GetRangedHttpReader(response.Body, httpRange.Start, httpRange.Length)
			if err != nil {
				return nil, err
			}
			return readCloser, nil
		}
		return response.Body, nil
	}
	return RangeReaderFunc(rangeReader), nil
}

// newMultiSourceRangeReader 构造一个把分片轮询分发到 link.MultiSource 各源直连的 RangeReader。
// 客户端请求的整段 [start, start+length) 被切成若干 PartSize 的子分片(切片规则对齐
// internal/net.Downloader:首片取 length%PartSize 的小片,其余 PartSize),第 i 个子分片
// 用 sources[i % len] 的 URL+Header 发 Range 请求。总并发为 N*Concurrency(N=源数)。
// 单片源失败时轮换到下一个源重试,所有源都失败才报错。
func newMultiSourceRangeReader(size int64, link *model.Link) model.RangeReaderIF {
	sources := link.MultiSource
	partSize := link.PartSize
	if partSize <= 0 {
		partSize = int(net.DefaultDownloadPartSize)
	}
	if conf.MaxBufferLimit > 0 && partSize > conf.MaxBufferLimit {
		partSize = conf.MaxBufferLimit
	}
	perSourceConcurrency := link.Concurrency
	if perSourceConcurrency <= 0 {
		perSourceConcurrency = net.DefaultDownloadConcurrency
	}
	// 总并发按源数放大,但加上限:单 IP 出口和 DNS 解析承受不了 N*perSource 的全开并发
	// (如 5 源 × 32 = 160 并发会压垮 DNS,出现大量 lookup canceled)。
	totalConcurrency := perSourceConcurrency * len(sources)
	if totalConcurrency > 64 {
		totalConcurrency = 64
	}
	if totalConcurrency < 1 {
		totalConcurrency = 1
	}

	type subChunk struct {
		start  int64
		length int64
	}
	// 对齐 Downloader 切分:首片为 length%PartSize 的余片(太小则取 PartSize/2),其余 PartSize。
	computeSubchunks := func(start, length int64) []subChunk {
		if length < 0 {
			length = size - start
		}
		if length < 0 {
			length = 0
		}
		if start+length > size {
			length = size - start
		}
		var chunks []subChunk
		pos := start
		remaining := length
		nextSize := int64(partSize)
		if first := length % int64(partSize); first > 0 {
			minSize := int64(partSize) / 2
			if first < minSize {
				nextSize = minSize
			} else {
				nextSize = first
			}
		}
		for remaining > 0 {
			sz := nextSize
			if sz > remaining {
				sz = remaining
			}
			chunks = append(chunks, subChunk{start: pos, length: sz})
			pos += sz
			remaining -= sz
			nextSize = int64(partSize)
		}
		return chunks
	}

	rangeReader := func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
		chunks := computeSubchunks(httpRange.Start, httpRange.Length)
		if len(chunks) == 0 {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}

		ctx, cancel := context.WithCancelCause(ctx)
		// 每个 subChunk 一个结果槽:buf 为下载到的字节,err 为失败原因,ready 通知。
		slots := make([]multiSourceSlot, len(chunks))
		for i := range slots {
			slots[i].ready = make(chan struct{})
		}

		// 信号量控制 worker 并发为 totalConcurrency;信号量在 worker 退出时释放。
		sem := make(chan struct{}, totalConcurrency)
		var wg sync.WaitGroup

		launch := func(idx int) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }() // 释放本 worker 槽位
				ch := chunks[idx]
				nSrc := len(sources)
				startSrc := idx % nSrc
				var lastErr error
				for attempt := 0; attempt < nSrc; attempt++ {
					if ctx.Err() != nil {
						lastErr = context.Cause(ctx)
						break
					}
					src := sources[(startSrc+attempt)%nSrc]
					data, err := downloadMultiSourceSubChunk(ctx, src, ch.start, ch.length, size)
					if err == nil {
						slots[idx].buf = data
						close(slots[idx].ready)
						return
					}
					lastErr = err
					// 客户端断开/seek 导致的取消是正常的(播放器频繁 Range 取消),不打日志避免刷屏。
					if !errors.Is(err, context.Canceled) && ctx.Err() == nil {
						log.Debugf("[multi-source] subChunk_%d src_%d failed: %v", idx, (startSrc+attempt)%nSrc, err)
					}
				}
				slots[idx].err = lastErr
				close(slots[idx].ready)
				if lastErr != nil && !errors.Is(lastErr, context.Canceled) {
					cancel(lastErr)
				}
			}()
		}

		// 调度器:满并发地启动所有分片(信号量自动限流),ctx 取消时收尾。
		go func() {
			for i := range chunks {
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}
				launch(i)
			}
		}()

		reader := &multiSourceReader{
			ctx:    ctx,
			slots:  slots,
			cancel: cancel,
		}
		// reader.Close 应等 worker 退出并取消残余请求。
		reader.wg = &wg
		return reader, nil
	}
	return RangeReaderFunc(rangeReader)
}

// multiSourceSlot 是单个 subChunk 的下载结果槽。
type multiSourceSlot struct {
	buf   []byte
	err   error
	ready chan struct{}
}

// multiSourceReader 按 subChunk 顺序阻塞地拼回各槽下载到的字节。
type multiSourceReader struct {
	ctx    context.Context
	slots  []multiSourceSlot
	cur    int // 当前正在读的 subChunk 索引
	off    int // 当前 subChunk 已读偏移
	cancel context.CancelCauseFunc
	wg     *sync.WaitGroup
}

func (r *multiSourceReader) Read(p []byte) (int, error) {
	for {
		if r.cur >= len(r.slots) {
			return 0, io.EOF
		}
		select {
		case <-r.ctx.Done():
			return 0, context.Cause(r.ctx)
		default:
		}
		slot := &r.slots[r.cur]
		select {
		case <-slot.ready:
		case <-r.ctx.Done():
			return 0, context.Cause(r.ctx)
		}
		if slot.err != nil {
			return 0, slot.err
		}
		// 还剩多少可从当前分片读。
		remaining := len(slot.buf) - r.off
		if remaining <= 0 {
			r.cur++
			r.off = 0
			continue
		}
		n := copy(p, slot.buf[r.off:])
		r.off += n
		if r.off >= len(slot.buf) {
			r.cur++
			r.off = 0
		}
		return n, nil
	}
}

// Close 取消所有残余 worker 并等其退出,释放并发槽位。
func (r *multiSourceReader) Close() error {
	if r.cancel != nil {
		r.cancel(context.Canceled)
	}
	if r.wg != nil {
		r.wg.Wait()
	}
	return nil
}

// downloadMultiSourceSubChunk 用单条源直连取 [start, start+length) 字节。
// header 中预置 Range;失败由调用方据此轮换源。带 ServerDownloadLimit 限速。
func downloadMultiSourceSubChunk(ctx context.Context, src model.LinkSource, start, length, totalSize int64) ([]byte, error) {
	if length <= 0 {
		if totalSize > start {
			length = totalSize - start
		} else {
			length = 0
		}
	}
	header := net.ProcessHeader(nil, src.Header)
	httpRange := http_range.Range{Start: start, Length: length}
	header = http_range.ApplyRangeToHttpHeader(httpRange, header)
	resp, err := net.RequestHttp(ctx, "GET", header, src.URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("multi-source chunk status %d", resp.StatusCode)
	}
	var reader io.Reader = resp.Body
	if ServerDownloadLimit != nil {
		reader = &RateLimitReader{Ctx: ctx, Reader: resp.Body, Limiter: ServerDownloadLimit}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != length {
		return data, fmt.Errorf("multi-source chunk size mismatch: want %d got %d", length, len(data))
	}
	return data, nil
}

func GetRangeReaderFromMFile(size int64, file model.File) *model.FileRangeReader {
	return &model.FileRangeReader{
		RangeReaderIF: RangeReaderFunc(func(ctx context.Context, httpRange http_range.Range) (io.ReadCloser, error) {
			length := httpRange.Length
			if length < 0 || httpRange.Start+length > size {
				length = size - httpRange.Start
			}
			return &model.FileCloser{File: io.NewSectionReader(file, httpRange.Start, length)}, nil
		}),
	}
}

// 139 cloud does not properly return 206 http status code, add a hack here
func checkContentRange(header *http.Header, offset int64) bool {
	start, _, err := http_range.ParseContentRange(header.Get("Content-Range"))
	if err != nil {
		log.Warnf("exception trying to parse Content-Range, will ignore,err=%s", err)
	}
	if start == offset {
		return true
	}
	return false
}

type ReaderWithCtx struct {
	io.Reader
	Ctx context.Context
}

func (r *ReaderWithCtx) Read(p []byte) (n int, err error) {
	if utils.IsCanceled(r.Ctx) {
		return 0, r.Ctx.Err()
	}
	return r.Reader.Read(p)
}

func (r *ReaderWithCtx) Close() error {
	if c, ok := r.Reader.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func CacheFullAndHash(stream model.FileStreamer, up *model.UpdateProgress, hashType *utils.HashType, hashParams ...any) (model.File, string, error) {
	h := hashType.NewFunc(hashParams...)
	tmpF, err := stream.CacheFullAndWriter(up, h)
	if err != nil {
		return nil, "", err
	}
	return tmpF, hex.EncodeToString(h.Sum(nil)), nil
}

type StreamSectionReaderIF interface {
	// 线程不安全
	GetSectionReader(off, length int64) (io.ReadSeeker, error)
	FreeSectionReader(sr io.ReadSeeker)
	// 线程不安全
	DiscardSection(off int64, length int64) error
}

func NewStreamSectionReader(file model.FileStreamer, maxBufferSize int, up *model.UpdateProgress) (StreamSectionReaderIF, error) {
	if file.GetFile() != nil {
		return &cachedSectionReader{file.GetFile()}, nil
	}

	maxBufferSize = min(maxBufferSize, int(file.GetSize()))
	if maxBufferSize > conf.MaxBufferLimit {
		f, err := os.CreateTemp(conf.Conf.TempDir, "file-*")
		if err != nil {
			return nil, err
		}

		if f.Truncate(file.GetSize()) != nil {
			// fallback to full cache
			_, _ = f.Close(), os.Remove(f.Name())
			cache, err := file.CacheFullAndWriter(up, nil)
			if err != nil {
				return nil, err
			}
			return &cachedSectionReader{cache}, nil
		}

		ss := &fileSectionReader{file: file, temp: f}
		ss.bufPool = &pool.Pool[*offsetWriterWithBase]{
			New: func() *offsetWriterWithBase {
				base := ss.tempOffset
				ss.tempOffset += int64(maxBufferSize)
				return &offsetWriterWithBase{io.NewOffsetWriter(ss.temp, base), base}
			},
		}
		file.Add(utils.CloseFunc(func() error {
			ss.bufPool.Reset()
			return errors.Join(ss.temp.Close(), os.Remove(ss.temp.Name()))
		}))
		return ss, nil
	}

	ss := &directSectionReader{file: file}
	if conf.MmapThreshold > 0 && maxBufferSize >= conf.MmapThreshold {
		ss.bufPool = &pool.Pool[[]byte]{
			New: func() []byte {
				buf, err := mmap.Alloc(maxBufferSize)
				if err == nil {
					file.Add(utils.CloseFunc(func() error {
						return mmap.Free(buf)
					}))
				} else {
					buf = make([]byte, maxBufferSize)
				}
				return buf
			},
		}
	} else {
		ss.bufPool = &pool.Pool[[]byte]{
			New: func() []byte {
				return make([]byte, maxBufferSize)
			},
		}
	}

	file.Add(utils.CloseFunc(func() error {
		ss.bufPool.Reset()
		return nil
	}))
	return ss, nil
}

type cachedSectionReader struct {
	cache io.ReaderAt
}

func (*cachedSectionReader) DiscardSection(off int64, length int64) error {
	return nil
}
func (s *cachedSectionReader) GetSectionReader(off, length int64) (io.ReadSeeker, error) {
	return io.NewSectionReader(s.cache, off, length), nil
}
func (*cachedSectionReader) FreeSectionReader(sr io.ReadSeeker) {}

type fileSectionReader struct {
	file       model.FileStreamer
	fileOffset int64
	temp       *os.File
	tempOffset int64
	bufPool    *pool.Pool[*offsetWriterWithBase]
}

type offsetWriterWithBase struct {
	*io.OffsetWriter
	base int64
}

// 线程不安全
func (ss *fileSectionReader) DiscardSection(off int64, length int64) error {
	if off != ss.fileOffset {
		return fmt.Errorf("stream not cached: request offset %d != current offset %d", off, ss.fileOffset)
	}
	n, err := utils.CopyWithBufferN(io.Discard, ss.file, length)
	ss.fileOffset += n
	if err != nil {
		return fmt.Errorf("failed to skip data: (expect =%d, actual =%d) %w", length, n, err)
	}
	return nil
}

type fileBufferSectionReader struct {
	io.ReadSeeker
	fileBuf *offsetWriterWithBase
}

// 线程不安全
func (ss *fileSectionReader) GetSectionReader(off, length int64) (io.ReadSeeker, error) {
	if off != ss.fileOffset {
		return nil, fmt.Errorf("stream not cached: request offset %d != current offset %d", off, ss.fileOffset)
	}
	fileBuf := ss.bufPool.Get()
	_, _ = fileBuf.Seek(0, io.SeekStart)
	n, err := utils.CopyWithBufferN(fileBuf, ss.file, length)
	ss.fileOffset += n
	if err != nil {
		return nil, fmt.Errorf("failed to read all data: (expect =%d, actual =%d) %w", length, n, err)
	}
	return &fileBufferSectionReader{io.NewSectionReader(ss.temp, fileBuf.base, length), fileBuf}, nil
}

func (ss *fileSectionReader) FreeSectionReader(rs io.ReadSeeker) {
	if sr, ok := rs.(*fileBufferSectionReader); ok {
		ss.bufPool.Put(sr.fileBuf)
		sr.fileBuf = nil
		sr.ReadSeeker = nil
	}
}

type directSectionReader struct {
	file       model.FileStreamer
	fileOffset int64
	bufPool    *pool.Pool[[]byte]
}

// 线程不安全
func (ss *directSectionReader) DiscardSection(off int64, length int64) error {
	if off != ss.fileOffset {
		return fmt.Errorf("stream not cached: request offset %d != current offset %d", off, ss.fileOffset)
	}
	n, err := utils.CopyWithBufferN(io.Discard, ss.file, length)
	ss.fileOffset += n
	if err != nil {
		return fmt.Errorf("failed to skip data: (expect =%d, actual =%d) %w", length, n, err)
	}
	return nil
}

type bufferSectionReader struct {
	io.ReadSeeker
	buf []byte
}

// 线程不安全
func (ss *directSectionReader) GetSectionReader(off, length int64) (io.ReadSeeker, error) {
	if off != ss.fileOffset {
		return nil, fmt.Errorf("stream not cached: request offset %d != current offset %d", off, ss.fileOffset)
	}
	tempBuf := ss.bufPool.Get()
	buf := tempBuf[:length]
	n, err := io.ReadFull(ss.file, buf)
	ss.fileOffset += int64(n)
	if int64(n) != length {
		return nil, fmt.Errorf("failed to read all data: (expect =%d, actual =%d) %w", length, n, err)
	}
	return &bufferSectionReader{bytes.NewReader(buf), buf}, nil
}
func (ss *directSectionReader) FreeSectionReader(rs io.ReadSeeker) {
	if sr, ok := rs.(*bufferSectionReader); ok {
		ss.bufPool.Put(sr.buf[0:cap(sr.buf)])
		sr.buf = nil
		sr.ReadSeeker = nil
	}
}
