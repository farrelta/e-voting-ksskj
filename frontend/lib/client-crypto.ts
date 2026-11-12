// Browser-side ballot preparation.
export type PreparedBallot = {
  encryptedVoteBase64: string;
  proofBase64: string;
};

type EncryptResult =
  | { ok: true; encryptedVote: string }
  | { ok: false; error: string };

type EvotingCrypto = {
  encryptBallot: (
    hePublicKey: string,
    kemPublicKey: string,
    candidateIndex: number,
    candidateCount: number,
  ) => EncryptResult;
};

type GoRuntime = {
  importObject: WebAssembly.Imports;
  run: (instance: WebAssembly.Instance) => Promise<void>;
};

declare global {
  interface Window {
    Go?: new () => GoRuntime;
    evotingCrypto?: EvotingCrypto;
  }
}

const WASM_EXEC_URL = "/crypto/wasm_exec.js";
const WASM_URL = "/crypto/clientcrypto.wasm";

let modulePromise: Promise<EvotingCrypto> | null = null;

export function isClientCryptoConfigured() {
  return typeof WebAssembly !== "undefined";
}

function loadScript(src: string) {
  return new Promise<void>((resolve, reject) => {
    const script = document.createElement("script");
    script.src = src;
    script.onload = () => resolve();
    script.onerror = () => reject(new Error(`Could not load ${src}`));
    document.head.appendChild(script);
  });
}

async function startModule(): Promise<EvotingCrypto> {
  if (!window.Go) await loadScript(WASM_EXEC_URL);
  if (!window.Go) throw new Error("The encryption runtime did not load.");

  const response = await fetch(WASM_URL, { cache: "force-cache" });
  if (!response.ok) {
    throw new Error(
      "The encryption module is missing from this server. Ask the administrator to build it (scripts/build-client-crypto.sh).",
    );
  }
  const go = new window.Go();
  const { instance } = await WebAssembly.instantiate(
    await response.arrayBuffer(),
    go.importObject,
  );
  // Go keeps running in the background; it is not awaited on purpose.
  void go.run(instance);

  // The module registers its API as soon as it starts; allow a brief moment.
  for (let attempt = 0; attempt < 50 && !window.evotingCrypto; attempt++) {
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
  if (!window.evotingCrypto) throw new Error("The encryption module did not start.");
  return window.evotingCrypto;
}

function loadCryptoModule() {
  if (!modulePromise) {
    modulePromise = startModule().catch((error) => {
      modulePromise = null; // allow a retry after a transient failure
      throw error;
    });
  }
  return modulePromise;
}

async function readJson<T>(response: Response): Promise<T> {
  let data: (T & { error?: string }) | null = null;
  try {
    data = (await response.json()) as T & { error?: string };
  } catch {
    // fall through to the generic message below
  }
  if (!response.ok || !data) {
    throw new Error(data?.error || `Request failed (${response.status})`);
  }
  return data;
}

export async function prepareBallot(params: {
  electionId: string;
  receiptId: string;
  candidateIndex: number;
  candidateCount: number;
}): Promise<PreparedBallot> {
  const { electionId, receiptId, candidateIndex, candidateCount } = params;
  const base = `/api/elections/${encodeURIComponent(electionId)}`;

  const [crypto, keys] = await Promise.all([
    loadCryptoModule(),
    fetch(`${base}/keys`, { credentials: "same-origin", cache: "no-store" }).then(
      (response) =>
        readJson<{ he_public_key: string; kem_public_key: string }>(response),
    ),
  ]);

  const encrypted = crypto.encryptBallot(
    keys.he_public_key,
    keys.kem_public_key,
    candidateIndex,
    candidateCount,
  );
  if (!encrypted.ok) throw new Error(`Could not encrypt the ballot: ${encrypted.error}`);

  const signed = await readJson<{ proof: string }>(
    await fetch(`${base}/sign-ballot`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        receipt_id: receiptId,
        encrypted_vote: encrypted.encryptedVote,
      }),
    }),
  );

  return { encryptedVoteBase64: encrypted.encryptedVote, proofBase64: signed.proof };
}
