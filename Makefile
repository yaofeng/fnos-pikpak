GO ?= go
VERSION ?= 2.1.1
LDFLAGS := -s -w -X fnos-pikpak/internal/version.Version=$(VERSION)

# Signing: fnpack needs a developer key/cert to produce an installable signed
# package. Without them we build an unsigned package, which is only suitable
# for local debugging.
FN_KEY ?=
FN_CERT ?=
ifeq ($(strip $(FN_KEY)),)
SIGN_ARGS := --nosign
else
SIGN_ARGS := --key $(FN_KEY) --cert $(FN_CERT)
endif

.PHONY: build build-linux test genicon web dev set-version fpk release

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
	# Vite cannot empty outDir (it holds the committed dist/.keep), and its
	# hashed asset names mean every build would otherwise leave the previous
	# ones behind — 28 MB of stale JS in a local package. Only assets/ is
	# Vite's own output; the rest is committed.
	rm -rf internal/webdist/dist/assets
	cd web && npm install --no-audit --no-fund && npm run build

# Run the app on this machine instead of reinstalling the fpk on the NAS.
# Data (bound accounts) lives in ./.data; the fnOS-only probes skip because
# there is no gateway socket here.
dev: web build
	./dist/pikpak --listen 127.0.0.1:8787 --dev-uid 1000

# Keep the manifest's version in step with the binary and the package name, so
# an installed app reports the same version as the release it came from.
set-version:
	@awk -v v="$(VERSION)" 'BEGIN{FS=OFS="="} $$1=="version"{$$2=v; seen=1} {print} END{if(!seen){print "manifest has no version line" > "/dev/stderr"; exit 1}}' manifest > manifest.tmp
	@mv manifest.tmp manifest

fpk: web build-linux genicon set-version
	tools/bin/fnpack build $(SIGN_ARGS)
	mkdir -p dist
	f=$$(ls -t *.fpk 2>/dev/null | head -1); \
	if [ -z "$$f" ]; then echo "fnpack produced no .fpk" >&2; exit 1; fi; \
	cp "$$f" "dist/pikpak-$(VERSION)-x86.fpk"; \
	echo "dist/pikpak-$(VERSION)-x86.fpk"

# Installable package plus a pointer at the device checklist to run before a tag.
release: fpk
	@echo "安装包: dist/pikpak-$(VERSION)-x86.fpk"
	@echo "安装与真机验证清单: docs/install.md"
