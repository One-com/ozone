package rproxymod

import (
	"net/http"
)

// Generic Ozone Reverse-Proxy Module
type ProxyModule interface {

	// ProcessRequest is called in sequence for each proxy module. If it returns (nil,nil) the
	// request (proxyReq) is passed down the chain to the next module until pushed to the HTTP Transport
	// Otherwise the http.Response is returned - or, if nil and err is non-nil: 500 Internal server error
	// inReq is provided for information containing the original request before module modification
	// reqCtx contains cross module context, a logger and a proxy global cache.
	ProcessRequest(reqCtx *RequestContext, inReq *http.Request, proxyReq *http.Request) (*http.Response, error)

	// ModifyResponse allows you to modify the response from the backend.
	// It's called in reverse module order.
	ModifyResponse(reqCtx *RequestContext, inReq *http.Request, resp *http.Response) error

	// Deinitilize Module, cleanup operations like close DB handles
	// Could be used to cleanup older connection during graceful configuration reloads
	Deinit() error
}

// Generic Ozone ReverseProxy Module base implementation with empty functions
// Other modules can use this as a Base and then implement required
// functionality.
type BaseModule struct{}

func (appCtx *BaseModule) ProcessRequest(reqCtx *RequestContext, inReq *http.Request, proxyReq *http.Request) (*http.Response, error) {
	return nil, nil
}

func (appCtx *BaseModule) ModifyResponse(reqCtx *RequestContext, inReq *http.Request, resp *http.Response) error {
	return nil
}

func (appCtx *BaseModule) Deinit() error {
	return nil
}
