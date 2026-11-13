const iterations = 600_000;
const encoder = new TextEncoder();

function encode(bytes: Uint8Array) {
  return btoa(Array.from(bytes, (byte) => String.fromCharCode(byte)).join(""));
}

function decode(value: string) {
  return Uint8Array.from(atob(value), (character) => character.charCodeAt(0));
}

async function deriveKey(passphrase: string, salt: Uint8Array<ArrayBuffer>) {
  const material = await crypto.subtle.importKey(
    "raw", encoder.encode(passphrase), "PBKDF2", false, ["deriveKey"],
  );
  return crypto.subtle.deriveKey(
    { name: "PBKDF2", salt, iterations, hash: "SHA-256" },
    material,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"],
  );
}

export async function encryptTallyKey(electionId: string, secret: string, passphrase: string) {
  const salt = crypto.getRandomValues(new Uint8Array(16));
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const key = await deriveKey(passphrase, salt);
  const ciphertext = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv, additionalData: encoder.encode(electionId) },
    key,
    encoder.encode(secret),
  );
  return JSON.stringify({
    version: 1, electionId, salt: encode(salt), iv: encode(iv),
    ciphertext: encode(new Uint8Array(ciphertext)),
  });
}

export async function decryptTallyKey(backup: string, electionId: string, passphrase: string) {
  let data;
  try {
    data = JSON.parse(backup);
    if (!data || data.version !== 1 || typeof data.electionId !== "string" ||
        typeof data.salt !== "string" || typeof data.iv !== "string" ||
        typeof data.ciphertext !== "string") {
      throw new Error();
    }
  } catch {
    throw new Error("The file is not a supported tally key backup.");
  }
  if (data.electionId !== electionId) {
    throw new Error("This tally key backup belongs to another election.");
  }
  try {
    const salt = decode(data.salt);
    const iv = decode(data.iv);
    if (salt.length !== 16 || iv.length !== 12) throw new Error();
    const key = await deriveKey(passphrase, salt);
    const plaintext = await crypto.subtle.decrypt(
      { name: "AES-GCM", iv, additionalData: encoder.encode(electionId) },
      key,
      decode(data.ciphertext),
    );
    return new TextDecoder().decode(plaintext);
  } catch {
    throw new Error("Could not unlock the tally key. Check the file and passphrase.");
  }
}
