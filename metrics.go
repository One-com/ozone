package ozone

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/One-com/gone/log"
	"github.com/One-com/gone/metric"
	"github.com/One-com/gone/metric/sink/statsd"

	"github.com/One-com/ozone/config"
)

// A metrics server implementing the gone/daemon. Server interface,
// but not using any file descriptors
type metricsService struct {
	Addr     string // statsd server to target.
	Prefix   string
	Interval time.Duration // how often to push data to statsd
}

func loadMetricsConfig(cfg *config.MetricsConfig) (srv *metricsService, err error) {

	if cfg.Address == "" || cfg.Ident == "" {
		return
	}

	app := cfg.Application
	if app == "" {
		app = "reverse-http-proxy"
	}

	ident := cfg.Ident
	// guess my name if not set.
	if len(ident) == 0 {
		var e error
		ident, e = os.Hostname()
		if e != nil {
			ident = "unknown"
		} else {
			parts := strings.Split(ident, ".")
			ident = parts[0]
		}
	}
	prefix := app + "." + ident
	if cfg.Prefix != "" {
		prefix = cfg.Prefix
	}

	srv = &metricsService{
		Addr:     cfg.Address,
		Prefix:   prefix,
		Interval: cfg.Interval.Duration,
	}

	return
}

func (ms *metricsService) Description() string {
	return "Metrics"
}

func (ms *metricsService) Serve(ctx context.Context) (err error) {

	statsd_host := ms.Addr

	statsd_interval := ms.Interval
	if statsd_interval <= time.Second {
		statsd_interval = time.Second
	}

	if statsd_host == "" {
		log.Println("Not sending metrics")
		return
	}

	var output statsd.Option
	if statsd_host == "!" {
		output = statsd.Output(os.Stdout)
	} else {
		output = statsd.Peer(statsd_host)
	}

	sink, err := statsd.New(
		output,
		statsd.Prefix(ms.Prefix),
		statsd.Buffer(1432))
	if err != nil {
		log.ERROR("Error initializing statsd sink", "err", err)
		return
	}

	log.Printf("Sending metrics (interval %s) for \"%s\" to %s\n", statsd_interval, ms.Prefix, statsd_host)

	metric.SetDefaultOptions(metric.FlushInterval(statsd_interval))

	// Activate draining metrics to statsd
	metric.SetDefaultSink(sink)

	metric.Start()

	<-ctx.Done()

	metric.Stop() // block until all have flushed

	return
}
