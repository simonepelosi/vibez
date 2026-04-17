GOFLAGS=-tags webkit2gtk_4_1
PKG_CONFIG_PATH=$(CURDIR)/pkg-config

.PHONY: build run test lint clean release build-with-token

build:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build -o vibez .

# build-with-token embeds the Apple developer token without obfuscation.
# Use this for local testing of the embedded-token flow.
# Requires: APPLE_KEY_ID, APPLE_TEAM_ID, APPLE_PRIVATE_KEY
# Optional: LASTFM_API_KEY, LASTFM_API_SECRET (embed Last.fm keys for testing)
build-with-token:
	@test -n "$$APPLE_KEY_ID"      || { echo "APPLE_KEY_ID is not set";      exit 1; }
	@test -n "$$APPLE_TEAM_ID"     || { echo "APPLE_TEAM_ID is not set";     exit 1; }
	@test -n "$$APPLE_PRIVATE_KEY" || { echo "APPLE_PRIVATE_KEY is not set"; exit 1; }
	@set -e; \
	echo "Generating developer token..."; \
	TOKEN=$$(GOFLAGS= go run ./scripts/gen-devtoken 2>/dev/null); \
	test -n "$$TOKEN" || { echo "gen-devtoken produced no output — check credentials"; exit 1; }; \
	LDFLAGS="-X 'github.com/simone-vibes/vibez/internal/auth.devToken=$$TOKEN'"; \
	if [ -n "$$LASTFM_API_KEY" ] && [ -n "$$LASTFM_API_SECRET" ]; then \
		LDFLAGS="$$LDFLAGS -X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=$$LASTFM_API_KEY'"; \
		LDFLAGS="$$LDFLAGS -X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=$$LASTFM_API_SECRET'"; \
		echo "Embedding Last.fm API keys..."; \
	fi; \
	echo "Building with embedded token..."; \
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go build \
		-ldflags "$$LDFLAGS" \
		-o vibez . && echo "Done. Run: vibez auth login"

# release embeds the Apple developer token into the binary and obfuscates it
# with garble so it does not appear in plaintext. Requires:
#   APPLE_KEY_ID, APPLE_TEAM_ID, APPLE_PRIVATE_KEY (contents of the .p8 file)
#   LASTFM_API_KEY, LASTFM_API_SECRET (from https://www.last.fm/api/account/create)
# Usage: make release APPLE_KEY_ID=... APPLE_TEAM_ID=... APPLE_PRIVATE_KEY="$(cat AuthKey.p8)" \
#                     LASTFM_API_KEY=... LASTFM_API_SECRET=...
release:
	@command -v garble >/dev/null 2>&1 || { echo "garble not found — install with: go install mvdan.cc/garble@latest"; exit 1; }
	@test -n "$$APPLE_KEY_ID"      || { echo "APPLE_KEY_ID is not set";      exit 1; }
	@test -n "$$APPLE_TEAM_ID"     || { echo "APPLE_TEAM_ID is not set";     exit 1; }
	@test -n "$$APPLE_PRIVATE_KEY" || { echo "APPLE_PRIVATE_KEY is not set"; exit 1; }
	@test -n "$$LASTFM_API_KEY"    || { echo "LASTFM_API_KEY is not set";    exit 1; }
	@test -n "$$LASTFM_API_SECRET" || { echo "LASTFM_API_SECRET is not set"; exit 1; }
	@set -e; \
	echo "Generating developer token..."; \
	TOKEN=$$(GOFLAGS= go run ./scripts/gen-devtoken 2>/dev/null); \
	test -n "$$TOKEN" || { echo "gen-devtoken produced no output — check credentials"; exit 1; }; \
	echo "Building obfuscated release binary..."; \
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) garble -literals build \
		-ldflags "-X 'github.com/simone-vibes/vibez/internal/auth.devToken=$$TOKEN' \
		          -X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=$$LASTFM_API_KEY' \
		          -X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=$$LASTFM_API_SECRET'" \
		-o vibez . && echo "Done."

run: build
	./vibez

test:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) go test ./...

lint:
	PKG_CONFIG_PATH=$(PKG_CONFIG_PATH) golangci-lint run

clean:
	rm -f vibez
