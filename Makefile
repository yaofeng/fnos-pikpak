GO ?= go
VERSION ?= 0.1.0
LDFLAGS := -s -w -X fnos-pikpak/internal/version.Version=$(VERSION)

.PHONY: build build-linux test genicon web fpk m0

build:
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/pikpak ./cmd/pikpak

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o target/pikpak ./cmd/pikpak
	cp target/pikpak app/pikpak

test:
	$(GO) test ./...

genicon:
	$(GO) run ./tools/genicon

web:
	cd web && npm install --no-audit --no-fund && npm run build

fpk: web build-linux genicon
	tools/bin/fnpack build --nosign
	mkdir -p dist
	f=$$(ls -t *.fpk 2>/dev/null | head -1); \
	if [ -z "$$f" ]; then echo "fnpack produced no .fpk" >&2; exit 1; fi; \
	cp "$$f" "dist/pikpak-$(VERSION)-x86.fpk"; \
	echo "dist/pikpak-$(VERSION)-x86.fpk"

# M0 deliverable: the installable package plus the two docs the user fills in.
m0: fpk
	@echo "安装包: dist/pikpak-$(VERSION)-x86.fpk"
	@echo "安装说明: docs/install-m0.md"
	@echo "报告模板: docs/m0-report.md"
