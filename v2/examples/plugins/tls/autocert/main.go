package main

import (
	"crypto/tls"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/jconf"

	"golang.org/x/crypto/acme/autocert"

	"github.com/One-com/ozone/v2"
)

func init() {

}

type Config struct {
	HostWhiteList []string `json:",omitempty"`
}

type plugin struct {
	manager autocert.Manager
}

func (p *plugin) SetSNICallback(c *tls.Config) (err error) {
	c.GetCertificate = p.manager.GetCertificate
	return
}

// TLSPluginTypes is a map of exported tls plugins
var TLSPluginTypes = map[string]ozone.TLSPluginConfigureFunc{
	"autocert": initTLSPlugin,
}

func initTLSPlugin(name string, js jconf.SubConfig) (cb func(*tls.Config) error, s []daemon.Server, c []daemon.CleanupFunc, err error) {

	var cfg *Config
	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	m := autocert.Manager{
		Prompt: autocert.AcceptTOS,
	}

	if cfg != nil {
		if cfg.HostWhiteList == nil {
			cfg.HostWhiteList = []string{}
		}
		m.HostPolicy = autocert.HostWhitelist(cfg.HostWhiteList...)
	}

	plugin := &plugin{m}
	cb = plugin.SetSNICallback

	return
}
