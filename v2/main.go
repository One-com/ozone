package ozone

import (
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/One-com/gone/daemon"
	"github.com/One-com/gone/daemon/ctrl"
	"github.com/One-com/gone/log"
	"github.com/One-com/gone/log/syslog"
	"github.com/One-com/gone/signals"
)

func init() {
	// Default to a simple systemd compatible log on stdout
	log.Minimal()
}

// configDumper writes configuration to the supplied writer.
type configDumper interface {
	Dump(io.Writer)
}

type dryRunFunc func() (configDumper, error)

// loadConfig will generate 2 functions to act on the result on using the config to instantiate
// everything from all the subconfigs.
// Calling either of these 2 functions will parse all config and initalize a server based on it.
// Calling configF will then return the initalized server ready for running.
// Calling dryrunF will throw away the server and instead dump the total parsed config to the
// provided configDumper
func loadConfig(cfgSpec interface{}) (configF daemon.ConfigFunc, dryrunF dryRunFunc) {

	configFunc := func() (s []daemon.Server, c []daemon.CleanupFunc, newCfg configDumper, err error) {
		log.INFO("Loading config")
		s, c, newCfg, err = instantiateServersFromConfig(cfgSpec)
		if err != nil {
			var filename string
			if f, ok := cfgSpec.(string); ok {
				filename = f
			}
			log.CRIT("Error configuring services", "file", filename, "err", err)
		}
		return
	}

	configF = daemon.ConfigFunc(
		func() ([]daemon.Server, []daemon.CleanupFunc, error) {
			servers, cleanup, _, err := configFunc()
			return servers, cleanup, err
		})
	dryrunF = dryRunFunc(
		func() (configDumper, error) {
			_, _, config, err := configFunc()
			return config, err
		})
	return
}

// A generic log function for lower level code.
func serverLogFunc(level int, message string) {
	log.Default().LogFromCaller(3, syslog.Priority(level), message)
}

/**************** SIGNAL HANDLING *******************/

// onSignalExit will ask the daemon to exit without waiting for graceful shutdown.
func onSignalExit() {
	log.Println("Signal Exit")
	daemon.Exit(false)
}

// onSignalExitGraceful will ask the daemon to exit waiting for graceful shutdown
func onSignalExitGraceful() {
	log.Println("Signal Exit")
	daemon.Exit(true)
}

// onSignalReload will ask the daemon to configure a new set of server objects
// to replace the old server objects letting the old shutdown gracefully.
func onSignalReload() {
	log.Println("Signal Reload")
	daemon.Reload()
}

// onSignalRespawn will fork/exec a new daemon process, re-initialized from config
// which will then ask the old daemon process to shutdown.
func onSignalRespawn() {
	log.Println("Signal Respawn")
	daemon.ReplaceProcess(syscall.SIGTERM)
}

// onSignalIncLogLevel will increase the log level for the default logger.
func onSignalIncLogLevel() {
	log.IncLevel()
	log.Print(fmt.Sprintf("Log level: %d", log.Level()))
}

// onSignalDecLogLevel will decrease the log level for the default logger.
func onSignalDecLogLevel() {
	log.DecLevel()
	log.Print(fmt.Sprintf("Log level: %d\n", log.Level()))
}

// onSignalReopenAccessLogFiles will ask any configured HTTP handler access log files
// to be re-opened. (for external log rotation)
func onSignalReopenAccessLogFiles() {
	ReopenAccessLogFiles()
}

// HandledSignals is a map (syscall.Signal->func()) defining default
// OS signals to handle and how.
// Change this by assigning to HandledSignals before calling Init() if you need.
// Default signals:
//
//   SIGINT: Exit immediately
//   SIGTERM: Wait for shutdown timeout before closing and exiting.
//   SIGHUP: Retain open file descriptors, but configure new servers from config reusing any open sockets.
//   SIGUSR2: Respawn the process with the same arguments inheriting file descriptors. The new process will send SIGTERM to the parent once it's configured.
//   SIGTTIN: Increase log level
//   SIGTTOU: Decrease log level
//   SIGUSR1: Reopen all access log files.
//
var HandledSignals = signals.Mappings{
	syscall.SIGINT:  onSignalExit,
	syscall.SIGTERM: onSignalExitGraceful,
	syscall.SIGHUP:  onSignalReload,
	syscall.SIGUSR2: onSignalRespawn,
	syscall.SIGTTIN: onSignalIncLogLevel,
	syscall.SIGTTOU: onSignalDecLogLevel,
	syscall.SIGUSR1: onSignalReopenAccessLogFiles,
}

/******************* Options ***********************************/

type runcfg struct {
	dryrun          bool           // just call dryrunF and exit.
	controlsocket   string         // path of the UNIX control socket
	shutdowntimeout time.Duration  // default delay to wait for graceful shutdown
	readymessage    string         // Message to send over systemd notify socket when ready
}

// Option to pass to Init()
type Option func(*runcfg)

// Default Config
var cfg = runcfg{
	readymessage:    "Ready and serving",
	controlsocket:   "./ozone-control.sock",
}

// DumpConfig makes Ozone dry-run and exit after dumping the parsed configuration to os.Stdout
func DumpConfig(dryrun bool) Option {
	return Option(func(c *runcfg) {
		c.dryrun = dryrun
	})
}

// ControlSocket specifies an alternative path for the daemon
// control socket. If "", the socket is disabled.
// The socket defaults to "ozone-control.sock" in the current working directory.
func ControlSocket(socketfile string) Option {
	return Option(func(c *runcfg) {
		c.controlsocket = socketfile
	})
}

// ShutdownTimeout changes how long to wait for Servers to do graceful
// Shutdown before forcefully closing them. You can use the control socket
// to shutdown with any timeout later, but this is the default which is
// used by SIGTERM.
func ShutdownTimeout(to time.Duration) Option {
	return Option(func(c *runcfg) {
		c.shutdowntimeout = to
	})
}

// SdNotifyReadyMessage changes the default message to send to systemd
// via the notify socket.
func SdNotifyReadyMessage(msg string) Option {
	return Option(func(c *runcfg) {
		c.readymessage = msg
	})
}

/******************* Init logic ********************************/

var initOnce sync.Once

// DisableInit disables Ozones default initialization of
// Logging, and OS signals using gone/log, gone/daemon and gone/signals.
// You are on your own now to handle signal and configure logging.
func DisableInit() {
	internalInit(false)
}

// Init initializes Ozone.
// If you don't call it, it is called for you by Main(). You
// can force initialization from your init() function by calling it here.
// Init will: 1) Set the github.com/One-com/gone/daemon logger function. 2) Register a daemon controlling control socket command. 3) Start the signal handler processing HandledSignals.
// If you don't want this and control daemon and logging your self, call DisableInit early.
func Init(opts ...Option) {
	internalInit(true, opts...)
}

func internalInit(doinit bool, opts ...Option) {

	// initialize config from options
	for _, o := range opts {
		o(&cfg)
	}

	// Setup default ozone configuration unless disabled.
	// If you want something else, call DisableInit() instead of Init()
	// Any options can be passed to Main()
	// * configure logging
	// * configure basic "daemon" control socket command
	// * Run the default signal handler
	initOnce.Do(func() {

		if doinit {
			daemon.SetLogger(serverLogFunc)
			ctrl.RegisterCommand("daemon", procControl)
			signals.RunSignalHandler(HandledSignals)
		}
	})
}

// Main starts Ozone serving, by parsing the provided config file
// and serving everything defined in it by calling github.com/One-com/gone/daemon.Run().
// Main can be provided options which will overwrite any options given to Init()
func Main(filename string, opts ...Option) error {

	return ozonemain(filename, opts...)

}

// ozonemain takes config as an interface to allow for an in memory buffer during tests
func ozonemain(config interface{}, opts ...Option) error {

	Init(opts...)

	configureFunc, dryrunFunc := loadConfig(config)

	if cfg.dryrun {
		newCfg, err := dryrunFunc()
		if err != nil {
			var filename string
			if f, ok := config.(string); ok {
				filename = f
			}
			log.CRIT("Error parsing config file", "file", filename, "err", err)
			return err
		}
		newCfg.Dump(os.Stdout)
		return err
	}

	runoptions := []daemon.RunOption{
		daemon.Configurator(configureFunc),
		daemon.ControlSocket("", cfg.controlsocket),
		daemon.ShutdownTimeout(cfg.shutdowntimeout),
		daemon.SdNotifyOnReady(true, cfg.readymessage),
		daemon.SignalParentOnReady(),
	}

	log.NOTICE("Starting server", "pid", os.Getpid())

	err := daemon.Run(runoptions...)
	if err != nil {
		log.CRIT("Daemon exit error", "err", err)
	}

	log.NOTICE("Halted")
	return err
}
