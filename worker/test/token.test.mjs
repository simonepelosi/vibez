import { test } from "node:test";
import assert from "node:assert/strict";
import { generateKeyPairSync, createPublicKey, verify } from "node:crypto";

import worker, { signToken, TOKEN_TTL_SECONDS } from "../src/index.js";

function makeEnv(overrides = {}) {
  const { privateKey } = generateKeyPairSync("ec", { namedCurve: "P-256" });
  const pem = privateKey.export({ type: "pkcs8", format: "pem" });
  return {
    pem,
    env: {
      APPLE_KEY_ID: "ABC123DEFG",
      APPLE_TEAM_ID: "TEAM123456",
      APPLE_PRIVATE_KEY: pem,
      CLIENT_KEY: "client-key",
      RATE_LIMITER: { limit: async () => ({ success: true }) },
      ...overrides,
    },
    privateKey,
  };
}

const req = (headers = {}, path = "/v1/token", method = "GET") =>
  new Request(`https://example.test${path}`, { method, headers });

test("signToken produces a JWT Apple accepts the shape of, verifiable with the public key", async () => {
  const { pem, privateKey } = makeEnv();
  const now = 1_800_000_000_000;
  const { token, exp } = await signToken({
    keyId: "ABC123DEFG",
    teamId: "TEAM123456",
    privateKeyPem: pem,
    now,
  });

  const [h, c, s] = token.split(".");
  const header = JSON.parse(Buffer.from(h, "base64url"));
  const claims = JSON.parse(Buffer.from(c, "base64url"));
  assert.deepEqual(header, { alg: "ES256", kid: "ABC123DEFG", typ: "JWT" });
  assert.equal(claims.iss, "TEAM123456");
  assert.equal(claims.iat, 1_800_000_000);
  assert.equal(claims.exp, claims.iat + TOKEN_TTL_SECONDS);
  assert.equal(exp, claims.exp);
  assert.ok(TOKEN_TTL_SECONDS <= 15777000, "must stay under Apple's six-month cap");

  const ok = verify(
    "sha256",
    Buffer.from(`${h}.${c}`),
    { key: createPublicKey(privateKey), dsaEncoding: "ieee-p1363" },
    Buffer.from(s, "base64url"),
  );
  assert.ok(ok, "signature must verify as raw r||s");
});

test("serves a token to a client with the shared header", async () => {
  const { env } = makeEnv();
  const res = await worker.fetch(req({ "x-vibez-client": "client-key" }), env);
  assert.equal(res.status, 200);
  assert.equal(res.headers.get("cache-control"), "no-store");
  const body = await res.json();
  assert.equal(body.token.split(".").length, 3);
  assert.ok(body.expires_at > Date.now() / 1000);
});

test("rejects a missing or wrong shared header", async () => {
  const { env } = makeEnv();
  assert.equal((await worker.fetch(req(), env)).status, 401);
  assert.equal((await worker.fetch(req({ "x-vibez-client": "nope" }), env)).status, 401);
});

test("rejects every request when CLIENT_KEY is unset", async () => {
  const { env } = makeEnv({ CLIENT_KEY: undefined });
  assert.equal((await worker.fetch(req({ "x-vibez-client": "" }), env)).status, 401);
});

test("returns 429 when the rate limiter says no", async () => {
  const { env } = makeEnv({ RATE_LIMITER: { limit: async () => ({ success: false }) } });
  const res = await worker.fetch(req({ "x-vibez-client": "client-key" }), env);
  assert.equal(res.status, 429);
  assert.equal(res.headers.get("retry-after"), "60");
});

test("rate limits per client IP", async () => {
  const keys = [];
  const { env } = makeEnv({
    RATE_LIMITER: { limit: async ({ key }) => (keys.push(key), { success: true }) },
  });
  await worker.fetch(req({ "x-vibez-client": "client-key", "cf-connecting-ip": "203.0.113.9" }), env);
  assert.deepEqual(keys, ["203.0.113.9"]);
});

test("only GET /v1/token is served", async () => {
  const { env } = makeEnv();
  assert.equal((await worker.fetch(req({}, "/"), env)).status, 404);
  assert.equal((await worker.fetch(req({ "x-vibez-client": "client-key" }, "/v1/token", "POST"), env)).status, 405);
});

test("a bad key yields a generic 503 and leaks nothing", async () => {
  const { env } = makeEnv({ APPLE_PRIVATE_KEY: "not a key" });
  // The module caches a signed token per isolate; import a fresh copy so an
  // earlier test's cached token cannot mask the bad key.
  const fresh = await import(`../src/index.js?bad=${Date.now()}`);
  const res = await fresh.default.fetch(req({ "x-vibez-client": "client-key" }), env);
  assert.equal(res.status, 503);
  assert.deepEqual(await res.json(), { error: "token unavailable" });
});
