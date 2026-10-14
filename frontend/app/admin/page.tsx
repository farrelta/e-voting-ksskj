import Link from "next/link";
import { createCandidate, updateElectionStatus } from "@/app/actions";
import { LogoutButton } from "@/app/components/logout-button";
import { getElections, getSessionUser } from "@/lib/backend";
import { ElectionForm } from "./election-form";

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
                  <option key={election.id} value={election.id}>{election.title}</option>
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
                </div>
              </article>
            ))}
          </div>
        )}
      </section>
    </main>
  );
}
