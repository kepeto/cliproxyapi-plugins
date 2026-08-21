CGO ?= 1
PLUGINS := nous-portal nous-portal-free opencode-free kilo-free
DIST := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -ldflags "-X main.pluginVersion=$(VERSION)"

.PHONY: all build verify clean dist arch-% help $(PLUGINS) deploy verify-deploy

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
	@echo "  make deploy         Build + install to CPA plugin dir (VERSION from git)"
	@echo "  make verify-deploy  Check embedded version == filename for installed plugins"

build: $(PLUGINS)

$(PLUGINS):
	cd plugins/$@ && CGO_ENABLED=$(CGO) go build $(LDFLAGS) -buildmode=c-shared -o $@.so .

verify: build
	gcc -D'SO="$(CURDIR)/plugins/nous-portal/nous-portal.so"' -O2 -o verify verify.c -ldl
	./verify

clean:
	rm -f plugins/*/*.so plugins/*/*.h verify
	rm -rf $(DIST)

# Install location of the CPA host; overridable for tests.
PLUGIN_DIR ?= $(HOME)/.cli-proxy-api/plugins/linux/amd64
DEPLOY_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0 2>/dev/null | sed 's/^v//' || echo dev)

# Single entry point for local installs: builds with the release version,
# copies under the matching name, then verifies embedded == filename. Never
# hand-copy .so files — a renamed file is how version mismatch happens.
deploy:
	@$(MAKE) --no-print-directory VERSION=$(DEPLOY_VERSION) build
	@for plugin in $(PLUGINS); do \
		cp plugins/$$plugin/$$plugin.so $(PLUGIN_DIR)/$$plugin-v$(DEPLOY_VERSION).so || exit 1; \
	done
	@$(MAKE) --no-print-directory DEPLOY_VERSION=$(DEPLOY_VERSION) verify-deploy
# Fails loudly if any installed .so does not embed its own filename version.
verify-deploy:
	@fail=0; \
	for f in $(PLUGIN_DIR)/*.so; do \
		expected=$$([ -f "$$f" ] && basename $$f | sed -E 's/.*-v?([0-9][0-9.]*)\.so/\1/'); \
		embedded=$$(strings "$$f" | grep -m1 -E "^$$expected$$"); \
		if [ "$$embedded" != "$$expected" ]; then \
			echo "MISMATCH: $$(basename $$f) embeds '$${embedded:-nothing}'"; fail=1; \
		fi; \
	done; \
	if [ $$fail -eq 0 ]; then echo "all installed plugins embed their filename version"; fi; \
	exit $$fail

$(DIST):
	mkdir -p $(DIST)

arch-linux-amd64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-amd64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-amd64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-amd64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-amd64.so ./plugins/kilo-free

arch-linux-arm64: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm64.so ./plugins/kilo-free

arch-linux-arm: $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm.so ./plugins/kilo-free

dist: arch-linux-amd64 arch-linux-arm64 arch-linux-arm
