import { PreviewShell } from "../preview-shell";

function DateGrid({ registration = false }: { registration?: boolean }) {
  return (
    <div className="date-grid">
      <div>
        <span className="field-caption">Registration</span>
        <span>{registration ? "Oct 20, 2026, 9:00 AM – Nov 5, 2026, 5:00 PM" : "Oct 20 – Nov 5, 2026"}</span>
      </div>
      <div>
        <span className="field-caption">Voting</span>
        <span>Nov 10, 2026, 8:00 AM – Nov 12, 2026, 6:00 PM</span>
      </div>
    </div>
  );
}

export default function DashboardPreviewPage() {
  return (
    <PreviewShell activeView="dashboard">
      <section className="page-heading">
        <p className="eyebrow">VOTER PORTAL</p>
        <h1>Your elections</h1>
        <p className="muted">Review open elections and manage your voter registration.</p>
      </section>
      <div className="election-list">
        <article className="panel election-card">
          <div className="election-card-heading">
            <div>
              <span className="status status-registration">registration</span>
              <h2>Student Council Election 2026</h2>
            </div>
          </div>
          <p>Choose your representatives for the upcoming academic year.</p>
          <DateGrid registration />
          <button className="button button-primary" disabled type="button">
            Register for this election
          </button>
        </article>

        <article className="panel election-card">
          <div className="election-card-heading">
            <div>
              <span className="status status-active_voting">active voting</span>
              <h2>Computer Science Society Officers</h2>
            </div>
          </div>
          <p>Voting is open. Your ballot selection is shown here as a visual preview.</p>
          <DateGrid />
          <aside className="receipt-panel">
            <div>
              <span className="field-caption">Your voter receipt</span>
              <code>EV-2026-7F3A-91C2</code>
            </div>
            <span className="text-link">Verify receipt</span>
          </aside>
          <div className="voting-section">
            <h3>Cast your ballot</h3>
            <div className="form-stack ballot-form">
              <fieldset className="candidate-options">
                <legend>Select a candidate</legend>
                <label className="candidate-option">
                  <input type="radio" name="preview-candidate" disabled />
                  <span>
                    <strong>Alex Morgan</strong>
                    <span className="muted candidate-description">Focused on community and student events.</span>
                  </span>
                </label>
                <label className="candidate-option">
                  <input type="radio" name="preview-candidate" disabled />
                  <span>
                    <strong>Sam Rivera</strong>
                    <span className="muted candidate-description">Advocating for peer mentoring and collaboration.</span>
                  </span>
                </label>
              </fieldset>
              <aside className="security-notice" role="note">
                <strong>Preview only — ballot submission is disabled.</strong>
                <span>This screen uses sample content and will not submit a vote.</span>
              </aside>
              <button className="button button-primary" disabled type="button">
                Submit secure ballot
              </button>
            </div>
          </div>
        </article>
      </div>
    </PreviewShell>
  );
}
