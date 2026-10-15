import Link from "next/link";
import {
  createCandidate,
  deleteCandidate,
  deleteElection,
  updateCandidate,
  updateElectionStatus,
} from "@/app/actions";
import { LogoutButton } from "@/app/components/logout-button";
import {
  getAdminCandidates,
  getElections,
  getSessionUser,
  getVoters,
} from "@/lib/backend";
import type { Candidate, VoterStatus } from "@/lib/backend";
import { ConfirmSubmitButton } from "./confirm-submit-button";
import { ElectionForm } from "./election-form";
import { VoterPanel } from "./voter-panel";

export default async function AdminPage({
  searchParams,
}: {
  searchParams: Promise<{ notice?: string; error?: string }>;
}) {
  const [{ user }, { elections }, params] = await Promise.all([
    getSessionUser(),
    getElections(),
    searchParams,
  ]);
  const candidateResults = await Promise.all(
    elections.map(async (election) => {
      const { candidates } = await getAdminCandidates(election.id);
      return [election.id, candidates] as const;
    }),
  );
  const candidatesByElection = new Map<string, Candidate[]>(candidateResults);
  const voterResults = await Promise.all(
    elections.map(async (election) => {
      const { voters } = await getVoters(election.id);
      return [election.id, voters] as const;
    }),
  );
  const votersByElection = new Map<string, VoterStatus[]>(voterResults);

  return (
    <main className="page-shell">
      <header className="topbar">
        <Link className="brand" href="/admin">E-Voting</Link>
        <div className="topbar-user">
          <span>{user.email}</span>
          <LogoutButton />
        </div>
      </header>
      <section className="page-heading">
        <p className="eyebrow">ADMINISTRATION</p>
        <h1>Election management</h1>
        <p className="muted">Create elections, manage candidates, and control milestones.</p>
      </section>
      {params.notice && <p className="notice">{params.notice}</p>}
      {params.error && <p className="notice error-message" role="alert">{params.error}</p>}

      <section className="panel section-panel">
        <div className="section-heading">
          <div>
            <p className="eyebrow">SETUP</p>
            <h2>Create an election</h2>
          </div>
        </div>
        <ElectionForm />
      </section>

      <section className="panel section-panel">
        <div className="section-heading">
          <div>
            <p className="eyebrow">CANDIDATES</p>
            <h2>Add a candidate</h2>
          </div>
        </div>
        {elections.length === 0 ? (
          <p className="muted">Create an election before adding candidates.</p>
        ) : (
          <form className="form-stack candidate-form" action={createCandidate}>
            <label>
              Election
              <select name="election_id" required defaultValue="">
                <option value="" disabled>Select an election</option>
                {elections.map((election) => (
                  <option
                    key={election.id}
                    value={election.id}
                    disabled={election.status === "active_voting" || election.status === "completed"}
                  >
                    {election.title}{election.status === "active_voting" || election.status === "completed" ? " (voting locked)" : ""}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Candidate name
              <input name="name" required />
            </label>
            <label>
              Description
              <textarea name="description" rows={2} />
            </label>
            <button className="button button-primary" type="submit">Add candidate</button>
          </form>
        )}
        {elections.length > 0 && (
          <p className="candidate-management-help">
            Candidates can be added, edited, or removed until voting starts.
          </p>
        )}
      </section>

      <section className="section-block">
        <div className="section-heading">
          <div>
            <p className="eyebrow">ELECTIONS</p>
            <h2>Manage elections</h2>
          </div>
        </div>
        {elections.length === 0 ? (
          <div className="empty-state">No elections have been created.</div>
        ) : (
          <div className="election-list">
            {elections.map((election) => (
              <article className="panel election-card" key={election.id}>
                <div className="election-card-heading">
                  <div>
                    <span className={`status status-${election.status}`}>
                      {election.status.replaceAll("_", " ")}
                    </span>
                    <h2>{election.title}</h2>
                  </div>
                </div>
                <details className="edit-details">
                  <summary>Edit election details</summary>
                  <ElectionForm election={election} />
                </details>
                <details className="edit-details candidate-management">
                  <summary>Manage candidates ({candidatesByElection.get(election.id)?.length ?? 0})</summary>
                  {candidatesByElection.get(election.id)?.length ? (
                    <div className="candidate-management-list">
                      {(candidatesByElection.get(election.id) ?? []).map((candidate) => {
                        const candidatesLocked = election.status === "active_voting" || election.status === "completed";
                        return (
                          <div className="candidate-management-item" key={candidate.id}>
                            <form className="candidate-edit-form" action={updateCandidate}>
                              <input type="hidden" name="candidate_id" value={candidate.id} />
                              <label>
                                Candidate name
                                <input name="name" defaultValue={candidate.name} disabled={candidatesLocked} required />
                              </label>
                              <label>
                                Description
                                <textarea name="description" rows={2} defaultValue={candidate.description} disabled={candidatesLocked} />
                              </label>
                              <button className="button button-secondary" disabled={candidatesLocked} type="submit">Save candidate</button>
                            </form>
                            <form action={deleteCandidate}>
                              <input type="hidden" name="candidate_id" value={candidate.id} />
                              <ConfirmSubmitButton
                                className="button button-danger"
                                disabled={candidatesLocked}
                                message={`Delete ${candidate.name}? This cannot be undone.`}
                              >
                                Delete candidate
                              </ConfirmSubmitButton>
                            </form>
                          </div>
                        );
                      })}
                    </div>
                  ) : (
                    <p className="muted">No candidates have been added to this election.</p>
                  )}
                  {(election.status === "draft" || election.status === "registration") ? (
                    <p className="candidate-management-help">Candidate details can be changed until voting starts.</p>
                  ) : <p className="candidate-management-help">Candidate details are locked after voting starts.</p>}
                </details>
                <VoterPanel
                  electionId={election.id}
                  status={election.status}
                  voters={votersByElection.get(election.id) ?? []}
                />
                <div className="milestone-form">
                  <form action={updateElectionStatus}>
                    <input type="hidden" name="election_id" value={election.id} />
                    <input type="hidden" name="status" value="registration" />
                    <button className="button button-secondary" type="submit">
                      Open voter registration
                    </button>
                  </form>
                  <form className="milestone-status-form" action={updateElectionStatus}>
                    <input type="hidden" name="election_id" value={election.id} />
                    <label>
                      Set status
                      <select name="status" defaultValue={election.status}>
                        <option value="draft">Draft</option>
                        <option value="registration">Registration</option>
                        <option value="active_voting">Active voting</option>
                        <option value="completed">Completed</option>
                      </select>
                    </label>
                    <button className="button button-secondary" type="submit">
                      Update status
                    </button>
                  </form>
                  <form action={deleteElection}>
                    <input type="hidden" name="election_id" value={election.id} />
                    <ConfirmSubmitButton
                      className="button button-danger"
                      message={`Delete “${election.title}”? Elections with voter registrations or ballots cannot be deleted.`}
                    >
                      Delete election
                    </ConfirmSubmitButton>
                  </form>
                </div>
              </article>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}
