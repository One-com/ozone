package proxypass

import (
	"net"
	"net/http"
	"net/url"

	"strings"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"

	"github.com/One-com/ozone/rproxymod"
)

type ProxyHeadersConfig struct {
	XFwdFor    bool   `json:"X-Forwarded-For,omitempty"`
	XFwdHost   bool   `json:"X-Forwarded-Host,omitempty"`
	XFwdServer bool   `json:"X-Forwarded-Server,omitempty"`
	XFwdProto  bool   `json:"X-Forwarded-Proto,omitempty"`
	Forwarded  string `json:"Forwarded,omitempty"` // RFC7239
	Via        string `json:",omitempty"`
}

type Config struct {
	RewriteHost    bool // Rewrite Host header to match target URL
	RewriteForward bool // Rewrite incoming URI headers like "Destination"
	RewriteReverse bool // Rewrite outgoing URI headers like "Location"
	Headers        ProxyHeadersConfig
}

type Module struct {
	rproxymod.BaseModule
	Config
}

func InitModule(cfg jconf.SubConfig) (rpmod rproxymod.ProxyModule, err error) {
	var jc *Config
	var mod *Module
	err = cfg.ParseInto(&jc)
	if err == nil {
		mod = &Module{Config: *jc}
	}
	rpmod = mod
	return
}

// Remember to test reqCtx.CopiedHeaders and maybe copy headers before modifying them.
func (mod *Module) ProcessRequest(reqCtx *rproxymod.RequestContext, inReq *http.Request, proxyReq *http.Request) (res *http.Response, err error) {

	if mod.Headers.XFwdHost {
		reqCtx.EnsureWritableHeader(proxyReq, inReq)
		proxyReq.Header.Set("X-Forwarded-Host", inReq.Host)
	}

	if mod.Headers.XFwdServer {
		reqCtx.EnsureWritableHeader(proxyReq, inReq)
		proxyReq.Header.Set("X-Forwarded-Server", rproxymod.ServerHostName())
	}

	if mod.Headers.XFwdProto {
		reqCtx.EnsureWritableHeader(proxyReq, inReq)
		var scheme string
		if inReq.TLS == nil {
			scheme = "http"
		} else {
			scheme = "https"
		}
		proxyReq.Header.Set("X-Forwarded-Proto", scheme)
	}

	if mod.Headers.XFwdFor {
		if clientIP, _, err := net.SplitHostPort(inReq.RemoteAddr); err == nil {
			reqCtx.EnsureWritableHeader(proxyReq, inReq)
			// If we aren't the first proxy retain prior
			// X-Forwarded-For information as a comma+space
			// separated list and fold multiple headers into one.
			if prior, ok := proxyReq.Header["X-Forwarded-For"]; ok {
				clientIP = strings.Join(prior, ", ") + ", " + clientIP
			}
			proxyReq.Header.Set("X-Forwarded-For", clientIP)
		}
	}

	if mod.RewriteHost {
		proxyReq.Host = proxyReq.URL.Host
	}

	if mod.RewriteForward {
		if dest := proxyReq.Header.Get("Destination"); dest != "" {
			destURL, err := url.Parse(dest)
			if err != nil {
				log.ERROR("Error Parsing URL", "err", err)
			} else {
				destURL.Scheme = proxyReq.URL.Scheme
				destURL.Host = proxyReq.Host // URL.Host may be overridden
				reqCtx.EnsureWritableHeader(proxyReq, inReq)
				proxyReq.Header.Set("Destination", destURL.String())
			}
		}
	}

	return
}

func (mod *Module) ModifyResponse(reqCtx *rproxymod.RequestContext, inReq *http.Request, resp *http.Response) error {
	if mod.RewriteReverse {
		if location := resp.Header.Get("Location"); location != "" {
			url, err := url.Parse(location)
			if err != nil {
				log.ERROR("Error parsing Location header", "err", err)
			}
			if inReq.TLS == nil {
				url.Scheme = "http"
			} else {
				url.Scheme = "https"
			}
			url.Host = inReq.Host
			resp.Header.Set("Location", url.String())
		}
	}
	return nil
}

func (mod *Module) Deinit() error {
	return nil
}
