package analysis

import (
	"strings"
	"testing"
)

func TestBuildUserPrompt(t *testing.T) {
	in := Input{
		Name: "Dividend portfolio",
		Holdings: []Holding{
			{Ticker: "JEPI", Actual: 42, Target: 34},
			{Ticker: "DGRO", Actual: 28, Target: 33},
			{Ticker: "SCHY", Actual: 30, Target: 33},
		},
		Dimensions: []string{"concentration risk", "drift detection", "rebalancing actions"},
	}

	got := buildUserPrompt(in)

	wantSubstrings := []string{
		"Analyze the following portfolio across these dimensions: concentration risk, drift detection, rebalancing actions.",
		"Dividend portfolio:",
		"  JEPI: actual 42%, target 34% (+8% over)",
		"  DGRO: actual 28%, target 33% (-5% under)",
		"  SCHY: actual 30%, target 33% (-3% under)",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("user prompt missing %q\n--- full prompt ---\n%s", want, got)
		}
	}

	// Persona/output rules belong in the system prompt, not the user prompt.
	if strings.Contains(got, "You are a portfolio analyst") {
		t.Errorf("user prompt should not contain the persona; it belongs in the system prompt")
	}
}

func TestBuildUserPrompt_Description(t *testing.T) {
	base := Input{
		Name:       "Dividend portfolio",
		Holdings:   []Holding{{Ticker: "JEPI", Actual: 42, Target: 34}},
		Dimensions: []string{"concentration risk"},
	}

	// Omitted description: header is just the name, no trailing separator.
	if got := buildUserPrompt(base); !strings.Contains(got, "Dividend portfolio:") || strings.Contains(got, "—") {
		t.Errorf("empty description should yield a plain name header, got:\n%s", got)
	}

	// Present description: appended to the name header.
	withDesc := base
	withDesc.Description = "income-focused, low volatility"
	if got := buildUserPrompt(withDesc); !strings.Contains(got, "Dividend portfolio — income-focused, low volatility:") {
		t.Errorf("description not included in header, got:\n%s", got)
	}
}

func TestSystemPrompt(t *testing.T) {
	for _, want := range []string{
		"You are a portfolio analyst reviewing an ETF investment portfolio",
		"No bullet points. No headers.",
		"Keep it under 250 words.",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
