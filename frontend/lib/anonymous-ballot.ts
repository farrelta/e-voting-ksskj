type PreparedBallot = { encrypted_vote: string; proof: string };
type Prepare = (scope: string, key: string, count: number, index: number) =>
  PreparedBallot | { error: string };
type GoRuntime = {
  importObject: WebAssembly.Imports;
  run(instance: WebAssembly.Instance): Promise<void>;
};

let runtime: Promise<Prepare> | undefined;

async function startRuntime(): Promise<Prepare> {
  const globals = window as Window & {
    Go?: new () => GoRuntime;
    evotingPrepareAnonymousBallot?: Prepare;
  };
  if (!globals.Go) {
    await new Promise<void>((resolve, reject) => {
      const script = document.createElement("script");
      script.src = "/api/anonymous/assets/wasm_exec.js";
      script.onload = () => resolve();
      script.onerror = () => {
        script.remove();
        reject(new Error("Could not load the ballot encryption runtime."));
      };
      document.head.append(script);
    });
  }
  if (!globals.Go) throw new Error("Ballot encryption runtime is missing.");
  const go = new globals.Go();
  const response = await fetch("/api/anonymous/assets/ballot.wasm", { credentials: "omit" });
  if (!response.ok) throw new Error("Could not load the ballot encryption module.");
  const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  void go.run(instance).catch(() => { runtime = undefined; });
  if (!globals.evotingPrepareAnonymousBallot) throw new Error("Ballot encryption did not start.");
  return globals.evotingPrepareAnonymousBallot;
}

function decode(data: string) {
  return Uint8Array.from(atob(data), (character) => character.charCodeAt(0));
}

export async function prepareAnonymousBallot(
  electionId: string, publicKey: string, manifestHash: string, token: string,
  count: number, index: number,
): Promise<PreparedBallot> {
  runtime ??= startRuntime().catch((error) => {
    runtime = undefined;
    throw error;
  });
  const prepare = await runtime;
  const electionBytes = electionId.replaceAll("-", "").match(/../g);
  if (!electionBytes || electionBytes.length !== 16) throw new Error("Invalid election ID.");
  const hash = decode(manifestHash);
  if (hash.length !== 32) throw new Error("Invalid ballot manifest.");
  const tokenBytes = decode(token);
  const messageEnd = tokenBytes.length - 384;
  const expected = new TextEncoder().encode(`evoting/anonymous-v1/credential/${electionId}/`);
  if (messageEnd !== 32 + expected.length + 32 ||
      expected.some((value, i) => tokenBytes[32 + i] !== value)) {
    throw new Error("Credential does not belong to this election.");
  }
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", tokenBytes.slice(32, messageEnd)));
  const scope = Uint8Array.from([...electionBytes.map((pair) => parseInt(pair, 16)), ...hash, ...digest]);
  const ballot = prepare(btoa(String.fromCharCode(...scope)), publicKey, count, index);
  if ("error" in ballot) throw new Error(ballot.error);
  return ballot;
}
