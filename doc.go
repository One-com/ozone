/*
Package ozone provide a generic HTTP server engine as a library well suited for creating
HTTP serving daemons running under Linux systemd.

The standard library provides the http.Handler and http.Server.

Ozone provides all the rest: Modularized JSON configuration, graceful and zero-downtime restarts using several restart schemes, plugable handlers, plugable TLS configuration and plugable modules to control reverse proxy request and responses, leveled logging, statsd metrics and a UNIX socket interface for controlling the process without stopping it.

With minimal "main" code you can launch your http.Handler with all the standard daemon behavior in place.

Running Ozone requires a JSON configuration file. (the file allows "//" comments). The primary structure of the file is:


   {
      "HTTP" : {  // defining HTTP servers
          "servername" : {
              "Handler" :  "handlername"  // top level HTTP handler
              "Listeners" : { ... }  // specification of server listeners.
          }
      },
      "Handlers" : {
         "handlername" : {
              "Type" : "handlertype",
               "Config" : { ... }
          }
      }
   }

Handler types are made available either by being built in, registered from code or loaded from plugins.

*/
package ozone
