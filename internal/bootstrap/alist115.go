package bootstrap

import (
	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/server/handles"
	log "github.com/sirupsen/logrus"
)

func InitAlist115() {
	dataDir := conf.Conf.Database.DBFile
	if dataDir == "" {
		dataDir = "data"
	}

	if err := handles.InitAlist115(dataDir); err != nil {
		log.Errorf("init alist115 error: %+v", err)
		return
	}

	log.Info("alist115 service initialized successfully")
}
