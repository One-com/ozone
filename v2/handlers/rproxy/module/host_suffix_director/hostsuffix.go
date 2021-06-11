package host_suffix_director

import (
	"net"
	"net/http"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/v2/rproxymod"
)

type Config struct {
	Suffix   string
	ViaCNAME bool
}

type Module struct {
	rproxymod.BaseModule
	Config
}

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

func (mod *Module) ProcessRequest(reqCtx *rproxymod.RequestContext, inReq *http.Request, proxyReq *http.Request) (res *http.Response, err error) {

	// Only do director stuff here.
	var inport string
	if inReq.TLS == nil {
		inport = "80"
		proxyReq.URL.Scheme = "http"
	} else {
		inport = "443"
		proxyReq.URL.Scheme = "https"
	}

	//	orighost := proxyReq.Host
	host, port, err := net.SplitHostPort(proxyReq.Host)
	if err != nil {
		host = proxyReq.Host
		port = inport
		err = nil
	}
	host = host + mod.Suffix
	if mod.ViaCNAME {
		host, err = net.LookupCNAME(host)
		if err != nil {
			res = rproxymod.CreateResponse(http.StatusBadGateway, "Failure to resolve")
			return
		}
	}
	proxyReq.URL.Host = net.JoinHostPort(host, port)
	//	proxyReq.Host = orighost
	return
}

func (mod *Module) ModifyResponse(reqCtx *rproxymod.RequestContext, inReq *http.Request, resp *http.Response) error {
	return nil
}

func (mod *Module) Deinit() error {
	return nil
}
