package stream

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

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
