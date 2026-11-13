import { addParticipants, revokeEligibility } from "@/app/actions";
import type { VoterStatus } from "@/lib/backend";
import { ConfirmSubmitButton } from "./confirm-submit-button";
import { VoterQr } from "./voter-qr";

export function VoterPanel({
  electionId,
  status,
  voters,
  anonymous,
  recordedBallots,
}: {
  electionId: string;
  status: string;
  voters: VoterStatus[];
  anonymous: boolean;
  recordedBallots?: number;
}) {
  const locked = !anonymous || status === "completed";
  const issuedCount = voters.filter((voter) => voter.credential_issued).length;

  return (
    <details className="edit-details voter-management">
      <summary>
        Eligible voters ({voters.length}{anonymous ? `, ${issuedCount} credentials issued` : ""})
      </summary>
      {anonymous && recordedBallots !== undefined && (
        <p className="muted">Ballots recorded: {recordedBallots}</p>
      )}
      {!locked && (
        <form className="voter-add-form" action={addParticipants}>
          <input type="hidden" name="election_id" value={electionId} />
          <label>
            Add participants: paste email addresses (one per line, or separated by commas)
            <textarea
              name="emails"
              rows={4}
              required
              placeholder={"name1@mail.ugm.ac.id\nname2@ugm.ac.id"}
            />
          </label>
          <button className="button button-primary" type="submit">Add participants</button>
        </form>
      )}
      {voters.length === 0 ? (
        <p className="muted">No voters have been made eligible yet.</p>
      ) : (
        <ul className="voter-list">
          {voters.map((voter) => {
            const used = voter.credential_issued;
            return (
              <li className="voter-row" key={voter.user_id}>
                <div className="voter-identity">
                  <span>{voter.email}</span>
                  {anonymous && (
                    <span className={used ? "status status-active_voting" : "status status-draft"}>
                      {used ? "Credential issued" : "Credential not issued"}
                    </span>
                  )}
                </div>
                {!locked && (
                  <div className="voter-actions">
                    <VoterQr electionId={electionId} userId={voter.user_id} email={voter.email} />
                    <form action={revokeEligibility}>
                      <input type="hidden" name="election_id" value={electionId} />
                      <input type="hidden" name="user_id" value={voter.user_id} />
                      <ConfirmSubmitButton
                        disabled={used}
                        className="button button-danger"
                        message={`Remove ${voter.email} from the eligible list?`}
                      >
                        Remove
                      </ConfirmSubmitButton>
                    </form>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </details>
  );
}
