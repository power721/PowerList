package handles

import (
	"encoding/json"
	"errors"
	stdpath "path"

	"github.com/OpenListTeam/OpenList/v4/internal/conf"
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/errs"
	"github.com/OpenListTeam/OpenList/v4/internal/fs"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/OpenListTeam/OpenList/v4/internal/net"
	"github.com/OpenListTeam/OpenList/v4/internal/setting"
	"github.com/OpenListTeam/OpenList/v4/pkg/utils"
	"github.com/OpenListTeam/OpenList/v4/server/common"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func Down(c *gin.Context) {
	rawPath := c.Request.Context().Value(conf.PathKey).(string)
	filename := stdpath.Base(rawPath)
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorPage(c, err, 500)
		return
	}
	if common.ShouldProxy(storage, filename) {
		Proxy(c)
		return
	} else {
		link, _, err := fs.Link(c.Request.Context(), rawPath, model.LinkArgs{
			IP:       c.ClientIP(),
			Header:   c.Request.Header,
			Type:     c.Query("type"),
			Redirect: true,
		})
		if err != nil {
			common.ErrorPage(c, err, 500)
			return
		}
		redirect(c, link)
	}
}

func Proxy(c *gin.Context) {
	rawPath := c.Request.Context().Value(conf.PathKey).(string)
	filename := stdpath.Base(rawPath)
	storage, err := fs.GetStorage(rawPath, &fs.GetStoragesArgs{})
	if err != nil {
		common.ErrorPage(c, err, 500)
		return
	}
	if canProxy(storage, filename) {
		if _, ok := c.GetQuery("d"); !ok {
			if url := common.GenerateDownProxyURL(storage.GetStorage(), rawPath); url != "" {
				c.Redirect(302, url)
				return
			}
		}
		link, file, err := fs.Link(c.Request.Context(), rawPath, model.LinkArgs{
			Header: c.Request.Header,
			Type:   c.Query("type"),
		})
		if err != nil {
			common.ErrorPage(c, err, 500)
			return
		}
		applyProxyConfig(storage.GetStorage().Driver, link)
		proxy(c, link, file, storage.GetStorage().ProxyRange)
	} else {
		common.ErrorPage(c, errors.New("proxy not allowed"), 403)
		return
	}
}

func redirect(c *gin.Context, link *model.Link) {
	defer link.Close()
	var err error
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Cache-Control", "max-age=0, no-cache, no-store, must-revalidate")
	if setting.GetBool(conf.ForwardDirectLinkParams) {
		query := c.Request.URL.Query()
		for _, v := range conf.SlicesMap[conf.IgnoreDirectLinkParams] {
			query.Del(v)
		}
		link.URL, err = utils.InjectQuery(link.URL, query)
		if err != nil {
			common.ErrorPage(c, err, 500)
			return
		}
	}
	c.Redirect(302, link.URL)
}

func proxy(c *gin.Context, link *model.Link, file model.Obj, proxyRange bool) {
	defer link.Close()
	var err error
	if link.URL != "" && setting.GetBool(conf.ForwardDirectLinkParams) {
		query := c.Request.URL.Query()
		for _, v := range conf.SlicesMap[conf.IgnoreDirectLinkParams] {
			query.Del(v)
		}
		link.URL, err = utils.InjectQuery(link.URL, query)
		if err != nil {
			common.ErrorPage(c, err, 500)
			return
		}
	}
	if proxyRange {
		link = common.ProxyRange(c, link, file.GetSize())
	}
	Writer := &common.WrittenResponseWriter{ResponseWriter: c.Writer}
	err = common.Proxy(Writer, c.Request, link, file)
	if err == nil {
		return
	}
	if Writer.IsWritten() {
		log.Errorf("%s %s local proxy error: %+v", c.Request.Method, c.Request.URL.Path, err)
	} else {
		if statusCode, ok := errs.UnwrapOrSelf(err).(net.HttpStatusCodeError); ok {
			common.ErrorPage(c, err, int(statusCode), true)
		} else {
			common.ErrorPage(c, err, 500, true)
		}
	}
}

// proxyDriverAliases 把分享驱动的 storage.Driver 名归一为它转存所用的网盘驱动名,
// 这样全局 proxy_config(以网盘驱动名为键)同样覆盖分享驱动。
// 未列出的驱动名按原值查询(查不到则保留驱动自身设定值)。
var proxyDriverAliases = map[string]string{
	"QuarkShare":       "Quark",
	"UCShare":          "UC",
	"115 Share":        "115 Cloud",
	"123PanShare":      "123Pan",
	"Yun139Share":      "139Yun",
	"189Share":         "189CloudPC",
	"BaiduShare2":      "BaiduNetdisk",
	"GuangYaPanShare":  "GuangYaPan",
	"AliyunShare":      "AliyundriveOpen",
	"AliyundriveShare": "AliyundriveOpen",
	"ThunderShare":     "ThunderBrowser",
	"PikPakShare":      "PikPak",
}

// applyProxyConfig 用全局 proxy_config(JSON, 按 storage.Driver 键控)覆盖 link 的多线程参数。
// 该配置由 alist-tvbox 的 local_proxy_config 推送而来;未配置的驱动保留驱动自身设定的值。
// 分享驱动先经 proxyDriverAliases 归一为对应网盘驱动名,使全局配置对分享同样生效。
func applyProxyConfig(driver string, link *model.Link) {
	raw := setting.GetStr(conf.ProxyConfig)
	if raw == "" || raw == "{}" {
		return
	}
	var cfg map[string]struct {
		Concurrency int `json:"concurrency"`
		ChunkSize   int `json:"chunk_size"`
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return
	}
	if alias, ok := proxyDriverAliases[driver]; ok {
		driver = alias
	}
	//log.Debugf("apply proxy config: %+v driver: %v", cfg, driver)
	item, ok := cfg[driver]
	if !ok {
		return
	}
	if item.Concurrency > 0 {
		link.Concurrency = item.Concurrency
	}
	if item.ChunkSize > 0 {
		link.PartSize = item.ChunkSize * utils.KB
	}
	//log.Debugf("[proxy] link: %+v", link)
}

// TODO need optimize
// when can be proxy?
// 1. text file
// 2. config.MustProxy()
// 3. storage.WebProxy
// 4. proxy_types
// solution: text_file + shouldProxy()
func canProxy(storage driver.Driver, filename string) bool {
	if storage.Config().MustProxy() || storage.GetStorage().WebProxy || storage.GetStorage().WebdavProxyURL() {
		return true
	}
	if utils.SliceContains(conf.SlicesMap[conf.ProxyTypes], utils.Ext(filename)) {
		return true
	}
	if utils.SliceContains(conf.SlicesMap[conf.TextTypes], utils.Ext(filename)) {
		return true
	}

	// AT: always true!
	return true
}
