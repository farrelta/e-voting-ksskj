package cryptoutil

import (
	"fmt"
	"slices"
	"testing"
)

func TestComparisonCorrectness(t *testing.T) {
	const numCandidates = 5
	votes := []int{0, 2, 4, 2, 1, 0, 3, 2, 4, 4, 1, 0, 2, 3, 2}
	expected := make([]uint64, numCandidates)
	for _, v := range votes {
		expected[v]++
	}

	for _, scheme := range DefaultComparisonSchemes() {
		t.Run(scheme.Name(), func(t *testing.T) {
			pub, sec, err := scheme.GenerateKeys()
			if err != nil {
				t.Fatalf("GenerateKeys: %v", err)
			}

			ballots := make([][]byte, 0, len(votes))
			for i, candidateID := range votes {
				ct, err := scheme.EncryptVote(pub, candidateID, numCandidates)
				if err != nil {
					t.Fatalf("EncryptVote[%d]: %v", i, err)
				}
				if err := scheme.ValidateBallot(ct); err != nil {
					t.Fatalf("ValidateBallot[%d]: %v", i, err)
				}
				ballots = append(ballots, ct)
			}

			got, err := scheme.TallyVotes(sec, ballots, numCandidates)
			if err != nil {
				t.Fatalf("TallyVotes: %v", err)
			}
			if !slices.Equal(got, expected) {
				t.Fatalf("tally mismatch: got %v, want %v", got, expected)
			}
		})
	}
}

func BenchmarkSchemeKeyGen(b *testing.B) {
	for _, scheme := range DefaultComparisonSchemes() {
		b.Run(scheme.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				pub, sec, err := scheme.GenerateKeys()
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(pub)), "pub_bytes")
				b.ReportMetric(float64(len(sec)), "sec_bytes")
			}
		})
	}
}

func BenchmarkSchemeEncrypt(b *testing.B) {
	const numCandidates = 5
	for _, scheme := range DefaultComparisonSchemes() {
		pub, _, err := scheme.GenerateKeys()
		if err != nil {
			b.Fatal(err)
		}
		b.Run(scheme.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				ct, err := scheme.EncryptVote(pub, i%numCandidates, numCandidates)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(len(ct)), "ballot_bytes")
			}
		})
	}
}

func BenchmarkSchemeValidate(b *testing.B) {
	const numCandidates = 5
	for _, scheme := range DefaultComparisonSchemes() {
		pub, _, err := scheme.GenerateKeys()
		if err != nil {
			b.Fatal(err)
		}
		ct, err := scheme.EncryptVote(pub, 1, numCandidates)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(scheme.Name(), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := scheme.ValidateBallot(ct); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSchemeTally(b *testing.B) {
	const numCandidates = 5
	voterCounts := []int{10, 100, 500}

	for _, scheme := range DefaultComparisonSchemes() {
		pub, sec, err := scheme.GenerateKeys()
		if err != nil {
			b.Fatal(err)
		}
		for _, n := range voterCounts {
			ballots := make([][]byte, n)
			for i := 0; i < n; i++ {
				ct, err := scheme.EncryptVote(pub, i%numCandidates, numCandidates)
				if err != nil {
					b.Fatal(err)
				}
				ballots[i] = ct
			}
			b.Run(fmt.Sprintf("%s/N=%d", scheme.Name(), n), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					if _, err := scheme.TallyVotes(sec, ballots, numCandidates); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
