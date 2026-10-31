.PHONY: all build install uninstall clean

all: build

build:
	go build -ldflags="-s -w" -o agys .

install:
	./install.sh

uninstall:
	rm -f /opt/homebrew/bin/agys /opt/homebrew/bin/agy-switch ~/.local/bin/agys ~/.local/bin/agy-switch /usr/local/bin/agys /usr/local/bin/agy-switch
	@echo "Uninstalled agys and agy-switch shortcuts."

clean:
	rm -f agys agy-switch
