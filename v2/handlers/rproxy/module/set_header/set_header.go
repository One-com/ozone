package set_header

import (
	"fmt"
	"net/http"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"

	"github.com/One-com/ozone/v2/rproxymod"
)

//       "Modules": {
//         "set_header": {
//           "Type": "set_header",
//           "Config": {
//             "ResponseHeader": {
//               "X-Foo": "bar"
//             },
//             "RequestHeader": {
//               "X-Foo": "bar"
//             }
//           }
//         }
// }

type Config struct {
	ResponseHeader map[string]string
	RequestHeader  map[string]string
}

type Module struct {
	rproxymod.BaseModule
	Config
}

func InitModule(cfg jconf.SubConfig) (rpmod rproxymod.ProxyModule, err error) {
	var mod *Module
	var jc *Config
	err = cfg.ParseInto(&jc)
	if err == nil {
		mod = &Module{Config: *jc}
	}
	rpmod = mod
	return
}

// Remember to test reqCtx.CopiedHeaders and maybe copy headers before modifying them.
func (mod *Module) ProcessRequest(reqCtx *rproxymod.RequestContext, inReq *http.Request, proxyReq *http.Request) (res *http.Response, err error) {
	if len(mod.RequestHeader) >= 1 {
		reqCtx.EnsureWritableHeader(proxyReq, inReq)
		for k, v := range mod.RequestHeader {
			proxyReq.Header.Set(k, v)
			if f, ok := log.DEBUGok(); ok {
				f(fmt.Sprintf("Setting Request Header %s:%s", k, v))
			}
		}
	}
	return
}

func (mod *Module) ModifyResponse(reqCtx *rproxymod.RequestContext, inReq *http.Request, resp *http.Response) error {

	if len(mod.ResponseHeader) >= 1 {
		for k, v := range mod.ResponseHeader {
			resp.Header.Set(k, v)
			if f, ok := log.DEBUGok(); ok {
				f(fmt.Sprintf("Setting Response Header %s:%s", k, v))
			}
		}
	}
	return nil
}

func (mod *Module) Deinit() error {
	return nil
}
