package forward_map_director

import (
	"net/http"
	"net/url"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"

	"github.com/One-com/ozone/v2/rproxymod"
)

type Config struct {
	Forward map[string]string
}

type Module struct {
	rproxymod.BaseModule
	forwardHostMap map[string]*url.URL
}

func InitModule(cfg jconf.SubConfig) (rpmod rproxymod.ProxyModule, err error) {
	var jc *Config
	err = cfg.ParseInto(&jc)
	if err != nil {
		return
	}
	mod := &Module{}
	mod.forwardHostMap = make(map[string]*url.URL)

	for host, forward := range jc.Forward {
		var url *url.URL
		url, err = url.Parse(forward)
		if err != nil {
			break
		}
		mod.forwardHostMap[host] = url
	}
	rpmod = mod
	return
}

func (m *Module) ProcessRequest(reqCtx *rproxymod.RequestContext, inReq *http.Request, proxyReq *http.Request) (res *http.Response, err error) {
	// Only do director stuff here.
	if target, ok := m.forwardHostMap[proxyReq.Host]; ok {
		proxyReq.URL.Scheme = target.Scheme
		proxyReq.URL.Host = target.Host
	} else {
		if fallbacktarget, ok := m.forwardHostMap[""]; ok {
			proxyReq.URL.Scheme = fallbacktarget.Scheme
			proxyReq.URL.Host = fallbacktarget.Host
		} else {
			res = rproxymod.CreateResponse(http.StatusNotFound, "No such host")
			return
		}
	}
	return
}

func (m *Module) Deinit() error {
	log.INFO("Deinitializing forward director module")
	return nil
}
