CGO ?= 1
PLUGINS := nous-portal nous-portal-free opencode-free kilo-free
DIST := dist

.PHONY: all build verify clean dist arch-% help $(PLUGINS)

all: build

help:
	@echo "Usage:"
	@echo "  make build          Build all plugins for current host arch"
	@echo "  make build PLUGIN=nous-portal-free  Build single plugin"
	@echo "  make verify         Build all + run C verifier"
	@echo "  make dist           Build all release arches"
	@echo "  make arch-linux-amd64"
	@echo "  make arch-linux-arm64"
	@echo "  make arch-linux-arm"
	@echo "  make clean"

build: $(PLUGINS)

$(PLUGINS):
	cd plugins/$@ && CGO_ENABLED=$(CGO) go build -buildmode=c-shared -o $@.so .

verify: build
	gcc -D'SO="$(CURDIR)/plugins/nous-portal/nous-portal.so"' -O2 -o verify verify.c -ldl
	./verify

clean:
	rm -f plugins/*/*.so plugins/*/*.h verify
	rm -rf $(DIST)

$(DIST):
	mkdir -p $(DIST)

arch-linux-amd64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o $(DIST)/nous-portal-linux-amd64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-amd64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o $(DIST)/opencode-free-linux-amd64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -buildmode=c-shared -o $(DIST)/kilo-free-linux-amd64.so ./plugins/kilo-free

arch-linux-arm64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm64.so ./plugins/kilo-free

arch-linux-arm: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm.so ./plugins/kilo-free

dist: arch-linux-amd64 arch-linux-arm64 arch-linux-arm
