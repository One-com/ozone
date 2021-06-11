package backendsettings

import (
	"net"
	"net/http"
	"strconv"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/v2/rproxymod"
)

//Config holds our config
type Config struct {
	ForceTLS bool
	Port     int
}

//Module holds all our functions and keeps Config
type Module struct {
	rproxymod.BaseModule
	Config
}

//InitModule is setting up our module with it's config
func InitModule(cfg jconf.SubConfig) (rpmod rproxymod.ProxyModule, err error) {
	var jc *Config
	var mod *Module
	err = cfg.ParseInto(&jc)
	if err != nil {
		return
	}
	mod = &Module{Config: *jc}
	rpmod = mod
	return
}

//ProcessRequest will change the proxyRequest (out going connection) to match the backend config
func (mod *Module) ProcessRequest(reqCtx *rproxymod.RequestContext, inReq *http.Request, proxyReq *http.Request) (res *http.Response, err error) {

	if mod.Port != 0 {
		host, _, err := net.SplitHostPort(proxyReq.Host)
		if err != nil {
			host = proxyReq.Host
		}
		proxyReq.URL.Host = net.JoinHostPort(host, strconv.Itoa(mod.Port))
	}
	if mod.ForceTLS {
		proxyReq.URL.Scheme = "https"
	}
	return
}

//ModifyResponse is unused in this case, but is part of the interface.
func (mod *Module) ModifyResponse(reqCtx *rproxymod.RequestContext, inReq *http.Request, resp *http.Response) error {
	return nil
}

//Deinit is unused in this case, but is part of the interface.
func (mod *Module) Deinit() error {
	return nil
}
