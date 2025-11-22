GO ?= go

BIN_DIR := bin
PREFIX := /usr/local
BINDIR := $(PREFIX)/bin

SERVER := $(BIN_DIR)/fas
CLIENT := $(BIN_DIR)/fs

.PHONY: all build server client test clean install uninstall

all: build

build: server client

server:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(SERVER) ./cmd/fas

client:
	@mkdir -p $(BIN_DIR)
	$(GO) build -o $(CLIENT) ./cmd/fs

test:
	$(GO) test ./...

clean:
	@rm -rf $(BIN_DIR)

install: build
	install -d $(BINDIR)
	install -m 0755 $(SERVER) $(BINDIR)/fas
	install -m 0755 $(CLIENT) $(BINDIR)/fs

uninstall:
	rm -f $(BINDIR)/fas $(BINDIR)/fs
