package assessmenteval

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"math"
	"sort"
	"strings"
	"time"
)

type scorePair struct {
	gold  AxisScores
	other AxisScores
}

type llmLogEvent struct {
	Event       string `json:"event"`
	CandidateID uint   `json:"candidate_id"`
	Status      string `json:"status"`
	LLMStage    string `json:"llm_stage"`
	Duration    int64  `json:"duration_ms"`
	Usage       *struct {
		Available        bool `json:"available"`
		PromptTokens     int  `json:"prompt_tokens"`
		CompletionTokens int  `json:"completion_tokens"`
		ReasoningTokens  int  `json:"reasoning_tokens"`
		CachedTokens     int  `json:"cached_tokens"`
		TotalTokens      int  `json:"total_tokens"`
	} `json:"llm_usage"`
}

func BuildReport(manifest Manifest, passOne, passTwo LabelSet, adjudication *LabelSet, scores ScoreSet, llmLog io.Reader, now time.Time) (Report, error) {
	for _, digest := range []string{passOne.ManifestDigest, passTwo.ManifestDigest, scores.ManifestDigest} {
		if err := ValidateArtifactLink(manifest, digest); err != nil {
			return Report{}, err
		}
	}
	if passOne.Pass != 1 || passTwo.Pass != 2 {
		return Report{}, errors.New("report requires pass one and pass two label files")
	}
	passOneLabels, err := indexLabels(passOne.Labels)
	if err != nil {
		return Report{}, err
	}
	passTwoLabels, err := indexLabels(passTwo.Labels)
	if err != nil {
		return Report{}, err
	}
	adjudicated := map[string]Label{}
	if adjudication != nil {
		if err := ValidateArtifactLink(manifest, adjudication.ManifestDigest); err != nil {
			return Report{}, err
		}
		adjudicated, err = indexLabels(adjudication.Labels)
		if err != nil {
			return Report{}, err
		}
	}
	firstRun, repeatRun := indexScoreRuns(scores.Records)
	report := Report{Version: ReportVersion, ManifestDigest: manifest.Digest, CreatedAt: now.UTC(), ActivationChecks: make(map[string]bool)}
	humanPairs := make([]scorePair, 0, len(passTwoLabels))
	gold := make(map[string]AxisScores, len(passOneLabels))
	excluded := make(map[string]struct{})
	for _, item := range manifest.Items {
		first, exists := passOneLabels[item.SampleID]
		if !exists || first.Unjudgeable || first.ExtractionBad {
			excluded[item.SampleID] = struct{}{}
			continue
		}
		gold[item.SampleID] = first.Scores
		second, repeated := passTwoLabels[item.SampleID]
		if !repeated || second.Unjudgeable || second.ExtractionBad {
			continue
		}
		humanPairs = append(humanPairs, scorePair{gold: first.Scores, other: second.Scores})
		if scoresConflict(first.Scores, second.Scores) {
			report.Conflicts = append(report.Conflicts, item.SampleID)
			if resolved, ok := adjudicated[item.SampleID]; ok && !resolved.Unjudgeable && !resolved.ExtractionBad {
				gold[item.SampleID] = resolved.Scores
			} else {
				report.UnresolvedConflicts++
				gold[item.SampleID] = averageScores(first.Scores, second.Scores)
			}
		} else {
			gold[item.SampleID] = averageScores(first.Scores, second.Scores)
		}
	}
	report.ExcludedSamples = len(excluded)
	report.HumanConsistency = comparisonMetrics(humanPairs)
	modelAllPairs := make([]scorePair, 0, GoldSampleCount)
	modelCorePairs := make([]scorePair, 0, CoreSampleCount)
	baselineCorePairs := make([]scorePair, 0, CoreSampleCount)
	modelRepeatPairs := make([]scorePair, 0, len(repeatRun))
	for _, item := range manifest.Items {
		goldScores, usable := gold[item.SampleID]
		if !usable {
			continue
		}
		model, scored := firstRun[item.SampleID]
		if scored && model.Error == "" {
			modelAllPairs = append(modelAllPairs, scorePair{gold: goldScores, other: model.Scores})
			if strings.HasPrefix(item.Stratum, "core:") {
				modelCorePairs = append(modelCorePairs, scorePair{gold: goldScores, other: model.Scores})
				baselineCorePairs = append(baselineCorePairs, scorePair{gold: goldScores, other: item.BaselineScores})
			}
			if repeated, ok := repeatRun[item.SampleID]; ok && repeated.Error == "" {
				modelRepeatPairs = append(modelRepeatPairs, scorePair{gold: model.Scores, other: repeated.Scores})
			}
		}
	}
	report.ModelAll = comparisonMetrics(modelAllPairs)
	report.ModelCore = comparisonMetrics(modelCorePairs)
	report.BaselineCore = comparisonMetrics(baselineCorePairs)
	report.ModelRepeat = comparisonMetrics(modelRepeatPairs)
	report.Protocol = protocolMetrics(manifest, scores, llmLog)
	checks := report.ActivationChecks
	checks["human_quality_spearman"] = report.HumanConsistency.Quality.Spearman >= .85
	checks["human_depth_spearman"] = report.HumanConsistency.Depth.Spearman >= .75
	checks["human_evergreen_spearman"] = report.HumanConsistency.Evergreen.Spearman >= .75
	checks["human_mae"] = report.HumanConsistency.Quality.MAE <= 10 && report.HumanConsistency.Depth.MAE <= 10 && report.HumanConsistency.Evergreen.MAE <= 10
	checks["human_band_agreement"] = report.HumanConsistency.Quality.BandAgreement >= .80 && report.HumanConsistency.Depth.BandAgreement >= .80 && report.HumanConsistency.Evergreen.BandAgreement >= .80
	checks["model_quality_rank"] = report.ModelCore.Quality.Spearman >= .70 && report.ModelCore.Quality.Kendall >= .50
	checks["model_quality_mae"] = report.ModelCore.Quality.MAE <= 12
	checks["model_secondary_rank"] = report.ModelCore.Depth.Spearman >= .60 && report.ModelCore.Evergreen.Spearman >= .60
	checks["model_head_tail"] = report.ModelCore.TopQuintileHitRate >= .70 && report.ModelCore.BottomQuintileHitRate >= .70
	checks["model_baseline_gain"] = report.ModelCore.Quality.Spearman-report.BaselineCore.Quality.Spearman >= .10
	checks["model_quality_gate"] = report.ModelAll.EligibleRecall >= .95 && report.ModelAll.LowQualityRejection >= .70
	checks["model_repeat"] = report.ModelRepeat.Quality.Count > 0 && report.ModelRepeat.Quality.MAE <= 5 && report.ModelRepeat.Depth.MAE <= 5 && report.ModelRepeat.Evergreen.MAE <= 5 && report.ModelRepeat.Quality.BandAgreement >= .90 && report.ModelRepeat.Depth.BandAgreement >= .90 && report.ModelRepeat.Evergreen.BandAgreement >= .90
	checks["protocol_valid_output"] = report.Protocol.ValidOutputRate >= .98
	checks["protocol_no_reasoning"] = report.Protocol.ReasoningTokens == 0
	checks["protocol_prompt_budget"] = report.Protocol.PromptTokenP95 > 0 && report.Protocol.PromptTokenP95 <= 7500 && report.Protocol.MaximumPromptTokens <= 8500
	report.ActivationReady = report.UnresolvedConflicts == 0 && len(checks) > 0
	for _, passed := range checks {
		report.ActivationReady = report.ActivationReady && passed
	}
	return report, nil
}

func indexLabels(labels []Label) (map[string]Label, error) {
	indexed := make(map[string]Label, len(labels))
	for _, label := range labels {
		if strings.TrimSpace(label.SampleID) == "" {
			return nil, errors.New("label has an empty sample ID")
		}
		if _, exists := indexed[label.SampleID]; exists {
			return nil, errors.New("label file contains duplicate sample IDs")
		}
		if !label.Unjudgeable && !validScores(label.Scores) {
			return nil, errors.New("label contains an out-of-range score")
		}
		indexed[label.SampleID] = label
	}
	return indexed, nil
}

func indexScoreRuns(records []ScoreRecord) (map[string]ScoreRecord, map[string]ScoreRecord) {
	first := make(map[string]ScoreRecord)
	repeat := make(map[string]ScoreRecord)
	for _, record := range records {
		if record.Run <= 1 {
			first[record.SampleID] = record
		} else if record.Run == 2 {
			repeat[record.SampleID] = record
		}
	}
	return first, repeat
}

func comparisonMetrics(pairs []scorePair) ComparisonMetrics {
	metrics := ComparisonMetrics{
		Quality:   axisMetrics(pairs, func(scores AxisScores) int { return scores.Quality }),
		Depth:     axisMetrics(pairs, func(scores AxisScores) int { return scores.Depth }),
		Evergreen: axisMetrics(pairs, func(scores AxisScores) int { return scores.Evergreen }),
	}
	if len(pairs) == 0 {
		return metrics
	}
	metrics.TopQuintileHitRate = quintileHitRate(pairs, true)
	metrics.BottomQuintileHitRate = quintileHitRate(pairs, false)
	var eligible, eligibleAccepted, low, lowRejected, saturated int
	for _, pair := range pairs {
		if pair.gold.Quality >= 40 {
			eligible++
			if pair.other.Quality >= 20 {
				eligibleAccepted++
			}
		}
		if pair.gold.Quality < 20 {
			low++
			if pair.other.Quality < 20 {
				lowRejected++
			}
		}
		if pair.other.Quality >= 95 {
			saturated++
		}
	}
	metrics.EligibleRecall = ratio(eligibleAccepted, eligible)
	metrics.LowQualityRejection = ratio(lowRejected, low)
	metrics.QualitySaturation = ratio(saturated, len(pairs))
	return metrics
}

func axisMetrics(pairs []scorePair, axis func(AxisScores) int) AxisMetrics {
	gold := make([]float64, 0, len(pairs))
	other := make([]float64, 0, len(pairs))
	var absolute, sameBand float64
	for _, pair := range pairs {
		left, right := axis(pair.gold), axis(pair.other)
		gold = append(gold, float64(left))
		other = append(other, float64(right))
		absolute += math.Abs(float64(left - right))
		if scoreBand(left) == scoreBand(right) {
			sameBand++
		}
	}
	return AxisMetrics{Count: len(pairs), Spearman: spearman(gold, other), Kendall: kendallTauB(gold, other), MAE: divide(absolute, float64(len(pairs))), BandAgreement: divide(sameBand, float64(len(pairs)))}
}

func spearman(left, right []float64) float64 { return pearson(ranks(left), ranks(right)) }

func ranks(values []float64) []float64 {
	indices := make([]int, len(values))
	for index := range indices {
		indices[index] = index
	}
	sort.Slice(indices, func(left, right int) bool { return values[indices[left]] < values[indices[right]] })
	ranked := make([]float64, len(values))
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && values[indices[end]] == values[indices[start]] {
			end++
		}
		rank := (float64(start+1) + float64(end)) / 2
		for position := start; position < end; position++ {
			ranked[indices[position]] = rank
		}
		start = end
	}
	return ranked
}

func pearson(left, right []float64) float64 {
	if len(left) < 2 || len(left) != len(right) {
		return 0
	}
	var leftMean, rightMean float64
	for index := range left {
		leftMean += left[index]
		rightMean += right[index]
	}
	leftMean /= float64(len(left))
	rightMean /= float64(len(right))
	var covariance, leftVariance, rightVariance float64
	for index := range left {
		a, b := left[index]-leftMean, right[index]-rightMean
		covariance += a * b
		leftVariance += a * a
		rightVariance += b * b
	}
	if leftVariance == 0 || rightVariance == 0 {
		return 0
	}
	return covariance / math.Sqrt(leftVariance*rightVariance)
}

func kendallTauB(left, right []float64) float64 {
	if len(left) < 2 || len(left) != len(right) {
		return 0
	}
	var concordant, discordant, tieLeft, tieRight float64
	for first := 0; first < len(left); first++ {
		for second := first + 1; second < len(left); second++ {
			a, b := sign(left[first]-left[second]), sign(right[first]-right[second])
			switch {
			case a == 0 && b != 0:
				tieLeft++
			case b == 0 && a != 0:
				tieRight++
			case a*b > 0:
				concordant++
			case a*b < 0:
				discordant++
			}
		}
	}
	denominator := math.Sqrt((concordant + discordant + tieLeft) * (concordant + discordant + tieRight))
	if denominator == 0 {
		return 0
	}
	return (concordant - discordant) / denominator
}

func quintileHitRate(pairs []scorePair, top bool) float64 {
	if len(pairs) < 5 {
		return 0
	}
	count := int(math.Ceil(float64(len(pairs)) * .20))
	goldOrder := make([]int, len(pairs))
	otherOrder := make([]int, len(pairs))
	for index := range pairs {
		goldOrder[index], otherOrder[index] = index, index
	}
	less := func(scores func(int) int) func(int, int) bool {
		return func(left, right int) bool {
			if top {
				return scores(left) > scores(right)
			}
			return scores(left) < scores(right)
		}
	}
	sort.Slice(goldOrder, less(func(index int) int { return pairs[index].gold.Quality }))
	sort.Slice(otherOrder, less(func(index int) int { return pairs[index].other.Quality }))
	selected := make(map[int]struct{}, count)
	for _, index := range otherOrder[:count] {
		selected[index] = struct{}{}
	}
	var hits int
	for _, index := range goldOrder[:count] {
		if _, ok := selected[index]; ok {
			hits++
		}
	}
	return ratio(hits, count)
}

func protocolMetrics(manifest Manifest, scores ScoreSet, reader io.Reader) ProtocolMetrics {
	metrics := ProtocolMetrics{Calls: len(scores.Records)}
	for _, record := range scores.Records {
		if record.Error == "" {
			metrics.SuccessfulCalls++
		}
	}
	metrics.ValidOutputRate = ratio(metrics.SuccessfulCalls, metrics.Calls)
	if reader == nil {
		return metrics
	}
	candidateIDs := make(map[uint]struct{}, len(manifest.Items))
	for _, item := range manifest.Items {
		candidateIDs[item.CandidateID] = struct{}{}
	}
	promptTokens := make([]int, 0, len(scores.Records))
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	for scanner.Scan() {
		line := scanner.Text()
		marker := strings.Index(line, "dataark_event ")
		if marker < 0 {
			continue
		}
		var event llmLogEvent
		if json.Unmarshal([]byte(line[marker+len("dataark_event "):]), &event) != nil || event.LLMStage != "article_assessment" {
			continue
		}
		if _, ok := candidateIDs[event.CandidateID]; !ok {
			continue
		}
		metrics.DurationMilliseconds += event.Duration
		if event.Usage == nil || !event.Usage.Available {
			continue
		}
		promptTokens = append(promptTokens, event.Usage.PromptTokens)
		metrics.CompletionTokens += event.Usage.CompletionTokens
		metrics.ReasoningTokens += event.Usage.ReasoningTokens
		metrics.CachedTokens += event.Usage.CachedTokens
		metrics.TotalTokens += event.Usage.TotalTokens
	}
	if len(promptTokens) > 0 {
		sort.Ints(promptTokens)
		metrics.MaximumPromptTokens = promptTokens[len(promptTokens)-1]
		metrics.PromptTokenP95 = promptTokens[int(math.Ceil(.95*float64(len(promptTokens))))-1]
	}
	return metrics
}

func scoresConflict(first, second AxisScores) bool {
	return math.Abs(float64(first.Quality-second.Quality)) > 15 || math.Abs(float64(first.Depth-second.Depth)) > 15 || math.Abs(float64(first.Evergreen-second.Evergreen)) > 15 || scoreBand(first.Quality) != scoreBand(second.Quality) || scoreBand(first.Depth) != scoreBand(second.Depth) || scoreBand(first.Evergreen) != scoreBand(second.Evergreen)
}
func averageScores(first, second AxisScores) AxisScores {
	return AxisScores{Quality: (first.Quality + second.Quality + 1) / 2, Depth: (first.Depth + second.Depth + 1) / 2, Evergreen: (first.Evergreen + second.Evergreen + 1) / 2}
}
func scoreBand(score int) int {
	switch {
	case score < 20:
		return 0
	case score < 40:
		return 1
	case score < 60:
		return 2
	case score < 75:
		return 3
	case score < 90:
		return 4
	default:
		return 5
	}
}
func validScores(scores AxisScores) bool {
	return scores.Quality >= 0 && scores.Quality <= 100 && scores.Depth >= 0 && scores.Depth <= 100 && scores.Evergreen >= 0 && scores.Evergreen <= 100
}
func sign(value float64) float64 {
	if value < 0 {
		return -1
	}
	if value > 0 {
		return 1
	}
	return 0
}
func ratio(numerator, denominator int) float64 {
	return divide(float64(numerator), float64(denominator))
}
func divide(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}
