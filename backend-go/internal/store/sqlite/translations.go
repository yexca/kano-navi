package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	moderncsqlite "modernc.org/sqlite"
)

// Translations of stored post text and video titles. The source wording stays
// in posts.text and videos.title; translations live beside it with one row per
// locale, origin and platform, so an operator's translation, a platform's and a
// model's can coexist. Readers resolve them by precedence: manual, then
// platform (YouTube before Bilibili), then machine. A machine row counts only
// while its source hash matches the current wording. Manual rows always count
// and are flagged in the console when the wording changed; platform rows are
// kept current by the source writers that own them.

const (
	TranslationOwnerPost  = "post"
	TranslationOwnerVideo = "video"

	TranslationOriginManual   = "manual"
	TranslationOriginPlatform = "platform"
	TranslationOriginMachine  = "machine"
)

// TranslationLocales are the target locales. Japanese readers see the source.
var TranslationLocales = []string{"en", "zh-CN"}

// translationLanguages maps a target locale to the detected source language
// that makes it unnecessary.
var translationLanguages = map[string]string{"en": "en", "zh-CN": "zh"}

// SourceLanguages are the codes the machine step records: none means there is
// no wording to translate (only links, tags, mentions or emoji).
var SourceLanguages = map[string]bool{"ja": true, "en": true, "zh": true, "ko": true, "other": true, "none": true}

// translationPlatforms orders platform rows; an unknown platform ranks last.
var translationPlatforms = []string{"youtube", "bilibili"}

// MaxTranslationRunes bounds stored and operator-entered translations.
const MaxTranslationRunes = 8000

var (
	ErrTranslationInvalid = errors.New("invalid_input")
	ErrTranslationOwner   = errors.New("not_found")
)

// videoTitleSource is the SQL wording video translations follow; %[1]s is the
// videos alias. An operator's corrected title replaces the platform's, so a
// correction retires its machine translations.
const videoTitleSource = "COALESCE(%[1]s.manual_title, %[1]s.title)"

// videoTranslationScope selects the videos the machine step translates.
// Bilibili metadata is already Chinese and is not sent to a model.
const videoTranslationScope = "%[1]s.source = 'youtube' AND %[1]s.merged_into IS NULL"

type translationOwner struct {
	table, field, source, instant, scope string
}

var translationOwners = map[string]translationOwner{
	TranslationOwnerPost:  {table: "posts", field: "text", source: "%[1]s.text", instant: "%[1]s.published_at", scope: "%[1]s.source = 'x'"},
	TranslationOwnerVideo: {table: "videos", field: "title", source: videoTitleSource, instant: "COALESCE(%[1]s.scheduled_at, %[1]s.published_at)", scope: videoTranslationScope},
}

func (o translationOwner) sourceSQL(alias string) string  { return fmt.Sprintf(o.source, alias) }
func (o translationOwner) instantSQL(alias string) string { return fmt.Sprintf(o.instant, alias) }
func (o translationOwner) scopeSQL(alias string) string   { return fmt.Sprintf(o.scope, alias) }

// TranslationField is the translated column of an owner type, or "".
func TranslationField(ownerType string) string { return translationOwners[ownerType].field }

// TranslationHash identifies the source wording a translation was made from.
func TranslationHash(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// translation_hash lets queries compare stored hashes with the current
// wording without reading every row into Go.
func init() {
	if err := moderncsqlite.RegisterDeterministicScalarFunction("translation_hash", 1, func(_ *moderncsqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		text := ""
		if len(args) > 0 {
			switch value := args[0].(type) {
			case string:
				text = value
			case []byte:
				text = string(value)
			}
		}
		return TranslationHash(text), nil
	}); err != nil {
		panic(err)
	}
}

func validTranslationLocale(locale string) bool {
	_, ok := translationLanguages[locale]
	return ok
}

// translationCoveredSQL is true when a locale has a usable translation of the
// owner's current wording.
func translationCoveredSQL(ownerType, alias, locale string) string {
	o := translationOwners[ownerType]
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM translations t WHERE t.owner_type = '%s' AND t.owner_id = %s.id AND t.field = '%s' AND t.locale = '%s' AND (t.origin <> 'machine' OR t.source_hash = translation_hash(%s)))`,
		ownerType, alias, o.field, locale, o.sourceSQL(alias))
}

// translationNeededSQL is false only when the machine step found that the
// current wording is already in the locale's language or has nothing to
// translate; an owner it never saw needs every locale.
func translationNeededSQL(ownerType, alias, locale string) string {
	o := translationOwners[ownerType]
	return fmt.Sprintf(`NOT COALESCE((SELECT ts.source_hash = translation_hash(%s) AND ts.source_language IN ('%s', 'none') FROM translation_states ts WHERE ts.owner_type = '%s' AND ts.owner_id = %s.id AND ts.field = '%s'), 0)`,
		o.sourceSQL(alias), translationLanguages[locale], ownerType, alias, o.field)
}

// TranslationIncompleteSQL is a condition on the owner row aliased alias:
// some target locale still lacks a usable translation.
func TranslationIncompleteSQL(ownerType, alias string) string {
	parts := make([]string, 0, len(TranslationLocales))
	for _, locale := range TranslationLocales {
		parts = append(parts, "("+translationNeededSQL(ownerType, alias, locale)+" AND NOT "+translationCoveredSQL(ownerType, alias, locale)+")")
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

// TranslationRequestedSQL is a condition on the owner row: an operator asked
// for a machine pass that has not run yet.
func TranslationRequestedSQL(ownerType, alias string) string {
	return fmt.Sprintf(`EXISTS (SELECT 1 FROM translation_states ts WHERE ts.owner_type = '%s' AND ts.owner_id = %s.id AND ts.field = '%s' AND ts.requested = 1)`,
		ownerType, alias, translationOwners[ownerType].field)
}

// --- public projection ------------------------------------------------------

// Translation is the resolved translation of one locale.
type Translation struct {
	Text     string
	Origin   string
	Platform *string
}

// TranslationSet is what readers receive for one post or video: at most one
// translation per target locale and the detected source language, if known.
type TranslationSet struct {
	SourceLanguage *string
	Locales        map[string]Translation
}

func translationRank(origin, platform string) int {
	switch origin {
	case TranslationOriginManual:
		return 0
	case TranslationOriginPlatform:
		for i, known := range translationPlatforms {
			if platform == known {
				return 1 + i
			}
		}
		return 1 + len(translationPlatforms)
	}
	return 2 + len(translationPlatforms)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// translationChunks keeps IN lists well below SQLite's variable limit.
func translationChunks(ids []string) [][]string {
	const size = 400
	out := [][]string{}
	for start := 0; start < len(ids); start += size {
		out = append(out, ids[start:min(len(ids), start+size)])
	}
	return out
}

func idArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// resolveTranslations returns the public translations of the given owners.
func (s *Store) resolveTranslations(ctx context.Context, ownerType string, ids []string) (map[string]TranslationSet, error) {
	o, ok := translationOwners[ownerType]
	out := map[string]TranslationSet{}
	if !ok || len(ids) == 0 {
		return out, nil
	}
	type candidate struct {
		Translation
		rank int
	}
	best := map[string]map[string]candidate{}
	languages := map[string]string{}
	for _, chunk := range translationChunks(ids) {
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT t.owner_id, t.locale, t.origin, t.platform, t.text FROM translations t JOIN %[1]s o ON o.id = t.owner_id
			WHERE t.owner_type = ? AND t.field = ? AND t.owner_id IN (%[2]s) AND (t.origin <> 'machine' OR t.source_hash = translation_hash(%[3]s))`,
			o.table, placeholders(len(chunk)), o.sourceSQL("o")), append([]any{ownerType, o.field}, idArgs(chunk)...)...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id, locale, origin, platform, text string
			if err := rows.Scan(&id, &locale, &origin, &platform, &text); err != nil {
				rows.Close()
				return nil, dbErr(err)
			}
			if !validTranslationLocale(locale) || strings.TrimSpace(text) == "" {
				continue
			}
			rank := translationRank(origin, platform)
			if best[id] == nil {
				best[id] = map[string]candidate{}
			}
			if current, seen := best[id][locale]; seen && current.rank <= rank {
				continue
			}
			value := Translation{Text: text, Origin: origin}
			if platform != "" {
				value.Platform = strPtr(platform)
			}
			best[id][locale] = candidate{value, rank}
		}
		if err := rows.Close(); err != nil {
			return nil, dbErr(err)
		}
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`SELECT ts.owner_id, ts.source_language FROM translation_states ts JOIN %[1]s o ON o.id = ts.owner_id
			WHERE ts.owner_type = ? AND ts.field = ? AND ts.owner_id IN (%[2]s) AND ts.source_language IS NOT NULL AND ts.source_hash = translation_hash(%[3]s)`,
			o.table, placeholders(len(chunk)), o.sourceSQL("o")), append([]any{ownerType, o.field}, idArgs(chunk)...)...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id, language string
			if err := rows.Scan(&id, &language); err != nil {
				rows.Close()
				return nil, dbErr(err)
			}
			languages[id] = language
		}
		if err := rows.Close(); err != nil {
			return nil, dbErr(err)
		}
	}
	for _, id := range ids {
		set := TranslationSet{Locales: map[string]Translation{}}
		if language, ok := languages[id]; ok && SourceLanguages[language] {
			set.SourceLanguage = strPtr(language)
		}
		for locale, value := range best[id] {
			set.Locales[locale] = value.Translation
		}
		out[id] = set
	}
	return out, nil
}

func (s *Store) attachPostTranslations(ctx context.Context, posts ...*Post) error {
	ids := []string{}
	for _, post := range posts {
		if post != nil {
			ids = append(ids, post.ID)
		}
	}
	sets, err := s.resolveTranslations(ctx, TranslationOwnerPost, ids)
	if err != nil {
		return err
	}
	for _, post := range posts {
		if post != nil {
			post.Translations = sets[post.ID]
		}
	}
	return nil
}

func (s *Store) attachVideoTranslations(ctx context.Context, videos ...*Video) error {
	ids := []string{}
	for _, video := range videos {
		if video != nil {
			ids = append(ids, video.ID)
		}
	}
	sets, err := s.resolveTranslations(ctx, TranslationOwnerVideo, ids)
	if err != nil {
		return err
	}
	for _, video := range videos {
		if video == nil {
			continue
		}
		video.Translations = sets[video.ID]
		// Bilibili metadata is Chinese; the machine step never sees it.
		if video.Source == "bilibili" && video.Translations.SourceLanguage == nil {
			video.Translations.SourceLanguage = strPtr("zh")
		}
	}
	return nil
}

// --- console ----------------------------------------------------------------

// AdminTranslationLocale is one locale of an owner in the console.
type AdminTranslationLocale struct {
	Locale string `json:"locale"`
	// Status is translated, missing, stale (only an outdated machine row
	// exists), source (the wording is already in this language) or
	// not_needed (nothing to translate).
	Status   string  `json:"status"`
	Origin   *string `json:"origin"`
	Platform *string `json:"platform"`
	Text     *string `json:"text"`
	// Manual is the operator's translation, which may predate the wording.
	Manual      *string `json:"manual"`
	ManualStale bool    `json:"manualStale"`
	Machine     *string `json:"machine"`
	MachineAt   *string `json:"machineAt"`
	Model       *string `json:"model"`
}

// AdminTranslation summarizes an owner's translations and machine state.
type AdminTranslation struct {
	// Status is complete or incomplete.
	Status         string                   `json:"status"`
	SourceLanguage *string                  `json:"sourceLanguage"`
	MachineStatus  string                   `json:"machineStatus"`
	LastAttemptAt  *string                  `json:"lastAttemptAt"`
	LastError      *string                  `json:"lastError"`
	Requested      bool                     `json:"requested"`
	Locales        []AdminTranslationLocale `json:"locales"`
}

type translationRow struct {
	locale, origin, platform, text, updatedAt string
	providerID, modelID                       sql.NullString
	fresh                                     bool
}

// AdminTranslations describes the given owners for the console.
func (s *Store) AdminTranslations(ctx context.Context, ownerType string, ids []string) (map[string]AdminTranslation, error) {
	o, ok := translationOwners[ownerType]
	out := map[string]AdminTranslation{}
	if !ok || len(ids) == 0 {
		return out, nil
	}
	rowsByID := map[string][]translationRow{}
	type state struct {
		current          bool
		status, language string
		attempt, lastErr sql.NullString
		requested        bool
	}
	states := map[string]state{}
	for _, chunk := range translationChunks(ids) {
		rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`SELECT t.owner_id, t.locale, t.origin, t.platform, t.text, t.updated_at, t.provider_id, t.model_id, t.source_hash = translation_hash(%[3]s)
			FROM translations t JOIN %[1]s o ON o.id = t.owner_id WHERE t.owner_type = ? AND t.field = ? AND t.owner_id IN (%[2]s)`,
			o.table, placeholders(len(chunk)), o.sourceSQL("o")), append([]any{ownerType, o.field}, idArgs(chunk)...)...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id string
			var row translationRow
			if err := rows.Scan(&id, &row.locale, &row.origin, &row.platform, &row.text, &row.updatedAt, &row.providerID, &row.modelID, &row.fresh); err != nil {
				rows.Close()
				return nil, dbErr(err)
			}
			rowsByID[id] = append(rowsByID[id], row)
		}
		if err := rows.Close(); err != nil {
			return nil, dbErr(err)
		}
		rows, err = s.db.QueryContext(ctx, fmt.Sprintf(`SELECT ts.owner_id, ts.source_hash = translation_hash(%[3]s), ts.status, COALESCE(ts.source_language, ''), ts.last_attempt_at, ts.last_error, ts.requested
			FROM translation_states ts JOIN %[1]s o ON o.id = ts.owner_id WHERE ts.owner_type = ? AND ts.field = ? AND ts.owner_id IN (%[2]s)`,
			o.table, placeholders(len(chunk)), o.sourceSQL("o")), append([]any{ownerType, o.field}, idArgs(chunk)...)...)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			var id string
			var value state
			var current sql.NullBool
			if err := rows.Scan(&id, &current, &value.status, &value.language, &value.attempt, &value.lastErr, &value.requested); err != nil {
				rows.Close()
				return nil, dbErr(err)
			}
			value.current = current.Valid && current.Bool
			states[id] = value
		}
		if err := rows.Close(); err != nil {
			return nil, dbErr(err)
		}
	}
	for _, id := range ids {
		st, known := states[id]
		item := AdminTranslation{Status: "complete", MachineStatus: "never", Locales: []AdminTranslationLocale{}}
		if known {
			item.Requested = st.requested
			item.LastAttemptAt = nullString(st.attempt)
			item.LastError = sanitizeLLMError(st.lastErr)
			if st.current {
				item.MachineStatus = st.status
				if SourceLanguages[st.language] {
					item.SourceLanguage = strPtr(st.language)
				}
			} else if st.status != "never" {
				// The wording changed after the last attempt.
				item.MachineStatus = "outdated"
			}
		}
		for _, locale := range TranslationLocales {
			entry := AdminTranslationLocale{Locale: locale, Status: "missing"}
			bestRank := -1
			stale := false
			for _, row := range rowsByID[id] {
				if row.locale != locale {
					continue
				}
				switch row.origin {
				case TranslationOriginManual:
					entry.Manual, entry.ManualStale = strPtr(row.text), !row.fresh
				case TranslationOriginMachine:
					if !row.fresh {
						stale = true
						continue
					}
					entry.Machine, entry.MachineAt = strPtr(row.text), strPtr(row.updatedAt)
					entry.Model = nullString(row.modelID)
				}
				if rank := translationRank(row.origin, row.platform); bestRank < 0 || rank < bestRank {
					bestRank = rank
					entry.Status, entry.Origin, entry.Text = "translated", strPtr(row.origin), strPtr(row.text)
					entry.Platform = nil
					if row.platform != "" {
						entry.Platform = strPtr(row.platform)
					}
				}
			}
			if entry.Status == "missing" {
				switch {
				case item.SourceLanguage != nil && *item.SourceLanguage == translationLanguages[locale]:
					entry.Status = "source"
				case item.SourceLanguage != nil && *item.SourceLanguage == "none":
					entry.Status = "not_needed"
				case stale:
					entry.Status = "stale"
				}
			}
			if entry.Status == "missing" || entry.Status == "stale" {
				item.Status = "incomplete"
			}
			item.Locales = append(item.Locales, entry)
		}
		out[id] = item
	}
	return out, nil
}

// TranslationCounts are the console totals of one owner type.
type TranslationCounts struct {
	Total      int `json:"total"`
	Incomplete int `json:"incomplete"`
	Requested  int `json:"requested"`
}

// TranslationSummary counts translatable owners, those lacking a usable
// translation, and those waiting for a requested machine pass.
func (s *Store) TranslationSummary(ctx context.Context) (map[string]TranslationCounts, error) {
	out := map[string]TranslationCounts{}
	for _, ownerType := range []string{TranslationOwnerPost, TranslationOwnerVideo} {
		o := translationOwners[ownerType]
		var counts TranslationCounts
		err := s.db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM(CASE WHEN %[2]s THEN 1 ELSE 0 END), 0), COALESCE(SUM(CASE WHEN %[3]s THEN 1 ELSE 0 END), 0) FROM %[1]s o WHERE %[4]s`,
			o.table, TranslationIncompleteSQL(ownerType, "o"), TranslationRequestedSQL(ownerType, "o"), o.scopeSQL("o"))).
			Scan(&counts.Total, &counts.Incomplete, &counts.Requested)
		if err != nil {
			return nil, dbErr(err)
		}
		out[ownerType] = counts
	}
	return out, nil
}

// --- machine step -----------------------------------------------------------

// TranslationCandidate is one current wording the machine step may translate.
type TranslationCandidate struct {
	OwnerType, OwnerID, Field, Text, SourceHash string
	Requested                                   bool
	attempt                                     string
	instant                                     string
}

// ListTranslationCandidates returns up to limit owners in the machine scope
// whose current wording lacks a usable translation of a needed locale, plus
// owners an operator requested. Requests come first and ignore since; then
// owners never attempted for this wording, then the least recently
// attempted, newest first within each group.
func (s *Store) ListTranslationCandidates(ctx context.Context, since time.Time, limit int) ([]TranslationCandidate, error) {
	if limit < 1 {
		return []TranslationCandidate{}, nil
	}
	cutoff := since.UTC().Format(time.RFC3339Nano)
	all := []TranslationCandidate{}
	for _, ownerType := range []string{TranslationOwnerPost, TranslationOwnerVideo} {
		o := translationOwners[ownerType]
		query := fmt.Sprintf(`SELECT o.id, %[2]s, COALESCE(ts.requested, 0), CASE WHEN ts.source_hash = translation_hash(%[2]s) THEN COALESCE(ts.last_attempt_at, '') ELSE '' END, COALESCE(%[3]s, '')
			FROM %[1]s o LEFT JOIN translation_states ts ON ts.owner_type = ? AND ts.owner_id = o.id AND ts.field = ?
			WHERE %[4]s AND (COALESCE(ts.requested, 0) = 1 OR (julianday(%[3]s) >= julianday(?) AND %[5]s))
			ORDER BY COALESCE(ts.requested, 0) DESC, julianday(%[3]s) DESC, o.id ASC LIMIT 2000`,
			o.table, o.sourceSQL("o"), o.instantSQL("o"), o.scopeSQL("o"), TranslationIncompleteSQL(ownerType, "o"))
		rows, err := s.db.QueryContext(ctx, query, ownerType, o.field, cutoff)
		if err != nil {
			return nil, dbErr(err)
		}
		for rows.Next() {
			item := TranslationCandidate{OwnerType: ownerType, Field: o.field}
			if err := rows.Scan(&item.OwnerID, &item.Text, &item.Requested, &item.attempt, &item.instant); err != nil {
				rows.Close()
				return nil, dbErr(err)
			}
			item.SourceHash = TranslationHash(item.Text)
			all = append(all, item)
		}
		if err := rows.Close(); err != nil {
			return nil, dbErr(err)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.Requested != b.Requested {
			return a.Requested
		}
		if (a.attempt == "") != (b.attempt == "") {
			return a.attempt == ""
		}
		if a.attempt != b.attempt {
			return timeKey(&a.attempt) < timeKey(&b.attempt)
		}
		return timeKey(&a.instant) > timeKey(&b.instant)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// MachineTranslation is a model's result for one candidate.
type MachineTranslation struct {
	OwnerType, OwnerID, SourceHash string
	// SourceLanguage is one of SourceLanguages.
	SourceLanguage string
	// Texts holds the translated locales; a locale in the source language is
	// absent.
	Texts               map[string]string
	ProviderID, ModelID string
}

// TranslationOutcome records a machine attempt that produced no translation.
type TranslationOutcome struct {
	OwnerType, OwnerID, SourceHash string
	// Status is failed or skipped; skipped with SourceLanguage none marks
	// wording with nothing to translate.
	Status, SourceLanguage, Error string
	// KeepRequest leaves an operator request pending, as when no model is
	// configured.
	KeepRequest bool
}

func (s *Store) translationTx(ctx context.Context) (*sql.Tx, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	if err = fenceSourceWrite(ctx, tx); err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// currentTranslationSource reports whether the owner exists and its wording
// still has the given hash.
func currentTranslationSource(ctx context.Context, tx *sql.Tx, ownerType, ownerID, hash string) (bool, error) {
	o := translationOwners[ownerType]
	var current string
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT translation_hash(%s) FROM %s o WHERE o.id = ?`, o.sourceSQL("o"), o.table), ownerID).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return current == hash, nil
}

func writeTranslationState(ctx context.Context, tx *sql.Tx, ownerType, ownerID, hash, status, language, lastError, now string, keepRequest bool) error {
	o := translationOwners[ownerType]
	var languageValue, errorValue any
	if language != "" {
		languageValue = language
	}
	if lastError != "" {
		errorValue = lastError
	}
	// A new state row has no request to keep.
	_, err := tx.ExecContext(ctx, `INSERT INTO translation_states(owner_type, owner_id, field, source_hash, source_language, status, last_attempt_at, last_error, requested, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)
		ON CONFLICT(owner_type, owner_id, field) DO UPDATE SET source_hash = excluded.source_hash, source_language = excluded.source_language, status = excluded.status,
			last_attempt_at = excluded.last_attempt_at, last_error = excluded.last_error, requested = CASE WHEN ? THEN translation_states.requested ELSE 0 END, updated_at = excluded.updated_at`,
		ownerType, ownerID, o.field, hash, languageValue, status, now, errorValue, now, keepRequest)
	return err
}

// SaveMachineTranslation stores a model result when the wording is unchanged:
// the owner's machine rows are replaced, its state records success, and the
// dashboard revision moves when a row or the source language changed. A
// result for changed or removed wording is dropped and reported as false.
func (s *Store) SaveMachineTranslation(ctx context.Context, result MachineTranslation, now time.Time) (bool, error) {
	o, ok := translationOwners[result.OwnerType]
	if !ok || !SourceLanguages[result.SourceLanguage] {
		return false, ErrTranslationInvalid
	}
	for locale, text := range result.Texts {
		if !validTranslationLocale(locale) || strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > MaxTranslationRunes {
			return false, ErrTranslationInvalid
		}
	}
	tx, err := s.translationTx(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	current, err := currentTranslationSource(ctx, tx, result.OwnerType, result.OwnerID, result.SourceHash)
	if err != nil {
		return false, dbErr(err)
	}
	if !current {
		return false, nil
	}
	stamp := ISOTime(now)
	changed := false
	var previousLanguage sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT CASE WHEN source_hash = ? THEN source_language END FROM translation_states WHERE owner_type = ? AND owner_id = ? AND field = ?`,
		result.SourceHash, result.OwnerType, result.OwnerID, o.field).Scan(&previousLanguage); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, dbErr(err)
	}
	if previousLanguage.String != result.SourceLanguage {
		changed = true
	}
	// visible holds the machine rows readers already see; stored also covers
	// outdated rows, which are replaced or removed.
	visible, stored := map[string]string{}, map[string]bool{}
	rows, err := tx.QueryContext(ctx, `SELECT locale, text, source_hash FROM translations WHERE owner_type = ? AND owner_id = ? AND field = ? AND origin = 'machine'`,
		result.OwnerType, result.OwnerID, o.field)
	if err != nil {
		return false, dbErr(err)
	}
	for rows.Next() {
		var locale, text, hash string
		if err = rows.Scan(&locale, &text, &hash); err != nil {
			rows.Close()
			return false, dbErr(err)
		}
		stored[locale] = true
		if hash == result.SourceHash {
			visible[locale] = text
		}
	}
	if err = rows.Close(); err != nil {
		return false, dbErr(err)
	}
	for locale := range stored {
		if _, keep := result.Texts[locale]; !keep {
			if _, err = tx.ExecContext(ctx, `DELETE FROM translations WHERE owner_type = ? AND owner_id = ? AND field = ? AND locale = ? AND origin = 'machine'`,
				result.OwnerType, result.OwnerID, o.field, locale); err != nil {
				return false, dbErr(err)
			}
			changed = true
		}
	}
	for locale, text := range result.Texts {
		text = strings.TrimSpace(text)
		if previous, ok := visible[locale]; !ok || previous != text {
			changed = true
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO translations(owner_type, owner_id, field, locale, origin, platform, text, source_hash, provider_id, model_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, 'machine', '', ?, ?, ?, ?, ?, ?)
			ON CONFLICT(owner_type, owner_id, field, locale, origin, platform) DO UPDATE SET text = excluded.text, source_hash = excluded.source_hash,
				provider_id = excluded.provider_id, model_id = excluded.model_id, updated_at = excluded.updated_at`,
			result.OwnerType, result.OwnerID, o.field, locale, text, result.SourceHash, result.ProviderID, result.ModelID, stamp, stamp); err != nil {
			return false, dbErr(err)
		}
	}
	if err = writeTranslationState(ctx, tx, result.OwnerType, result.OwnerID, result.SourceHash, "success", result.SourceLanguage, "", stamp, false); err != nil {
		return false, dbErr(err)
	}
	if changed {
		if err = bumpRevision(ctx, tx, stamp); err != nil {
			return false, dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, ErrUnavailable
	}
	return true, nil
}

// RecordTranslationOutcome stores a failed or skipped attempt. Existing
// translations stay in place.
func (s *Store) RecordTranslationOutcome(ctx context.Context, outcome TranslationOutcome, now time.Time) error {
	if _, ok := translationOwners[outcome.OwnerType]; !ok || (outcome.Status != "failed" && outcome.Status != "skipped") {
		return ErrTranslationInvalid
	}
	if outcome.SourceLanguage != "" && !SourceLanguages[outcome.SourceLanguage] {
		return ErrTranslationInvalid
	}
	tx, err := s.translationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	current, err := currentTranslationSource(ctx, tx, outcome.OwnerType, outcome.OwnerID, outcome.SourceHash)
	if err != nil {
		return dbErr(err)
	}
	if !current {
		return nil
	}
	stamp := ISOTime(now)
	if err = writeTranslationState(ctx, tx, outcome.OwnerType, outcome.OwnerID, outcome.SourceHash, outcome.Status, outcome.SourceLanguage, outcome.Error, stamp, outcome.KeepRequest); err != nil {
		return dbErr(err)
	}
	// Wording with nothing to translate changes what readers are told.
	if outcome.SourceLanguage == "none" {
		if err = bumpRevision(ctx, tx, stamp); err != nil {
			return dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// --- platform writers -------------------------------------------------------

// TranslationWrite is a platform-provided translation: a YouTube localized
// title, or the Chinese title of a Bilibili upload merged into a video.
// SourceText is the wording it accompanies.
type TranslationWrite struct {
	OwnerType, OwnerID, Field, Locale, Platform, Text, SourceText string
}

func (w TranslationWrite) validate() error {
	o, ok := translationOwners[w.OwnerType]
	if !ok || w.Field != o.field || !validTranslationLocale(w.Locale) || strings.TrimSpace(w.OwnerID) == "" {
		return ErrTranslationInvalid
	}
	platform := strings.TrimSpace(w.Platform)
	if platform == "" || platform != w.Platform || len(platform) > 40 {
		return ErrTranslationInvalid
	}
	text := strings.TrimSpace(w.Text)
	if text == "" || utf8.RuneCountInString(text) > MaxTranslationRunes {
		return ErrTranslationInvalid
	}
	return nil
}

// UpsertPlatformTranslationTx stores a platform translation inside the
// caller's transaction and reports whether a row changed. The caller moves the
// dashboard revision when it reports true.
func UpsertPlatformTranslationTx(ctx context.Context, tx *sql.Tx, w TranslationWrite, now string) (bool, error) {
	if err := w.validate(); err != nil {
		return false, err
	}
	text := strings.TrimSpace(w.Text)
	var existing string
	found := true
	err := tx.QueryRowContext(ctx, `SELECT text FROM translations WHERE owner_type = ? AND owner_id = ? AND field = ? AND locale = ? AND origin = 'platform' AND platform = ?`,
		w.OwnerType, w.OwnerID, w.Field, w.Locale, w.Platform).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		found = false
	} else if err != nil {
		return false, dbErr(err)
	}
	hash := TranslationHash(w.SourceText)
	if _, err = tx.ExecContext(ctx, `INSERT INTO translations(owner_type, owner_id, field, locale, origin, platform, text, source_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'platform', ?, ?, ?, ?, ?)
		ON CONFLICT(owner_type, owner_id, field, locale, origin, platform) DO UPDATE SET text = excluded.text, source_hash = excluded.source_hash,
			updated_at = CASE WHEN translations.text = excluded.text AND translations.source_hash = excluded.source_hash THEN translations.updated_at ELSE excluded.updated_at END`,
		w.OwnerType, w.OwnerID, w.Field, w.Locale, w.Platform, text, hash, now, now); err != nil {
		return false, dbErr(err)
	}
	// Readers do not compare a platform row's hash, so only its text matters.
	return !found || existing != text, nil
}

// DeletePlatformTranslationTx removes a platform translation that its source
// no longer provides and reports whether a row was removed.
func DeletePlatformTranslationTx(ctx context.Context, tx *sql.Tx, ownerType, ownerID, field, locale, platform string) (bool, error) {
	if o, ok := translationOwners[ownerType]; !ok || field != o.field || !validTranslationLocale(locale) {
		return false, ErrTranslationInvalid
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM translations WHERE owner_type = ? AND owner_id = ? AND field = ? AND locale = ? AND origin = 'platform' AND platform = ?`,
		ownerType, ownerID, field, locale, platform)
	if err != nil {
		return false, dbErr(err)
	}
	n, err := result.RowsAffected()
	return n > 0, err
}

// UpsertPlatformTranslation stores a platform translation in its own
// transaction and moves the dashboard revision when it changed.
func (s *Store) UpsertPlatformTranslation(ctx context.Context, w TranslationWrite) error {
	return s.platformWrite(ctx, func(tx *sql.Tx, now string) (bool, error) {
		return UpsertPlatformTranslationTx(ctx, tx, w, now)
	})
}

// DeletePlatformTranslation removes a platform translation in its own
// transaction and moves the dashboard revision when a row was removed.
func (s *Store) DeletePlatformTranslation(ctx context.Context, ownerType, ownerID, field, locale, platform string) error {
	return s.platformWrite(ctx, func(tx *sql.Tx, _ string) (bool, error) {
		return DeletePlatformTranslationTx(ctx, tx, ownerType, ownerID, field, locale, platform)
	})
}

func (s *Store) platformWrite(ctx context.Context, write func(*sql.Tx, string) (bool, error)) error {
	tx, err := s.translationTx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := ISOTime(time.Now())
	changed, err := write(tx, now)
	if err != nil {
		return err
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// --- operator commands ------------------------------------------------------

func (s *Store) adminTranslationTx(ctx context.Context, ownerType, ownerID string) (*sql.Tx, translationOwner, error) {
	if !s.Writable() {
		return nil, translationOwner{}, ErrReadOnly
	}
	o, ok := translationOwners[ownerType]
	if !ok {
		return nil, o, ErrTranslationInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, o, ErrUnavailable
	}
	var exists int
	if err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE id = ?`, o.table), ownerID).Scan(&exists); err != nil {
		tx.Rollback()
		return nil, o, dbErr(err)
	}
	if exists == 0 {
		tx.Rollback()
		return nil, o, ErrTranslationOwner
	}
	return tx, o, nil
}

// SetManualTranslation stores an operator's translation of the owner's
// current wording. Automatic writers never replace it.
func (s *Store) SetManualTranslation(ctx context.Context, ownerType, ownerID, locale, text string) error {
	text = strings.TrimSpace(text)
	if !validTranslationLocale(locale) || text == "" || utf8.RuneCountInString(text) > MaxTranslationRunes {
		return ErrTranslationInvalid
	}
	tx, o, err := s.adminTranslationTx(ctx, ownerType, ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := ISOTime(time.Now())
	if _, err = tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO translations(owner_type, owner_id, field, locale, origin, platform, text, source_hash, created_at, updated_at)
		SELECT ?, o.id, ?, ?, 'manual', '', ?, translation_hash(%s), ?, ? FROM %s o WHERE o.id = ?
		ON CONFLICT(owner_type, owner_id, field, locale, origin, platform) DO UPDATE SET text = excluded.text, source_hash = excluded.source_hash, updated_at = excluded.updated_at`,
		o.sourceSQL("o"), o.table), ownerType, o.field, locale, text, now, now, ownerID); err != nil {
		return dbErr(err)
	}
	if err = bumpRevision(ctx, tx, now); err != nil {
		return dbErr(err)
	}
	if err = tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

// DeleteManualTranslation removes an operator's translation, so a platform or
// machine translation shows again.
func (s *Store) DeleteManualTranslation(ctx context.Context, ownerType, ownerID, locale string) error {
	if !validTranslationLocale(locale) {
		return ErrTranslationInvalid
	}
	tx, o, err := s.adminTranslationTx(ctx, ownerType, ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM translations WHERE owner_type = ? AND owner_id = ? AND field = ? AND locale = ? AND origin = 'manual'`, ownerType, ownerID, o.field, locale)
	if err != nil {
		return dbErr(err)
	}
	now := ISOTime(time.Now())
	if n, _ := result.RowsAffected(); n > 0 {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func requestTranslationSQL(ownerType string) string {
	o := translationOwners[ownerType]
	return fmt.Sprintf(`INSERT INTO translation_states(owner_type, owner_id, field, status, requested, updated_at)
		SELECT '%[1]s', o.id, '%[2]s', 'never', 1, ? FROM %[3]s o WHERE %%s
		ON CONFLICT(owner_type, owner_id, field) DO UPDATE SET requested = 1, updated_at = excluded.updated_at`, ownerType, o.field, o.table)
}

// RequestTranslations queues a forced machine pass for the given owners,
// replacing their machine translations on the next translate step. Owners
// outside the machine scope are ignored.
func (s *Store) RequestTranslations(ctx context.Context, ownerType string, ids []string) (int, error) {
	if !s.Writable() {
		return 0, ErrReadOnly
	}
	o, ok := translationOwners[ownerType]
	if !ok || len(ids) == 0 || len(ids) > 200 {
		return 0, ErrTranslationInvalid
	}
	now := ISOTime(time.Now())
	query := fmt.Sprintf(requestTranslationSQL(ownerType), "o.id IN ("+placeholders(len(ids))+") AND "+o.scopeSQL("o"))
	result, err := s.db.ExecContext(ctx, query, append([]any{now}, idArgs(ids)...)...)
	if err != nil {
		return 0, dbErr(err)
	}
	n, _ := result.RowsAffected()
	return int(n), nil
}

// RequestMissingTranslations queues a machine pass for every owner of the
// type (or of both types when ownerType is empty) that lacks a usable
// translation, whatever its age. It returns the number queued.
func (s *Store) RequestMissingTranslations(ctx context.Context, ownerType string) (int, error) {
	if !s.Writable() {
		return 0, ErrReadOnly
	}
	types := []string{TranslationOwnerPost, TranslationOwnerVideo}
	if ownerType != "" {
		if _, ok := translationOwners[ownerType]; !ok {
			return 0, ErrTranslationInvalid
		}
		types = []string{ownerType}
	}
	now := ISOTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, ErrUnavailable
	}
	defer tx.Rollback()
	total := 0
	for _, kind := range types {
		o := translationOwners[kind]
		query := fmt.Sprintf(requestTranslationSQL(kind), o.scopeSQL("o")+" AND "+TranslationIncompleteSQL(kind, "o"))
		result, err := tx.ExecContext(ctx, query, now)
		if err != nil {
			return 0, dbErr(err)
		}
		n, _ := result.RowsAffected()
		total += int(n)
	}
	if err = tx.Commit(); err != nil {
		return 0, ErrUnavailable
	}
	return total, nil
}
