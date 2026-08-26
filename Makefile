CGO ?= 1
PLUGINS := nous-portal nous-portal-free opencode-free kilo-free
DIST := dist

normalize_version = $(patsubst v%,%,$(strip $(1)))
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PLUGIN_VERSION := $(call normalize_version,$(VERSION))
LDFLAGS := -ldflags "-X main.pluginVersion=$(PLUGIN_VERSION)"

.PHONY: all build test vet fmt-check check verify clean dist arch-linux-amd64 arch-linux-arm64 arch-linux-arm help $(PLUGINS) deploy verify-deploy

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
	@echo "  Cross-arch c-shared builds require CC_ARM64/CC_ARM (default: aarch64-linux-gnu-gcc / arm-linux-gnueabihf-gcc)"
	@echo "  make deploy         Build + install to CPA plugin dir (VERSION from git)"
	@echo "  make verify-deploy  Check embedded version == filename for installed plugins"

build: $(if $(PLUGIN),$(PLUGIN),$(PLUGINS))

test:
	@for dir in shared plugins/nous-portal plugins/nous-portal-free plugins/opencode-free plugins/kilo-free; do \
		echo "== test $$dir =="; \
		(cd "$$dir" && go test ./...) || exit 1; \
	done

vet:
	@for dir in shared plugins/nous-portal plugins/nous-portal-free plugins/opencode-free plugins/kilo-free; do \
		echo "== vet $$dir =="; \
		(cd "$$dir" && go vet ./...) || exit 1; \
	done

fmt-check:
	@files="$$(find shared plugins/nous-portal plugins/nous-portal-free plugins/opencode-free plugins/kilo-free -name '*.go' -not -path '*/vendor/*' -print)"; \
	unformatted="$$(gofmt -l $$files)"; \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted Go files:"; echo "$$unformatted"; exit 1; \
	fi

check: fmt-check test vet

$(PLUGINS):
	cd plugins/$@ && CGO_ENABLED=$(CGO) go build $(LDFLAGS) -buildmode=c-shared -o $@.so .

verify:
	$(MAKE) --no-print-directory PLUGIN= build
	gcc -O2 -o verify verify.c -ldl
	@for plugin in $(PLUGINS); do \
		echo "== verify $$plugin =="; \
		./verify "$(CURDIR)/plugins/$$plugin/$$plugin.so" || exit 1; \
	done

clean:
	rm -f plugins/*/*.so plugins/*/*.h verify
	rm -rf $(DIST)

# Install location of the CPA host; overridable for tests.
CPA_CONFIG ?= $(HOME)/.cli-proxy-api/config.yaml
# CPA loads plugin shared objects directly from this directory; it does not recurse.
PLUGIN_DIR ?= $(HOME)/.cli-proxy-api/plugins
DEPLOY_VERSION ?= $(shell git describe --tags --match 'v*' --abbrev=0 2>/dev/null | sed 's/^v//' || echo dev)
DEPLOY_VERSION_NORMALIZED := $(call normalize_version,$(DEPLOY_VERSION))
CC_ARM64 ?= aarch64-linux-gnu-gcc
CC_ARM ?= arm-linux-gnueabihf-gcc

# Single entry point for local installs: builds with the release version,
# copies under the matching name, then verifies embedded == filename. Never
# hand-copy .so files — a renamed file is how version mismatch happens.
deploy:
	@case "$(DEPLOY_VERSION_NORMALIZED)" in \
		""|dev|*-dirty) echo "refusing deploy with invalid version '$(DEPLOY_VERSION_NORMALIZED)'" >&2; exit 1;; \
	esac
	@mkdir -p "$(PLUGIN_DIR)"
	@$(MAKE) --no-print-directory VERSION=$(DEPLOY_VERSION_NORMALIZED) build
	@for plugin in $(PLUGINS); do \
		cp plugins/$$plugin/$$plugin.so $(PLUGIN_DIR)/$$plugin-v$(DEPLOY_VERSION_NORMALIZED).so || exit 1; \
		find $(PLUGIN_DIR) -name "$$plugin-v*.so" ! -name "$$plugin-v$(DEPLOY_VERSION_NORMALIZED).so" -delete; \
	done
	@python3 scripts/sync_store_versions.py $(CPA_CONFIG) $(DEPLOY_VERSION_NORMALIZED) $(PLUGINS)
	@$(MAKE) --no-print-directory DEPLOY_VERSION=$(DEPLOY_VERSION_NORMALIZED) verify-deploy
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


arch-linux-amd64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-amd64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-amd64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-amd64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-amd64.so ./plugins/kilo-free

arch-linux-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(CC_ARM64) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm64.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(CC_ARM64) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm64.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(CC_ARM64) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm64.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=$(CC_ARM64) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm64.so ./plugins/kilo-free

arch-linux-arm:
	mkdir -p $(DIST)
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC=$(CC_ARM) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-linux-arm.so ./plugins/nous-portal
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC=$(CC_ARM) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/nous-portal-free-linux-arm.so ./plugins/nous-portal-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC=$(CC_ARM) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/opencode-free-linux-arm.so ./plugins/opencode-free
	CGO_ENABLED=1 GOOS=linux GOARCH=arm GOARM=7 CC=$(CC_ARM) go build $(LDFLAGS) -buildmode=c-shared -o $(DIST)/kilo-free-linux-arm.so ./plugins/kilo-free

dist: arch-linux-amd64 arch-linux-arm64 arch-linux-arm
