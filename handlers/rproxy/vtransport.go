package rproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"time"
	"sync"

	"github.com/One-com/gone/http/vtransport"
	"github.com/One-com/gone/http/vtransport/upstream/rr"

	"github.com/One-com/gone/log"
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
	Upstreams        map[string][]string
	Config           *jconf.OptionalSubConfig
	// Legacy
	MaxFails         int
	Quarantine       jconf.Duration
	BackendPin       jconf.Duration
	RoutingKeyHeader string
}

type HealthCheckConfig struct {
	Interval jconf.Duration
	URIPath  string
	Timeout  jconf.Duration
	Expect   int
}

type RRUpstreamConfig struct {
	MaxFails         int
	Quarantine       jconf.Duration
	BackendPin       jconf.Duration
	BurstFailGrace   jconf.Duration
	RoutingKeyHeader string
	HealthCheck      *HealthCheckConfig
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

func initVirtualTransport(cc rproxymod.Cache, wrapped *http.Transport, js jconf.SubConfig) (vt *vtransport.VirtualTransport, servicefunc func(context.Context) error, err error) {

	cfg := new(VirtualTransportConfig)
	err = js.ParseInto(&cfg)
	if err != nil {
		return
	}

	if cfg.Type != "RoundRobin" {
		err = fmt.Errorf("Unknown upstream type: %s", cfg.Type)
		return
	}

	// Check RR specific config
	var rrcfg *RRUpstreamConfig
	var healthuri *url.URL
	if cfg.Config != nil {
		err = cfg.Config.ParseInto(&rrcfg)
		if err != nil {
			return
		}
	}
	// If not defined, fall back to create it from legacy
	if rrcfg == nil {
		rrcfg = new(RRUpstreamConfig)
		rrcfg.BackendPin = cfg.BackendPin
		rrcfg.MaxFails = cfg.MaxFails
		rrcfg.Quarantine = cfg.Quarantine
		rrcfg.RoutingKeyHeader = cfg.RoutingKeyHeader
	} else {
		if rrcfg.HealthCheck != nil {
			// parse health check URI once and for all
			healthuri, err = url.ParseRequestURI(rrcfg.HealthCheck.URIPath)
			if err != nil {
				return
			}
		}
	}

	rrcache := &rrPinCache{cc: cc}

	logger := log.Default()

	var services []func(context.Context) error
	upstreams := make(map[string]vtransport.VirtualUpstream)
	for k, v := range cfg.Upstreams {
		var urls = make([]*url.URL, 0)
		for _, urlStr := range v {
			url, e := url.Parse(urlStr)
			if e != nil {
				err = e
				return

			}
			urls = append(urls, url)
		}

		logfunc := func(e rr.Event) {
			target := ""
			if e.Target != nil {
				target = e.Target.String()
			}
			if e.Err == nil {
				logger.Printf("Virtual Transport: %s, %s %s", k, e.Name, target)
			} else {
				logger.Printf("Virtual Transport: %s, %s %s: %s", k, e.Name, target, e.Err.Error())
			}
		}

		RROptions := []rr.RROption{
			rr.Targets(urls...),
			rr.PinRequestsWith(rrcache, rrcfg.BackendPin.Duration,
				rr.PinKeyFunc(func(req *http.Request) string {
					key := req.Header.Get(rrcfg.RoutingKeyHeader)
					return key
				})),
			rr.MaxFails(rrcfg.MaxFails),
			rr.Quarantine(rrcfg.Quarantine.Duration),
			rr.BurstFailGrace(rrcfg.BurstFailGrace.Duration),
			rr.EventCallback(logfunc),
		}

		if rrcfg.HealthCheck != nil &&
			rrcfg.HealthCheck.Timeout.Duration != 0 {

			client := &http.Client{Timeout: rrcfg.HealthCheck.Timeout.Duration }

			checkfunc := func(u *url.URL) error {
				u2 := *healthuri
				u2.Scheme = u.Scheme
				u2.Host = u.Host

				res, err := client.Get(u2.String())
				if err != nil {
					return err
				}
				res.Body.Close()
				if res.StatusCode != rrcfg.HealthCheck.Expect {
					return fmt.Errorf("Expected %d, got %d", rrcfg.HealthCheck.Expect, res.StatusCode )
				}
				return nil
			}

			healthcheckoption, service := rr.HealthCheck(
				rrcfg.HealthCheck.Interval.Duration,
				checkfunc)

			if healthcheckoption != nil {
				services = append(services, service)
				RROptions = append(RROptions, healthcheckoption)
			}
		}

		upstreams[k], err = rr.NewRoundRobinUpstream(RROptions...)

		if err != nil {
			return
		}
	}

	// Make an overall service function for all upstream monitoring health checks
	servicefunc = func(ctx context.Context) error {
		var wg sync.WaitGroup
		for s := range services {
			i := s
			wg.Add(1)
			// All these go-routines will exit when ctx is canceled.
			go func() {
				services[i](ctx)
				wg.Done()
			}()
		}
		// ... We just wait for them. We're allowed to exit without context being canceled.
		wg.Wait()
		return nil
	}

	vt = &vtransport.VirtualTransport{
		Transport:   wrapped,
		Upstreams:   upstreams,
		RetryPolicy: vtransport.Retries(cfg.Retries, 0, false),
	}
	return
}
