package rproxy

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"plugin"
	"sync"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/ozone/v2/rproxymod"

	"github.com/One-com/ozone/v2/handlers/rproxy/module/backendsettings"
	"github.com/One-com/ozone/v2/handlers/rproxy/module/forward_map_director"
	"github.com/One-com/ozone/v2/handlers/rproxy/module/host_suffix_director"
	"github.com/One-com/ozone/v2/handlers/rproxy/module/proxypass"
	"github.com/One-com/ozone/v2/handlers/rproxy/module/set_header"
)

var moduleRegistryMu sync.Mutex
var moduleRegistry = map[string]func(jconf.SubConfig) (rproxymod.ProxyModule, error){}

func init() {
	preloadBuiltinModules()
}

func preloadBuiltinModules() {
	moduleRegistryMu.Lock()
	defer moduleRegistryMu.Unlock()
	moduleRegistry["forward_map_director"] = forward_map_director.InitModule
	moduleRegistry["host_suffix_director"] = host_suffix_director.InitModule
	moduleRegistry["set_header"] = set_header.InitModule
	moduleRegistry["proxypass"] = proxypass.InitModule
	moduleRegistry["backendsettings"] = backendsettings.InitModule
}

// RegisterReverseProxyModule registers an initalization function for a reverse proxy
// module under a type name, so it can be used in the config.
// If a "ReverseProxy" handler is configured with this module type name, the
// supplied init function is called with the module config.
func RegisterReverseProxyModule(typename string, initfunc func(jconf.SubConfig) (rproxymod.ProxyModule, error)) {
	moduleRegistryMu.Lock()
	defer moduleRegistryMu.Unlock()
	moduleRegistry[typename] = initfunc
}

func proxyModuleFor(cfg *ModuleConfig) (mod rproxymod.ProxyModule, err error) {
	moduleRegistryMu.Lock()
	defer moduleRegistryMu.Unlock()

	if initfunc, ok := moduleRegistry[cfg.Type]; ok {
		return initfunc(cfg.Config)
	}
	err = fmt.Errorf("No such module type: %s", cfg.Type)
	return
}

func scanPluginDir(dir string) error {

	absdir, e := filepath.Abs(dir)
	if e != nil {
		return e
	}

	files, err := ioutil.ReadDir(absdir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".so" {
			continue
		}
		abspath := filepath.Join(absdir, file.Name())
		_, err = plugin.Open(abspath)
		if err != nil {
			return err
		}
	}
	return nil
}
