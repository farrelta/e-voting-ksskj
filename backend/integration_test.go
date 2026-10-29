package main

// Database integration tests. Runs only when TEST_DATABASE_URL points at a PostgreSQL database that has database/schema.sql loaded.
import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &App{db: pool, cfg: &Config{AllowedEmailDomain: "ugm.ac.id"}}
}

// testAdmin creates an admin account and removes everything created under it.
func testAdmin(t *testing.T, a *App) (adminID string, createdElections *[]string) {
	t.Helper()
	ctx := context.Background()
	email := "it-admin-" + uuid.NewString() + "@ugm.ac.id"
	if err := a.db.QueryRow(ctx, `
		INSERT INTO evoting.users (email, password_hash, role, is_verified)
		VALUES ($1, '!', 'admin', TRUE) RETURNING id::text`, email).Scan(&adminID); err != nil {
		t.Fatal(err)
	}
	elections := &[]string{}
	t.Cleanup(func() {
		for _, id := range *elections {
			a.db.Exec(ctx, `DELETE FROM evoting.encrypted_ballots WHERE election_id = $1::uuid`, id)
			a.db.Exec(ctx, `DELETE FROM evoting.voter_eligibility WHERE election_id = $1::uuid`, id)
			a.db.Exec(ctx, `DELETE FROM evoting.receipts WHERE election_id = $1::uuid`, id)
			a.db.Exec(ctx, `DELETE FROM evoting.candidates WHERE election_id = $1::uuid`, id)
			a.db.Exec(ctx, `DELETE FROM evoting.elections WHERE id = $1::uuid`, id)
		}
		a.db.Exec(ctx, `DELETE FROM evoting.users WHERE email LIKE 'it-%'`)
	})
	return adminID, elections
}

// inserts an election whose voting window starts/ends the given durations from now.
func newTestElection(t *testing.T, a *App, adminID string, created *[]string, status string, start, end time.Duration) string {
	t.Helper()
	now := time.Now()
	var id string
	err := a.db.QueryRow(context.Background(), `
		INSERT INTO evoting.elections
			(title, registration_start, registration_end, voting_start, voting_end, status, created_by)
		VALUES ('it-election', $1, $2, $3, $4, $5, $6::uuid)
		RETURNING id::text`,
		now.Add(-48*time.Hour), now.Add(-47*time.Hour), now.Add(start), now.Add(end), status, adminID,
	).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	*created = append(*created, id)
	return id
}

func electionStatus(t *testing.T, a *App, id string) string {
	t.Helper()
	var status string
	if err := a.db.QueryRow(context.Background(),
		`SELECT status FROM evoting.elections WHERE id = $1::uuid`, id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestSchedulerOpensAndClosesByClock(t *testing.T) {
	a := testApp(t)
	adminID, created := testAdmin(t, a)
	h := time.Hour

	shouldOpen := newTestElection(t, a, adminID, created, "registration", -h, h)
	futureStaysRegistration := newTestElection(t, a, adminID, created, "registration", h, 2*h)
	shouldClose := newTestElection(t, a, adminID, created, "active_voting", -2*h, -time.Minute)
	neverOpenedShouldClose := newTestElection(t, a, adminID, created, "registration", -3*h, -2*h)
	draftUntouched := newTestElection(t, a, adminID, created, "draft", -h, h)
	completedStays := newTestElection(t, a, adminID, created, "completed", -h, h)

	if _, _, err := a.advanceElectionStatuses(context.Background()); err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		shouldOpen:              "active_voting",
		futureStaysRegistration: "registration",
		shouldClose:             "completed",
		neverOpenedShouldClose:  "completed",
		draftUntouched:          "draft",
		completedStays:          "completed",
	}
	for id, status := range want {
		if got := electionStatus(t, a, id); got != status {
			t.Errorf("election %s status = %q, want %q", id, got, status)
		}
	}

	if _, _, err := a.advanceElectionStatuses(context.Background()); err != nil {
		t.Fatal(err)
	}
	for id, status := range want {
		if got := electionStatus(t, a, id); got != status {
			t.Errorf("after second run: election %s status = %q, want %q", id, got, status)
		}
	}
}

func TestAddParticipants(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	adminID, created := testAdmin(t, a)
	electionID := newTestElection(t, a, adminID, created, "registration", time.Hour, 2*time.Hour)

	tag := uuid.NewString()[:8]
	newVoter := "it-new-" + tag + "@ugm.ac.id"
	existingVoter := "it-existing-" + tag + "@ugm.ac.id"
	unverified := "it-unverified-" + tag + "@ugm.ac.id"
	adminEmail := "it-admin2-" + tag + "@ugm.ac.id"
	for _, row := range []struct {
		email, role string
		verified    bool
	}{{existingVoter, "voter", true}, {unverified, "voter", false}, {adminEmail, "admin", true}} {
		if _, err := a.db.Exec(ctx, `
			INSERT INTO evoting.users (email, password_hash, role, is_verified)
			VALUES ($1, '!', $2::evoting.roles, $3)`, row.email, row.role, row.verified); err != nil {
			t.Fatal(err)
		}
	}

	sum, err := a.addParticipants(ctx, electionID, []string{newVoter, existingVoter, unverified, adminEmail})
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Added) != 2 || len(sum.Skipped) != 2 || len(sum.AlreadyEligible) != 0 {
		t.Fatalf("unexpected summary: %+v", sum)
	}

	//gives the new voter a verified password-less voter account.
	var role, hash string
	var verified bool
	if err := a.db.QueryRow(ctx,
		`SELECT role::text, password_hash, is_verified FROM evoting.users WHERE email = $1`, newVoter,
	).Scan(&role, &hash, &verified); err != nil {
		t.Fatal(err)
	}
	if role != "voter" || !verified || hash != lockedPasswordHash {
		t.Fatalf("new account = role %q verified %v hash %q", role, verified, hash)
	}
	var eligible, receipts int
	a.db.QueryRow(ctx, `SELECT count(*) FROM evoting.voter_eligibility WHERE election_id = $1::uuid AND receipt_id IS NOT NULL`, electionID).Scan(&eligible)
	a.db.QueryRow(ctx, `SELECT count(*) FROM evoting.receipts WHERE election_id = $1::uuid`, electionID).Scan(&receipts)
	if eligible != 2 || receipts != 2 {
		t.Fatalf("eligibility rows = %d, receipts = %d, want 2 and 2", eligible, receipts)
	}

	//case for adding the same person again
	again, err := a.addParticipants(ctx, electionID, []string{newVoter, existingVoter})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Added) != 0 || len(again.AlreadyEligible) != 2 {
		t.Fatalf("second run: %+v", again)
	}

	//dissallow changes for a completed election
	a.db.Exec(ctx, `UPDATE evoting.elections SET status = 'completed' WHERE id = $1::uuid`, electionID)
	if _, err := a.addParticipants(ctx, electionID, []string{"it-late-" + tag + "@ugm.ac.id"}); err != errElectionClosed {
		t.Fatalf("expected errElectionClosed, got %v", err)
	}
}

func TestBulkEndpointRejectsOtherDomains(t *testing.T) {
	a := testApp(t)
	adminID, created := testAdmin(t, a)
	electionID := newTestElection(t, a, adminID, created, "registration", time.Hour, 2*time.Hour)

	tag := uuid.NewString()[:8]
	good := "it-good-" + tag + "@ugm.ac.id"
	body, _ := json.Marshal(map[string]string{"text": "Name,Email\nA," + good + "\nB,outsider@gmail.com\nC,it-sub-" + tag + "@mail.ugm.ac.id"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.SetPathValue("electionId", electionID)
	rec := httptest.NewRecorder()
	a.handleBulkAddParticipants(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Added   []addedParticipant `json:"added"`
		Invalid []string           `json:"invalid"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Added) != 2 || len(resp.Invalid) != 1 || resp.Invalid[0] != "outsider@gmail.com" {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
	var n int
	a.db.QueryRow(context.Background(), `SELECT count(*) FROM evoting.users WHERE email = 'outsider@gmail.com'`).Scan(&n)
	if n != 0 {
		t.Fatal("an address outside the allowed domain must not get an account")
	}
}

type acceptAllVerifier struct{}

func (acceptAllVerifier) Verify(context.Context, string, []byte, []byte) error { return nil }

// The ballot endpoint needs to enforce the voting window by the clock, even when the status flag says "active_voting".
func TestBallotRejectedOutsideVotingWindow(t *testing.T) {
	a := testApp(t)
	a.ballotVerifier = acceptAllVerifier{}
	ctx := context.Background()
	adminID, created := testAdmin(t, a)
	h := time.Hour

	submit := func(electionID, voterID, receiptID string) int {
		body, _ := json.Marshal(map[string]string{
			"receipt_id":     receiptID,
			"encrypted_vote": base64.StdEncoding.EncodeToString([]byte("ciphertext")),
			"proof":          base64.StdEncoding.EncodeToString([]byte("proof")),
		})
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
		req.SetPathValue("electionId", electionID)
		req = req.WithContext(context.WithValue(req.Context(), ctxKey{},
			&Session{User: &SessionUser{ID: voterID, Email: "x", Role: "voter"}}))
		rec := httptest.NewRecorder()
		a.handleSubmitBallot(rec, req)
		return rec.Code
	}
	setup := func(start, end time.Duration) (electionID, voterID, receiptID string) {
		electionID = newTestElection(t, a, adminID, created, "active_voting", start, end)
		email := "it-voter-" + uuid.NewString()[:8] + "@ugm.ac.id"
		sum, err := a.addParticipants(ctx, electionID, []string{email})
		if err != nil || len(sum.Added) != 1 {
			t.Fatalf("setup: %v %+v", err, sum)
		}
		voterID = sum.Added[0].UserID
		if err := a.db.QueryRow(ctx, `SELECT receipt_id::text FROM evoting.voter_eligibility WHERE election_id = $1::uuid`, electionID).Scan(&receiptID); err != nil {
			t.Fatal(err)
		}
		return
	}

	e, v, r := setup(-h, -time.Minute) //status says active, but the window has ended
	if code := submit(e, v, r); code != http.StatusConflict {
		t.Errorf("after the window: status %d, want 409", code)
	}
	e, v, r = setup(h, 2*h) //status set early by hand, window not started
	if code := submit(e, v, r); code != http.StatusConflict {
		t.Errorf("before the window: status %d, want 409", code)
	}
	e, v, r = setup(-h, h) //inside the window
	if code := submit(e, v, r); code != http.StatusCreated {
		t.Errorf("inside the window: status %d, want 201", code)
	}
	var voted bool
	a.db.QueryRow(ctx, `SELECT has_voted FROM evoting.voter_eligibility WHERE election_id = $1::uuid`, e).Scan(&voted)
	if !voted {
		t.Error("a ballot accepted inside the window must mark the voter as having voted")
	}
}

type fakeMailer struct {
	fail bool
	sent []struct{ to, code string }
}

func (m *fakeMailer) SendOTP(_ context.Context, to, code string) error {
	if m.fail {
		return errors.New("smtp down")
	}
	m.sent = append(m.sent, struct{ to, code string }{to, code})
	return nil
}

// Full QR sign-in through the real router, token starts an email OTP challenge (sent through the mailer), has resend feature, the code completes the login
func TestQRLoginSendsEmailOTPAndCompletesLogin(t *testing.T) {
	a := testApp(t)
	ctx := context.Background()
	adminID, created := testAdmin(t, a)
	electionID := newTestElection(t, a, adminID, created, "registration", time.Hour, 2*time.Hour)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { rdb.Close() })
	mailer := &fakeMailer{}
	a.cfg = &Config{Env: "production", AllowedEmailDomain: "ugm.ac.id", FrontendDir: t.TempDir()}
	a.rdb, a.mailer = rdb, mailer
	a.sessions = newSessionStore(rdb, false)
	a.otp = newOTPService(rdb, "0123456789abcdef0123")
	a.qr = newQRService(rdb)

	voterEmail := "it-qr-" + uuid.NewString()[:8] + "@ugm.ac.id"
	sum, err := a.addParticipants(ctx, electionID, []string{voterEmail})
	if err != nil || len(sum.Added) != 1 {
		t.Fatalf("setup: %v %+v", err, sum)
	}
	voterID := sum.Added[0].UserID
	token, err := a.qr.Issue(ctx, voterID)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(a.routes())
	t.Cleanup(srv.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	post := func(path string, body any) (int, string) {
		b, _ := json.Marshal(body)
		resp, err := client.Post(srv.URL+path, "application/json", bytes.NewReader(b))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.String()
	}

	mailer.fail = true
	if code, _ := post("/api/login/qr", map[string]string{"token": token}); code != http.StatusServiceUnavailable {
		t.Fatalf("with a failing mailer: status %d, want 503", code)
	}
	mailer.fail = false
	token, _ = a.qr.Issue(ctx, voterID)

	if code, body := post("/api/login/qr", map[string]string{"token": token}); code != http.StatusOK {
		t.Fatalf("qr login: %d %s", code, body)
	}
	if len(mailer.sent) != 1 || mailer.sent[0].to != voterEmail || len(mailer.sent[0].code) != 6 {
		t.Fatalf("expected one 6-digit code emailed to %s, got %+v", voterEmail, mailer.sent)
	}

	//Not logged in yet: scanning alone must not authenticate
	resp, _ := client.Get(srv.URL + "/api/me")
	if resp.StatusCode == http.StatusOK {
		t.Fatal("scanning must not log the voter in before the code is entered")
	}
	resp.Body.Close()

	//"Resend code" must know who to email
	mr.FastForward(otpResendCooldown + time.Second)
	if code, body := post("/api/login/resend-otp", map[string]string{}); code != http.StatusOK {
		t.Fatalf("resend: %d %s", code, body)
	}
	if len(mailer.sent) != 2 || mailer.sent[1].to != voterEmail {
		t.Fatalf("resend should email the same voter, got %+v", mailer.sent)
	}

	if code, body := post("/api/login/verify-otp", map[string]string{"otp": mailer.sent[1].code}); code != http.StatusOK {
		t.Fatalf("verify: %d %s", code, body)
	}
	resp, err = client.Get(srv.URL + "/api/me")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("after verifying the code the voter should be logged in: %v %v", err, resp)
	}
	resp.Body.Close()
}
