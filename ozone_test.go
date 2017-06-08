package ozone

import (
	"github.com/One-com/gone/jconf"
	"github.com/One-com/gone/log"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func init() {
	log.SetOutput(ioutil.Discard)
	RegisterStaticHTTPHandler("OzoneTest", ozoneTestHandler)
	RegisterHTTPHandlerType("testhandler", createHandler)
}

func TestMain(m *testing.M) {

	Init(ControlSocket("@ozonetest"))

	os.Exit(m.Run())
}

func shutdown(t *testing.T) {
	time.Sleep(time.Second)
	addr, err := net.ResolveUnixAddr("unix", "@ozonetest")
	if err != nil {
		t.Fatal(err)
	}
	tries := 0
	for {
		time.Sleep(100 * time.Millisecond)
		stopconn, err := net.DialUnix("unix", nil, addr)
		if err == nil && stopconn != nil {
			stopconn.Write([]byte("daemon stop\n"))
			stopconn.Close() // Close the conn to not re-establish it in next test
			break
		}
		tries++
		if tries == 4 {
			t.Error("Too many shutdown tries")
		}

	}
}

var stopconfig = `{
    "HTTP" : {
        "TestServer" : {
            "Listeners" : {
                "http" : {
                    "Port" : 8180
                }
            },
            "Handler" : "NotFound"
        }
    }
}
`

func TestStop(t *testing.T) {
	var done = make(chan struct{})
	go func() {
		ozonemain(strings.NewReader(stopconfig))
		close(done)
	}()

	shutdown(t)
	<-done
}

//----------------------------------------------------

const teststring = "test ok\n"

var ozoneTestHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte(teststring))
})

var answerRequestConfig = `{
    "HTTP" : {
        "HelloServer" : {
            "Listeners" : {
                "http" : {
                    "Port" : 8180
                }
            },
            "Handler" : "OzoneTest"
        }
    }
}
`

func TestAnswerRequest(t *testing.T) {
	var done = make(chan struct{})
	go func() {
		ozonemain(strings.NewReader(answerRequestConfig))
		close(done)
	}()

	resp, err := http.Get("http://localhost:8180")
	if err != nil {
		t.Fatal(err)
	}
	data, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != teststring {
		t.Error("No ok response")
	}

	shutdown(t)
	<-done
}

//----------------------------------------------------

type handlerConfig struct {
	Reply string
}

func createHandler(name string, js jconf.SubConfig, handlerByName func(string) (http.Handler, error)) (h http.Handler, cleanup func() error, err error) {
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var cfg *handlerConfig
		err = js.ParseInto(&cfg)
		if err != nil {
			return
		}
		w.Write([]byte(cfg.Reply))
	})
	return
}

var proxyConfig = `{
    "HTTP" : {
        "ProxyServer" : {
            "Listeners" : {
                "http" : {
                    "Port" : 8180
                }
            },
            "Handler" : "theproxy"
        },
        "Server1" : {
            "Listeners" : {
                "http" : {
                    "Port" : 8181
                }
            },
            "Handler" : "handler1"
        },
        "Server2" : {
            "Listeners" : {
                "http" : {
                    "Port" : 8182
                }
            },
            "Handler" : "handler2"
        }
    },
    "Handlers" : {
        "theproxy" : {
             "Type" : "ReverseProxy",
             "Config" : {
                "Transport" : {
                    "Type" : "Virtual",
                    "Config" : {
                        "Type" : "RoundRobin",
                        "Retries" : 1,
                        "MaxFails" : 2,
                        "Quarantine": "1m",
                        "BackendPin": "10s",
                        "RoutingKeyHeader": "X-PinKey",
                        "Upstreams": {
                            "cluster" : [ "http://localhost:8181/", "http://localhost:8182/" ]
                        }
                    }
                },
                "ModuleOrder" : ["director", "rewrites"],
                "Modules": {
                    "director" : {
                        "Type": "forward_map_director",
                        "Config": {
                            "Forward": {
                                "" : "vt://cluster"
                            }
                        }
                    },
                    "rewrites" : {
                        "Type": "proxypass",
                        "Config": {
                            "RewriteHost" : false,
                            "RewriteForward" : false,
                            "RewriteReverse" : false
                        }
                    }
                }
            }
        },
        "handler1" : {
             "Type" : "testhandler",
             "Config" : {
                   "Reply" : "1"
              }
        },
        "handler2" : {
             "Type" : "testhandler",
             "Config" : {
                    "Reply" : "2"
              }
        }
    }
}
`

func TestProxy(t *testing.T) {
	var done = make(chan struct{})
	go func() {
		err := ozonemain(strings.NewReader(proxyConfig))
		if err != nil {
			t.Fatal(err)
		}
		close(done)
	}()

	time.Sleep(time.Second)

	var replies []string

	for i := 0; i < 10; i++ {
		resp, err := http.Get("http://localhost:8180")
		if err != nil {
			t.Fatal(err)
		}
		data, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}

		replies = append(replies, string(data))
	}
	shutdown(t)
	<-done

	var c1, c2 int
	var maxdiff = 2
	for _, r := range replies {
		switch r {
		case "1":
			c1++
		case "2":
			c2++
		default:
			t.Fail()
		}
	}
	if c2-c1 > maxdiff || c1-c2 > maxdiff {
		t.Error("Proxy cluster not balanced")
	}
}
