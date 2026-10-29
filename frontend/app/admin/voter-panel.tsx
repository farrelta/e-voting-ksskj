import { addParticipants, revokeEligibility } from "@/app/actions";
import type { VoterStatus } from "@/lib/backend";
import { ConfirmSubmitButton } from "./confirm-submit-button";
import { VoterQr } from "./voter-qr";

// Admin-side voter list. By design it only ever shows WHO is eligible and WHETHER
// they have voted. It has no way to show a voter's choice.
export function VoterPanel({
  electionId,
  status,
  voters,
}: {
  electionId: string;
  status: string;
  voters: VoterStatus[];
}) {
  const locked = status === "completed";
  const votedCount = voters.filter((voter) => voter.has_voted).length;

  return (
    <details className="edit-details voter-management">
      <summary>
        Eligible voters ({voters.length}, {votedCount} voted)
      </summary>
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
          {voters.map((voter) => (
            <li className="voter-row" key={voter.user_id}>
              <div className="voter-identity">
                <span>{voter.email}</span>
                <span className={voter.has_voted ? "status status-active_voting" : "status status-draft"}>
                  {voter.has_voted ? "Voted" : "Not voted"}
                </span>
              </div>
              {!voter.has_voted && !locked && (
                <div className="voter-actions">
                  <VoterQr electionId={electionId} userId={voter.user_id} email={voter.email} />
                  <form action={revokeEligibility}>
                    <input type="hidden" name="election_id" value={electionId} />
                    <input type="hidden" name="user_id" value={voter.user_id} />
                    <ConfirmSubmitButton
                      className="button button-danger"
                      message={`Remove ${voter.email} from the eligible list?`}
                    >
                      Remove
                    </ConfirmSubmitButton>
                  </form>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
