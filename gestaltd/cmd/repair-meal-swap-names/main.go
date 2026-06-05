package main

import (
	"context"
	"database/sql"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/valon-technologies/gestalt/server/internal/indexeddbcodec"
)

const storeName = "meal_swap_meals"

var userSubjectNamePattern = regexp.MustCompile(`^user:[0-9a-f-]+$`)

type mealRecord struct {
	pkHash             []byte
	id                 string
	name               string
	claimedBy          string
	createdBySubjectID string
	createdAt          string
	envelope           indexeddbcodec.Record
	payload            indexeddbcodec.Record
}

type resolverMaps struct {
	subjectIDToName map[string]string
	emailToName     map[string]string
}

type patchSummary struct {
	patched            int
	skippedOK          int
	skippedUnresolved  int
	claimedByPatched   int
	claimedByUnresolved int
}

func main() {
	dryRun := flag.Bool("dry-run", true, "report changes without writing (default)")
	apply := flag.Bool("apply", false, "write patched records to the database")
	since := flag.String("since", "", "only consider meals with created_at on or after YYYY-MM-DD")
	dsn := flag.String("dsn", "", "MySQL DSN (defaults to MYSQL_DSN)")
	flag.Parse()

	if *apply {
		*dryRun = false
	}

	conn := strings.TrimSpace(*dsn)
	if conn == "" {
		conn = strings.TrimSpace(os.Getenv("MYSQL_DSN"))
	}
	if conn == "" {
		fmt.Fprintln(os.Stderr, "MYSQL_DSN or --dsn required")
		os.Exit(1)
	}

	var sinceCutoff time.Time
	if strings.TrimSpace(*since) != "" {
		parsed, err := time.Parse("2006-01-02", strings.TrimSpace(*since))
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid --since %q: %v\n", *since, err)
			os.Exit(1)
		}
		sinceCutoff = parsed
	}

	db, err := sql.Open("mysql", conn)
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()
	allRows, err := loadMeals(ctx, db)
	if err != nil {
		panic(err)
	}

	maps := buildResolverMaps(allRows)
	rows := allRows
	if !sinceCutoff.IsZero() {
		rows = filterMealsSince(allRows, sinceCutoff)
	}
	summary, patches := planPatches(rows, maps)

	fmt.Printf("loaded %d meal_swap_meals records (%d eligible for patching)\n", len(allRows), len(rows))
	fmt.Printf("resolver: %d subject_id names, %d email names\n", len(maps.subjectIDToName), len(maps.emailToName))
	fmt.Printf("already ok: %d\n", summary.skippedOK)
	fmt.Printf("name patches: %d (%d unresolved)\n", summary.patched, summary.skippedUnresolved)
	fmt.Printf("claimed_by patches: %d (%d unresolved)\n", summary.claimedByPatched, summary.claimedByUnresolved)

	for _, patch := range patches {
		fmt.Printf(
			"patch pk=%s id=%s name %q -> %q claimed_by %q -> %q\n",
			hex.EncodeToString(patch.pkHash),
			patch.id,
			patch.oldName,
			patch.newName,
			patch.oldClaimedBy,
			patch.newClaimedBy,
		)
	}

	if *dryRun {
		fmt.Println("dry-run: no database writes")
		return
	}

	applied, err := applyPatches(ctx, db, patches)
	if err != nil {
		panic(err)
	}
	fmt.Printf("applied %d updates\n", applied)
}

func loadMeals(ctx context.Context, db *sql.DB) ([]mealRecord, error) {
	queryRows, err := db.QueryContext(ctx,
		"SELECT pk_hash, record_blob FROM gestaltd._gestalt_records WHERE store_name=?",
		storeName,
	)
	if err != nil {
		return nil, err
	}
	defer queryRows.Close()

	var out []mealRecord
	for queryRows.Next() {
		var pkHash, blob []byte
		if err := queryRows.Scan(&pkHash, &blob); err != nil {
			return nil, err
		}
		envelope, err := indexeddbcodec.DecodeRecord(blob)
		if err != nil {
			return nil, fmt.Errorf("decode pk %s: %w", hex.EncodeToString(pkHash), err)
		}
		payload := mealPayload(envelope)
		out = append(out, mealRecord{
			pkHash:             append([]byte(nil), pkHash...),
			id:                 stringField(payload, "id"),
			name:               stringField(payload, "name"),
			claimedBy:          stringField(payload, "claimed_by"),
			createdBySubjectID: stringField(payload, "created_by_subject_id"),
			createdAt:          stringField(payload, "created_at"),
			envelope:           envelope,
			payload:            payload,
		})
	}
	return out, queryRows.Err()
}

func filterMealsSince(rows []mealRecord, since time.Time) []mealRecord {
	filtered := make([]mealRecord, 0, len(rows))
	for _, row := range rows {
		createdAt, err := time.Parse(time.RFC3339, strings.TrimSpace(row.createdAt))
		if err != nil {
			continue
		}
		if !createdAt.Before(since) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func buildResolverMaps(rows []mealRecord) resolverMaps {
	type subjectCandidate struct {
		name      string
		createdAt time.Time
	}
	subjectCandidates := make(map[string]subjectCandidate)

	for _, row := range rows {
		subjectID := strings.TrimSpace(row.createdBySubjectID)
		name := normalizeWhitespace(row.name)
		if subjectID == "" || !isGoodDisplayName(name, subjectID) {
			continue
		}
		createdAt, _ := time.Parse(time.RFC3339, strings.TrimSpace(row.createdAt))
		current, ok := subjectCandidates[subjectID]
		if !ok || createdAt.After(current.createdAt) {
			subjectCandidates[subjectID] = subjectCandidate{name: name, createdAt: createdAt}
		}
	}

	subjectIDToName := make(map[string]string, len(subjectCandidates))
	for subjectID, candidate := range subjectCandidates {
		subjectIDToName[subjectID] = candidate.name
	}

	emailToName := make(map[string]string)
	for _, row := range rows {
		subjectID := strings.TrimSpace(row.createdBySubjectID)
		email := strings.ToLower(strings.TrimSpace(row.name))
		if subjectID == "" || !strings.Contains(email, "@") {
			continue
		}
		if resolved, ok := subjectIDToName[subjectID]; ok {
			emailToName[email] = resolved
		}
	}

	return resolverMaps{
		subjectIDToName: subjectIDToName,
		emailToName:     emailToName,
	}
}

type mealPatch struct {
	pkHash       []byte
	id           string
	oldName      string
	newName      string
	oldClaimedBy string
	newClaimedBy string
	envelope     indexeddbcodec.Record
	payload      indexeddbcodec.Record
}

func planPatches(rows []mealRecord, maps resolverMaps) (patchSummary, []mealPatch) {
	var summary patchSummary
	var patches []mealPatch

	for _, row := range rows {
		subjectID := strings.TrimSpace(row.createdBySubjectID)
		patch := mealPatch{
			pkHash:       row.pkHash,
			id:           row.id,
			oldName:      row.name,
			newName:      row.name,
			oldClaimedBy: row.claimedBy,
			newClaimedBy: row.claimedBy,
			envelope:     cloneRecord(row.envelope),
			payload:      cloneRecord(row.payload),
		}

		nameBad := isBadDisplayName(row.name, subjectID)
		claimedBad := row.claimedBy != "" && isBadDisplayName(row.claimedBy, "")

		if !nameBad && !claimedBad {
			summary.skippedOK++
			continue
		}

		changed := false
		if nameBad {
			if resolved, ok := resolveDisplayName(row.name, subjectID, maps); ok {
				patch.newName = resolved
				patch.payload["name"] = resolved
				summary.patched++
				changed = true
			} else {
				summary.skippedUnresolved++
			}
		}

		if claimedBad {
			if resolved, ok := resolveDisplayName(row.claimedBy, "", maps); ok {
				patch.newClaimedBy = resolved
				patch.payload["claimed_by"] = resolved
				summary.claimedByPatched++
				changed = true
			} else {
				summary.claimedByUnresolved++
			}
		}

		if changed {
			patches = append(patches, patch)
		}
	}

	sort.Slice(patches, func(i, j int) bool {
		return patches[i].id < patches[j].id
	})
	return summary, patches
}

func resolveDisplayName(current, subjectID string, maps resolverMaps) (string, bool) {
	if subjectID != "" {
		if name, ok := maps.subjectIDToName[subjectID]; ok {
			return name, true
		}
	}
	email := strings.ToLower(strings.TrimSpace(current))
	if strings.Contains(email, "@") {
		if name, ok := maps.emailToName[email]; ok {
			return name, true
		}
	}
	return "", false
}

func applyPatches(ctx context.Context, db *sql.DB, patches []mealPatch) (int, error) {
	applied := 0
	for _, patch := range patches {
		envelope := patch.envelope
		if _, ok := envelope["payload"]; ok {
			envelope["payload"] = patch.payload
		} else {
			envelope = patch.payload
		}
		blob, err := indexeddbcodec.EncodeRecord(envelope)
		if err != nil {
			return applied, fmt.Errorf("encode %s: %w", patch.id, err)
		}
		result, err := db.ExecContext(ctx,
			"UPDATE gestaltd._gestalt_records SET record_blob=? WHERE store_name=? AND pk_hash=?",
			blob,
			storeName,
			patch.pkHash,
		)
		if err != nil {
			return applied, fmt.Errorf("update %s: %w", patch.id, err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return applied, err
		}
		if rows == 1 {
			applied++
		}
	}
	return applied, nil
}

func mealPayload(envelope indexeddbcodec.Record) indexeddbcodec.Record {
	if envelope == nil {
		return indexeddbcodec.Record{}
	}
	if payload, ok := envelope["payload"]; ok {
		if record, ok := payload.(map[string]any); ok {
			return cloneRecord(indexeddbcodec.Record(record))
		}
	}
	return cloneRecord(envelope)
}

func stringField(record indexeddbcodec.Record, key string) string {
	value, ok := record[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func cloneRecord(record indexeddbcodec.Record) indexeddbcodec.Record {
	out := make(indexeddbcodec.Record, len(record))
	for key, value := range record {
		out[key] = value
	}
	return out
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func isBadDisplayName(name, subjectID string) bool {
	name = normalizeWhitespace(name)
	if name == "" {
		return true
	}
	if strings.Contains(name, "@") {
		return true
	}
	if userSubjectNamePattern.MatchString(name) {
		return true
	}
	if subjectID != "" && name == strings.TrimSpace(subjectID) {
		return true
	}
	return false
}

func isGoodDisplayName(name, subjectID string) bool {
	name = normalizeWhitespace(name)
	return name != "" && !isBadDisplayName(name, subjectID)
}
