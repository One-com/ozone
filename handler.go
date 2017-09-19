package ozone

import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"plugin"
	"sync"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"

	"github.com/One-com/ozone/config"
	"github.com/One-com/ozone/handlers/rproxy"
)

// HandlerConfigureFunc is called to create a http.Handler from a JSON config stanza. Each registered handler type must define such a function.
// The function is passed a way to lookup other handlers by name of it needs to wrap around them.
type HandlerConfigureFunc func(name string, cfg jconf.SubConfig, lookupHandler func(string) (http.Handler, error)) (handler http.Handler, cleanup func() error, err error)

// Variables to help build handler setup resolution.
// When configuring a handler it is passed a handlerByName() function which
// lets it lookup another named handler. If that handler is not configured it
// should be done at that point. However, we don't want to carry the total handler config
// as a parameter through all handlers. (they shouldn't care who other handlers are configured).
// ... so handlerByName() should be a closure over total handler config.
// Sadly ... it also needs to be recursive. - and recursive closures in Go is... well... hard.
// So the whole process of configuring handlers needs to have the config as a global variable
// and thus be serialized through this mutex and these variables.  ... :(
var (
	handlerResolutionMutex sync.Mutex

	handlerResolutionMap  map[string]http.Handler
	handlerCfg            config.HandlersConfig
	handlerCleanups       []daemon.CleanupFunc
	handlerServices       []daemon.Server
	handlerResolutionPath []string // to detect handler cycles
)

//
var handlerTypes = map[string]HandlerConfigureFunc{}
var staticHandlers = map[string]http.Handler{
	"NotFound": http.NotFoundHandler(),
}

// RegisterHTTPHandlerType defines a handler type, so it can be used in the
// config file by "Type" and be used to configure http.Handler's from the
// associated config.
// Referencing a handler of type "name" in the config file, will
// use the provided function to configure the handler.
func RegisterHTTPHandlerType(typename string, f HandlerConfigureFunc) {
	handlerTypes[typename] = f
}

// RegisterStaticHTTPHandler makes it possible to directly reference an stdlib HTTP handler
// in the config file and from other handlers generated dynamically from config.
// Such handlers are not configurable. If you want to be able to configure
// your handler from the config file, you probably want to call
// RegisterHTTPHandlerType.
func RegisterStaticHTTPHandler(name string, h http.Handler) {
	staticHandlers[name] = h
}

// A new handler resolution is done each time instantiateServersFromConfig is called.
// Caller must have locked handlerResolutionMutex
func handlerResolutionReset(cfg config.HandlersConfig) {
	handlerResolutionMap = make(map[string]http.Handler)
	handlerCfg = cfg
	handlerCleanups = nil
	handlerServices = nil
	handlerResolutionPath = nil // detecting cycles
}

func handlerForSpec(srvName string, handlerSpec interface{}) (handler http.Handler, err error) {

	// Create a HTTP Handler for the Service.
	switch handlerKind := handlerSpec.(type) {
	case string:
		// Single Handler for all URLs
		handler, err = handlerByName(handlerKind)
		if err != nil {
			err = fmt.Errorf("Handler(%s): %s", handlerKind, err.Error())
		}
	case map[string]interface{}:
		// Set up a serve mux Handler mapping several handlers to URLs
		mhandler := http.NewServeMux()
		for path, handlerIName := range handlerKind {
			var phandler http.Handler // handler for a specific URL path
			switch handlerName := handlerIName.(type) {
			case string:
				phandler, err = handlerByName(handlerName)
				if err != nil {
					err = fmt.Errorf("Handler(%s): %s", handlerName, err.Error())
				}
			default:
				err = fmt.Errorf("Handlername must be string for service %s, was: %v", srvName, handlerName)
			}
			if err != nil {
				return
			}
			mhandler.Handle(path, phandler)
		}
		if err != nil {
			return
		}
		handler = mhandler
	default:
		log.DEBUG("Invalid handler:" + fmt.Sprintf("%v", handlerSpec))
		err = fmt.Errorf("Invalid handler config for service %s", srvName)
	}

	return
}

// given a name of a handler, return it if it's already configured,
// else configure it - if possible and return it.
func handlerByName(name string) (handler http.Handler, err error) {

	// If we already have resolved this handler, return it.
	var ok bool
	if handler, ok = handlerResolutionMap[name]; ok {
		return
	}

	// See if we can configure it from the handlers config.
	// if that fails, return any static handler with that name.
	// This means handlers defined by config override static handlers
	// from code.
	var cfg config.HandlerConfig
	if cfg, ok = handlerCfg[name]; !ok {
		if handler, ok = staticHandlers[name]; !ok {
			err = fmt.Errorf("No such handler config: %s", name)
		}
		return
	}

	// Need to configure this handler, but first
	// check for cycles
	for _, n := range handlerResolutionPath {
		if n == name {
			err = fmt.Errorf("Handler cycle detected for %s: %v", name, handlerResolutionPath)
		}
	}
	pathlen := len(handlerResolutionPath)
	handlerResolutionPath = append(handlerResolutionPath, name)
	defer func() {
		handlerResolutionPath = handlerResolutionPath[:pathlen]
	}()

	// We didn't have the handler ready. Configure the handler from config.
	var cleanupfuncs []daemon.CleanupFunc
	var cf daemon.CleanupFunc
	var handlerservice daemon.Server

	handler, handlerservice, cf, err = handlerForConfig(name, &cfg)
	if err != nil {
		return
	}
	if cf != nil {
		// appending to a slice, since we might add another below in wrapping
		cleanupfuncs = append(cleanupfuncs, cf)
	}
	if handler != nil {
		mcfg := cfg.Metrics
		// If this handler has metrics enabled, wrap an extra audithandler.
		if mcfg != "" {
			mfunc := metricsFunction(name, mcfg)

			var logcleanup daemon.CleanupFunc
			handler, logcleanup = wrapAuditHandler("", handler, "", mfunc) // no accesslog here
			if logcleanup != nil {
				cleanupfuncs = append(cleanupfuncs, logcleanup)
			}
		}
		// Store the handler for later lookup to avoid re-initializing
		handlerResolutionMap[name] = handler
	}

	if handlerservice != nil {
		handlerServices = append(handlerServices, handlerservice)
	}
	handlerCleanups = append(handlerCleanups, cleanupfuncs...)
	return
}

func handlerForConfig(name string, cfg *config.HandlerConfig) (handler http.Handler, service daemon.Server, cleanup daemon.CleanupFunc, err error) {

	// Load the handler from a plugin
	if cfg.Plugin != "" {
		handler, cleanup, err = handlerFromPlugin(name, cfg)
		if err != nil {
			return
		}
	}

	// Fall back to look for a built-in handler
	if handler == nil {
		switch cfg.Type {
		case "ReverseProxy":
			var proxy *rproxy.OzoneProxy
			proxy, err = rproxy.NewProxy(name, cfg.Config)
			if err == nil {
				cleanup = proxy.Deinit
				srv := proxy.Service()
				if srv != nil {
					service = srv
				}
				handler = proxy
			}
		//case "NotFound":
		//	handler = http.NotFoundHandler()
		case "Redirect":
			handler, err = makeRedirectHandler(cfg.Config)
		default:
			if hinit, ok := handlerTypes[cfg.Type]; ok {
				h, c, e := hinit(name, cfg.Config, handlerByName)
				return h, nil, c, e
			}
			err = fmt.Errorf("No such Handler type: %s", cfg.Type)
		}
	}

	return
}

// handlerFromPlugin loads the plugin, it then tries to look for a HandlerTypeMap, and if not found
// it assumed the plugin as manually registered the handler type in it's init() function.
func handlerFromPlugin(name string, cfg *config.HandlerConfig) (handler http.Handler, cleanup func() error, err error) {
	abspath, e := filepath.Abs(cfg.Plugin)
	if e != nil {
		err = e
		return
	}
	p, e := plugin.Open(abspath)
	if e != nil {
		err = e
		return
	}

	s, e := p.Lookup("HandlerTypes")
	if e != nil {
		// There was no HandlerTypeMap in this plugin.
		// so the plugin must have registered any types in it's init()
		return
	}

	m := s.(*map[string]HandlerConfigureFunc)
	if m == nil {
		err = errors.New("Defect handler plugin")
	}

	f := (*m)[cfg.Type]
	if f == nil {
		err = errors.New("Defect handler plugin type initialization function")
	}

	return f(name, cfg.Config, handlerByName)
}

func makeRedirectHandler(js jconf.SubConfig) (handler http.Handler, err error) {
	cfg := new(config.RedirectHandlerConfig)
	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	handler = http.RedirectHandler(cfg.URL, cfg.Code)
	return
}
