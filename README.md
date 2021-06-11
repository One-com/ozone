# Version2 of ozone

Cache has been removed in order to remove dependencies for CGO and now the caching is up to the client otherwise the interface is the same.

# ozone
Golang HTTP daemon engine.

Ozone is not a web framework. It's a daemon engine which will serve your http.Handler's and take care of the boilerplate to make a nice configurable well behaving daemon.

The use case for using Ozone is HTTP based micro services and infrastructure glue daemons where you know what your http.Handler should do. You just want it served with graceful zero-downtime reloads/upgrades, logging and ready made configuration integrating nicely with Linux systemd. Ozone comes with a ready made configurable and modular reverse HTTP proxy handler.

Ozone reads a JSON config file which defines HTTP servers, their listeners (any TLS configuration) and their http.Handler. Configuration is fully modular, letting you provide your own dedicated handler configuration.

Ozone can be asked to listen on a UNIX socket for commands, allowing you to control the running daemon. New commands can be implemented and registered by the application.

Ozone doesn't per default do access-logging. It can be configured to do that, but you can also just issue the "alog" command on the control socket to get access log for a specific HTTP server.

#### Feature list

- Modular plugable JSON configuration
- Plugable and configurable HTTP handlers
- Plugable reverse HTTP proxy
- Plugable TLS configuration
- Plugable UNIX socket control interface.
- Graceful restarts and zero-downtime upgrades
- Dump entire config, as parsed, to stdout in "dry run" mode.
- Tunable logging and statsd metrics.
- Client side failover for reverse proxy backends using "virtual upstream" pools of backend servers.

Ozone is build on the github.com/One-com/gone set of libraries which provide much of the functionality.

## Example

A simple 

``` go
//...declarations left out

type handlerConfig struct {
	Response string
}

func createHandler(name string, js jconf.SubConfig, handlerByName func(string) (http.Handler, error)) (h http.Handler, cleanup func() error, err error) {
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cfg *handlerConfig
		err = js.ParseInto(&cfg)
		if err != nil {
			return
		}
		w.Write([]byte(cfg.Response + "\n"))
	})
	return
}

func init() {
	flag.StringVar(&configFile, "c", "options.json", "Configuration file")

	flag.BoolVar(&dryrun, "n", false, "Dryrun - Dump full config")
	flag.StringVar(&controlSocket, "s", "", "Path to control socket")
	flag.DurationVar(&shutdownTimeout, "g", time.Duration(0), "Default timeout (seconds) to do graceful shutdown")

	flag.Parse()
	
	ozone.Init(ozone.DumpConfig(dryrun), ozone.ControlSocket(controlSocket), ozone.ShutdownTimeout(shutdownTimeout))
}

func main() {

    ozone.RegisterHTTPHandlerType("MyConfigurableHandler", createHandler)

	err := ozone.Main(configFile)
	if err != nil {
		os.Exit(1)
	}
}

```

The configuration file is structured as:

``` json
{
      "HTTP" : {  // defining HTTP servers
          "servername" : {
              "Handler" :  "handlername"  // top level HTTP handler
              "Listeners" : { ... }  // specification of server listeners.
          }
      },
      "Handlers" : {
         "handlername" : {
              "Type" : "MyConfurableHandler",
               "Config" : { ... }
          }
      }
}
```
