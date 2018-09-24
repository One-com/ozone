package ozone

import (
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"plugin"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/http/handlers/accesslog"
	"github.com/One-com/gone/log"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/config"
	"github.com/One-com/ozone/tlsconf"
)

// TLSPLUGINPATH is the default directory containing TLS .so plugins.
// It default to "" (to not be used) but can be set by the linker (by using -ldflags " -X ozone.TLSPLUGINPATH=...").
// Otherwise "TLSPluginDir" in the config is used.
var TLSPLUGINPATH string

// TLSPluginConfigureFunc is the function called to configure a TLS plugin.
// It should return a function to modify the *tls.Config already configured from static config.
// This function can in principle do anything, but most useful is to add callbacks like GetCertificate() for SNI.
// Additionally it can return servers and cleanup functions to be run in parallel with the other services.
type TLSPluginConfigureFunc func(name string, cfg jconf.SubConfig) (func(*tls.Config) error, []daemon.Server, []daemon.CleanupFunc, error)

var tlsPluginTypes = map[string]TLSPluginConfigureFunc{}

// RegisterTLSPluginType makes a named type of TLS plugin available via config.
// Not go-routine safe.
func RegisterTLSPluginType(typename string, f TLSPluginConfigureFunc) {
	tlsPluginTypes[typename] = f
}

// instantiateServersFromConfig (re)parse the provided config file and use it to initialize
// a new set of Server objects to run along with the parsed config.
// If anything fails during this process an error is returned to avoid stopping the current
// runnings servers. A slice of cleanup functions are returned to be run after the servers
// are shut down.
//
// The Config provides definitions for:
// HTTP servers (with associated listeners)
// HTTP Handlers
// TLS plugins (for SNI, etc..) - if any
// Global Log config
// Global Metrics config
// ---
// Access logging can be configured globally or individual for all servers.
// Metrics is set per server or per handler
//
func instantiateServersFromConfig(cfgdata interface{}) (servers []daemon.Server, cleanups []daemon.CleanupFunc, cfg *config.Config, err error) {

	switch c := cfgdata.(type) {
	case string:
		cfg, err = config.ParseConfigFromFile(c)
	case io.ReadSeeker:
		cfg, err = config.ParseConfigFromReadSeeker(c)
	default:
		err = fmt.Errorf("Dont know how to read config from (%T)", c)
		return
	}

	if err != nil || cfg == nil {
		err = config.WrapError(err)
		return
	}

	// Initialize internal services like metrics and SNI (if configured)
	metricsService, e := loadMetricsConfig(cfg.Metrics)
	if e != nil {
		err = e
		log.CRIT("Error processing 'Metrics' configuration section", "err", err)
		return
	}
	if metricsService != nil {
		servers = append(servers, metricsService)
	}

	// Create a TLSPluginRegistry with the legacy cfg.SNI config as default config.
	tlsPluginRegistry := newTLSPluginRegistry(cfg.TLSPluginDir, cfg.TLSPlugins, cfg.SNI)

	//-----------------------------------------------------------
	// Initialize configured HTTP services with handlers

	handlerRegistry := newHandlerRegistry(cfg.Handlers)

	accessLogSpec := ""
	if cfg.Log != nil {
		accessLogSpec = cfg.Log.AccessLog
	}

HTTP_SETUP:
	for srvName, srvCfg := range cfg.HTTPServers {

		var handler http.Handler
		var handlerSpec interface{}

		// Allow for server handler specification to be more than a string.
		// ...mostly used for a mux.
		handlerJSON := srvCfg.Handler
		err = handlerJSON.ParseInto(&handlerSpec)
		if err != nil {
			break HTTP_SETUP
		}

		// shadow accessLogspec to set the default - if any.
		accessLogSpec := accessLogSpec
		if srvCfg.AccessLog != "" {
			accessLogSpec = srvCfg.AccessLog
		}

		// Look up the HTTP handler for this server by handlerSpec
		handler, err = handlerRegistry.HandlerForSpec(srvName, handlerSpec)
		if err != nil {
			break HTTP_SETUP // bail out, return error
		}

		// If handler lookup is OK, Wrap it in any access logging and/or metrics,
		// Create the server with the resulting handler and append it to the
		// list of servers to serve.

		// any metrics for this server.
		var mfunc accesslog.AuditFunction
		if srvCfg.Metrics != "" {
			mfunc = metricsFunction(srvName, srvCfg.Metrics)
		}

		// Always wrap handler with audithandler to allow dynamic accesslog.
		wrappedHandler, logcleanup := wrapAuditHandler(srvName, handler, accessLogSpec, mfunc)
		if logcleanup != nil {
			cleanups = append(cleanups, logcleanup)
		}
		// Create the actual server with the resulting handler
		server, e := newHTTPServer(srvName, srvCfg, tlsPluginRegistry, wrappedHandler)
		if e != nil {
			err = e
			log.CRIT(fmt.Sprintf("Failed to initialize service '%s'", srvName), "err", err)
			break HTTP_SETUP
		}

		servers = append(servers, server)
	}

	// Add services and cleanups generated by handlers
	servers = append(servers, handlerRegistry.Services()...)
	cleanups = append(cleanups, handlerRegistry.Cleanups()...)

	// Add services from the TLSPluginRegistry
	servers = append(servers, tlsPluginRegistry.Services...)
	cleanups = append(cleanups, tlsPluginRegistry.Cleanups...)

	return
}

func getTLSServerConfigWithPlugin(cfg *config.TLSServerConfig, plugins *tlsPluginRegistry) (tlsConf *tls.Config, err error) {

	var tc *tls.Config
	tc, err = tlsconf.GetTLSServerConfig(&cfg.TLSServerConfig)
	if err != nil {
		return
	}
	if tc == nil {
		log.WARN("No TLS config generated")
		return
	}

	if cfg.EnableExternalSNI || cfg.TLSPlugin != "" {

		pluginname := cfg.TLSPlugin

		var modifyConfigF func(*tls.Config) error
		modifyConfigF, err = plugins.getTLSPlugin(pluginname)
		if err != nil {
			return
		}
		if modifyConfigF == nil {
			log.Fatal(fmt.Sprintf("TLS plugin enabled but plugin '%s' not found", pluginname))
		}
		err = modifyConfigF(tc)
		if err != nil {
			return
		}
	}

	tlsConf = tc
	return
}

// For managing the TLS plugin resolution
type tlsPluginRegistry struct {
	// plugin dir
	dir string
	// map of available configs - by name
	cfg config.TLSPluginsConfig

	// map of instantiated tls plugins - by name
	callbacks map[string]func(*tls.Config) error

	// resulting services and cleanups
	Services []daemon.Server
	Cleanups []daemon.CleanupFunc
}

// newTLSPluginRegistry initializes a tlsPluginRegistry for plugin resolution with a default plugin entry named ""
func newTLSPluginRegistry(plugindir string, tlsplugcfg config.TLSPluginsConfig, defaultcfg *jconf.OptionalSubConfig) (registry *tlsPluginRegistry) {

	// Add fallback legacy default plugin
	if tlsplugcfg == nil {
		tlsplugcfg = make(config.TLSPluginsConfig) // make a map (notice the plural)
	}
	tlsplugcfg[""] = config.TLSPluginConfig{Type: "", Plugin: "default_sni.so", Config: defaultcfg}

	if plugindir == "" {
		plugindir = TLSPLUGINPATH
	}

	registry = &tlsPluginRegistry{dir: plugindir, cfg: tlsplugcfg}

	return
}

// getTLSPlugin returns a tls.Config manipulating callback based on it's name, or...
// in the case it's not configured, create it from plugins.
func (r *tlsPluginRegistry) getTLSPlugin(name string) (cb func(*tls.Config) error, err error) {

	var exists bool
	if cb, exists = r.callbacks[name]; exists {
		return
	}

	// We haven't instantiated this tlsplugin yet, so:

	// find the config
	var cfg config.TLSPluginConfig
	if cfg, exists = r.cfg[name]; !exists {
		// We have no config for this plugin name anyway.
		return // not found
	}

	// Test if the Plugin Type should already exist (might be statically loaded)
	var initf TLSPluginConfigureFunc
	initf, exists = tlsPluginTypes[cfg.Type]

	// Try see if the name requires loading a plugin
	if (cfg.Type != "" || !exists) && cfg.Plugin != "" {

		filename := cfg.Plugin
		if filename[0] != '/' {
			if r.dir == "" {
				err = errors.New("Can't create absolute path for TLS Plugin. Set TLSPluginDir in config")
				return
			}
			filename = r.dir + "/" + filename
		}

		abspath, e := filepath.Abs(filename)
		if e != nil {
			err = e
			return
		}

		p, e := plugin.Open(abspath)
		if e != nil {
			err = e
			return
		}

		var f TLSPluginConfigureFunc
		s, e := p.Lookup("TLSPluginTypes")
		if e != nil {
			// No type map in the plug. Let's assume it registrered the type manually
			f = tlsPluginTypes[cfg.Type]

		} else {
			m := s.(*map[string]TLSPluginConfigureFunc)
			if m == nil {
				err = errors.New("Defect TLS plugin type map")
				return
			}
			f = (*m)[cfg.Type]
		}

		initf = f
	}

	if initf == nil {
		err = errors.New("No TLS plugin initializer")
		return
	}

	var servers []daemon.Server
	var cleanups []daemon.CleanupFunc
	cb, servers, cleanups, err = initf(name, cfg.Config)
	if err != nil {
		return
	}

	r.Services = append(r.Services, servers...)
	r.Cleanups = append(r.Cleanups, cleanups...)

	return
}
