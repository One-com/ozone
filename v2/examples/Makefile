GO ?= go

examples: minimal options handlers tlsplugin

%: %.go
	$(GO) build $(GOFLAGS) $<

handlers: handlers.go plugins/handler/hello/main.go
	$(GO) build $(GOFLAGS) handlers.go
	$(GO) build -buildmode=plugin -o hello.so plugins/handler/hello/main.go
	@echo "Run handlers -c handlers.json"

tlsplugin: minimal plugins/tls/autocert/main.go
	$(GO) build -buildmode=plugin -o autocert.so plugins/tls/autocert/main.go
	@echo "Run miminal -c tlsplugin.json"

clean:
	rm -f minimal options handlers
