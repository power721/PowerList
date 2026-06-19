package bootstrap

import (
	"context"
	"errors"
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

func InitIndex115Service(ctx context.Context) error {
	if conf.Conf == nil {
		return errors.New("config not initialized")
	}
	if conf.Conf.Index115.DBFile == "" || conf.Conf.Index115.BleveDir == "" {
		return errors.New("index115 paths not configured")
	}
	store, searcher, err := index115.NewRuntime(ctx, conf.Conf.Index115.DBFile, conf.Conf.Index115.BleveDir)
	if err != nil {
		return err
	}
	delay := time.Duration(index115DeleteDelaySeconds()) * time.Second
	service := index115.NewService(
		store,
		searcher,
		index115.NewLinkResolver(index115.NewDriver115ShareClient(), delay),
	)
	handles.SetIndex115Service(service)
	server.SetIndex115BrowseService(service)
	return nil
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
