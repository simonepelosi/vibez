// vibez token broker.
//
// Signs short-lived Apple Music developer tokens (ES256 JWT) so the vibez
// binary does not have to ship one. The MusicKit private key lives only in
// this Worker's secrets; clients get a token that expires in TOKEN_TTL_SECONDS
// and fetch a fresh one on demand, so rotating the key or cutting off abuse
// never needs a vibez release.
//
// The token is not secret from whoever runs vibez (MusicKit JS needs it in the
// page), so the gate below limits abuse; it does not make the token private.
//
// Secrets: APPLE_KEY_ID, APPLE_TEAM_ID, APPLE_PRIVATE_KEY (PEM, PKCS#8 .p8
// contents), CLIENT_KEY (shared header value baked into release builds).

export const TOKEN_TTL_SECONDS = 24 * 60 * 60;
// A signed token is reused for this long, so a burst of clients costs one
// signature instead of one each.
const REUSE_SECONDS = 60 * 60;

const enc = new TextEncoder();

function b64url(bytes) {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function pemToDer(pem) {
  const body = pem
    .replace(/-----BEGIN [A-Z ]+-----/, "")
    .replace(/-----END [A-Z ]+-----/, "")
    .replace(/\s+/g, "");
  const bin = atob(body);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

// signToken returns an Apple developer JWT. WebCrypto's ECDSA output is the
// raw r||s form JWS ES256 requires, so it needs no DER conversion.
export async function signToken({ keyId, teamId, privateKeyPem, now = Date.now() }) {
  const key = await crypto.subtle.importKey(
    "pkcs8",
    pemToDer(privateKeyPem),
    { name: "ECDSA", namedCurve: "P-256" },
    false,
    ["sign"],
  );
  const iat = Math.floor(now / 1000);
  const exp = iat + TOKEN_TTL_SECONDS;
  const header = b64url(enc.encode(JSON.stringify({ alg: "ES256", kid: keyId, typ: "JWT" })));
  const claims = b64url(enc.encode(JSON.stringify({ iss: teamId, iat, exp })));
  const signingInput = `${header}.${claims}`;
  const sig = await crypto.subtle.sign(
    { name: "ECDSA", hash: "SHA-256" },
    key,
    enc.encode(signingInput),
  );
  return { token: `${signingInput}.${b64url(new Uint8Array(sig))}`, exp };
}

function timingSafeEqual(a, b) {
  const x = enc.encode(a);
  const y = enc.encode(b);
  if (x.length !== y.length) return false;
  let diff = 0;
  for (let i = 0; i < x.length; i++) diff |= x[i] ^ y[i];
  return diff === 0;
}

function json(body, status = 200, extra = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "content-type": "application/json",
      "cache-control": "no-store",
      "x-content-type-options": "nosniff",
      ...extra,
    },
  });
}

let cached = null; // { token, exp } for this isolate

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname !== "/v1/token") return json({ error: "not found" }, 404);
    if (request.method !== "GET") return json({ error: "method not allowed" }, 405, { allow: "GET" });

    // Shared header: keeps drive-by scanners out. It is baked into release
    // builds, so it is not proof of identity.
    const given = request.headers.get("x-vibez-client") ?? "";
    if (!env.CLIENT_KEY || !timingSafeEqual(given, env.CLIENT_KEY)) {
      return json({ error: "unauthorized" }, 401);
    }

    const ip = request.headers.get("cf-connecting-ip") ?? "unknown";
    const { success } = await env.RATE_LIMITER.limit({ key: ip });
    if (!success) return json({ error: "rate limited" }, 429, { "retry-after": "60" });

    const nowSec = Math.floor(Date.now() / 1000);
    if (!cached || cached.exp - nowSec < TOKEN_TTL_SECONDS - REUSE_SECONDS) {
      try {
        cached = await signToken({
          keyId: env.APPLE_KEY_ID,
          teamId: env.APPLE_TEAM_ID,
          privateKeyPem: env.APPLE_PRIVATE_KEY,
        });
      } catch {
        // Never echo key-handling errors to a client.
        return json({ error: "token unavailable" }, 503);
      }
    }
    return json({ token: cached.token, expires_at: cached.exp });
  },
};
