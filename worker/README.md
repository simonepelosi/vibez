# vibez token broker

A small Cloudflare Worker that signs short-lived Apple Music developer tokens,
so release binaries do not embed one.

- `GET /v1/token` with the header `X-Vibez-Client: <CLIENT_KEY>` returns
  `{"token": "<jwt>", "expires_at": <unix>}`. Tokens last 24 hours.
- The MusicKit private key (`.p8`) lives only in this Worker's secrets. It is not
  in GitHub, in the repo, or in any binary.
- Requests are rate limited per IP. The client key keeps drive-by scanners out; it
  ships in release builds, so it is not proof of identity.
- The token is not secret from whoever runs vibez (MusicKit JS needs it in the
  page). Short lifetimes and the ability to rotate are what limit a leak.

The client side is `internal/auth/devtoken.go`: it fetches a token at startup,
caches it in the user cache dir, and falls back to a cached token that has not yet
expired if the broker is unreachable.

## Deploy

```sh
cd worker
npx wrangler login
npx wrangler deploy
```

`wrangler deploy` prints the Worker URL (`https://vibez-token.<subdomain>.workers.dev`).

## Secrets

Secrets are set with `wrangler secret put` and never committed:

```sh
cd worker
npx wrangler secret put APPLE_KEY_ID
npx wrangler secret put APPLE_TEAM_ID
npx wrangler secret put APPLE_PRIVATE_KEY < /path/to/AuthKey_XXXXXXXXXX.p8
```

Generate `CLIENT_KEY` once and give the same value to the Worker and to GitHub
without printing it:

```sh
cd worker
KEY="$(openssl rand -hex 32)"
printf %s "$KEY" | npx wrangler secret put CLIENT_KEY
printf %s "$KEY" | gh secret set VIBEZ_CLIENT_KEY
unset KEY
```

Then point release builds at the Worker:

```sh
gh variable set VIBEZ_TOKEN_URL --body "https://vibez-token.<subdomain>.workers.dev/v1/token"
```

`release.yml` fails fast if `VIBEZ_TOKEN_URL` or `VIBEZ_CLIENT_KEY` is missing.

## Rotating the MusicKit key

1. Create a new MusicKit key in the Apple Developer portal.
2. `wrangler secret put APPLE_KEY_ID` and `APPLE_PRIVATE_KEY` with the new values.
3. Revoke the old key. No vibez release is needed: clients fetch a fresh token
   once their cached one is within two hours of expiring, so within a day.

## Test

```sh
node --test worker/test/token.test.mjs
```
