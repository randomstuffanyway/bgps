APP_PREPARE=prepare_trekking
APP_VIEWER=trekking_viewer
PKG_NAME=bgps
VERSION ?= 0.1.0
BIN_DIR=bin
DIST_DIR=dist
PKG_DIR=$(DIST_DIR)/pkg
ARM64_BIN_DIR=$(BIN_DIR)/aarch64
AARCH64_CC ?= aarch64-linux-gnu-gcc
DEB_ARCH ?= $(shell dpkg --print-architecture 2>/dev/null || echo amd64)
PACK_DIR ?= offline_pack
PACK_BASENAME ?= $(notdir $(abspath $(PACK_DIR)))

.PHONY: all build clean aarch64 aarch64-prepare aarch64-viewer deb deb-aarch64 pack-tar prepare viewer

all: build

build: prepare viewer

prepare:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP_PREPARE) ./cmd/prepare_trekking

viewer:
	mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/$(APP_VIEWER) ./cmd/trekking_viewer

aarch64: aarch64-prepare aarch64-viewer

aarch64-prepare:
	mkdir -p $(ARM64_BIN_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(ARM64_BIN_DIR)/$(APP_PREPARE) ./cmd/prepare_trekking

aarch64-viewer:
	mkdir -p $(ARM64_BIN_DIR)
	@command -v $(AARCH64_CC) >/dev/null 2>&1 || (echo "missing cross compiler: $(AARCH64_CC)" && exit 1)
	CC=$(AARCH64_CC) CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -o $(ARM64_BIN_DIR)/$(APP_VIEWER) ./cmd/trekking_viewer

deb: build
	PKGROOT=$(PKG_DIR)/$(PKG_NAME)_$(VERSION)_$(DEB_ARCH); \
	OUT=$(DIST_DIR)/$(PKG_NAME)_$(VERSION)_$(DEB_ARCH).deb; \
	rm -rf $$PKGROOT; \
	mkdir -p $$PKGROOT/DEBIAN \
		$$PKGROOT/usr/bin \
		$$PKGROOT/usr/share/applications \
		$$PKGROOT/usr/share/icons/hicolor/scalable/apps \
		$$PKGROOT/usr/share/doc/$(PKG_NAME); \
	install -m755 $(BIN_DIR)/$(APP_PREPARE) $$PKGROOT/usr/bin/$(APP_PREPARE); \
	install -m755 $(BIN_DIR)/$(APP_VIEWER) $$PKGROOT/usr/bin/$(APP_VIEWER); \
	install -m644 assets/bgps.desktop $$PKGROOT/usr/share/applications/bgps.desktop; \
	install -m644 assets/bgps.svg $$PKGROOT/usr/share/icons/hicolor/scalable/apps/bgps.svg; \
	install -m644 README.md $$PKGROOT/usr/share/doc/$(PKG_NAME)/README.md; \
	printf 'Package: $(PKG_NAME)\nVersion: $(VERSION)\nSection: utils\nPriority: optional\nArchitecture: $(DEB_ARCH)\nMaintainer: bgps builder\nDepends: libc6, libx11-6\nDescription: Offline trekking map prep and GPS viewer\n Offline GPX pack builder and USB GPS trekking viewer.\n' > $$PKGROOT/DEBIAN/control; \
	dpkg-deb --root-owner-group --build $$PKGROOT $$OUT

deb-aarch64: aarch64
	PKGROOT=$(PKG_DIR)/$(PKG_NAME)_$(VERSION)_arm64; \
	OUT=$(DIST_DIR)/$(PKG_NAME)_$(VERSION)_arm64.deb; \
	rm -rf $$PKGROOT; \
	mkdir -p $$PKGROOT/DEBIAN \
		$$PKGROOT/usr/bin \
		$$PKGROOT/usr/share/applications \
		$$PKGROOT/usr/share/icons/hicolor/scalable/apps \
		$$PKGROOT/usr/share/doc/$(PKG_NAME); \
	install -m755 $(ARM64_BIN_DIR)/$(APP_PREPARE) $$PKGROOT/usr/bin/$(APP_PREPARE); \
	install -m755 $(ARM64_BIN_DIR)/$(APP_VIEWER) $$PKGROOT/usr/bin/$(APP_VIEWER); \
	install -m644 assets/bgps.desktop $$PKGROOT/usr/share/applications/bgps.desktop; \
	install -m644 assets/bgps.svg $$PKGROOT/usr/share/icons/hicolor/scalable/apps/bgps.svg; \
	install -m644 README.md $$PKGROOT/usr/share/doc/$(PKG_NAME)/README.md; \
	printf 'Package: $(PKG_NAME)\nVersion: $(VERSION)\nSection: utils\nPriority: optional\nArchitecture: arm64\nMaintainer: bgps builder\nDepends: libc6, libx11-6\nDescription: Offline trekking map prep and GPS viewer\n Offline GPX pack builder and USB GPS trekking viewer.\n' > $$PKGROOT/DEBIAN/control; \
	dpkg-deb --root-owner-group --build $$PKGROOT $$OUT

pack-tar:
	mkdir -p $(DIST_DIR)
	test -d $(PACK_DIR)
	tar -czf $(DIST_DIR)/$(PACK_BASENAME).tar.gz -C $(dir $(abspath $(PACK_DIR))) $(PACK_BASENAME)

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
