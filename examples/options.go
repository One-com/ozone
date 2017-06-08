package main

import (
	"flag"
	"fmt"
	"github.com/One-com/gone/log"
	"github.com/One-com/gone/log/syslog"
	"github.com/One-com/ozone"
	"os"
	"runtime"
	"time"
)

var (
	printVersion bool

	VERSION   = "Not set"
	BUILDTIME = "In the past"
	REVISION  = "Unknown"
)

var (
	configFile      string
	controlSocket   string
	shutdownTimeout time.Duration
	dryrun          bool
	logLevel        int
)

func init() {
	var maxprocs int

	flag.BoolVar(&printVersion, "v", false, "Print version")
	flag.StringVar(&configFile, "c", "options.json", "Configuration file")
	flag.IntVar(&maxprocs, "j", runtime.NumCPU(), "Set GOMAXPROCS")

	flag.IntVar(&logLevel, "d", int(syslog.LOG_NOTICE), "Server syslog loglevel [0..7]")
	flag.BoolVar(&dryrun, "n", false, "Dryrun - Dump full config")
	flag.StringVar(&controlSocket, "s", "", "Path to control socket")
	flag.DurationVar(&shutdownTimeout, "g", time.Duration(0), "Default timeout (seconds) to do graceful shutdown")

	flag.Parse()

	runtime.GOMAXPROCS(maxprocs)

	log.SetLevel(syslog.Priority(logLevel))
	log.SetFlags(log.Llevel | log.Lname)
	log.AutoColoring()

	ozone.Init(ozone.DumpConfig(dryrun), ozone.ControlSocket(controlSocket), ozone.ShutdownTimeout(shutdownTimeout))

}

func main() {

	if printVersion {
		fmt.Printf("Version:     \t%s\n", VERSION)
		fmt.Printf("Revision:    \t%s\n", REVISION)
		fmt.Printf("Build date:  \t%s\n", BUILDTIME)
		fmt.Printf("Go Compiler: \t%s\n", runtime.Version())
		return
	}

	err := ozone.Main(configFile)
	if err != nil {
		os.Exit(1)
	}
}
