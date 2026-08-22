package recommendation

import (
	"DataArk/discovery"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// EmbedDiscoveryCandidate 为单篇候选写入 embedding；SQLite 只记录模型名。
func EmbedDiscoveryCandidate(ctx context.Context, candidateID uint, provider EmbeddingProvider, model string) error {
	if provider == nil {
		return errors.New("missing embedding provider")
	}
	if db == nil || candidateID == 0 {
		return nil
	}
	var candidate DiscoveryCandidate
	if err := db.First(&candidate, candidateID).Error; err != nil {
		return err
	}
	text := strings.Join(strings.Fields(candidate.Title+" "+candidate.Summary+" "+candidate.BodyText), " ")
	if text == "" {
		text = candidate.URL
	}
	vectors, err := provider.Embed(ctx, []string{text})
	if err != nil {
		return err
	}
	if len(vectors) != 1 {
		return fmt.Errorf("embedding provider returned %d vectors, want 1", len(vectors))
	}
	return StoreCandidateEmbedding(ctx, candidateID, model, vectors[0])
}

// EmbedReadyDiscoveryCandidates 给尚未写入向量的硬合格代表补 embedding。
func EmbedReadyDiscoveryCandidates(ctx context.Context, limit int, provider EmbeddingProvider, model string) (int, error) {
	if db == nil || provider == nil {
		return 0, nil
	}
	if limit <= 0 {
		limit = 50
	}
	// 只给 v3 硬合格代表补向量，不再依赖已停用的 enrichment_status=ready。
	var candidates []DiscoveryCandidate
	if err := db.Where("processing_state = ? AND eligibility_state = ? AND dedupe_state = ?",
		discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible, discovery.DiscoveryDedupeReady).
		Where("representative_id IS NULL OR representative_id = id").
		Where("embedding_model = '' OR embedding_model IS NULL").
		Order("last_seen_at desc, id desc").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return 0, err
	}
	count := 0
	var firstErr error
	for _, candidate := range candidates {
		if err := EmbedDiscoveryCandidate(ctx, candidate.ID, provider, model); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		count++
	}
	return count, firstErr
}

func StoreCandidateEmbedding(ctx context.Context, candidateID uint, model string, vector []float32) error {
	if db == nil || candidateID == 0 {
		return nil
	}
	vectorLiteral, err := FormatPGVector(vector)
	if err != nil {
		return err
	}
	if db.Dialector.Name() != "postgres" {
		return db.Model(&DiscoveryCandidate{}).Where("id = ?", candidateID).Updates(map[string]interface{}{
			"embedding_model": strings.TrimSpace(model),
			"updated_at":      time.Now(),
		}).Error
	}
	return db.WithContext(ctx).Exec(
		"UPDATE discovery_candidates SET embedding = ?::vector, embedding_model = ?, updated_at = NOW() WHERE id = ?",
		vectorLiteral,
		strings.TrimSpace(model),
		candidateID,
	).Error
}

func loadPGVectorCandidateIDs(ctx context.Context, profile *UserRecommendationProfile, limit int) ([]uint, error) {
	if db == nil || db.Dialector.Name() != "postgres" || profile == nil || limit <= 0 {
		return []uint{}, nil
	}
	vector, err := parseFloat32JSONVector(profile.PositiveEmbedding)
	if err != nil || len(vector) == 0 {
		return []uint{}, nil
	}
	vectorLiteral, err := FormatPGVector(vector)
	if err != nil {
		return []uint{}, nil
	}
	var rows []struct {
		ID uint `gorm:"column:id"`
	}
	// 向量召回与选文共用硬合格门禁，避免已停用的 enrichment_status 把召回永远滤空。
	if err := db.WithContext(ctx).
		Raw(`SELECT id FROM discovery_candidates
			WHERE processing_state = ? AND eligibility_state = ? AND dedupe_state = ?
			  AND (representative_id IS NULL OR representative_id = id)
			  AND embedding IS NOT NULL
			ORDER BY embedding <=> ?::vector
			LIMIT ?`,
			discovery.DiscoveryProcessingReady, discovery.DiscoveryEligibilityEligible, discovery.DiscoveryDedupeReady,
			vectorLiteral, limit).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids, nil
}

func loadCandidateEmbeddingVectors(candidateIDs []uint) (map[uint][]float32, error) {
	vectors := make(map[uint][]float32)
	if db == nil || db.Dialector.Name() != "postgres" || len(candidateIDs) == 0 {
		return vectors, nil
	}
	var rows []struct {
		ID        uint   `gorm:"column:id"`
		Embedding string `gorm:"column:embedding"`
	}
	if err := db.Raw("SELECT id, embedding::text AS embedding FROM discovery_candidates WHERE id IN ? AND embedding IS NOT NULL", candidateIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		vector, err := ParsePGVector(row.Embedding)
		if err == nil {
			vectors[row.ID] = vector
		}
	}
	return vectors, nil
}

func FormatPGVector(vector []float32) (string, error) {
	if len(vector) == 0 {
		return "", errors.New("empty embedding vector")
	}
	parts := make([]string, 0, len(vector))
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return "", errors.New("embedding vector contains non-finite value")
		}
		parts = append(parts, strconv.FormatFloat(float64(value), 'f', -1, 32))
	}
	return "[" + strings.Join(parts, ",") + "]", nil
}

func ParsePGVector(raw string) ([]float32, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(strings.TrimSuffix(raw, "]"), "[")
	if strings.TrimSpace(raw) == "" {
		return []float32{}, nil
	}
	parts := strings.Split(raw, ",")
	vector := make([]float32, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseFloat(strings.TrimSpace(part), 32)
		if err != nil {
			return nil, err
		}
		vector = append(vector, float32(value))
	}
	return vector, nil
}

func averageVectors(vectors [][]float32) []float32 {
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return []float32{}
	}
	dimension := len(vectors[0])
	sum := make([]float32, dimension)
	count := 0
	for _, vector := range vectors {
		if len(vector) != dimension {
			continue
		}
		for index, value := range vector {
			sum[index] += value
		}
		count++
	}
	if count == 0 {
		return []float32{}
	}
	for index := range sum {
		sum[index] /= float32(count)
	}
	return sum
}

func marshalVector(vector []float32) string {
	data, _ := json.Marshal(vector)
	return string(data)
}

func parseFloat32JSONVector(raw string) ([]float32, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []float32{}, nil
	}
	var values []float64
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	vector := make([]float32, 0, len(values))
	for _, value := range values {
		vector = append(vector, float32(value))
	}
	return vector, nil
}
