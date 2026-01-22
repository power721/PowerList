package setting

import (
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"strconv"

	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

func GetStr(key string, defaultValue ...string) string {
	val, _ := op.GetSettingItemByKey(key)
	if val == nil {
		if len(defaultValue) > 0 {
			return defaultValue[0]
		}
		return ""
	}
	return val.Value
}

func GetInt(key string, defaultVal int) int {
	i, err := strconv.Atoi(GetStr(key))
	if err != nil {
		return defaultVal
	}
	return i
}

func GetBool(key string) bool {
	return GetStr(key) == "true" || GetStr(key) == "1"
}

func SaveSetting(item *model.SettingItem) (err error) {
	return op.SaveSettingItem(item)
}

func GetFloat(key string, defaultVal float64) float64 {
	f, err := strconv.ParseFloat(GetStr(key), 64)
	if err != nil {
		return defaultVal
	}
	return f
}
