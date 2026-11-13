import { RSABSSA } from "@cloudflare/blindrsa-ts";

const rsaBytes = 384;
const suite = RSABSSA.SHA384.PSS.Randomized();
const encoder = new TextEncoder();

function encode(bytes: Uint8Array) {
  return btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join(""));
}

function decode(value: string) {
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}

type PendingCredential = {
  publicKey: string;
  message: string;
  request: string;
  inverse: string;
};

async function getJSON<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, cache: "no-store", credentials: "same-origin" });
  if (response.redirected) throw new Error("Sign in again and retry with the same browser.");
  const data = await response.json();
  if (!response.ok) throw new Error(data.error ?? "Could not obtain the credential.");
  return data as T;
}

function importPublicKey(publicKey: string) {
  return crypto.subtle.importKey(
    "spki", decode(publicKey), { name: "RSA-PSS", hash: "SHA-384" }, true, ["verify"],
  );
}

export async function obtainAnonymousCredential(electionId: string, userId: string) {
  if (!window.isSecureContext || !window.crypto?.subtle) {
    throw new Error("Use HTTPS or localhost to prepare a voting credential.");
  }
  if (!/^[0-9a-f]{8}(?:-[0-9a-f]{4}){3}-[0-9a-f]{12}$/.test(electionId)) {
    throw new Error("Invalid election ID.");
  }
  // Serialize requests across tabs, and persist the blinding state before
  // issuing so a reload or a lost response can retry the exact request.
  if (!navigator.locks) throw new Error("This browser cannot safely save credential retries.");
  const storageKey = `evoting:credential:v1:${electionId}:${userId}`;
  const path = `/api/elections/${encodeURIComponent(electionId)}`;
  return navigator.locks.request(storageKey, async () => {
    const saved = localStorage.getItem(storageKey);
    const record = saved ? JSON.parse(saved) as { token?: string; pending?: PendingCredential } : {};
    if (record.token) return record.token;
    const { public_key: publishedKey } = await getJSON<{ public_key: string }>(`${path}/credential-key`);
    if (record.pending && record.pending.publicKey !== publishedKey) {
      throw new Error("The issuer key changed. Contact the election administrator.");
    }
    const key = await importPublicKey(publishedKey);
    const algorithm = key.algorithm as RsaHashedKeyAlgorithm;
    if (algorithm.modulusLength !== rsaBytes * 8 || encode(algorithm.publicExponent) !== "AQAB") {
      throw new Error("The issuer key is invalid.");
    }
    let pending = record.pending;
    if (!pending) {
      const prefix = encoder.encode(`evoting/anonymous-v1/credential/${electionId}/`);
      const nonce = crypto.getRandomValues(new Uint8Array(32));
      const message = suite.prepare(Uint8Array.from([...prefix, ...nonce]));
      const { blindedMsg, inv } = await suite.blind(key, message);
      pending = {
        publicKey: publishedKey, message: encode(message), request: encode(blindedMsg), inverse: encode(inv),
      };
      localStorage.setItem(storageKey, JSON.stringify({ pending }));
    }
    const { blind_response: response } = await getJSON<{ blind_response: string }>(`${path}/credential`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ account_id: userId, blind_request: pending.request }),
    });
    const message = decode(pending.message);
    const signature = await suite.finalize(key, message, decode(response), decode(pending.inverse));
    const token = encode(Uint8Array.from([...message, ...signature]));
    localStorage.setItem(storageKey, JSON.stringify({ token }));
    return token;
  });
}
