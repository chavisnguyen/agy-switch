.PHONY: all build install uninstall clean

VERSION ?= $(shell git describe --tags --exact-match 2>/dev/null)
LDFLAGS := -s -w $(if $(VERSION),-X main.Version=$(VERSION))

all: build

build:
	go build -ldflags="$(LDFLAGS)" -o agys .

install:
	./install.sh

uninstall:
	rm -f /opt/homebrew/bin/agys /opt/homebrew/bin/agy-switch ~/.local/bin/agys ~/.local/bin/agy-switch /usr/local/bin/agys /usr/local/bin/agy-switch
	@echo "Uninstalled agys and agy-switch shortcuts."

clean:
	rm -f agys agy-switch
