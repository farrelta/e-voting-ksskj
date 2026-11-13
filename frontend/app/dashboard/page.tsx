import Link from "next/link";
import { LogoutButton } from "@/app/components/logout-button";
import {
  getElections,
  getSessionUser,
} from "@/lib/backend";
import { CredentialForm } from "./credential-form";
import { AnonymousBallotForm } from "./anonymous-ballot-form";

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
  const [{ user }, { elections, ballot_pilot }, params] = await Promise.all([
    getSessionUser(),
    getElections(),
    searchParams,
  ]);
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
        <p className="muted">Review your elections and cast your ballot when voting is open.</p>
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

              {!election.is_registered && (
                <p className="muted">
                  You are not on the eligible voter list for this election.
                </p>
              )}
              {election.is_registered && election.has_issuer && (
                <>
                  {ballot_pilot && election.status === "registration" && (
                    <CredentialForm electionId={election.id} userId={user.id} />
                  )}
                  {ballot_pilot && election.status !== "registration" ? (
                    <AnonymousBallotForm electionId={election.id} userId={user.id} closed={election.status === "completed"} />
                  ) : (
                    <p className="muted">{ballot_pilot && election.status === "registration"
                      ? "Voting opens at the scheduled start time."
                      : "Anonymous ballot submission is unavailable."}</p>
                  )}
                </>
              )}
            </article>
          ))}
        </div>
      )}
    </main>
  );
}
