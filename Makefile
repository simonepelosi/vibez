PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: build run test lint clean release build-with-token refresh-token install uninstall check-deps

check-deps:
ifeq ($(shell uname -s),Linux)
	@echo "Checking dependencies..."
	@pkg-config --exists gstreamer-1.0 2>/dev/null || \
		{ echo ""; \
		echo "ERROR: gstreamer-1.0 not found."; \
		echo "Please install: gstreamer, gstreamer-plugins-base, gstreamer-plugins-good, and gst-libav (or gstreamer1.0-libav / gstreamer1.0-plugins-bad)"; \
		echo ""; exit 1; }

	@echo "All dependencies found."
endif

build: check-deps
	go build -o vibez .

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
	TOKEN=$$(go run ./scripts/gen-devtoken 2>/dev/null); \
	test -n "$$TOKEN" || { echo "gen-devtoken produced no output — check credentials"; exit 1; }; \
	LDFLAGS="-X 'github.com/simone-vibes/vibez/internal/auth.devToken=$$TOKEN'"; \
	if [ -n "$$LASTFM_API_KEY" ] && [ -n "$$LASTFM_API_SECRET" ]; then \
		LDFLAGS="$$LDFLAGS -X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=$$LASTFM_API_KEY'"; \
		LDFLAGS="$$LDFLAGS -X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=$$LASTFM_API_SECRET'"; \
		echo "Embedding Last.fm API keys..."; \
	fi; \
	echo "Building with embedded token..."; \
	go build \
		-ldflags "$$LDFLAGS" \
		-o vibez . && echo "Done. Run: vibez auth login"

# release builds a release binary wired to the token broker and Last.fm API keys.
# Requires:
#   VIBEZ_TOKEN_URL, VIBEZ_CLIENT_KEY
#   LASTFM_API_KEY, LASTFM_API_SECRET (from https://www.last.fm/api/account/create)
# Usage: make release VIBEZ_TOKEN_URL=https://.../v1/token VIBEZ_CLIENT_KEY=... \
#                     LASTFM_API_KEY=... LASTFM_API_SECRET=...
release: check-deps
	@test -n "$$VIBEZ_TOKEN_URL"   || { echo "VIBEZ_TOKEN_URL is not set";   exit 1; }
	@test -n "$$VIBEZ_CLIENT_KEY"  || { echo "VIBEZ_CLIENT_KEY is not set";  exit 1; }
	@test -n "$$LASTFM_API_KEY"    || { echo "LASTFM_API_KEY is not set";    exit 1; }
	@test -n "$$LASTFM_API_SECRET" || { echo "LASTFM_API_SECRET is not set"; exit 1; }
	@set -e; \
	echo "Building release binary..."; \
	go build \
		-ldflags "-s -w \
		          -X 'github.com/simone-vibes/vibez/internal/auth.tokenURL=$$VIBEZ_TOKEN_URL' \
		          -X 'github.com/simone-vibes/vibez/internal/auth.clientKey=$$VIBEZ_CLIENT_KEY' \
		          -X 'github.com/simone-vibes/vibez/internal/lastfm.apiKey=$$LASTFM_API_KEY' \
		          -X 'github.com/simone-vibes/vibez/internal/lastfm.apiSecret=$$LASTFM_API_SECRET'" \
		-o vibez . && echo "Done."

# refresh-token generates a fresh Apple developer JWT and writes it into your
# local vibez config (apple_developer_token). Use it when the token expires
# (~30 days) and you build without an embedded token (plain `make build`).
#
# Credentials are read from the environment or a local .env file (gitignored):
#   APPLE_KEY_ID, APPLE_TEAM_ID
#   APPLE_PRIVATE_KEY       - PEM contents of the .p8 key, OR
#   APPLE_PRIVATE_KEY_FILE  - path to the .p8 key (~ is expanded)
# After refreshing, run `vibez auth login` if your user token is not set.
refresh-token:
	@set -e; \
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	if [ -z "$$APPLE_PRIVATE_KEY" ] && [ -n "$$APPLE_PRIVATE_KEY_FILE" ]; then \
		APPLE_PRIVATE_KEY="$$(cat "$$(eval echo $$APPLE_PRIVATE_KEY_FILE)")"; \
		export APPLE_PRIVATE_KEY; \
	fi; \
	test -n "$$APPLE_KEY_ID"      || { echo "APPLE_KEY_ID is not set (env or .env)";      exit 1; }; \
	test -n "$$APPLE_TEAM_ID"     || { echo "APPLE_TEAM_ID is not set (env or .env)";     exit 1; }; \
	test -n "$$APPLE_PRIVATE_KEY" || { echo "APPLE_PRIVATE_KEY or APPLE_PRIVATE_KEY_FILE is not set (env or .env)"; exit 1; }; \
	export APPLE_KEY_ID APPLE_TEAM_ID APPLE_PRIVATE_KEY; \
	go run ./scripts/gen-devtoken -write

# install rebuilds with an embedded developer token and copies the binary onto
# PATH, so the installed copy cannot silently go stale behind a newer ./vibez in
# the working tree. Credentials come from the environment or a local .env
# (gitignored), which may point at the key with APPLE_PRIVATE_KEY_FILE instead
# of inlining its contents as APPLE_PRIVATE_KEY — the same inputs refresh-token
# accepts.
#
# Embedded tokens expire after 30 days, so re-run this when the catalog starts
# returning 401s. Override the location with: make install PREFIX=/usr/local
install:
	@set -e; \
	if [ -f .env ]; then set -a; . ./.env; set +a; fi; \
	if [ -z "$$APPLE_PRIVATE_KEY" ] && [ -n "$$APPLE_PRIVATE_KEY_FILE" ]; then \
		key_file="$$(eval echo $$APPLE_PRIVATE_KEY_FILE)"; \
		test -f "$$key_file" || { echo "APPLE_PRIVATE_KEY_FILE not found: $$key_file"; exit 1; }; \
		APPLE_PRIVATE_KEY="$$(cat "$$key_file")"; \
	fi; \
	export APPLE_KEY_ID APPLE_TEAM_ID APPLE_PRIVATE_KEY LASTFM_API_KEY LASTFM_API_SECRET; \
	$(MAKE) --no-print-directory build-with-token; \
	install -d "$(BINDIR)"; \
	install -m 755 vibez "$(BINDIR)/vibez"; \
	echo "Installed vibez to $(BINDIR)/vibez"

uninstall:
	rm -f "$(BINDIR)/vibez"

run: build
	./vibez

test:
	go test ./...

lint:
	golangci-lint run

clean:
	rm -f vibez
