package discovery

import (
	"DataArk/articlevalue"
	"DataArk/config"
	"DataArk/dedupe"
	"DataArk/jobqueue"
	"DataArk/material"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockDedupeSetting(database *gorm.DB) (dedupe.Setting, error) {
	seed := dedupe.Setting{ID: 1, Mode: "shadow"}
	if err := database.Clauses(clause.OnConflict{DoNothing: true}).Create(&seed).Error; err != nil {
		return seed, err
	}
	var setting dedupe.Setting
	err := database.Clauses(clause.Locking{Strength: "UPDATE"}).First(&setting, 1).Error
	return setting, err
}
func CreateDedupeRun(ctx context.Context) (dedupe.Run, error) {
	return createDedupeRun(ctx, false)
}

// createDedupeRun idempotently builds or reuses a full-library run. autoApply
// marks the run to flow straight from previewing into applying; it is set only
// by the assessment orchestration so manually created runs keep their operator
// preview checkpoint at ready.
func createDedupeRun(ctx context.Context, autoApply bool) (dedupe.Run, error) {
	var run dedupe.Run
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		setting, err := lockDedupeSetting(tx)
		if err != nil {
			return err
		}
		var existing dedupe.Run
		if err := tx.Where("status IN ?", []string{"indexing", "previewing", "applying", "rolling_back", "ready"}).Where("status<>? OR previous_run_id=?", "ready", setting.ActiveRunID).Order("id DESC").Limit(1).Find(&existing).Error; err != nil {
			return err
		}
		if existing.ID != 0 {
			run = existing
			if autoApply && !run.AutoApply {
				if err := tx.Model(&dedupe.Run{}).Where("id=?", run.ID).Update("auto_apply", true).Error; err != nil {
					return err
				}
				run.AutoApply = true
			}
			return nil
		}
		run = dedupe.Run{Algorithm: dedupe.Algorithm, Status: "indexing", PreviousRunID: setting.ActiveRunID, AutoApply: autoApply, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		if err := dedupeArticles(tx).Select("COALESCE(MAX(r.id),0)").Scan(&run.HighWaterID).Error; err != nil {
			return err
		}
		return tx.Create(&run).Error
	})
	if err != nil {
		return run, err
	}
	if run.Status != "ready" {
		err = enqueueDedupeTask(ctx, runDedupeArgs(run))
	}
	return run, err
}
func runDedupeArgs(run dedupe.Run) jobqueue.DedupeArgs {
	return jobqueue.DedupeArgs{RunID: run.ID, Algorithm: run.Algorithm, Phase: run.Status, Cursor: run.Cursor}
}
func RequestDedupeAction(ctx context.Context, id uint, action string) (dedupe.Run, error) {
	var run dedupe.Run
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		setting, err := lockDedupeSetting(tx)
		if err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, id).Error; err != nil {
			return err
		}
		switch action {
		case "apply":
			if run.Status == "active" || run.Status == "applying" {
				if run.Status == "active" && setting.ActiveRunID != run.ID {
					return ErrDedupeConflict
				}
				return nil
			}
			if run.Status != "ready" {
				return ErrDedupeConflict
			}
			// Only one writer may apply/rollback; previews created against an older generation are rejected.
			var count int64
			if err := tx.Model(&dedupe.Run{}).Where("id<>? AND status IN ?", id, []string{"indexing", "previewing", "applying", "rolling_back"}).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 || run.PreviousRunID != setting.ActiveRunID {
				return ErrDedupeConflict
			}
			run.Status = "applying"
		case "rollback":
			if run.Status == "rolled_back" || run.Status == "rolling_back" {
				return nil
			}
			if run.Status != "active" || setting.ActiveRunID != id {
				return ErrDedupeConflict
			}
			var builds int64
			if err := tx.Model(&dedupe.Run{}).Where("id<>? AND status IN ?", id, []string{"indexing", "previewing", "applying", "rolling_back"}).Count(&builds).Error; err != nil {
				return err
			}
			if builds > 0 {
				return ErrDedupeConflict
			}
			run.Status = "rolling_back"
		default:
			return ErrDedupeConflict
		}
		run.Cursor = 0
		run.Error = ""
		run.UpdatedAt = time.Now()
		return tx.Save(&run).Error
	})
	if err != nil {
		return run, err
	}
	if run.Status == "applying" || run.Status == "rolling_back" {
		err = enqueueDedupeTask(ctx, runDedupeArgs(run))
	}
	return run, err
}

// EnsureDedupeForAssessment makes duplicate suppression live before an
// assessment run may spend LLM calls, so the pipeline never pays for articles
// suppression is about to hide. It reports whether the caller may start
// assessing now; when false, an auto-apply run is building (or was already in
// flight) and recovery releases the deferred assessment once suppression goes
// active. Mode exact means an operator deliberately rolled suppression back,
// so assessment proceeds under exact-only gating instead of re-applying a run
// behind their back.
func EnsureDedupeForAssessment(ctx context.Context) (dedupe.Run, bool, error) {
	var run dedupe.Run
	if db == nil {
		return run, true, nil
	}
	var proceed bool
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		setting, err := lockDedupeSetting(tx)
		if err != nil {
			return err
		}
		if setting.Mode == "active" || setting.Mode == "exact" {
			proceed = true
			return nil
		}
		setting.PendingAssessment = true
		return tx.Save(&setting).Error
	})
	if err != nil || proceed {
		return run, proceed, err
	}
	run, err = createDedupeRun(ctx, true)
	if err != nil {
		return run, false, err
	}
	if run.Status == "ready" {
		run, err = RequestDedupeAction(ctx, run.ID, "apply")
	}
	return run, false, err
}

type DedupeReport struct {
	LatencyP50MS             int64           `json:"latencyP50Ms"`
	LatencyP95MS             int64           `json:"latencyP95Ms"`
	MaxCandidates            int             `json:"maxCandidates"`
	Run                      dedupe.Run      `json:"run"`
	TotalCurrent             int64           `json:"totalCurrent"`
	IndexedCurrent           int64           `json:"indexedCurrent"`
	Representatives          int64           `json:"representatives"`
	ExactCovered             int64           `json:"exactCovered"`
	NearCovered              int64           `json:"nearCovered"`
	ExistingAssessmentReused int64           `json:"existingAssessmentReused"`
	PendingTargetsSaved      int64           `json:"pendingTargetsSaved"`
	Members                  []dedupe.Member `json:"members"`
	NextCursor               uint            `json:"nextCursor"`
}

func GetDedupeReport(ctx context.Context, id, cursor uint, limit int) (DedupeReport, error) {
	result := DedupeReport{Members: []dedupe.Member{}}
	database := db.WithContext(ctx)
	if err := database.First(&result.Run, id).Error; err != nil {
		return result, err
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 250 {
		limit = 250
	}
	if err := dedupeArticles(database).Count(&result.TotalCurrent).Error; err != nil {
		return result, err
	}
	if err := dedupeArticles(database).Joins("JOIN article_dedupe_fingerprints f ON f.representation_id=r.id AND f.algorithm=?", dedupe.Algorithm).Count(&result.IndexedCurrent).Error; err != nil {
		return result, err
	}
	members := func() *gorm.DB {
		return database.Table("article_dedupe_members d").Joins("JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").Where("d.run_id=?", id).Where("NOT EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id) OR EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=m.id AND c.processing_state='ready')")
	}
	if err := members().Where("d.representation_id=d.representative_id").Count(&result.Representatives).Error; err != nil {
		return result, err
	}
	if err := members().Where("d.representation_id<>d.representative_id AND d.method=?", "exact_body").Count(&result.ExactCovered).Error; err != nil {
		return result, err
	}
	if err := members().Where("d.representation_id<>d.representative_id AND d.method=?", "near_body").Count(&result.NearCovered).Error; err != nil {
		return result, err
	}
	// Count saved logical targets, not duplicate candidates or already independently assessed members.
	if err := members().Joins("JOIN material_article_states s ON s.material_id=d.material_id").Where("d.representation_id<>d.representative_id AND d.baseline_pending=? AND s.assessment_state=?", true, "pending").Count(&result.PendingTargetsSaved).Error; err != nil {
		return result, err
	}
	// One aggregate replaces a per-representative loop; a report request must
	// not scale with library size.
	if database.Migrator().HasTable("discovery_article_assessments") {
		if err := members().Select("COUNT(*)").
			Joins("JOIN material_representations r ON r.id=d.representation_id AND r.kind='text' AND r.role='body' AND r.text <> ''").
			Joins("JOIN material_versions v ON v.id=d.version_id").
			Joins("JOIN material_article_states s ON s.material_id=d.material_id").
			Joins("JOIN discovery_article_assessments a ON a.id=s.current_assessment_id AND a.material_id=d.material_id AND a.content_version=v.version AND a.assessor=? AND a.assessor_version=? AND a.policy_version=?",
				dedupeAssessorName(), dedupeAssessorVersion(), articlevalue.PolicyVersion).
			Where("d.representation_id=d.representative_id").
			Scan(&result.ExistingAssessmentReused).Error; err != nil {
			return result, err
		}
	}

	if err := database.Where("run_id=? AND representation_id>?", id, cursor).Order("representation_id").Limit(limit).Find(&result.Members).Error; err != nil {
		return result, err
	}
	var durations []int64
	if err := members().Where("d.duration_ms>0").Pluck("d.duration_ms", &durations).Error; err != nil {
		return result, err
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if len(durations) > 0 {
		result.LatencyP50MS = durations[(len(durations)-1)*50/100]
		result.LatencyP95MS = durations[(len(durations)-1)*95/100]
	}
	if err := members().Select("COALESCE(MAX(d.candidate_count),0)").Scan(&result.MaxCandidates).Error; err != nil {
		return result, err
	}
	if len(result.Members) == limit {
		result.NextCursor = result.Members[len(result.Members)-1].RepresentationID
	}
	return result, nil
}

type DedupeClusterDetail struct {
	ClusterID  string                       `json:"clusterId"`
	Members    []dedupe.Member              `json:"members"`
	Coverage   map[uint]DedupeQualification `json:"coverage"`
	NextCursor uint                         `json:"nextCursor"`
}

func GetDedupeCluster(ctx context.Context, clusterID string, cursor uint, limit int) (DedupeClusterDetail, error) {
	result := DedupeClusterDetail{ClusterID: clusterID, Members: []dedupe.Member{}, Coverage: map[uint]DedupeQualification{}}
	var runID, representative uint
	if _, err := fmt.Sscanf(clusterID, "idx-%d-%d", &runID, &representative); err != nil || runID == 0 || representative == 0 {
		return result, gorm.ErrRecordNotFound
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 250 {
		limit = 250
	}
	var count int64
	if clusterID != fmt.Sprintf("idx-%d-%d", runID, representative) {
		return result, gorm.ErrRecordNotFound
	}
	if err := db.WithContext(ctx).Model(&dedupe.Member{}).Where("run_id=? AND representative_id=?", runID, representative).Count(&count).Error; err != nil {
		return result, err
	}
	if count == 0 {
		return result, gorm.ErrRecordNotFound
	}
	if err := db.WithContext(ctx).Where("run_id=? AND representative_id=? AND representation_id>?", runID, representative, cursor).Order("representation_id").Limit(limit).Find(&result.Members).Error; err != nil {
		return result, err
	}
	for _, member := range result.Members {
		qualification, err := ArticleDedupeQualification(db.WithContext(ctx), member.MaterialID)
		if err != nil {
			return result, err
		}
		result.Coverage[member.MaterialID] = qualification
	}
	if len(result.Members) == limit {
		result.NextCursor = result.Members[len(result.Members)-1].RepresentationID
	}
	return result, nil
}

func applyDedupeGroup(database *gorm.DB, runID, representativeID uint) error {
	representative, err := loadDedupeArticle(database, representativeID)
	usable := false
	if err == nil {
		usable, err = usableDedupeArticle(database, representative)
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !usable {
		// The preview's representative may have changed or become inaccessible.
		// Revalidate current members; never apply an obsolete preview blindly.
		var group []dedupe.Member
		if err := database.Where("run_id=? AND representative_id=?", runID, representativeID).Order("representation_id").Find(&group).Error; err != nil {
			return err
		}
		current := []dedupeArticle{}
		for _, member := range group {
			article, err := loadDedupeArticle(database, member.RepresentationID)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			current = append(current, article)
		}
		if len(current) == 0 {
			return nil
		}
		selected, err := selectDedupeRepresentative(database, current, 0)
		if err != nil {
			return err
		}
		if selected.RepresentationID != 0 {
			if err := database.Model(&dedupe.Member{}).Where("run_id=? AND representative_id=?", runID, representativeID).Updates(map[string]interface{}{"representative_id": selected.RepresentationID, "applied": false, "revision": gorm.Expr("revision+1")}).Error; err != nil {
				return err
			}
			for _, article := range current {
				score, _ := dedupe.Verify(article.Text, selected.Text)
				method := "near_body"
				if article.ContentHash == selected.ContentHash {
					method = "exact_body"
				}
				if article.RepresentationID == selected.RepresentationID {
					method = "unique"
				}
				if err := database.Model(&dedupe.Member{}).Where("run_id=? AND representation_id=?", runID, article.RepresentationID).Updates(map[string]interface{}{"method": method, "similarity": score}).Error; err != nil {
					return err
				}
			}
			return applyDedupeGroup(database, runID, selected.RepresentationID)
		}
		// No safe representative: retain independent articles rather than suppress them.
		for _, article := range current {
			if err := database.Model(&dedupe.Member{}).Where("run_id=? AND representation_id=?", runID, article.RepresentationID).Updates(map[string]interface{}{"representative_id": article.RepresentationID, "method": "unique", "similarity": 1, "applied": false, "revision": gorm.Expr("revision+1")}).Error; err != nil {
				return err
			}
			available, err := usableDedupeArticle(database, article)
			if err != nil {
				return err
			}
			if available {
				if err := applyDedupeGroup(database, runID, article.RepresentationID); err != nil {
					return err
				}
			}
		}
		return nil
	}

	var members []dedupe.Member
	if err := database.Where("run_id=? AND representative_id=?", runID, representativeID).
		Where("NOT applied OR EXISTS (SELECT 1 FROM discovery_candidates c WHERE c.material_id=article_dedupe_members.material_id AND c.processing_state='ready' AND c.dedupe_state<>'ready')").
		Order("representation_id").Find(&members).Error; err != nil {
		return err
	}
	clusterID := fmt.Sprintf("idx-%d-%d", runID, representativeID)
	var candidateCount int64
	if err := database.Table("article_dedupe_members d").Joins("JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").Joins("JOIN discovery_candidates c ON c.material_id=d.material_id").Where("d.run_id=? AND d.representative_id=?", runID, representativeID).Count(&candidateCount).Error; err != nil {
		return err
	}
	if err := LockDedupeMaterialState(database, representative.MaterialID); err != nil {
		return err
	}
	for _, member := range members {
		article, err := loadDedupeArticle(database, member.RepresentationID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if _, ok := dedupe.Verify(article.Text, representative.Text); !ok {
			continue
		}
		if err := LockDedupeMaterialState(database, article.MaterialID); err != nil {
			return err
		}
		var candidates []DiscoveryCandidate
		if err := Candidates(database).Where("material_id=?", article.MaterialID).Order("id").Find(&candidates).Error; err != nil {
			return err
		}
		for _, candidate := range candidates {
			before := jsonProjection(candidate)
			values := map[string]interface{}{"duplicate_cluster_id": clusterID, "dedupe_key": clusterID, "dedupe_state": DiscoveryDedupeReady, "updated_at": time.Now()}
			if representative.CandidateID != 0 {
				values["representative_id"] = representative.CandidateID
			} else {
				values["representative_id"] = nil
			}
			if article.MaterialID != representative.MaterialID {
				values["eligibility_state"] = DiscoveryEligibilityIneligible
				values["eligibility_reasons"] = "duplicate_non_representative"
			} else if candidate.EligibilityReasons == "duplicate_non_representative" {
				values["eligibility_state"] = DiscoveryEligibilityUnknown
				values["eligibility_reasons"] = "assessment_pending"
				valid, err := hasValidDedupeAssessment(database, article)
				if err != nil {
					return err
				}
				if valid {
					values["eligibility_state"] = DiscoveryEligibilityEligible
					values["eligibility_reasons"] = ""
					if candidate.QualityScore < config.DISCOVERYARTICLEQUALITYTHRESHOLD {
						values["eligibility_state"] = DiscoveryEligibilityIneligible
						values["eligibility_reasons"] = "quality_below_threshold"
					}
				}
			}
			if err := UpdateCandidate(database, candidate.ID, values).Error; err != nil {
				return err
			}
			var updated DiscoveryCandidate
			if err := database.First(&updated, candidate.ID).Error; err != nil {
				return err
			}
			var change dedupe.Change
			if err := database.Where("run_id=? AND candidate_id=?", runID, candidate.ID).Limit(1).Find(&change).Error; err != nil {
				return err
			}
			if change.ID == 0 || change.VersionID != article.VersionID {
				change.Before = before
			}
			change.RunID = runID
			change.CandidateID = candidate.ID
			change.VersionID = article.VersionID
			change.Revision = member.Revision
			change.After = jsonProjection(updated)
			if err := database.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "candidate_id"}}, DoUpdates: clause.AssignmentColumns([]string{"version_id", "revision", "before", "after"})}).Create(&change).Error; err != nil {
				return err
			}
		}
		if err := database.Model(&dedupe.Member{}).Where("run_id=? AND representation_id=? AND revision=?", runID, member.RepresentationID, member.Revision).Update("applied", true).Error; err != nil {
			return err
		}
	}
	if representative.CandidateID != 0 {
		now := time.Now()
		cluster := DiscoveryDuplicateCluster{ClusterID: clusterID, RepresentativeID: representative.CandidateID, MatchMethod: "near_body", RepresentativeReason: "versioned full-library fingerprint verification", MemberCount: uint(candidateCount), CreatedAt: now, UpdatedAt: now}
		return database.Clauses(clause.OnConflict{UpdateAll: true}).Create(&cluster).Error
	}
	return nil
}

func RunDedupeTask(ctx context.Context, args jobqueue.DedupeArgs) error {
	if args.Algorithm != dedupe.Algorithm {
		return nil
	}
	if args.RunID == 0 {
		return processIncrementalDedupe(ctx, args.RepresentationID)
	}
	err := processDedupeRunBatch(ctx, args)
	if err != nil {
		return markDedupeFailure(ctx, args.RunID, err)
	}
	return nil
}
func processDedupeRunBatch(ctx context.Context, args jobqueue.DedupeArgs) error {
	var next dedupe.Run
	prepared := map[uint][]byte{}
	var input []dedupeArticle
	if args.Phase == "indexing" || args.Phase == "previewing" {
		var snapshot dedupe.Run
		if err := db.WithContext(ctx).First(&snapshot, args.RunID).Error; err != nil {
			return err
		}
		if err := dedupeArticles(db.WithContext(ctx)).Where("r.id>? AND r.id<=?", args.Cursor, snapshot.HighWaterID).Order("r.id").Limit(dedupeBatchSize).Scan(&input).Error; err != nil {
			return err
		}
		ids := make([]uint, 0, len(input))
		for _, article := range input {
			ids = append(ids, article.RepresentationID)
		}
		if len(ids) > 0 {
			var fingerprints []dedupe.Fingerprint
			if err := db.WithContext(ctx).Where("representation_id IN ? AND algorithm=?", ids, dedupe.Algorithm).Find(&fingerprints).Error; err != nil {
				return err
			}
			for _, fingerprint := range fingerprints {
				prepared[fingerprint.RepresentationID] = fingerprint.Signature
			}
		}
		for _, article := range input {
			if len(prepared[article.RepresentationID]) == 0 {
				prepared[article.RepresentationID] = dedupe.Signature(article.Text)
			}
		}

	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		setting, err := lockDedupeSetting(tx)
		if err != nil {
			return err
		}
		var run dedupe.Run
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, args.RunID).Error; err != nil {
			return err
		}
		// A redelivered job must continue from durable state even after the checkpoint commit.
		next = run
		if run.Status != args.Phase || run.Cursor != args.Cursor {
			return nil
		}
		switch run.Status {
		case "indexing", "previewing":
			articles := input
			for _, article := range articles {
				run.Cursor = article.RepresentationID
				current, err := loadDedupeArticle(tx, article.RepresentationID)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				article = current
				signature := prepared[article.RepresentationID]
				if err := indexDedupeArticle(tx, article, signature); err != nil {
					return err
				}
				if run.Status == "previewing" {
					if err := assignDedupeMember(tx, run.ID, article, signature); err != nil {
						return err
					}
					run.Processed++
				} else {
					run.Indexed++
				}
				run.Cursor = article.RepresentationID
			}
			if len(articles) < dedupeBatchSize {
				run.Cursor = 0
				switch {
				case run.Status == "indexing":
					run.Status = "previewing"
				case run.AutoApply:
					run.Status = "applying"
				default:
					run.Status = "ready"
				}
			}
		case "applying":
			var members []dedupe.Member
			if err := tx.Where("run_id=? AND representation_id>? AND representation_id=representative_id", run.ID, run.Cursor).Order("representation_id").Limit(dedupeWriteBatchSize).Find(&members).Error; err != nil {
				return err
			}
			for _, member := range members {
				if err := applyDedupeGroup(tx, run.ID, member.RepresentationID); err != nil {
					return err
				}
				run.Applied++
				run.Cursor = member.RepresentationID
			}
			if len(members) < dedupeWriteBatchSize {
				setting.ActiveRunID = run.ID
				setting.Mode = "active"
				if err := tx.Save(&setting).Error; err != nil {
					return err
				}
				run.Status = "active"
				run.Cursor = 0
			}
		case "rolling_back":
			var changes []dedupe.Change
			if err := tx.Where("run_id=? AND id>?", run.ID, run.Cursor).Order("id").Limit(dedupeWriteBatchSize).Find(&changes).Error; err != nil {
				return err
			}
			for _, change := range changes {
				if err := rollbackDedupeChange(tx, change); err != nil {
					return err
				}
				run.Cursor = change.ID
			}
			if len(changes) < dedupeWriteBatchSize {
				// Restore legacy projections where safe, but disable old heuristic near suppression.
				setting.ActiveRunID = 0
				setting.Mode = "exact"
				if err := tx.Save(&setting).Error; err != nil {
					return err
				}
				run.Status = "rolled_back"
				run.Cursor = 0
			}
		default:
			return nil
		}
		run.Error = ""
		run.UpdatedAt = time.Now()
		next = run
		return tx.Save(&run).Error
	})
	if err != nil {
		return err
	}
	switch next.Status {
	case "indexing", "previewing", "applying", "rolling_back":
		return enqueueDedupeTask(ctx, runDedupeArgs(next))
	case "active":
		return RecoverDedupeJobs(ctx)
	case "rolled_back":
		return enqueueCurrentDedupeRepresentatives(ctx)
	}
	return nil
}

func rollbackDedupeChange(database *gorm.DB, change dedupe.Change) error {
	var candidate DiscoveryCandidate
	if err := database.First(&candidate, change.CandidateID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if err := LockDedupeMaterialState(database, candidate.MaterialID); err != nil {
		return err
	}
	var article dedupeArticle
	result := dedupeArticles(database).Where("m.id=?", candidate.MaterialID).Scan(&article)
	if result.Error != nil {
		return result.Error
	}
	var values map[string]interface{}
	if article.VersionID == change.VersionID && jsonProjection(candidate) == change.After {
		if err := json.Unmarshal([]byte(change.Before), &values); err != nil {
			return err
		}
	} else {
		// Changed content/eligibility cannot safely restore an old snapshot.
		values = map[string]interface{}{"duplicate_cluster_id": "", "dedupe_key": fmt.Sprintf("material:%d", candidate.MaterialID), "representative_id": nil, "dedupe_state": DiscoveryDedupeReady}
		if candidate.EligibilityReasons == "duplicate_non_representative" {
			values["eligibility_state"] = DiscoveryEligibilityUnknown
			values["eligibility_reasons"] = "assessment_pending"
		}
	}
	// JSON numbers must retain integral IDs for both database dialects.
	if value, ok := values["representative_id"].(float64); ok {
		values["representative_id"] = uint(value)
	}
	return UpdateCandidate(database, candidate.ID, values).Error
}

func processIncrementalDedupe(ctx context.Context, representationID uint) error {
	article, err := loadDedupeArticle(db.WithContext(ctx), representationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	signature := dedupe.Signature(article.Text)
	var representativeID uint
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Consistent ordering with run application; extraction does not acquire this lock.
		setting, err := lockDedupeSetting(tx)
		if err != nil {
			return err
		}
		if setting.Mode != "active" || setting.ActiveRunID == 0 {
			return nil
		}
		var run dedupe.Run
		if err := tx.First(&run, setting.ActiveRunID).Error; err != nil {
			return err
		}
		if run.Status == "rolling_back" {
			return nil
		}
		var applying int64
		if err := tx.Model(&dedupe.Run{}).Where("status=?", "applying").Count(&applying).Error; err != nil {
			return err
		}
		if applying > 0 {
			return nil
		}
		current, err := loadDedupeArticle(tx, representationID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := indexDedupeArticle(tx, current, signature); err != nil {
			return err
		}
		if err := assignDedupeMember(tx, setting.ActiveRunID, current, signature); err != nil {
			return err
		}
		var member dedupe.Member
		if err := tx.First(&member, "run_id=? AND representation_id=?", setting.ActiveRunID, representationID).Error; err != nil {
			return err
		}
		representativeID = member.RepresentativeID
		return applyDedupeGroup(tx, setting.ActiveRunID, member.RepresentativeID)
	})
	if err != nil {
		return err
	}
	return enqueueDedupeRepresentative(ctx, representativeID)
}

func enqueueCurrentDedupeRepresentatives(ctx context.Context) error {
	queue, ok := jobqueue.Default()
	if !ok {
		return nil
	}
	targets, err := PendingAssessmentTargets(ctx, db, false, nil)
	if err != nil {
		return err
	}
	for start := 0; start < len(targets); start += dedupeBatchSize {
		if err := queue.EnqueueAssessArticles(ctx, targets[start:min(start+dedupeBatchSize, len(targets))]); err != nil {
			return err
		}
	}
	return nil
}

// ReleaseDeferredAssessment resumes an assessment run that was deferred until
// suppression went live. The composition root installs it because discovery
// cannot import assessment. It must register due articles and resume the paused
// queue exactly like the owner HTTP run does.
var ReleaseDeferredAssessment func(ctx context.Context) error

// PendingDedupeAssessmentRun reports the auto-apply run a deferred assessment
// is waiting on, so the queue snapshot can show the build instead of the
// misleading paused-queue state. It returns false once the pending intent is
// cleared, i.e. the assessment has been released.
func PendingDedupeAssessmentRun(ctx context.Context) (dedupe.Run, bool, error) {
	var run dedupe.Run
	if db == nil {
		return run, false, nil
	}
	setting, err := dedupeSetting(db.WithContext(ctx))
	if err != nil {
		return run, false, err
	}
	if !setting.PendingAssessment {
		return run, false, nil
	}
	if err := db.WithContext(ctx).Where("auto_apply=?", true).Order("id DESC").Limit(1).Find(&run).Error; err != nil {
		return run, false, err
	}
	return run, true, nil
}

// releaseDeferredAssessment clears the durable intent only after the hook
// succeeds, so a crash in between is retried by recovery instead of silently
// dropping the owner's request.
func releaseDeferredAssessment(ctx context.Context) error {
	if ReleaseDeferredAssessment == nil {
		return errors.New("deferred assessment release is not wired")
	}
	if err := ReleaseDeferredAssessment(ctx); err != nil {
		return err
	}
	return db.WithContext(ctx).Model(&dedupe.Setting{}).Where("id=?", 1).Update("pending_assessment", false).Error
}

// RecoverDedupeJobs performs a full recovery sweep. Startup and dedupe run
// state transitions use it; the periodic ticker decides for itself whether a
// full sweep is warranted.
func RecoverDedupeJobs(ctx context.Context) error {
	_, err := recoverDedupeJobs(ctx, true)
	return err
}

// recoverDedupeJobs reports how much work it handed to the queue. A non-full
// pass costs two indexed counts and returns as soon as the library looks
// converged, so an idle deployment never rescans the whole corpus.
func recoverDedupeJobs(ctx context.Context, full bool) (int, error) {
	if db == nil {
		return 0, nil
	}
	enqueued := 0
	var runs []dedupe.Run
	if err := db.WithContext(ctx).Where("status IN ?", []string{"indexing", "previewing", "applying", "rolling_back"}).Find(&runs).Error; err != nil {
		return enqueued, err
	}
	for _, run := range runs {
		if err := enqueueDedupeTask(ctx, runDedupeArgs(run)); err != nil {
			return enqueued, err
		}
		enqueued++
	}
	// A crash between preview completion and the auto-apply transition leaves a
	// ready run that a deferred assessment waits on; push it forward. A conflict
	// means a newer generation superseded the run, which recovery must not fight.
	var stalled []dedupe.Run
	if err := db.WithContext(ctx).Where("status=? AND auto_apply=?", "ready", true).Find(&stalled).Error; err != nil {
		return enqueued, err
	}
	for _, run := range stalled {
		if _, err := RequestDedupeAction(ctx, run.ID, "apply"); err != nil && !errors.Is(err, ErrDedupeConflict) {
			return enqueued, err
		}
	}
	active, err := DedupeActive()
	if err != nil {
		return enqueued, err
	}
	if !active {
		return enqueued, nil
	}
	setting, err := dedupeSetting(db)
	if err != nil {
		return enqueued, err
	}
	drift, inactive, err := dedupeRecoveryDriftCounts(ctx, setting.ActiveRunID)
	if err != nil {
		return enqueued, err
	}
	if !full && drift == 0 && !setting.PendingAssessment {
		return enqueued, nil
	}
	started := time.Now()
	var scanned, cancelled int64
	gapTargets := 0
	var gapMS, enqueueMS, pruneMS int64
	defer func() {
		log.Printf("dedupe recovery sweep run_id=%d full=%t actionable_drift=%d inactive_unapplied=%d gap_targets=%d assessment_jobs_scanned=%d cancelled=%d gap_ms=%d enqueue_ms=%d prune_ms=%d duration_ms=%d", setting.ActiveRunID, full, drift, inactive, gapTargets, scanned, cancelled, gapMS, enqueueMS, pruneMS, time.Since(started).Milliseconds())
	}()
	// Read the narrow repair set once. LIMIT pagination used to re-evaluate
	// coverage for the whole corpus on every page, including blocked members.
	var articles []struct{ RepresentationID uint }
	if err := dedupeRecoveryCandidates(db.WithContext(ctx), setting.ActiveRunID).Scan(&articles).Error; err != nil {
		return enqueued, err
	}
	gapMS = time.Since(started).Milliseconds()
	for _, article := range articles {
		if err := enqueueDedupeTask(ctx, jobqueue.DedupeArgs{RepresentationID: article.RepresentationID, Algorithm: dedupe.Algorithm}); err != nil {
			return enqueued, err
		}
		enqueued++
		gapTargets++
	}
	enqueueStarted := time.Now()
	if err := enqueueCurrentDedupeRepresentatives(ctx); err != nil {
		return enqueued, err
	}
	enqueueMS = time.Since(enqueueStarted).Milliseconds()
	queue, ok := jobqueue.Default()
	if !ok {
		return enqueued, nil
	}
	cleaner, ok := queue.(jobqueue.AssessmentPruner)
	if !ok {
		return enqueued, nil
	}
	pruneStarted := time.Now()
	cancelled, err = cleaner.PruneAssessments(ctx, func(targets []jobqueue.AssessmentTarget) (map[uint]bool, error) {
		scanned += int64(len(targets))
		return suppressedDedupeAssessmentTargets(db.WithContext(ctx), setting.ActiveRunID, targets)
	})
	pruneMS = time.Since(pruneStarted).Milliseconds()
	if err != nil {
		return enqueued, err
	}
	if cancelled > 0 {
		if err := db.Model(&dedupe.Run{}).Where("id=?", setting.ActiveRunID).Update("cancelled", gorm.Expr("cancelled + ?", cancelled)).Error; err != nil {
			return enqueued, err
		}
	}
	// The deferred assessment run is released only after pruning and
	// re-enqueueing representatives, so the resumed queue never pays for a
	// suppressed duplicate.
	if setting.PendingAssessment {
		if err := releaseDeferredAssessment(ctx); err != nil {
			return enqueued + int(cancelled), err
		}
	}
	return enqueued + int(cancelled), nil
}

// Only current, usable articles can advance. Blocked/obsolete members remain
// unapplied so they cannot qualify for assessment; removal of a blacklist rule
// or a ready candidate makes them actionable again without a state rewrite.
func dedupeRecoveryDrift(ctx context.Context, runID uint) (int64, error) {
	actionable, _, err := dedupeRecoveryDriftCounts(ctx, runID)
	return actionable, err
}

func dedupeRecoveryDriftCounts(ctx context.Context, runID uint) (int64, int64, error) {
	var counts struct{ Total, Actionable int64 }
	query := db.WithContext(ctx).Table("article_dedupe_members d").
		Joins("LEFT JOIN material m ON m.id=d.material_id AND m.current_version_id=d.version_id").
		Joins("LEFT JOIN material_representations r ON r.id=d.representation_id AND r.version_id=d.version_id").
		Joins("LEFT JOIN material_article_states s ON s.material_id=m.id").
		Where("d.run_id=? AND NOT d.applied", runID).
		Select("COUNT(*) AS total, COALESCE(SUM(CASE WHEN m.id IS NOT NULL AND s.material_id IS NOT NULL AND r.kind='text' AND r.role='body' AND r.text<>'' AND " + usableDedupeMaterialSQL("m") + " THEN 1 ELSE 0 END),0) AS actionable")
	if err := query.Scan(&counts).Error; err != nil {
		return 0, 0, err
	}
	inactive := counts.Total - counts.Actionable
	if counts.Actionable > 0 {
		return counts.Actionable, inactive, nil
	}
	var pending int64
	err := db.WithContext(ctx).Table("discovery_candidates c").
		Joins("JOIN material m ON m.id=c.material_id").
		Joins("JOIN material_representations r ON r.version_id=m.current_version_id").
		Joins("JOIN material_article_states s ON s.material_id=m.id").
		Where("c.processing_state='ready' AND c.dedupe_state<>'ready'").
		Where("r.kind='text' AND r.role='body' AND r.text<>''").
		Where(usableDedupeMaterialSQL("m")).Count(&pending).Error
	return pending, inactive, err
}

func enqueueDedupeRepresentative(ctx context.Context, representationID uint) error {
	if representationID == 0 {
		return nil
	}
	queue, ok := jobqueue.Default()
	if !ok {
		return nil
	}
	article, err := loadDedupeArticle(db.WithContext(ctx), representationID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	qualification, err := ArticleDedupeQualification(db.WithContext(ctx), article.MaterialID)
	if err != nil {
		return err
	}
	if !qualification.Ready || !qualification.Representative {
		return nil
	}
	var state material.ArticleState
	if err := db.WithContext(ctx).First(&state, "material_id=?", article.MaterialID).Error; err != nil {
		return err
	}
	valid, err := hasValidDedupeAssessment(db.WithContext(ctx), article)
	if err != nil {
		return err
	}
	if valid {
		return nil
	}
	// A previously covered member may be promoted without a valid independent assessment.
	if state.AssessmentState != "pending" && state.AssessmentError == "" {
		if err := db.WithContext(ctx).Model(&material.ArticleState{}).Where("material_id=? AND content_version=?", article.MaterialID, article.ContentVersion).Update("assessment_state", "pending").Error; err != nil {
			return err
		}
	} else if state.AssessmentState != "pending" {
		return nil
	}
	return queue.EnqueueAssessArticle(ctx, article.MaterialID, fmt.Sprint(article.ContentVersion))
}

// fullSweepTicks is how often periodic recovery sweeps the whole library even
// when the drift probe reports nothing. Found work resets the counter so repair
// is never delayed by the safety-net schedule.
const fullSweepTicks = 30

// StartDedupeRecovery repairs missed handoffs independently of the crawl scheduler.
func StartDedupeRecovery(ctx context.Context) func() {
	recoveryCtx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		ticks := 0
		for {
			select {
			case <-recoveryCtx.Done():
				return
			case <-ticker.C:
			}
			ticks++
			full := ticks >= fullSweepTicks
			pending, err := recoverDedupeJobs(recoveryCtx, full)
			if err != nil {
				if recoveryCtx.Err() == nil {
					log.Printf("dedupe recovery failed: %v", err)
				}
				continue
			}
			if full || pending > 0 {
				ticks = 0
			}
		}
	}()
	return cancel
}
