package main

import (
	"regexp"
	"testing"
)

func TestParseElectionInput(t *testing.T) {
	valid := electionInput{
		Title:             "Student council",
		RegistrationStart: "2026-10-01T09:00:00Z",
		RegistrationEnd:   "2026-10-10T09:00:00Z",
		VotingStart:       "2026-10-10T09:00:00Z",
		VotingEnd:         "2026-10-12T09:00:00Z",
		Status:            "registration",
	}
	if _, _, _, _, _, ok := parseElectionInput(valid); !ok {
		t.Fatal("expected valid election details to be accepted")
	}

	tests := []struct {
		name  string
		mutate func(*electionInput)
	}{
		{
			name: "unsupported status",
			mutate: func(input *electionInput) {
				input.Status = "open"
			},
		},
		{
			name: "registration ends after voting starts",
			mutate: func(input *electionInput) {
				input.RegistrationEnd = "2026-10-11T09:00:00Z"
			},
		},
		{
			name: "invalid timestamp",
			mutate: func(input *electionInput) {
				input.VotingEnd = "not-a-time"
			},
		},
		{
			name: "blank title",
			mutate: func(input *electionInput) {
				input.Title = "  "
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, _, _, _, _, ok := parseElectionInput(input); ok {
				t.Fatal("expected invalid election details to be rejected")
			}
		})
	}
}

func TestNewReceiptCode(t *testing.T) {
	code, err := newReceiptCode()
	if err != nil {
		t.Fatalf("generate receipt: %v", err)
	}
	if !regexp.MustCompile(`^EV-[A-Z2-7]{32}$`).MatchString(code) {
		t.Fatalf("receipt code has an invalid format: %q", code)
	}
}
