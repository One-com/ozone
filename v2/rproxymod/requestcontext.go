package rproxymod

import (
	"crypto/rand"
	"fmt"
	"net/http"

	"github.com/One-com/gone/log"
)

// RequestContext is used to pass additional across the chain of rproxy modules.
type RequestContext struct {
	sessionId   string
	sessionInfo map[string]string

	// CtxCache provides cross-request caching
	copiedHeaders bool

	// Log will attach request session id to all log output
	Log *log.Logger
}

// NewRequestContext is used by the reverse proxy to create a new context for each request
func NewRequestContext(logger *log.Logger, key, id string) (*RequestContext, error) {
	var err error
	if id == "" {
		id, err = getNextUUID()
		if err != nil {
			return nil, err
		}
	}
	si := make(map[string]string)
	ctx := &RequestContext{
		sessionId:   id,
		sessionInfo: si,
		Log:         logger.With(key, id),
	}

	return ctx, nil
}

// GetSessionID returns the request session id
func (ctx *RequestContext) GetSessionId() string {
	return ctx.sessionId
}

// GetRequestCtxInfoKeys return the session info keys
func (ctx *RequestContext) GetRequestCtxInfoKeys() []string {
	var keys []string
	for k := range ctx.sessionInfo {
		keys = append(keys, k)
	}
	return keys
}

// GetRequestCtxInfo
func (ctx *RequestContext) GetRequestCtxInfo(k string) string {
	v, ok := ctx.sessionInfo[k]
	if !ok {
		return ""
	}
	return v
}

func (ctx *RequestContext) SetRequestCtxInfo(k, v string) {
	ctx.sessionInfo[k] = v
}

func (ctx *RequestContext) EnsureWritableHeader(outgoing, incomming *http.Request) {
	if !ctx.copiedHeaders {
		outgoing.Header = make(http.Header)
		for k, vv := range incomming.Header {
			for _, v := range vv {
				outgoing.Header.Add(k, v)
			}
		}
		ctx.copiedHeaders = true
	}
}

// UUID util function from crypto/rand, though ideally to be replaced with a more optimal library
func getNextUUID() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
