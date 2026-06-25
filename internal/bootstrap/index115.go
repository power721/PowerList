package bootstrap

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/index115"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/server"
	"github.com/OpenListTeam/OpenList/v4/server/handles"
	log "github.com/sirupsen/logrus"
)

func InitIndex115() {
	if err := InitIndex115Service(context.Background()); err != nil {
		log.Errorf("init index115 error: %+v", err)
		return
	}
	log.Info("index115 service initialized successfully")
}

var (
	index115Mu       sync.Mutex
	index115Linker   *index115.LinkResolver
	index115Store    *index115.Store
	index115Searcher *index115.Searcher
)

// index115ReloadCloseGrace is how long the previous store/searcher stay open
// after a reload, letting in-flight requests finish against the still-alive old
// inode before its file handles are closed.
const index115ReloadCloseGrace = 10 * time.Second

func InitIndex115Service(ctx context.Context) error {
	if conf.Conf == nil {
		return errors.New("config not initialized")
	}
	if conf.Conf.Index115.DBFile == "" || conf.Conf.Index115.BleveDir == "" {
		return errors.New("index115 paths not configured")
	}
	store, err := index115.OpenStoreRuntime(ctx, conf.Conf.Index115.DBFile)
	if err != nil {
		return err
	}
	searcher, err := index115.NewSearcher(ctx, store, conf.Conf.Index115.BleveDir)
	if err != nil {
		log.Warnf("index115 search disabled: %+v", err)
	}
	// The linker owns in-flight 115 cleanup leases; create it once and retain it
	// across reloads so a reload never orphans scheduled temp-file deletions.
	if index115Linker == nil {
		delay := time.Duration(index115DeleteDelaySeconds()) * time.Second
		index115Linker = index115.NewLinkResolver(index115.NewDriver115ShareClient(), delay)
	}
	swapIndex115(store, searcher)
	handles.SetIndex115Reloader(func() error { return ReloadIndex115() })
	return nil
}

// ReloadIndex115 reopens the index115 SQLite store and bleve index from disk and
// swaps them in as the live service, so an externally swapped DB/index (e.g.
// alist-tvbox Index115Extractor's atomic dir rename) takes effect without a
// process restart. The LinkResolver is retained across reloads.
func ReloadIndex115() error {
	index115Mu.Lock()
	defer index115Mu.Unlock()

	if conf.Conf == nil {
		return errors.New("config not initialized")
	}
	if conf.Conf.Index115.DBFile == "" || conf.Conf.Index115.BleveDir == "" {
		return errors.New("index115 paths not configured")
	}
	ctx := context.Background()
	newStore, err := index115.OpenStoreRuntime(ctx, conf.Conf.Index115.DBFile)
	if err != nil {
		return err
	}
	newSearcher, err := index115.NewSearcher(ctx, newStore, conf.Conf.Index115.BleveDir)
	if err != nil {
		log.Warnf("index115 search disabled after reload: %+v", err)
		newSearcher = nil
	}
	swapIndex115(newStore, newSearcher)
	log.Info("index115 service reloaded")
	return nil
}

// swapIndex115 wraps freshly opened store/searcher in a service and installs it
// as the live service through the package globals, then schedules the previous
// handles to close after a grace period. index115.NewService also assigns the
// package-level MyIndex115Service, so the 115_index driver picks up the swap on
// its next call.
func swapIndex115(newStore *index115.Store, newSearcher *index115.Searcher) {
	service := index115.NewService(newStore, newSearcher, index115Linker)
	oldStore, oldSearcher := index115Store, index115Searcher
	index115Store, index115Searcher = newStore, newSearcher
	handles.SetIndex115Service(service)
	server.SetIndex115BrowseService(service)
	go func() {
		time.Sleep(index115ReloadCloseGrace)
		if oldStore != nil {
			_ = oldStore.Close()
		}
		if oldSearcher != nil {
			_ = oldSearcher.Close()
		}
	}()
}

func index115DeleteDelaySeconds() int {
	if conf.Conf == nil {
		return 900
	}
	defer func() {
		_ = recover()
	}()
	return setting.GetInt(conf.DeleteDelayTime, 900)
}
