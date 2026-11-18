# Build helpers. Plain `go build ./cmd/...` works too.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/solworktech/trackz/internal/version.Version=$(VERSION)
GOOS    ?= $(shell go env GOOS)
EXE     := $(if $(filter windows,$(GOOS)),.exe,)
# On Windows, build without a console window.
GUIFLAG := $(if $(filter windows,$(GOOS)),-H=windowsgui,)

PREFIX  ?= /usr/local

.PHONY: all trackz trackz-viz test vet clean install icons

all: tidy trackz trackz-viz

tidy:   
	go mod tidy

trackz:
	go build -trimpath -ldflags '$(LDFLAGS) $(GUIFLAG)' -o bin/trackz$(EXE) ./cmd/trackz

trackz-viz:
	go build -trimpath -ldflags '$(LDFLAGS) $(GUIFLAG)' -o bin/trackz-viz$(EXE) ./cmd/trackz-viz

test:
	go test ./...

vet:
	go vet ./...

install: all
	install -Dm755 bin/trackz $(DESTDIR)$(PREFIX)/bin/trackz
	install -Dm755 bin/trackz-viz $(DESTDIR)$(PREFIX)/bin/trackz-viz
	install -Dm644 packaging/xdg/trackz-viz.desktop $(DESTDIR)$(PREFIX)/share/applications/trackz-viz.desktop
	cd packaging/icons && find hicolor -type f -exec install -Dm644 {} $(DESTDIR)$(PREFIX)/share/icons/{} \;

# Regenerate icons (PNG, ICO, Windows resources) after editing
# internal/assets/logo.svg.
icons:
	cd tools/genassets && go run . -root ../..

clean:
	rm -rf bin
