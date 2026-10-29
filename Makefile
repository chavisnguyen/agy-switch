.PHONY: all build install uninstall clean

all: build

build:
	go build -ldflags="-s -w" -o agy-switch .

install:
	./install.sh

uninstall:
	rm -f /opt/homebrew/bin/agy-switch ~/.local/bin/agy-switch /usr/local/bin/agy-switch
	sed -i '' '/alias agy="agy-switch run"/d' ~/.zshrc 2>/dev/null || true
	sed -i '' '/# agy-switch auto-failover wrapper/d' ~/.zshrc 2>/dev/null || true
	@echo "Uninstalled agy-switch shortcut and alias."

clean:
	rm -f agy-switch
