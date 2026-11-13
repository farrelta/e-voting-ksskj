import Link from "next/link";
import { registerForElection } from "@/app/actions";
import { LogoutButton } from "@/app/components/logout-button";
import {
  getCandidates,
  getElections,
  getSessionUser,
} from "@/lib/backend";
import type { Candidate } from "@/lib/backend";
import { BallotForm } from "./ballot-form";

function displayDate(value: string) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value));
}

export default async function DashboardPage({
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
      if (
        election.status !== "active_voting" ||
        !election.is_registered ||
        election.has_voted
      ) {
        return [election.id, [] as Candidate[]] as const;
      }
      const { candidates } = await getCandidates(election.id);
      return [election.id, candidates] as const;
    }),
  );
  const candidatesByElection = new Map<string, Candidate[]>(candidateResults);

  return (
    <main className="page-shell">
      <header className="topbar">
        <Link className="brand" href="/dashboard">E-Voting</Link>
        <div className="topbar-user">
          <span>{user.email}</span>
          <LogoutButton />
        </div>
      </header>
      <section className="page-heading">
        <p className="eyebrow">VOTER PORTAL</p>
        <h1>Your elections</h1>
        <p className="muted">Review open elections and manage your voter registration.</p>
      </section>
      {params.notice && <p className="notice">{params.notice}</p>}
      {params.error && <p className="notice error-message" role="alert">{params.error}</p>}
      {elections.length === 0 ? (
        <div className="empty-state">There are no open elections right now.</div>
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
              {election.description && <p>{election.description}</p>}
              <div className="date-grid">
                <div>
                  <span className="field-caption">Registration</span>
                  <span>{displayDate(election.registration_start)} – {displayDate(election.registration_end)}</span>
                </div>
                <div>
                  <span className="field-caption">Voting</span>
                  <span>{displayDate(election.voting_start)} – {displayDate(election.voting_end)}</span>
                </div>
              </div>

              {election.status === "registration" && !election.is_registered && (
                <form action={registerForElection}>
                  <input type="hidden" name="election_id" value={election.id} />
                  <button className="button button-primary" type="submit">
                    Register for this election
                  </button>
                </form>
              )}
              {election.is_registered && election.receipt_code && (
                <aside className="receipt-panel">
                  <div>
                    <span className="field-caption">Your voter receipt</span>
                    <code>{election.receipt_code}</code>
                  </div>
                  <Link
                    className="text-link"
                    href={`/verify?receipt=${encodeURIComponent(election.receipt_code)}`}
                  >
                    Verify receipt
                  </Link>
                </aside>
              )}
              {election.status === "active_voting" && !election.is_registered && (
                <p className="muted">You are not registered for this election.</p>
              )}
              {election.status === "active_voting" &&
                election.is_registered &&
                election.has_voted && (
                  <p className="success-text">Your ballot has been recorded.</p>
                )}
              {election.status === "active_voting" &&
                election.is_registered &&
                !election.has_voted &&
                election.receipt_id && (
                  <div className="voting-section">
                    <h3>Cast your ballot</h3>
                    <BallotForm
                      electionId={election.id}
                      receiptId={election.receipt_id}
                      candidates={candidatesByElection.get(election.id) ?? []}
                    />
                  </div>
                )}
            </article>
          ))}
        </div>
      )}
    </main>
  );
}
