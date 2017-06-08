package rproxy

import (
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/One-com/gone/http/vtransport"
	"github.com/One-com/gone/http/vtransport/upstream/rr"

	"github.com/One-com/gone/jconf"

	"github.com/One-com/ozone/rproxymod"
)

// VirtualTransport defines JSON for configuring a virtual backend transport.
// Multiple named virtual upstreams can be defined as an array of "scheme://host:port/" URIs
// Requests are send to the hosts in a virtual upstream in a RR manner, but if the request
// contains a RoutingKey Header a specific backend host will be used for all req. with the same
// Header for "BackendPin" duration. This can be used to ensure backend caches are used optimally.
// If a host has MaxFails errors it will be put in Quarantine and request will be routes to a different
// host.
type VirtualTransportConfig struct {
	Type             string
	Retries          int
	MaxFails         int
	Quarantine       jconf.Duration
	BackendPin       jconf.Duration
	RoutingKeyHeader string
	Upstreams        map[string][]string
}

type rrPinCache struct {
	cc rproxymod.Cache
}

func (c *rrPinCache) Set(key string, value int, ttl time.Duration) {
	var buf [8]byte
	binary.PutUvarint(buf[:], uint64(value))
	err := c.cc.Set([]byte(key), buf[:], ttl)
	if err != nil {

	}
}
func (c *rrPinCache) Get(key string) (value int) {
	bval, err := c.cc.Get([]byte(key))
	if err != nil {
		return -1
	}
	ival, _ := binary.Uvarint(bval)
	value = int(ival)
	return

}
func (c *rrPinCache) Delete(key string) {
	c.cc.Delete([]byte(key))
}

func initVirtualTransport(cc rproxymod.Cache, wrapped *http.Transport, js jconf.SubConfig) (vt *vtransport.VirtualTransport, err error) {

	cfg := new(VirtualTransportConfig)
	err = js.ParseInto(&cfg)
	if err != nil {
		return nil, err
	}

	if cfg.Type != "RoundRobin" {
		return nil, fmt.Errorf("Unknown upstream type: %s", cfg.Type)
	}

	rrcache := &rrPinCache{cc: cc}

	upstreams := make(map[string]vtransport.VirtualUpstream)
	for k, v := range cfg.Upstreams {
		var urls = make([]*url.URL, 0)
		for _, urlStr := range v {
			url, err := url.Parse(urlStr)
			if err != nil {
				return nil, err

			}
			urls = append(urls, url)
		}
		upstreams[k], err = rr.NewRoundRobinUpstream(
			rr.Targets(urls...),
			rr.PinRequestsWith(rrcache, cfg.BackendPin.Duration,
				rr.PinKeyFunc(func(req *http.Request) string {
					key := req.Header.Get(cfg.RoutingKeyHeader)
					return key
				})),
			rr.MaxFails(cfg.MaxFails),
			rr.Quarantine(cfg.Quarantine.Duration))
		if err != nil {
			return nil, err
		}
	}

	vt = &vtransport.VirtualTransport{
		Transport:   *wrapped,
		Upstreams:   upstreams,
		RetryPolicy: vtransport.Retries(cfg.Retries, 0, false),
	}
	return
}
