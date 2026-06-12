package common

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/pkg/http_range"
)

func TestProxyObfuscatedMatroska(t *testing.T) {
	conf.Conf = &conf.Config{}
	data := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x1a, 0x45, 0xdf, 0xa3}, []byte("payload")...)
	upstream := newRangeServer(t, data, "video/x-matroska")
	defer upstream.Close()

	file := &model.Object{
		Name:     "movie.mkv",
		Size:     int64(len(data)),
		Modified: time.Unix(1, 0),
	}
	link := &model.Link{URL: upstream.URL}
	req := httptest.NewRequest(http.MethodGet, "/p/movie.mkv", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()

	if err := Proxy(rec, req, link, file); err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}

	resp := rec.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	wantBody := []byte{0x1a, 0x45, 0xdf, 0xa3}
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("body = % x, want % x", body, wantBody)
	}
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusPartialContent)
	}
	if got, want := resp.Header.Get("Content-Type"), "video/x-matroska"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
	if got, want := resp.Header.Get("Content-Length"), "4"; got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got, want := resp.Header.Get("Content-Range"), fmt.Sprintf("bytes 0-3/%d", len(data)-8); got != want {
		t.Fatalf("Content-Range = %q, want %q", got, want)
	}
}

func TestProxyObfuscatedMatroskaFullResponse(t *testing.T) {
	conf.Conf = &conf.Config{}
	data := append([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x1a, 0x45, 0xdf, 0xa3}, []byte("payload")...)
	upstream := newRangeServer(t, data, "video/x-matroska")
	defer upstream.Close()

	file := &model.Object{
		Name:     "movie.mkv",
		Size:     int64(len(data)),
		Modified: time.Unix(1, 0),
	}
	req := httptest.NewRequest(http.MethodGet, "/p/movie.mkv", nil)
	rec := httptest.NewRecorder()

	if err := Proxy(rec, req, &model.Link{URL: upstream.URL}, file); err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}

	resp := rec.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	wantBody := data[8:]
	if !bytes.Equal(body, wantBody) {
		t.Fatalf("body = % x, want % x", body, wantBody)
	}
	if got, want := resp.Header.Get("Content-Length"), strconv.Itoa(len(wantBody)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
}

func TestProxyOrdinaryFileUnchanged(t *testing.T) {
	conf.Conf = &conf.Config{}
	data := []byte("ordinary-data")
	upstream := newRangeServer(t, data, "application/x-upstream")
	defer upstream.Close()

	file := &model.Object{
		Name:     "ordinary.bin",
		Size:     int64(len(data)),
		Modified: time.Unix(1, 0),
	}
	link := &model.Link{URL: upstream.URL}
	req := httptest.NewRequest(http.MethodGet, "/p/ordinary.bin", nil)
	rec := httptest.NewRecorder()

	if err := Proxy(rec, req, link, file); err != nil {
		t.Fatalf("Proxy returned error: %v", err)
	}

	resp := rec.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll returned error: %v", err)
	}
	if !bytes.Equal(body, data) {
		t.Fatalf("body = %q, want %q", body, data)
	}
	if got, want := resp.Header.Get("Content-Length"), strconv.Itoa(len(data)); got != want {
		t.Fatalf("Content-Length = %q, want %q", got, want)
	}
	if got, want := resp.Header.Get("Content-Type"), "application/x-upstream"; got != want {
		t.Fatalf("Content-Type = %q, want %q", got, want)
	}
}

func newRangeServer(t *testing.T, data []byte, contentType string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))

		ranges, err := http_range.ParseRange(r.Header.Get("Range"), int64(len(data)))
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if len(ranges) == 0 {
			w.WriteHeader(http.StatusOK)
			if r.Method != http.MethodHead {
				_, _ = w.Write(data)
			}
			return
		}

		hr := ranges[0]
		w.Header().Set("Content-Length", strconv.FormatInt(hr.Length, 10))
		w.Header().Set("Content-Range", hr.ContentRange(int64(len(data))))
		w.WriteHeader(http.StatusPartialContent)
		if r.Method != http.MethodHead {
			_, _ = w.Write(data[hr.Start : hr.Start+hr.Length])
		}
	}))
}
