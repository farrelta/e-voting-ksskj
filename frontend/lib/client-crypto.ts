export type PreparedBallot = {
  encryptedVoteBase64: string;
  proofBase64: string;
};

export function isClientCryptoConfigured() {
  return false;
}

export async function prepareBallot(
  _candidateId: string,
  _electionId: string,
): Promise<PreparedBallot> {
  throw new Error(
    "Ballot submission is disabled until an audited voting protocol, election public key, and proof verifier are configured.",
  );
}
