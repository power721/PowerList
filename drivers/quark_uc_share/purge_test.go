package quark_uc_share

import (
	"errors"
	"testing"
)

func TestMatchRecycleRecordPrefersFid(t *testing.T) {
	records := []RecycleRecord{
		{RecordId: "r1", Fid: "other-fid", FileName: "same-name.mkv"},
		{RecordId: "r2", Fid: "fid-123", FileName: "same-name.mkv"},
	}
	if got := matchRecycleRecord(records, "fid-123", "same-name.mkv"); got != "r2" {
		t.Fatalf("expect fid 精确命中 r2, got %q", got)
	}
}

func TestMatchRecycleRecordFallsBackToNameWhenFidAbsent(t *testing.T) {
	records := []RecycleRecord{
		{RecordId: "r1", FileName: "别的文件.mkv"},
		{RecordId: "r2", Name: "temp-video.mkv"},
	}
	if got := matchRecycleRecord(records, "fid-123", "temp-video.mkv"); got != "r2" {
		t.Fatalf("expect 无 fid 字段时按文件名兜底命中 r2, got %q", got)
	}
	// 记录带 fid(即便不是目标)时不允许按名字兜底,防误清用户自己的同名删除记录
	records[1].Fid = "someone-else"
	if got := matchRecycleRecord(records, "fid-123", "temp-video.mkv"); got != "" {
		t.Fatalf("expect 有 fid 字段的记录不参与名字兜底, got %q", got)
	}
}

func TestMatchRecycleRecordNoMatch(t *testing.T) {
	records := []RecycleRecord{{RecordId: "r1", Fid: "a", FileName: "x.mkv"}}
	if got := matchRecycleRecord(records, "b", "y.mkv"); got != "" {
		t.Fatalf("expect no match, got %q", got)
	}
	if got := matchRecycleRecord(records, "b", ""); got != "" {
		t.Fatalf("expect empty fileName no fallback, got %q", got)
	}
}

func TestListRecycleRecordIDPaginatesUntilFound(t *testing.T) {
	// 真实分页形态:每页 200 条,total>200 时继续翻,target 落在第 3 页
	filler := func(n int, prefix string) []RecycleRecord {
		out := make([]RecycleRecord, n)
		for i := range out {
			out[i] = RecycleRecord{RecordId: prefix, Fid: prefix}
		}
		return out
	}
	pages := map[int][]RecycleRecord{
		1: filler(200, "f1"),
		2: filler(200, "f2"),
		3: append(filler(199, "f3"), RecycleRecord{RecordId: "r3", Fid: "target"}),
	}
	calls := 0
	fetch := func(page int) ([]RecycleRecord, int, error) {
		calls++
		return pages[page], 599, nil
	}
	if got := listRecycleRecordID(fetch, "target", ""); got != "r3" {
		t.Fatalf("expect r3 on page 3, got %q", got)
	}
	if calls != 3 {
		t.Fatalf("expect 3 page fetches, got %d", calls)
	}
}

func TestListRecycleRecordIDStopsAtEmptyPage(t *testing.T) {
	calls := 0
	fetch := func(page int) ([]RecycleRecord, int, error) {
		calls++
		return nil, 5, nil // 风控隐藏:total 计数但每页恒空
	}
	if got := listRecycleRecordID(fetch, "target", ""); got != "" {
		t.Fatalf("expect empty result, got %q", got)
	}
	if calls != 1 {
		t.Fatalf("expect stop after first empty page, got %d calls", calls)
	}
}

func TestListRecycleRecordIDReturnsEmptyOnError(t *testing.T) {
	fetch := func(page int) ([]RecycleRecord, int, error) {
		return nil, 0, errors.New("boom")
	}
	if got := listRecycleRecordID(fetch, "target", ""); got != "" {
		t.Fatalf("expect empty on error, got %q", got)
	}
}
