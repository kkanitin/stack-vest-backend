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
		"Analyze this portfolio.",
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

	// Dimensions now live in the system prompt and must not leak into the user prompt.
	if strings.Contains(got, "concentration risk") || strings.Contains(got, "dimensions") {
		t.Errorf("user prompt should not contain dimensions; they belong in the system prompt:\n%s", got)
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

	// Omitted description: no context block at all.
	if got := buildUserPrompt(base); strings.Contains(got, "<user_context>") {
		t.Errorf("empty description should omit the <user_context> block, got:\n%s", got)
	}

	// Present description: wrapped in a <user_context> block, not appended to the name.
	withDesc := base
	withDesc.Description = "income-focused, low volatility"
	got := buildUserPrompt(withDesc)
	if !strings.Contains(got, "<user_context>\nincome-focused, low volatility\n</user_context>") {
		t.Errorf("description not wrapped in a <user_context> block, got:\n%s", got)
	}
	if strings.Contains(got, "Dividend portfolio — income-focused") {
		t.Errorf("description should no longer be appended to the name header, got:\n%s", got)
	}
}

func TestSystemPrompt(t *testing.T) {
	for _, want := range []string{
		"You are a portfolio analyst that evaluates ETF investment portfolios",
		"valid JSON object",
		"Rebalance Score",
	} {
		if !strings.Contains(systemPrompt, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}
