package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/farrelta/e-voting-ksskj/backend-go/cryptoutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

type CryptoResult struct {
	Scheme           string  `json:"scheme"`
	Voters           int     `json:"voters"`
	Candidates       int     `json:"candidates"`
	Runs             int     `json:"runs"`
	PubKeyBytes      int     `json:"pub_key_bytes"`
	SecKeyBytes      int     `json:"sec_key_bytes"`
	BallotBytes      int     `json:"ballot_bytes"`
	Base64Bytes      int     `json:"base64_bytes"`
	KeyGenMs         float64 `json:"keygen_ms"`
	EncryptAvgMs     float64 `json:"encrypt_avg_ms"`
	EncryptP95Ms     float64 `json:"encrypt_p95_ms"`
	EncryptBPS       float64 `json:"encrypt_bps"`
	ValidateAvgMs    float64 `json:"validate_avg_ms"`
	ValidateP95Ms    float64 `json:"validate_p95_ms"`
	TallyAvgMs       float64 `json:"tally_avg_ms"`
	TallyP95Ms       float64 `json:"tally_p95_ms"`
	TallyBPS         float64 `json:"tally_bps"`
	TallyAllocMB     float64 `json:"tally_alloc_mb"`
	Storage10kBallot float64 `json:"storage_10k_mb"`
}

type E2EResult struct {
	Scheme          string  `json:"scheme"`
	Voters          int     `json:"voters"`
	Concurrency     int     `json:"concurrency"`
	BallotBytes     int     `json:"ballot_bytes"`
	Base64Bytes     int     `json:"base64_bytes"`
	SubmitAvgMs     float64 `json:"submit_avg_ms"`
	SubmitP95Ms     float64 `json:"submit_p95_ms"`
	SubmitP99Ms     float64 `json:"submit_p99_ms"`
	SubmitBPS       float64 `json:"submit_bps"`
	TallyHTTPMs     float64 `json:"tally_http_ms"`
	DBTableBytes    int64   `json:"db_table_bytes"`
	DBBytesPerVote  float64 `json:"db_bytes_per_vote"`
}

func main() {
	mode := flag.String("mode", "crypto", "Benchmark mode: crypto or e2e")
	votersFlag := flag.String("voters", "10,50,100,500", "Comma-separated voter counts")
	candidates := flag.Int("candidates", 5, "Number of candidates")
	runs := flag.Int("runs", 3, "Number of repetitions for tally/keygen")
	concurrency := flag.Int("concurrency", 10, "HTTP concurrency for e2e mode")
	dsn := flag.String("dsn", defaultDSN(), "PostgreSQL DSN for e2e mode")
	outDir := flag.String("out", "evaluation_results", "Output directory for CSV, JSON, and Markdown reports")
	flag.Parse()

	voterCounts, err := parseIntList(*votersFlag)
	if err != nil {
		fatalf("invalid -voters: %v", err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}

	switch *mode {
	case "crypto":
		results, err := runCryptoSuite(voterCounts, *candidates, *runs)
		if err != nil {
			fatalf("crypto suite: %v", err)
		}
		if err := writeCryptoOutputs(*outDir, results); err != nil {
			fatalf("write crypto outputs: %v", err)
		}
	case "e2e":
		results, err := runE2ESuite(context.Background(), *dsn, voterCounts, *candidates, *concurrency)
		if err != nil {
			fatalf("e2e suite: %v", err)
		}
		if err := writeE2EOutputs(*outDir, results); err != nil {
			fatalf("write e2e outputs: %v", err)
		}
	default:
		fatalf("unknown mode %q (expected crypto or e2e)", *mode)
	}
}

func runCryptoSuite(voterCounts []int, numCandidates, runs int) ([]CryptoResult, error) {
	schemes := cryptoutil.DefaultComparisonSchemes()
	var results []CryptoResult

	for _, scheme := range schemes {
		keyGenDurs := make([]float64, 0, runs)
		var pubKey, secKey []byte
		for r := 0; r < runs; r++ {
			start := time.Now()
			p, s, err := scheme.GenerateKeys()
			if err != nil {
				return nil, fmt.Errorf("%s keygen: %w", scheme.Name(), err)
			}
			keyGenDurs = append(keyGenDurs, msSince(start))
			pubKey, secKey = p, s
		}
		keyGenAvg := avg(keyGenDurs)

		for _, n := range voterCounts {
			encDurs := make([]float64, 0, n)
			valDurs := make([]float64, 0, n)
			ballots := make([][]byte, n)

			encWallStart := time.Now()
			for i := 0; i < n; i++ {
				t0 := time.Now()
				ct, err := scheme.EncryptVote(pubKey, i%numCandidates, numCandidates)
				if err != nil {
					return nil, fmt.Errorf("%s encrypt: %w", scheme.Name(), err)
				}
				encDurs = append(encDurs, msSince(t0))
				ballots[i] = ct
			}
			encWallSec := time.Since(encWallStart).Seconds()

			for i := 0; i < n; i++ {
				t0 := time.Now()
				if err := scheme.ValidateBallot(ballots[i]); err != nil {
					return nil, fmt.Errorf("%s validate: %w", scheme.Name(), err)
				}
				valDurs = append(valDurs, msSince(t0))
			}

			tallyDurs := make([]float64, 0, runs)
			var totalAlloc uint64
			for r := 0; r < runs; r++ {
				runtime.GC()
				var mBefore, mAfter runtime.MemStats
				runtime.ReadMemStats(&mBefore)
				t0 := time.Now()
				if _, err := scheme.TallyVotes(secKey, ballots, numCandidates); err != nil {
					return nil, fmt.Errorf("%s tally: %w", scheme.Name(), err)
				}
				tallyDurs = append(tallyDurs, msSince(t0))
				runtime.ReadMemStats(&mAfter)
				totalAlloc += mAfter.TotalAlloc - mBefore.TotalAlloc
			}

			ballotSize := len(ballots[0])
			b64Size := base64.StdEncoding.EncodedLen(ballotSize)
			tallyAvg := avg(tallyDurs)
			tallyBPS := 0.0
			if tallyAvg > 0 {
				tallyBPS = float64(n) / (tallyAvg / 1000.0)
			}

			res := CryptoResult{
				Scheme:           scheme.Name(),
				Voters:           n,
				Candidates:       numCandidates,
				Runs:             runs,
				PubKeyBytes:      len(pubKey),
				SecKeyBytes:      len(secKey),
				BallotBytes:      ballotSize,
				Base64Bytes:      b64Size,
				KeyGenMs:         keyGenAvg,
				EncryptAvgMs:     avg(encDurs),
				EncryptP95Ms:     percentile(encDurs, 95),
				EncryptBPS:       float64(n) / math.Max(encWallSec, 1e-9),
				ValidateAvgMs:    avg(valDurs),
				ValidateP95Ms:    percentile(valDurs, 95),
				TallyAvgMs:       tallyAvg,
				TallyP95Ms:       percentile(tallyDurs, 95),
				TallyBPS:         tallyBPS,
				TallyAllocMB:     float64(totalAlloc) / float64(runs) / (1024 * 1024),
				Storage10kBallot: float64(ballotSize) * 10000.0 / (1024 * 1024),
			}
			results = append(results, res)
		}
	}
	return results, nil
}

func runE2ESuite(ctx context.Context, dsn string, voterCounts []int, numCandidates, concurrency int) ([]E2EResult, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	_, err = pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS evoting.bench_ballots (
			id BIGSERIAL PRIMARY KEY,
			scheme TEXT NOT NULL,
			encrypted_vote BYTEA NOT NULL
		)`)
	if err != nil {
		return nil, fmt.Errorf("create bench table: %w", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DROP TABLE IF EXISTS evoting.bench_ballots`)
	}()

	var results []E2EResult
	for _, scheme := range cryptoutil.DefaultComparisonSchemes() {
		pubKey, secKey, err := scheme.GenerateKeys()
		if err != nil {
			return nil, err
		}

		for _, n := range voterCounts {
			if _, err := pool.Exec(ctx, `TRUNCATE TABLE evoting.bench_ballots`); err != nil {
				return nil, err
			}

			ballotsB64 := make([]string, n)
			var rawSize int
			for i := 0; i < n; i++ {
				ct, err := scheme.EncryptVote(pubKey, i%numCandidates, numCandidates)
				if err != nil {
					return nil, err
				}
				rawSize = len(ct)
				ballotsB64[i] = base64.StdEncoding.EncodeToString(ct)
			}

			srv := newBenchServer(pool, scheme, secKey, numCandidates)
			latencies := make([]float64, n)
			var wg sync.WaitGroup
			sem := make(chan struct{}, concurrency)
			var reqErr error
			var errOnce sync.Once

			wallStart := time.Now()
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()

					payload, _ := json.Marshal(map[string]string{"encrypted_vote": ballotsB64[idx]})
					t0 := time.Now()
					resp, err := http.Post(srv.URL+"/ballots", "application/json", bytes.NewReader(payload))
					latencies[idx] = msSince(t0)
					if err != nil {
						errOnce.Do(func() { reqErr = err })
						return
					}
					resp.Body.Close()
					if resp.StatusCode != http.StatusCreated {
						errOnce.Do(func() { reqErr = fmt.Errorf("unexpected status %d", resp.StatusCode) })
					}
				}(i)
			}
			wg.Wait()
			wallSec := time.Since(wallStart).Seconds()
			if reqErr != nil {
				srv.Close()
				return nil, reqErr
			}

			var tableBytes int64
			err = pool.QueryRow(ctx, `SELECT pg_total_relation_size('evoting.bench_ballots')`).Scan(&tableBytes)
			if err != nil {
				srv.Close()
				return nil, err
			}

			t0 := time.Now()
			resp, err := http.Post(srv.URL+"/tally", "application/json", nil)
			tallyMs := msSince(t0)
			if err != nil {
				srv.Close()
				return nil, err
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				srv.Close()
				return nil, fmt.Errorf("tally status %d", resp.StatusCode)
			}
			srv.Close()

			results = append(results, E2EResult{
				Scheme:         scheme.Name(),
				Voters:         n,
				Concurrency:    concurrency,
				BallotBytes:    rawSize,
				Base64Bytes:    base64.StdEncoding.EncodedLen(rawSize),
				SubmitAvgMs:    avg(latencies),
				SubmitP95Ms:    percentile(latencies, 95),
				SubmitP99Ms:    percentile(latencies, 99),
				SubmitBPS:      float64(n) / math.Max(wallSec, 1e-9),
				TallyHTTPMs:    tallyMs,
				DBTableBytes:   tableBytes,
				DBBytesPerVote: float64(tableBytes) / float64(n),
			})
		}
	}
	return results, nil
}

func newBenchServer(pool *pgxpool.Pool, scheme cryptoutil.VoteCryptoScheme, secKey []byte, numCandidates int) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ballots", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			EncryptedVote string `json:"encrypted_vote"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		raw, err := base64.StdEncoding.DecodeString(body.EncryptedVote)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := scheme.ValidateBallot(raw); err != nil {
			http.Error(w, err.Error(), http.StatusUnprocessableEntity)
			return
		}
		if _, err := pool.Exec(r.Context(),
			`INSERT INTO evoting.bench_ballots (scheme, encrypted_vote) VALUES ($1, $2)`,
			scheme.Name(), raw,
		); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	})

	mux.HandleFunc("POST /tally", func(w http.ResponseWriter, r *http.Request) {
		rows, err := pool.Query(r.Context(), `SELECT encrypted_vote FROM evoting.bench_ballots`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var ballots [][]byte
		for rows.Next() {
			var ct []byte
			if err := rows.Scan(&ct); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			ballots = append(ballots, ct)
		}
		counts, err := scheme.TallyVotes(secKey, ballots, numCandidates)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(counts)
	})

	return httptest.NewServer(mux)
}

func writeCryptoOutputs(dir string, results []CryptoResult) error {
	if err := writeJSONFile(filepath.Join(dir, "crypto_scale.json"), results); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "crypto_scale.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{
		"scheme", "voters", "candidates", "pub_key_bytes", "sec_key_bytes",
		"ballot_bytes", "base64_bytes", "keygen_ms", "encrypt_avg_ms", "encrypt_p95_ms",
		"encrypt_bps", "validate_avg_ms", "validate_p95_ms", "tally_avg_ms", "tally_p95_ms",
		"tally_bps", "tally_alloc_mb", "storage_10k_mb",
	})
	for _, r := range results {
		_ = w.Write([]string{
			r.Scheme, strconv.Itoa(r.Voters), strconv.Itoa(r.Candidates),
			strconv.Itoa(r.PubKeyBytes), strconv.Itoa(r.SecKeyBytes),
			strconv.Itoa(r.BallotBytes), strconv.Itoa(r.Base64Bytes),
			f2(r.KeyGenMs), f4(r.EncryptAvgMs), f4(r.EncryptP95Ms), f2(r.EncryptBPS),
			f4(r.ValidateAvgMs), f4(r.ValidateP95Ms), f2(r.TallyAvgMs), f2(r.TallyP95Ms),
			f2(r.TallyBPS), f2(r.TallyAllocMB), f2(r.Storage10kBallot),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("# Crypto Scalability Report (HE vs Non-HE)\n\n")
	md.WriteString("| Scheme | N | Ballot (B) | Base64 (B) | KeyGen (ms) | Encrypt Avg/P95 (ms) | Tally Avg/P95 (ms) | Tally BPS | Heap Alloc (MB) | Est. 10k Votes (MB) |\n")
	md.WriteString("| :--- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range results {
		md.WriteString(fmt.Sprintf(
			"| %s | %d | %d | %d | %.2f | %.4f / %.4f | %.2f / %.2f | %.1f | %.2f | %.2f |\n",
			r.Scheme, r.Voters, r.BallotBytes, r.Base64Bytes, r.KeyGenMs,
			r.EncryptAvgMs, r.EncryptP95Ms, r.TallyAvgMs, r.TallyP95Ms,
			r.TallyBPS, r.TallyAllocMB, r.Storage10kBallot,
		))
	}
	fmt.Print(md.String())
	return os.WriteFile(filepath.Join(dir, "crypto_report.md"), []byte(md.String()), 0o644)
}

func writeE2EOutputs(dir string, results []E2EResult) error {
	if err := writeJSONFile(filepath.Join(dir, "e2e_results.json"), results); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "e2e_results.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{
		"scheme", "voters", "concurrency", "ballot_bytes", "base64_bytes",
		"submit_avg_ms", "submit_p95_ms", "submit_p99_ms", "submit_bps",
		"tally_http_ms", "db_table_bytes", "db_bytes_per_vote",
	})
	for _, r := range results {
		_ = w.Write([]string{
			r.Scheme, strconv.Itoa(r.Voters), strconv.Itoa(r.Concurrency),
			strconv.Itoa(r.BallotBytes), strconv.Itoa(r.Base64Bytes),
			f2(r.SubmitAvgMs), f2(r.SubmitP95Ms), f2(r.SubmitP99Ms), f2(r.SubmitBPS),
			f2(r.TallyHTTPMs), strconv.FormatInt(r.DBTableBytes, 10), f2(r.DBBytesPerVote),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	var md strings.Builder
	md.WriteString("# End-to-End HTTP + PostgreSQL Report\n\n")
	md.WriteString("| Scheme | N | Concurrency | Submit Avg/P95/P99 (ms) | Submit BPS | Tally HTTP (ms) | DB Size (KB) | DB/Vote (B) |\n")
	md.WriteString("| :--- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, r := range results {
		md.WriteString(fmt.Sprintf(
			"| %s | %d | %d | %.2f / %.2f / %.2f | %.1f | %.2f | %.1f | %.0f |\n",
			r.Scheme, r.Voters, r.Concurrency, r.SubmitAvgMs, r.SubmitP95Ms, r.SubmitP99Ms,
			r.SubmitBPS, r.TallyHTTPMs, float64(r.DBTableBytes)/1024.0, r.DBBytesPerVote,
		))
	}
	fmt.Print(md.String())
	return os.WriteFile(filepath.Join(dir, "e2e_report.md"), []byte(md.String()), 0o644)
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func parseIntList(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid positive int %q", p)
		}
		out = append(out, n)
	}
	return out, nil
}

func defaultDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://evoting_app:change-me@localhost:5433/evoting_db?sslmode=disable"
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t).Nanoseconds()) / 1e6
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	idx := int(math.Ceil((p/100.0)*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func f2(v float64) string { return fmt.Sprintf("%.2f", v) }
func f4(v float64) string { return fmt.Sprintf("%.4f", v) }

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
