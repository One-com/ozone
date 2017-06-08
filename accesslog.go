package ozone

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/daemon/ctrl"
	"github.com/One-com/gone/log"

	"github.com/One-com/gone/http/handlers/accesslog"
)

// representing an active accesslog file and the handler logging to it.
type activeAccesslog struct {
	name     string
	filename string
	handler  accesslog.DynamicLogHandler
	writer   io.WriteCloser
}

// a global registry of all active accesslogs
var registryLock sync.Mutex
var registry map[string]*activeAccesslog

// a control socket command to control the access logs.
var accessLogControl = newAccessLogCommand(daemon.Log)

func init() {
	registry = make(map[string]*activeAccesslog)
	ctrl.RegisterCommand("alog", accessLogControl)
}

// ReopenAccessLogFiles opens the configured accesslog files and atomically replaces
// the old filehandles with the new ones - and closes the old file handles.
func ReopenAccessLogFiles() {
	registryLock.Lock()
	defer registryLock.Unlock()

	log.NOTICE("Reopening access log files")
	for _, spec := range registry {
		// Open the file again
		file, err := accessLogFile(spec.filename)
		if err != nil {
			log.ERROR("Could not reopen accesslog", "err", err, "file", spec.filename)
		}
		spec.handler.ToggleAccessLog(spec.writer, file) // swap the writer this handler is writing to
		spec.writer.Close()
		spec.writer = file
	}
}

func registerAccessLogFile(name, filename string, handler accesslog.DynamicLogHandler, w io.WriteCloser) {
	registryLock.Lock()
	defer registryLock.Unlock()
	registry[name] = &activeAccesslog{name: name, filename: filename, writer: w, handler: handler}
}

func unregisterAccessLogFile(name string, w io.Writer) {
	registryLock.Lock()
	defer registryLock.Unlock()
	delete(registry, name)
}

// wrapAuditHandler takes an http.Handler and wraps it in a accesslog capable handler which also does a callback to the provided audit function.
// it returns the resulting handler and a function to be called to cleanup when the handler is no longer in use.
func wrapAuditHandler(servername string, h http.Handler, accessLogDest string, mfunc accesslog.AuditFunction) (oh accesslog.DynamicLogHandler, cleanup daemon.CleanupFunc) {

	var out io.WriteCloser
	var err error

	oh = accesslog.NewDynamicLogHandler(h, mfunc)
	accessLogControl.RegisterLogHandler(servername, oh)

	if accessLogDest != "" {
		log.INFO("Opening logfile", "file", accessLogDest)
		out, err = accessLogFile(accessLogDest)
		if err != nil {
			log.CRIT("Unable to open access log", "file", accessLogDest, "err", err)
		}
		if out == nil {
			log.DEBUG("No access log")
		}
		if f, ok := log.DEBUGok(); ok {
			f(fmt.Sprintf("Setting up access log: %s", accessLogDest))
		}
		registerAccessLogFile(servername, accessLogDest, oh, out)
		oh.ToggleAccessLog(nil, out)
		cleanup = func() error {
			oh.ToggleAccessLog(out, nil)
			unregisterAccessLogFile(servername, out)
			log.INFO("Closing logfile", "file", accessLogDest)
			return out.Close()
		}
	}

	return
}

func accessLogFile(dest string) (file io.WriteCloser, err error) {

	if dest == "" {
		// None - should not happen
		err = fmt.Errorf("Invalid access log specification: \"\"")
		return
	}

	switch dest[0] {
	case '|':
		err = fmt.Errorf("Unimplemented access log spec: |")
		return
	case '/': // file
		fallthrough
	default:
		file, err = os.OpenFile(dest, os.O_APPEND|os.O_CREATE|os.O_WRONLY, os.ModeAppend|0640)
	}
	return
}

// -----------------------------  Control socket ------------------------------------

// A command turning on/off accesslog for registered HTTP handlers.

type accessLogCommand struct {
	mu       sync.Mutex
	handlers map[string]accesslog.DynamicLogHandler
	logger   daemon.LoggerFunc
}

func (lc *accessLogCommand) Reset() {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.handlers = make(map[string]accesslog.DynamicLogHandler)
}

func (lc *accessLogCommand) RegisterLogHandler(name string, lh accesslog.DynamicLogHandler) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.handlers[name] = lh
}

func (lc *accessLogCommand) ShortUsage() (syntax, comment string) {
	syntax = "-list | <handler>"
	comment = "Output accesslog"
	return
}

func (lc *accessLogCommand) Usage(cmd string, w io.Writer) {
	fmt.Fprintln(w, cmd, "-list       List available handlers")
	fmt.Fprintln(w, cmd, "<handler>   Output access log for this handler")
}

func (lc *accessLogCommand) Invoke(ctx context.Context, w io.Writer, cmd string, args []string) (async func(), persistent string, err error) {

	fs := flag.NewFlagSet("alog", flag.ContinueOnError)
	list := fs.Bool("list", false, "List HTTP handlers capable of access log")
	fs.SetOutput(w)
	err = fs.Parse(args)
	if err != nil {
		fmt.Fprintf(w, "Syntax error: %s", err.Error())
		return
	}

	if *list && fs.NArg() == 0 {
		lc.mu.Lock()
		for handler := range lc.handlers {
			fmt.Fprintln(w, handler)
		}
		lc.mu.Unlock()
		return
	}

	args = fs.Args()

	var hname string
	if len(args) == 1 {
		hname = args[0]
	}
	lc.mu.Lock()
	handler, ok := lc.handlers[hname]
	lc.mu.Unlock()
	if !ok {
		fmt.Fprintln(w, "No logging")
		return
	}

	argstr := strings.Join(args, " ")
	persistent = strings.Join([]string{cmd, argstr}, " ")

	async = func() {
		lc.logger(daemon.LvlINFO, "Turning on accesslog")
		handler.ToggleAccessLog(nil, w)
		<-ctx.Done()
		lc.logger(daemon.LvlINFO, "Turning off accesslog")
		handler.ToggleAccessLog(w, nil)
	}
	return
}

func newAccessLogCommand(logger daemon.LoggerFunc) *accessLogCommand {
	lc := &accessLogCommand{logger: logger}
	lc.Reset()
	return lc
}
