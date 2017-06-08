package ozone

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/One-com/gone/daemon"
	nshttp "github.com/One-com/gone/http"
	"github.com/One-com/gone/log"
	"github.com/One-com/gone/log/syslog"
	"github.com/One-com/gone/netutil"
	stdlog "log"

	"github.com/One-com/ozone/config"

	"github.com/One-com/gone/netutil/reaper"
)

// Callback turning access logging on/off for use as a http.Server.ConnState callback
func toggleIOActivityTimeout(conn net.Conn, state http.ConnState) {
	switch state {
	case http.StateNew, http.StateActive:
		reaper.IOActivityTimeout(conn, true)
	case http.StateIdle, http.StateClosed, http.StateHijacked:
		reaper.IOActivityTimeout(conn, false)
	}
}

// newHTTPServer creates a deamon.Server complying HTTP server from config with the given handler
func newHTTPServer(name string, cfg config.HTTPServerConfig, snis *tlsPluginRegistry, handler http.Handler) (srv *nshttp.Server, err error) {

	var listeners daemon.ListenerGroup

	var usingReaper bool
	for _, lcfg := range cfg.Listeners { // ignore name
		addr := lcfg.Address + ":" + strconv.Itoa(lcfg.Port)

		var tlsCfg *tls.Config
		if lcfg.TLS != nil {
			tlsCfg, err = getTLSServerConfigWithPlugin(lcfg.TLS, snis)
			if err != nil {
				return nil, err
			}
			if tlsCfg == nil {
				log.ERROR("TLS requested, but not configured")
			} else {
				log.DEBUG("TLS Config", "ciphers", log.Lazy(func() interface{} { return fmt.Sprint(tlsCfg.CipherSuites) }))
				if tlsCfg.GetCertificate == nil && len(tlsCfg.Certificates) == 0 {
					log.WARN("No Server certificates and no SNI callback enabled")
				}
			}
		}

		listener := daemon.ListenerSpec{
			Addr:           addr,
			TLSConfig:      tlsCfg,
			ListenerFdName: lcfg.SocketFdName,
			InheritOnly:    lcfg.SocketInheritOnly,
		}

		if lcfg.IOActivityTimeout.Duration != time.Duration(0) {
			usingReaper = true
			to := lcfg.IOActivityTimeout.Duration
			reaperInterval := to / time.Duration(2)

			listener.PrepareListener = func(lin net.Listener) (lout net.Listener) {
				lout = reaper.NewIOActivityTimeoutListener(lin, to, reaperInterval)
				return
			}
		}

		listeners = append(listeners, listener)
	}

	var configuredListeners netutil.StreamListener
	if len(listeners) != 0 {
		configuredListeners = listeners
	}

	// Set up a log adapter for stdlib HTTP server.
	errorGonelog := log.NewStdlibAdapter(log.GetLogger(name), syslog.LOG_CRIT)
	errorlog := stdlog.New(errorGonelog, "", 0)

	// Assemble the HTTP server object.
	httpserver := &http.Server{
		Handler:           handler,
		ErrorLog:          errorlog,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout.Duration,
		IdleTimeout:       cfg.IdleTimeout.Duration,
		ReadTimeout:       cfg.ReadTimeout.Duration,
		WriteTimeout:      cfg.WriteTimeout.Duration,
	}
	if usingReaper {
		httpserver.ConnState = toggleIOActivityTimeout
	}
	if cfg.DisableKeepAlives {
		httpserver.SetKeepAlivesEnabled(false)
	}
	srv = &nshttp.Server{
		Name:      name,
		Server:    httpserver,
		Listeners: configuredListeners,
	}

	return
}
