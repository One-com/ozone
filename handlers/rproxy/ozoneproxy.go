package rproxy

import (
	"context"
	"errors"
	"net"

	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"strings"
	"time"

	werr "github.com/pkg/errors"

	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"

	"github.com/One-com/gone/netutil/reaper"

	"github.com/One-com/ozone/rproxymod"
	"github.com/One-com/ozone/tlsconf"

	"github.com/One-com/ozone/config"

	"github.com/One-com/ozone/handlers/rproxy/cache"
)

// PLUGINPATH can be set by linker to define a default proxy module dir
var PLUGINPATH string

// RIDKEY can be set by linker to define the default log key for request id (default to "rid")
var RIDKEY string

// RIDHEADER can be by linker to define the default HTTP header for request id (Defaults to "X-Request-ID")
var RIDHEADER string

func init() {
	if RIDKEY == "" && RIDHEADER == "" {
		RIDKEY = "rid"
		RIDHEADER = "X-Request-ID"
	}
}

// TransportConfig defines JSON for configuring the HTTP transport used by the proxy.
// Type "Virtual" enabled the virtual transport which recognizes the URI scheme "vt:<name>" to
// refer to a named target cluster of multiple backends as destination.
type TransportConfig struct {
	Type                string
	Config              *jconf.OptionalSubConfig
	TLS                 *tlsconf.TLSClientConfig
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	IdleConnTimeout     jconf.Duration

	// close the connection after this amount of time without traffic.
	IOActivityTimeout jconf.Duration

	TLSHandshakeTimeout   jconf.Duration
	ResponseHeaderTimeout jconf.Duration
	DisableKeepAlives     bool
	DisableCompression    bool
}

// ModuleConfig configures a rproxy module.
type ModuleConfig struct {
	Type   string
	Config *jconf.MandatorySubConfig
}

// ProxyConfig defines JSON configuration for the "ReverseProxy" handler type
type ProxyConfig struct {
	Transport   *TransportConfig
	ModuleDir   string
	Modules     map[string]ModuleConfig
	ModuleOrder []string
	Cache       *jconf.OptionalSubConfig
}

// OzoneProxy is an HTTP Handler that takes an incoming request and
// sends it to another server, proxying the response back to the
// client.
// It's build upon the standard library ReverseProxy, but allows
// for plugin modules to process incoming requests and to modify
// responses and to proxy to a "virtual transport" consisting of
// multiple backend HTTP servers. Incoming request processing
// modules can pick a destination like the stdlib "Director" function
// but in general control every aspect of the proxy behavior.
type OzoneProxy struct {
	reverseProxy
	modules []rproxymod.ProxyModule
	cache   rproxymod.Cache
	service func(context.Context) error
}

// NewProxy instantiates a new reverse proxy handler based on the provided JSON config
func NewProxy(name string, js jconf.SubConfig) (proxy *OzoneProxy, err error) {

	var cfg *ProxyConfig
	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	// Load available modules
	if cfg.ModuleDir == "" {
		cfg.ModuleDir = PLUGINPATH
	}
	err = scanPluginDir(cfg.ModuleDir)
	if err != nil {
		log.ERROR("Plugin scan failed", "err", err)
		return
	}

	var cc rproxymod.Cache
	cc, err = cache.NewCache(cfg.Cache)
	if err != nil {
		return
	}

	var tCfg = cfg.Transport
	var tlsCfg *tls.Config

	var responseHeaderTimeout time.Duration
	var tlsHandshakeTimeout time.Duration
	var maxIdleConns int
	var maxIdleConnsPerHost int
	var idleConnTimeout time.Duration
	var ioActivityTimeout time.Duration

	var disableKeepAlives bool
	var disableCompression bool

	var transportType string
	if tCfg != nil {

		responseHeaderTimeout = tCfg.ResponseHeaderTimeout.Duration
		tlsHandshakeTimeout = tCfg.TLSHandshakeTimeout.Duration

		idleConnTimeout = tCfg.IdleConnTimeout.Duration
		ioActivityTimeout = tCfg.IOActivityTimeout.Duration
		maxIdleConns = tCfg.MaxIdleConns
		maxIdleConnsPerHost = tCfg.MaxIdleConnsPerHost

		disableKeepAlives = tCfg.DisableKeepAlives
		disableCompression = tCfg.DisableCompression

		transportType = tCfg.Type
		if transportType == "" {
			transportType = "Default"
		} else {
			transportType = tCfg.Type
		}
		if tCfg.TLS != nil {
			tlsCfg, err = tlsconf.GetTLSClientConfig(tCfg.TLS)
			if err != nil {
				return nil, err
			}
		}
	} else {
		transportType = "Default"
	}

	dialer := &net.Dialer{
		// TODO: This needs to be done properly... revert to old code in the mean time.
		Timeout:   60 * time.Second,
		KeepAlive: 60 * time.Second,
	}

	defaultTransport := &http.Transport{
		Dial:                dialer.Dial,
		TLSClientConfig:     tlsCfg,
		MaxIdleConns:        maxIdleConns,
		MaxIdleConnsPerHost: maxIdleConnsPerHost,
		IdleConnTimeout:     idleConnTimeout,

		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		DisableKeepAlives:     disableKeepAlives,
		DisableCompression:    disableCompression,
	}

	if ioActivityTimeout != time.Duration(0) {
		reaperInterval := time.Duration(ioActivityTimeout.Nanoseconds()/2) * time.Nanosecond
		ioActivityTimeoutDialer := reaper.NewIOActivityTimeoutDialer(dialer, ioActivityTimeout, reaperInterval, true)

		defaultTransport.Dial = ioActivityTimeoutDialer.Dial
	}

	var transport http.RoundTripper
	var service func(context.Context) error // if non-nil, should be called to perform autonomous handler activity
	switch transportType {
	case "Virtual":
		transport, service, err = initVirtualTransport(cc, defaultTransport, cfg.Transport.Config)
		if err != nil {
			return nil, err
		}
	case "Default":
		transport = defaultTransport
	default:
		fmt.Printf("Unknown transport: %s", transportType)
	}

	// TODO read timeouts from the configuration object
	moduleCount := len(cfg.ModuleOrder)
	modules := make([]rproxymod.ProxyModule, moduleCount)

	//var mod_names string
	for i, modName := range cfg.ModuleOrder {
		log.DEBUG(fmt.Sprintf("Adding proxy module \"%s\"", modName))
		var mod rproxymod.ProxyModule
		modCfg, ok := cfg.Modules[modName]
		if !ok {
			err = fmt.Errorf("No such module: %s", modName)
			log.CRIT(err.Error())
			err = config.WrapError(err)
			return
		}
		mod, err = proxyModuleFor(&modCfg)
		if err != nil {
			err = werr.Wrapf(err, "Configuring module %s", modCfg.Type)
			err = config.WrapError(err)
			return
		}
		modules[i] = mod
		//mod_names += modName + ","
		log.DEBUG(fmt.Sprintf("Successfully added proxy module \"%s\" (%s)", modName, modCfg.Type))
	}

	proxy = &OzoneProxy{
		reverseProxy: reverseProxy{
			Transport: transport,
			// We can ignore Director - it will never get called.
		},
		modules: modules,
		//name:    name + "[" + mod_names + "]",
		cache: cc,
		service : service,
	}

	return proxy, nil
}

// An object representing any autonomous activity the proxy handler might
// perform - like health check on backends.
type proxyservice struct{
	service func(context.Context) error
}

// Service returns any activity object the proxy might want to perform.
// To start the activity, call Serve(context) on the returned object.
// The activity will run to the context is canceled.
func (p *OzoneProxy) Service() *proxyservice {
	if p.service != nil {
		return &proxyservice{service: p.service}
	}
	return nil
}

// Serve makes the proxyservice perform its activity.
// This can be used to monitor backend health.
func (p *proxyservice) Serve(ctx context.Context) error {
	return p.service(ctx)
}

func (p *proxyservice) Description() string {
	return "Ozone proxy backend health monitor"
}

// ServeHTTP implements stdlib http.Handler
func (p *OzoneProxy) ServeHTTP(rw http.ResponseWriter, req *http.Request) {

	DEBUGlogf, debug := log.DEBUGok()

	transport := p.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	var notifyChan <-chan bool
	var propagateCancel bool
	ctx := req.Context()
	if cn, ok := rw.(http.CloseNotifier); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		notifyChan = cn.CloseNotify()
		go func() {
			select {
			case <-notifyChan:
				cancel()
			case <-ctx.Done():
			}
		}()
		propagateCancel = true
	}

	outreq := req.WithContext(ctx) // includes shallow copies of maps, but okay
	if req.ContentLength == 0 {
		outreq.Body = nil // Issue 16036: nil Body for http.Transport retries
	}

	/////////////////////////////////////////////////////////////////////
	// Instead of just a call to "Director" we invoke a chain of modules.

	var res *http.Response = nil
	reqCtx, err := rproxymod.NewRequestContext(p.cache, log.Default(), RIDKEY, req.Header.Get(RIDKEY))
	if err != nil {
		sendErrorResponse(rw, http.StatusInternalServerError, err)
		return
	}

	for _, mod := range p.modules {
		res, err = mod.ProcessRequest(reqCtx, req, outreq)
		if err != nil {
			sendErrorResponse(rw, http.StatusInternalServerError, err)
			return
		}
		// If module gave an alternate response, don't contact upstream
		// this would be mostly for http redirect/not-found/method-not-allowed/auth-challenge response
		if res != nil {
			if outreq.Body != nil {
				outreq.Body.Close()
			}
			if propagateCancel {
				// Check that the client is still there
				select {
				case <-notifyChan:
					if debug {
						DEBUGlogf("Client gone, skipping response")
					}
					return
				default:
				}
			}
			goto SENDRESPONSE
		}
	}

	////////////////////////////////////////////////////////////////////

	outreq.Close = false

	// Remove hop-by-hop headers listed in the "Connection" header.
	// See RFC 2616, section 14.10.
	if c := outreq.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				reqCtx.EnsureWritableHeader(outreq, req)
				outreq.Header.Del(f)
			}
		}
	}

	// Remove hop-by-hop headers to the backend. Especially
	// important is "Connection" because we want a persistent
	// connection, regardless of what the client sent to us.
	for _, h := range hopHeaders {
		if outreq.Header.Get(h) != "" {
			reqCtx.EnsureWritableHeader(outreq, req)
			outreq.Header.Del(h)
		}
	}

	if _, ok := outreq.Header["User-Agent"]; !ok {
		// explicitly disable User-Agent so it's not set to default value
		outreq.Header.Set("User-Agent", "")
	}

	if req.Header.Get(RIDHEADER) == "" {
		outreq.Header.Set(RIDHEADER, reqCtx.GetSessionId())
	}

	/////////////////////////////////////////////////////////////////////////////
	// ROUNDTRIPPING!
	// Do the proxying to the selected (virtual?) upstream

	// Do the actual backend request.
	res, err = transport.RoundTrip(outreq)

	// If the last RoundTrip failed....
	if err != nil {
		p.logf("http: proxy error: %v, URI:%s", err, outreq.URL.String())
		switch err.(type) {
		case x509.CertificateInvalidError, x509.HostnameError, x509.UnknownAuthorityError, x509.ConstraintViolationError:
			rw.WriteHeader(http.StatusBadGateway)
		default:
			// Test whether the req. was just canceled
			if outreq.Context().Err() == context.Canceled {
				http.Error(rw, err.Error(), 499) // nginx compliant client cancellation code.
			} else {
				rw.WriteHeader(http.StatusInternalServerError)
			}
		}
		return
	}

	/////////////////////////////////////////////////////////////////////////////

	// Remove hop-by-hop headers listed in the
	// "Connection" header of the response.
	if c := res.Header.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				res.Header.Del(f)
			}
		}
	}

	for _, h := range hopHeaders {
		res.Header.Del(h)
	}

	/////////////////////////////////////////////////////////////////////
	// Invoke modules in reverse.
	// Ignore the new ModifyResponse

	for i := len(p.modules) - 1; i >= 0; i-- {
		mod := p.modules[i]
		err = mod.ModifyResponse(reqCtx, req, res)
		if err != nil {
			sendErrorResponse(rw, http.StatusInternalServerError, err)
			return
		}
	}

	////////////////////////////////////////////////////////////////////

SENDRESPONSE:

	copyHeader(rw.Header(), res.Header)

	// The "Trailer" header isn't included in the Transport's response,
	// at least for *http.Transport. Build it up from Trailer.
	if len(res.Trailer) > 0 {
		trailerKeys := make([]string, 0, len(res.Trailer))
		for k := range res.Trailer {
			trailerKeys = append(trailerKeys, k)
		}
		rw.Header().Add("Trailer", strings.Join(trailerKeys, ", "))
	}

	rw.WriteHeader(res.StatusCode)
	if len(res.Trailer) > 0 {
		// Force chunking if we saw a response trailer.
		// This prevents net/http from calculating the length for short
		// bodies and adding a Content-Length.
		if fl, ok := rw.(http.Flusher); ok {
			fl.Flush()
		}
	}
	p.copyResponse(rw, res.Body)
	res.Body.Close() // close now, instead of defer, to populate res.Trailer
	copyHeader(rw.Header(), res.Trailer)
}

// Deinit is a function hook invoked when the associated HTTP server receives a shutdown signal
func (p *OzoneProxy) Deinit() error {

	var rError string
	for _, mod := range p.modules {
		err := mod.Deinit()
		if err != nil {
			rError = rError + " " + err.Error()
		}
	}
	err := p.cache.Close()
	if err != nil {
		rError = rError + " " + err.Error()
	}

	if rError != "" {
		return errors.New(rError)
	}

	return nil
}

// Helper function for all HTTP errored responses
func sendErrorResponse(rw http.ResponseWriter, status int, err error) {
	// Always close remote client connection in case of error
	rw.Header().Set("Connection", "close")

	errBody := ""
	if err != nil {
		errBody = " :: " + err.Error()
	}

	// We can have cases as granular as we like, if we wanted to
	// return custom errors for specific status codes.
	switch status {
	case http.StatusNotFound:
		http.Error(rw, http.StatusText(http.StatusNotFound)+errBody, http.StatusNotFound)
	case http.StatusGatewayTimeout:
		http.Error(rw, http.StatusText(http.StatusGatewayTimeout)+errBody, http.StatusGatewayTimeout)
	case http.StatusInternalServerError:
		http.Error(rw, http.StatusText(http.StatusInternalServerError)+errBody, http.StatusInternalServerError)
	default:
		// Catch any other errors we haven't explicitly handled
		http.Error(rw, http.StatusText(http.StatusInternalServerError)+errBody, http.StatusInternalServerError)
	}
}
