CGO ?= 1
PLUGIN ?= nous-portal
PLUGIN_DIR := plugins/$(PLUGIN)
SO := $(PLUGIN).so
HDR := $(PLUGIN).h
DIST := dist

.PHONY: all build verify clean dist arch-% help

all: build

help:
	@echo "Usage:"
	@echo "  make build          Build plugin for current host arch"
	@echo "  make verify         Build + run C verifier"
	@echo "  make dist           Build all release arches"
	@echo "  make arch-linux-amd64"
	@echo "  make arch-linux-arm64"
	@echo "  make arch-linux-arm"
	@echo "  make clean"

build:
	cd $(PLUGIN_DIR) && CGO_ENABLED=$(CGO) go build -buildmode=c-shared -o ../$(SO) .

verify: build
	gcc -D'SO="$(CURDIR)/$(SO)"' -O2 -o verify verify.c -ldl
	./verify

clean:
	rm -f $(SO) $(HDR) verify
	rm -rf $(DIST)

$(DIST):
	mkdir -p $(DIST)

arch-linux-amd64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o $(DIST)/$(PLUGIN)-linux-amd64.so ./$(PLUGIN_DIR)

arch-linux-arm64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o $(DIST)/$(PLUGIN)-linux-arm64.so ./$(PLUGIN_DIR)

arch-linux-arm: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -buildmode=c-shared -o $(DIST)/$(PLUGIN)-linux-arm.so ./$(PLUGIN_DIR)

dist: arch-linux-amd64 arch-linux-arm64 arch-linux-arm
