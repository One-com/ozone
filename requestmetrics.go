package ozone

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/One-com/gone/log"
	"github.com/One-com/gone/metric"

	"github.com/One-com/gone/http/handlers/accesslog"
	"github.com/One-com/gone/http/rrwriter"
)

type meter interface {
	Measure(rrwriter.RecordingResponseWriter)
}

type status_meter struct {
	test  func(code int) bool
	meter *metric.Counter
}

func (m *status_meter) Measure(rec rrwriter.RecordingResponseWriter) {
	status := rec.Status()
	if m.test(status) {
		m.meter.Inc(1)
	}
}

type size_meter struct {
	meter metric.Histogram
}

func (m *size_meter) Measure(rec rrwriter.RecordingResponseWriter) {
	size := rec.Size()
	m.meter.Sample(int64(size))
}

// make a function testing status code for exact value
func exactCodeTest(val int) func(int) bool {
	return func(code int) bool {
		return val == code
	}
}

// make a function testing status code for being in range.
// val is 100,200,300....
func rangeCodeTest(val int) func(int) bool {
	return func(code int) bool {
		diff := code - val
		return diff >= 0 && diff < 100
	}
}

// Creates an accesslog.AuditFunction based on the provided metrics spec, which
// increments metrics counters
func metricsFunction(name, spec string) accesslog.AuditFunction {

	var meters []meter

	spcs := strings.Split(spec, ",")
	for _, spc := range spcs {
		matched_ddd, _ := regexp.MatchString("\\d\\d\\d", spc)
		matched_dxx, _ := regexp.MatchString("\\d[xX]{2}", spc)
		switch {
		case spc == "size":
			log.DEBUG("Creating size metric")
			meter := metric.RegisterHistogram(name + ".resp-size")
			meters = append(meters, &size_meter{meter: meter})
		case matched_ddd:
			i, _ := strconv.Atoi(spc)
			log.DEBUG("Creating status metric", "code", spc)
			meter := metric.RegisterCounter(name + ".code." + spc)
			meters = append(meters, &status_meter{test: exactCodeTest(i), meter: meter})
		case matched_dxx:
			i, _ := strconv.Atoi(spc[0:1])
			log.DEBUG("Creating status metric", "code", spc)
			meter := metric.RegisterCounter(name + ".code." + spc)
			meters = append(meters, &status_meter{test: rangeCodeTest(i * 100), meter: meter})
		}
	}

	return accesslog.AuditFunction(func(rec rrwriter.RecordingResponseWriter) {
		for _, mt := range meters {
			mt.Measure(rec)
		}
	})
}
