package guangyapan

import (
	"context"
	"time"

	"github.com/OpenListTeam/OpenList/v4/drivers/guangyapan"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
	"github.com/OpenListTeam/OpenList/v4/pkg/singleflight"
	"github.com/OpenListTeam/go-cache"
)

var taskCache = cache.NewMemCache(cache.WithShards[[]guangyapan.OfflineTask](16))
var taskG singleflight.Group[[]guangyapan.OfflineTask]

func (g *GuangYaPan) GetTasks(driver *guangyapan.GuangYaPan) ([]guangyapan.OfflineTask, error) {
	key := op.Key(driver, "/cloud/tasks")
	if !g.refreshTaskCache {
		if tasks, ok := taskCache.Get(key); ok {
			return tasks, nil
		}
	}
	g.refreshTaskCache = false
	tasks, err, _ := taskG.Do(key, func() ([]guangyapan.OfflineTask, error) {
		ctx := context.Background()
		tasks, err := driver.OfflineList(ctx, "")
		if err != nil {
			return nil, err
		}
		if len(tasks) > 0 {
			taskCache.Set(key, tasks, cache.WithEx[[]guangyapan.OfflineTask](time.Second*10))
		} else {
			taskCache.Del(key)
		}
		return tasks, nil
	})
	if err != nil {
		return nil, err
	}
	return tasks, nil
}
