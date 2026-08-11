package conf

import (
	"net/url"
	"regexp"
	"sync"
)

var (
	BuiltAt    string = "unknown"
	GitAuthor  string = "unknown"
	GitCommit  string = "unknown"
	Version    string = "dev"
	WebVersion string = "rolling"
	ConfigPath string
)

var (
	Conf *Config
	URL  *url.URL
)

var SlicesMap = make(map[string][]string)

var (
	// 在HybridCache中使用[]byte缓存数据流的限制，内存为Go自动管理，直到GC
	AutoMemoryLimit uint64 = 4 * 1024 * 1024
	// 最小空闲内存，当内存不足时，HybridCache会回退到文件缓存。
	// 如果为0，HybridCache会使用文件缓存，不占用内存。
	MinFreeMemory uint64 = 16 * 1024 * 1024
	// 限制HybridCache手动管理内存单次的扩容大小，超过该阈值将分多次扩容。
	// MinFreeMemory大于0时，也限制 Downloader 的PartSize
	MaxBlockLimit uint64 = 16 * 1024 * 1024
)
var FilenameCharMap = make(map[string]string)
var PrivacyReg []*regexp.Regexp

var (
	// AT
	LazyLoad = false

	// 单个Buffer最大限制
	MaxBufferLimit = 16 * 1024 * 1024
	// 超过该阈值的Buffer将使用 mmap 分配，可主动释放内存
	MmapThreshold = 4 * 1024 * 1024
)
var (
	RawIndexHtml string
	ManageHtml   string
	IndexHtml    string
)

var (
	// StoragesLoaded loaded success if empty
	StoragesLoaded     = false
	storagesLoadMu     sync.RWMutex
	storagesLoadSignal chan struct{} = make(chan struct{})
)

func StoragesLoadSignal() <-chan struct{} {
	storagesLoadMu.RLock()
	ch := storagesLoadSignal
	storagesLoadMu.RUnlock()
	return ch
}
func SendStoragesLoadedSignal() {
	storagesLoadMu.Lock()
	select {
	case <-storagesLoadSignal:
		// already closed
	default:
		StoragesLoaded = true
		close(storagesLoadSignal)
	}
	storagesLoadMu.Unlock()
}
func ResetStoragesLoadSignal() {
	storagesLoadMu.Lock()
	select {
	case <-storagesLoadSignal:
		StoragesLoaded = false
		storagesLoadSignal = make(chan struct{})
	default:
		// not closed -> nothing to do
	}
	storagesLoadMu.Unlock()
}
