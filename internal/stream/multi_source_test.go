package stream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
)

func init() {
	// NewHttpClient 读 conf.Conf.TlsInsecureSkipVerify,测试需初始化一份默认配置避免空指针。
	conf.Conf = conf.DefaultConfig("data")
}

// parseRangeHeader 解析 "bytes=start-end" 含 end 所指偏移(开区间上界+1)。
func parseRangeHeader(hdr string, total int) (int64, int64, error) {
	if hdr == "" {
		return 0, int64(total), nil
	}
	var start, end int64
	if _, err := fmt.Sscanf(hdr, "bytes=%d-%d", &start, &end); err != nil {
		return 0, 0, err
	}
	return start, end + 1, nil
}

func TestMultiSourceRangeReader_RoundRobinAcrossSources(t *testing.T) {
	// 构造 4 个 PartSize 分片大小的文件,起 2 个 server,各"只"正确响应奇偶分片:
	// server0 负责片 0、2;server1 负责片 1、3。验证拼回完整文件、奇偶片确实分到两 host。
	const partSize = 1 << 8 // 256
	const numParts = 4
	payload := make([]byte, partSize*numParts)
	for i := range payload {
		payload[i] = byte(i)
	}

	// 每个 server 内部按"该片段不被任何片跨越"硬性只接自己负责的片。
	mkServer := func(responsibleParts []int) (*httptest.Server, *int32) {
		allow := make(map[int]bool)
		for _, p := range responsibleParts {
			allow[p] = true
		}
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&hits, 1)
			start, end, err := parseRangeHeader(r.Header.Get("Range"), len(payload))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			whichPart := int(start / int64(partSize))
			if !allow[whichPart] {
				http.Error(w, fmt.Sprintf("not responsible for part %d", whichPart), http.StatusRequestedRangeNotSatisfiable)
				return
			}
			if end > int64(len(payload)) {
				end = int64(len(payload))
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(payload)))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:end])
		}))
		return srv, &hits
	}
	s0, h0 := mkServer([]int{0, 2})
	s1, h1 := mkServer([]int{1, 3})
	defer s0.Close()
	defer s1.Close()

	link := &model.Link{
		PartSize:    partSize,
		Concurrency: 2,
		MultiSource: []model.LinkSource{
			{URL: s0.URL},
			{URL: s1.URL},
		},
	}
	rr := newMultiSourceRangeReader(int64(len(payload)), link)

	// 整段读回。
	rc, err := rr.RangeRead(context.Background(), http_range.Range{Start: 0, Length: int64(len(payload))})
	if err != nil {
		t.Fatalf("RangeRead: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("size mismatch: want %d got %d", len(payload), len(got))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d mismatch: want %d got %d", i, payload[i], got[i])
		}
	}

	// 奇偶片应分别落到两 server:server0 至少处理片0(4片首片小);
	// server1 至少处理片1。
	if atomic.LoadInt32(h0) == 0 {
		t.Fatalf("server0 never hit; round-robin not working")
	}
	if atomic.LoadInt32(h1) == 0 {
		t.Fatalf("server1 never hit; round-robin not working")
	}
	t.Logf("hits: server0=%d server1=%d", atomic.LoadInt32(h0), atomic.LoadInt32(h1))
}

func TestMultiSourceRangeReader_SourceFailover(t *testing.T) {
	// server0 对所有片都报错;server1 全部正确。验证任意片轮换到 server1 成功。
	const partSize = 1 << 8
	const numParts = 3
	payload := make([]byte, partSize*numParts)
	for i := range payload {
		payload[i] = byte(0xAB)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always fail", http.StatusServiceUnavailable)
	}))
	defer bad.Close()

	var goodHits int32
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&goodHits, 1)
		start, end, err := parseRangeHeader(r.Header.Get("Range"), len(payload))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if end > int64(len(payload)) {
			end = int64(len(payload))
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(payload)))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start:end])
	}))
	defer good.Close()

	link := &model.Link{
		PartSize:    partSize,
		Concurrency: 2,
		MultiSource: []model.LinkSource{
			{URL: bad.URL},
			{URL: good.URL},
		},
	}
	rr := newMultiSourceRangeReader(int64(len(payload)), link)
	rc, err := rr.RangeRead(context.Background(), http_range.Range{Start: 0, Length: int64(len(payload))})
	if err != nil {
		t.Fatalf("RangeRead: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("size mismatch: want %d got %d", len(payload), len(got))
	}
	if atomic.LoadInt32(&goodHits) == 0 {
		t.Fatalf("good server never hit; failover broken")
	}
	t.Logf("good server hits=%d", atomic.LoadInt32(&goodHits))
}

// TestMultiSourceRangeReader_ReadAheadWindow 验证读窗口闸门:上游(本地 httptest,瞬时)
// 远快于客户端(每次读睡 3ms)时,任一时刻"已下发字节终点 - 客户端已消费"不得超过
// 窗口+一片的余量——不设窗口时整个文件会被抢跑进内存(线上 88VIP 1.2GB/s 吃满 RAM+swap 的根因)。
func TestMultiSourceRangeReader_ReadAheadWindow(t *testing.T) {
	const partSize = 128 * 1024
	const numParts = 24
	const total = partSize * numParts
	const window = 4 * partSize // 512KB
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte(i)
	}

	oldLimit := conf.ReadAheadLimit
	conf.ReadAheadLimit = window
	t.Cleanup(func() { conf.ReadAheadLimit = oldLimit })

	// servedEnd 记录两 server 已下发 Range 的最大终点(绝对偏移)。
	var servedEnd atomic.Int64
	mkServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start, end, err := parseRangeHeader(r.Header.Get("Range"), total)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if end > int64(total) {
				end = int64(total)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, total))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:end])
			for {
				old := servedEnd.Load()
				if end <= old || servedEnd.CompareAndSwap(old, end) {
					break
				}
			}
		}))
	}
	s0, s1 := mkServer(), mkServer()
	defer s0.Close()
	defer s1.Close()

	link := &model.Link{
		PartSize:    partSize,
		Concurrency: 2,
		MultiSource: []model.LinkSource{
			{URL: s0.URL},
			{URL: s1.URL},
		},
	}
	rr := newMultiSourceRangeReader(total, link)
	rc, err := rr.RangeRead(context.Background(), http_range.Range{Start: 0, Length: total})
	if err != nil {
		t.Fatalf("RangeRead: %v", err)
	}
	defer rc.Close()

	got := make([]byte, 0, total)
	buf := make([]byte, 64*1024)
	consumed := 0
	maxAhead := int64(0)
	for {
		n, err := rc.Read(buf)
		consumed += n
		got = append(got, buf[:n]...)
		if ahead := servedEnd.Load() - int64(consumed); ahead > maxAhead {
			maxAhead = ahead
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		time.Sleep(3 * time.Millisecond)
	}

	if len(got) != total {
		t.Fatalf("size mismatch: want %d got %d", total, len(got))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d mismatch", i)
		}
	}
	// 闸门界:已下发终点 <= 消费位置 + 窗口 + 一片余量(在途分片起点刚过闸门时)。
	if limit := int64(window + partSize); maxAhead > limit {
		t.Fatalf("read-ahead not bounded: max ahead %d > window+part %d", maxAhead, limit)
	}
	// 首次 Read 必然等到分片 0 下发完成(ready),故 ahead 必然被观测到 >0,闸门确实参与。
	if maxAhead <= 0 {
		t.Fatalf("window never engaged; test setup broken")
	}
	t.Logf("total=%d maxAhead=%d (window=%d)", total, maxAhead, window)
}

// TestMultiSourceRangeReader_ReadAheadUnlimited 窗口置 0(不限)时保持旧的满速预跑行为。
func TestMultiSourceRangeReader_ReadAheadUnlimited(t *testing.T) {
	const partSize = 128 * 1024
	const numParts = 24
	const total = partSize * numParts
	payload := make([]byte, total)

	oldLimit := conf.ReadAheadLimit
	conf.ReadAheadLimit = 0
	t.Cleanup(func() { conf.ReadAheadLimit = oldLimit })

	var servedEnd atomic.Int64
	mkServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start, end, err := parseRangeHeader(r.Header.Get("Range"), total)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if end > int64(total) {
				end = int64(total)
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, total))
			w.Header().Set("Content-Length", fmt.Sprintf("%d", end-start))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start:end])
			for {
				old := servedEnd.Load()
				if end <= old || servedEnd.CompareAndSwap(old, end) {
					break
				}
			}
		}))
	}
	s0, s1 := mkServer(), mkServer()
	defer s0.Close()
	defer s1.Close()

	link := &model.Link{
		PartSize:    partSize,
		Concurrency: 2,
		MultiSource: []model.LinkSource{
			{URL: s0.URL},
			{URL: s1.URL},
		},
	}
	rr := newMultiSourceRangeReader(total, link)
	rc, err := rr.RangeRead(context.Background(), http_range.Range{Start: 0, Length: total})
	if err != nil {
		t.Fatalf("RangeRead: %v", err)
	}
	defer rc.Close()

	// 慢消费,等上游把整个文件都下发完;此时客户端消费应远未过半。
	buf := make([]byte, 64*1024)
	n, err := rc.Read(buf)
	if err != nil || n == 0 {
		t.Fatalf("first Read: n=%d err=%v", n, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for servedEnd.Load() < int64(total) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if servedEnd.Load() < int64(total) {
		t.Fatalf("unlimited mode did not fetch ahead: served=%d/%d", servedEnd.Load(), total)
	}
	if ahead := servedEnd.Load() - int64(n); ahead < int64(total)/2 {
		t.Fatalf("expected eager prefetch, ahead=%d", ahead)
	}
	time.Sleep(20 * time.Millisecond)
	// 排空读完,验证 Close 不挂死。
	for {
		_, err := rc.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("drain Read: %v", err)
		}
	}
}
