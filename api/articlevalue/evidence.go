package articlevalue

import (
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

const (
	PolicyVersion       = "article-value-v4" // 评估唯一键含此版本，避免复用没有摘要的 v3 行
	PromptVersion       = "openai-compatible-article-assessment-v4"
	EvidenceTokenBudget = 6000
	TitleTokenBudget    = 512
	QualityFloor        = 0.20
	OmissionMarker      = "[... omitted from assessment evidence ...]"
)

// Evidence is the bounded, source-independent text supplied to an article
// assessor. BodyText is already extracted clean text; raw HTML, URL, author,
// source identity, publication date, and user behavior are intentionally not
// representable here.
type Evidence struct {
	Title                   string
	BodyText                string
	EstimatedTokens         int
	OriginalEstimatedTokens int
	Truncated               bool
}

type Scores struct {
	Quality   float64
	Depth     float64
	Evergreen float64
}

// EstimateTokens deliberately favors a stable upper-bound-like estimate over
// a provider-specific tokenizer. ASCII runes cost one quarter token and every
// non-ASCII rune costs one token. The same unit function drives slicing, so the
// configured budget is deterministic across languages and models.
func EstimateTokens(value string) int {
	units := estimateUnits(value)
	if units == 0 {
		return 0
	}
	return (units + 3) / 4
}

func BuildEvidence(title string, body string) (Evidence, error) {
	title = normalizeText(title)
	body = normalizeText(body)
	if title == "" && body == "" {
		return Evidence{}, errors.New("article assessment requires title or body text")
	}

	originalTokens := EstimateTokens(title + "\n\n" + body)
	if estimateUnits(title) > TitleTokenBudget*4 {
		titleRunes := []rune(title)
		title = strings.TrimSpace(string(titleRunes[:prefixRuneCount(titleRunes, TitleTokenBudget*4-estimateUnits(OmissionMarker))])) + " " + OmissionMarker
	}
	budgetUnits := EvidenceTokenBudget * 4
	titleUnits := estimateUnits(title)
	separatorUnits := estimateUnits("\n\n")
	bodyBudgetUnits := budgetUnits - titleUnits - separatorUnits
	if bodyBudgetUnits < 0 {
		bodyBudgetUnits = 0
	}
	if estimateUnits(body) <= bodyBudgetUnits {
		text := strings.TrimSpace(title + "\n\n" + body)
		return Evidence{
			Title: title, BodyText: body, EstimatedTokens: EstimateTokens(text),
			OriginalEstimatedTokens: originalTokens,
		}, nil
	}

	markerUnits := estimateUnits("\n\n"+OmissionMarker+"\n\n") * 2
	availableUnits := bodyBudgetUnits - markerUnits
	if availableUnits < 0 {
		availableUnits = 0
	}
	runes := []rune(body)
	headBudget := availableUnits * 45 / 100
	middleBudget := availableUnits * 20 / 100
	tailBudget := availableUnits - headBudget - middleBudget

	headEnd := prefixRuneCount(runes, headBudget)
	tailCount := suffixRuneCount(runes[headEnd:], tailBudget)
	tailStart := len(runes) - tailCount
	if tailStart < headEnd {
		tailStart = headEnd
	}
	middleStart, middleEnd := centeredWindow(runes, headEnd, tailStart, middleBudget)

	parts := make([]string, 0, 5)
	if headEnd > 0 {
		parts = append(parts, strings.TrimSpace(string(runes[:headEnd])))
	}
	parts = append(parts, OmissionMarker)
	if middleStart < middleEnd {
		parts = append(parts, strings.TrimSpace(string(runes[middleStart:middleEnd])))
	}
	parts = append(parts, OmissionMarker)
	if tailStart < len(runes) {
		parts = append(parts, strings.TrimSpace(string(runes[tailStart:])))
	}
	excerpt := strings.Join(parts, "\n\n")
	text := strings.TrimSpace(title + "\n\n" + excerpt)
	return Evidence{
		Title: title, BodyText: excerpt, EstimatedTokens: EstimateTokens(text),
		OriginalEstimatedTokens: originalTokens, Truncated: true,
	}, nil
}

func ApplyEvidenceCaps(scores Scores, originalTokens int) Scores {
	qualityCap, depthCap, evergreenCap := 1.0, 1.0, 1.0
	switch {
	case originalTokens < 80:
		qualityCap, depthCap, evergreenCap = 0.40, 0.35, 0.50
	case originalTokens < 200:
		qualityCap, depthCap, evergreenCap = 0.55, 0.50, 0.65
	case originalTokens < 400:
		qualityCap, depthCap, evergreenCap = 0.70, 0.65, 0.80
	}
	scores.Quality = clamp(math.Min(scores.Quality, qualityCap))
	scores.Depth = clamp(math.Min(scores.Depth, depthCap))
	scores.Evergreen = clamp(math.Min(scores.Evergreen, evergreenCap))
	return scores
}

func EvidenceConfidence(originalTokens int, truncated bool) float64 {
	switch {
	case originalTokens < 80:
		return 0.35
	case originalTokens < 200:
		return 0.50
	case originalTokens < 400:
		return 0.70
	case truncated:
		return 0.80
	default:
		return 0.90
	}
}

// FallbackScores are deliberately conservative. They describe evidence
// sufficiency, not a pretend semantic judgment, and keep a new article usable
// while an optional model is unavailable.
func FallbackScores(originalTokens int) Scores {
	switch {
	case originalTokens < 80:
		return Scores{Quality: 0.30, Depth: 0.20, Evergreen: 0.25}
	case originalTokens < 200:
		return Scores{Quality: 0.35, Depth: 0.25, Evergreen: 0.30}
	case originalTokens < 400:
		return Scores{Quality: 0.40, Depth: 0.30, Evergreen: 0.35}
	default:
		return Scores{Quality: 0.45, Depth: 0.35, Evergreen: 0.40}
	}
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func estimateUnits(value string) int {
	units := 0
	for _, character := range value {
		if character <= 0x7f {
			units++
		} else {
			units += 4
		}
	}
	return units
}

func prefixRuneCount(runes []rune, budgetUnits int) int {
	if budgetUnits <= 0 {
		return 0
	}
	used := 0
	for index, character := range runes {
		cost := runeUnits(character)
		if used+cost > budgetUnits {
			return index
		}
		used += cost
	}
	return len(runes)
}

func suffixRuneCount(runes []rune, budgetUnits int) int {
	if budgetUnits <= 0 {
		return 0
	}
	used := 0
	for index := len(runes) - 1; index >= 0; index-- {
		cost := runeUnits(runes[index])
		if used+cost > budgetUnits {
			return len(runes) - 1 - index
		}
		used += cost
	}
	return len(runes)
}

func centeredWindow(runes []rune, lower int, upper int, budgetUnits int) (int, int) {
	if lower >= upper || budgetUnits <= 0 {
		return lower, lower
	}
	middle := lower + (upper-lower)/2
	leftBudget := budgetUnits / 2
	leftCount := suffixRuneCount(runes[lower:middle], leftBudget)
	start := middle - leftCount
	remaining := budgetUnits - estimateRuneUnits(runes[start:middle])
	rightCount := prefixRuneCount(runes[middle:upper], remaining)
	end := middle + rightCount
	if remainingAfterRight := budgetUnits - estimateRuneUnits(runes[start:end]); remainingAfterRight > 0 && start > lower {
		start -= suffixRuneCount(runes[lower:start], remainingAfterRight)
	}
	return start, end
}

func estimateRuneUnits(runes []rune) int {
	units := 0
	for _, character := range runes {
		units += runeUnits(character)
	}
	return units
}

func runeUnits(character rune) int {
	if character <= 0x7f {
		return 1
	}
	if !utf8.ValidRune(character) {
		return 4
	}
	return 4
}

func clamp(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
