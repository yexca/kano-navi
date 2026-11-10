package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	xsource "github.com/yexca/kano-navi/backend/internal/source/x"
)

// Related X accounts are accounts such as Kano's agency whose own
// timelines are read into the X status pool (x_statuses) with the schedule's
// incremental cap. A status that matches the account's keyword rules, or that
// an operator promotes, becomes a post (posts.promoted_at); the rest stay in
// the pool,
// which no public reader reads. The account list lives in the app setting
// x_related_accounts, apart from the own accounts (X_HANDLES).

// XRelatedSetting is the app setting holding the related accounts as JSON.
const XRelatedSetting = "x_related_accounts"

// XRelatedSource is the sync_state source of a related account's timeline
// cursor, apart from the own accounts' "x" rows.
const XRelatedSource = "x-related"

// DefaultXRelatedKeywords are the keyword rules the console offers a newly
// added related account.
var DefaultXRelatedKeywords = []string{"鹿乃", "@kano_2525", "#鹿乃まほろ"}

// Bounds of the related account setting.
const (
	maxXRelatedAccounts = 10
	maxXRelatedKeywords = 30
	maxXRelatedKeyword  = 100
	maxXDecisionReason  = 500
)

// XRelatedAccount is one related account and its keyword rules.
type XRelatedAccount struct {
	Handle   string   `json:"handle"`
	Keywords []string `json:"keywords"`
}

// Errors of the related account commands.
var (
	ErrXRelatedInvalid = errors.New("invalid related accounts")
	ErrXStatusNotFound = errors.New("x status not found")
)

var xHandlePattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,30}$`)

type xQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// readXRelatedAccounts reads the setting. A missing or unreadable value is
// no related account, so a damaged setting never reads anything.
func readXRelatedAccounts(ctx context.Context, q xQueryer) ([]XRelatedAccount, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT value FROM app_settings WHERE key=?`, XRelatedSetting).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return []XRelatedAccount{}, nil
	}
	if err != nil {
		return nil, err
	}
	var accounts []XRelatedAccount
	if json.Unmarshal([]byte(raw), &accounts) != nil {
		return []XRelatedAccount{}, nil
	}
	out, err := NormalizeXRelatedAccounts(accounts, nil)
	if err != nil {
		return []XRelatedAccount{}, nil
	}
	return out, nil
}

// XRelatedAccounts lists the configured related accounts.
func (s *Store) XRelatedAccounts(ctx context.Context) ([]XRelatedAccount, error) {
	accounts, err := readXRelatedAccounts(ctx, s.db)
	if err != nil {
		return nil, dbErr(err)
	}
	return accounts, nil
}

// NormalizeXRelatedAccounts validates a related account list: valid handles
// without "@", each once and none of the own accounts, and trimmed, distinct
// keyword rules. A handle is in one role at a time.
func NormalizeXRelatedAccounts(accounts []XRelatedAccount, own []string) ([]XRelatedAccount, error) {
	if len(accounts) > maxXRelatedAccounts {
		return nil, fmt.Errorf("%w: at most %d related accounts", ErrXRelatedInvalid, maxXRelatedAccounts)
	}
	owned := map[string]bool{}
	for _, handle := range own {
		owned[strings.ToLower(strings.TrimPrefix(handle, "@"))] = true
	}
	seen := map[string]bool{}
	out := make([]XRelatedAccount, 0, len(accounts))
	for _, account := range accounts {
		handle := strings.TrimPrefix(strings.TrimSpace(account.Handle), "@")
		key := strings.ToLower(handle)
		switch {
		case !xHandlePattern.MatchString(handle):
			return nil, fmt.Errorf("%w: %q is not an X handle", ErrXRelatedInvalid, account.Handle)
		case seen[key]:
			return nil, fmt.Errorf("%w: @%s is listed twice", ErrXRelatedInvalid, handle)
		case owned[key]:
			return nil, fmt.Errorf("%w: @%s is one of the own accounts", ErrXRelatedInvalid, handle)
		}
		seen[key] = true
		keywords := []string{}
		known := map[string]bool{}
		for _, keyword := range account.Keywords {
			keyword = strings.TrimSpace(keyword)
			if keyword == "" || known[strings.ToLower(keyword)] {
				continue
			}
			if utf8.RuneCountInString(keyword) > maxXRelatedKeyword {
				return nil, fmt.Errorf("%w: a keyword of @%s is longer than %d characters", ErrXRelatedInvalid, handle, maxXRelatedKeyword)
			}
			known[strings.ToLower(keyword)] = true
			keywords = append(keywords, keyword)
		}
		if len(keywords) > maxXRelatedKeywords {
			return nil, fmt.Errorf("%w: @%s has more than %d keywords", ErrXRelatedInvalid, handle, maxXRelatedKeywords)
		}
		out = append(out, XRelatedAccount{Handle: handle, Keywords: keywords})
	}
	return out, nil
}

// SetXRelatedAccounts stores the related accounts and evaluates every pooled
// status's reasons against the new rules, promoting and demoting in the same
// transaction.
func (s *Store) SetXRelatedAccounts(ctx context.Context, accounts []XRelatedAccount, own []string) ([]XRelatedAccount, error) {
	if !s.Writable() {
		return nil, ErrReadOnly
	}
	normalized, err := NormalizeXRelatedAccounts(accounts, own)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(normalized)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer tx.Rollback()
	now := ISOTime(time.Now())
	if _, err = tx.ExecContext(ctx, `INSERT INTO app_settings(key,value,updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at`, XRelatedSetting, string(body), now); err != nil {
		return nil, dbErr(err)
	}
	changed, err := reconcileXStatuses(ctx, tx, normalized, nil, now)
	if err != nil {
		return nil, dbErr(err)
	}
	if changed {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return nil, dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, ErrUnavailable
	}
	return normalized, nil
}

// SettleXRelatedRoles keeps a handle in one role when X_HANDLES changes: at
// every start, an own account leaves the related list, and the posts the pool
// promoted for it become own posts, shown again when only the pool hid them.
func (s *Store) SettleXRelatedRoles(ctx context.Context, own []string) error {
	if !s.Writable() || len(own) == 0 {
		return nil
	}
	owned := map[string]bool{}
	args := []any{}
	for _, handle := range own {
		key := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
		if key != "" && !owned[key] {
			owned[key] = true
			args = append(args, key)
		}
	}
	if len(args) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrUnavailable
	}
	defer tx.Rollback()
	accounts, err := readXRelatedAccounts(ctx, tx)
	if err != nil {
		return dbErr(err)
	}
	kept := slices.DeleteFunc(slices.Clone(accounts), func(account XRelatedAccount) bool {
		return owned[strings.ToLower(account.Handle)]
	})
	now := ISOTime(time.Now())
	changed := false
	if len(kept) != len(accounts) {
		body, _ := json.Marshal(kept)
		if _, err = tx.ExecContext(ctx, `UPDATE app_settings SET value=?,updated_at=? WHERE key=?`, string(body), now, XRelatedSetting); err != nil {
			return dbErr(err)
		}
	}
	in := `lower(account_handle) IN (` + placeholders(len(args)) + `) AND promoted_at IS NOT NULL`
	shown, err := tx.ExecContext(ctx, `UPDATE posts SET hidden_at=NULL WHERE `+in+`
		AND hidden_at IS NOT NULL AND id IN (SELECT id FROM x_statuses WHERE demoted_at IS NOT NULL AND manual_decision IS NULL)`, args...)
	if err != nil {
		return dbErr(err)
	}
	adopted, err := tx.ExecContext(ctx, `UPDATE posts SET promoted_at=NULL WHERE `+in, args...)
	if err != nil {
		return dbErr(err)
	}
	if n, _ := shown.RowsAffected(); n > 0 {
		changed = true
	}
	if n, _ := adopted.RowsAffected(); n > 0 {
		changed = true
	}
	if _, err = tx.ExecContext(ctx, `UPDATE x_statuses SET demoted_at=NULL,manual_decision=NULL,manual_reason=NULL,manual_at=NULL,updated_at=?
		WHERE lower(author_handle) IN (`+placeholders(len(args))+`) AND (demoted_at IS NOT NULL OR manual_decision IS NOT NULL)`, append([]any{now}, args...)...); err != nil {
		return dbErr(err)
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

// relatedKeywords maps each related account's lower-case handle to its
// keyword rules.
func relatedKeywords(accounts []XRelatedAccount) map[string][]string {
	out := map[string][]string{}
	for _, account := range accounts {
		out[strings.ToLower(account.Handle)] = account.Keywords
	}
	return out
}

// matchesXKeyword reports whether text contains one of the rules, ignoring
// letter case.
func matchesXKeyword(text string, keywords []string) bool {
	lowered := strings.ToLower(text)
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(lowered, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// XRelatedState is what a related account's timeline read needs: the cursor
// and gap of its x-related row and the statuses its own timeline already
// listed into the pool.
type XRelatedState struct {
	KnownIDs    map[string]bool
	CursorID    string
	Gap         *xsource.Gap
	GapCeilings map[string]string
}

// XRelatedState reads a related account's incremental state. Statuses the
// pool holds only because an own post quoted them are not known: they say
// nothing about how far the timeline was read.
func (s *Store) XRelatedState(ctx context.Context, account string) (XRelatedState, error) {
	out := XRelatedState{KnownIDs: map[string]bool{}}
	var meta string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(metadata_json,'{}'),COALESCE(cursor_id,'') FROM sync_state WHERE source=? AND account_id=?`, XRelatedSource, strings.ToLower(account)).Scan(&meta, &out.CursorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, ErrSourceWrite
	}
	var data struct {
		Gap *xsource.Gap `json:"timelineGap"`
	}
	_ = json.Unmarshal([]byte(meta), &data)
	listed := `FROM x_statuses WHERE lower(author_handle)=lower(?) AND listed_at IS NOT NULL`
	if data.Gap != nil && data.Gap.Floor != "" {
		out.Gap = data.Gap
		out.GapCeilings = map[string]string{}
		for gap := data.Gap; gap != nil; gap = gap.Older {
			var ceiling string
			err = s.db.QueryRowContext(ctx, `SELECT id `+listed+` AND (length(id)>length(?) OR (length(id)=length(?) AND id>?)) ORDER BY length(id),id LIMIT 1`, account, gap.Floor, gap.Floor, gap.Floor).Scan(&ceiling)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return out, ErrSourceWrite
			}
			if ceiling != "" {
				out.GapCeilings[gap.Floor] = ceiling
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id `+listed+` ORDER BY length(id) DESC,id DESC LIMIT 200`, account)
	if err != nil {
		return out, ErrSourceWrite
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return out, ErrSourceWrite
		}
		out.KnownIDs[id] = true
	}
	if rows.Err() != nil {
		return out, ErrSourceWrite
	}
	return out, nil
}

// WriteXRelated stores a related account's timeline read in the pool,
// evaluates the listed statuses' reasons, promotes or demotes them, and
// replaces the account's x-related state, all in one transaction. Its media
// are registered only for the statuses it promotes.
func (s *Store) WriteXRelated(ctx context.Context, account string, result xsource.Result, success bool) (XRelatedWrite, error) {
	out := XRelatedWrite{}
	if s == nil || s.db == nil || !s.workflowWritable {
		return out, ErrSourceWrite
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, ErrSourceWrite
	}
	defer tx.Rollback()
	if err = fenceSourceWrite(ctx, tx); err != nil {
		return out, err
	}
	now := ISOTime(time.Now())
	accounts, err := readXRelatedAccounts(ctx, tx)
	if err != nil {
		return out, ErrSourceWrite
	}
	ids := []string{}
	for _, p := range result.Posts {
		if p.AuthorScreenName != "" && !strings.EqualFold(p.AuthorScreenName, account) {
			continue
		}
		raw, _ := json.Marshal(p.Raw)
		name, _ := objectValue(p.Raw["author"])["name"].(string)
		if _, err = tx.ExecContext(ctx, `INSERT INTO x_statuses(id,author_handle,author_name,text,published_at,url,raw_json,listed_at,first_seen_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET author_handle=excluded.author_handle,author_name=excluded.author_name,text=excluded.text,
				published_at=excluded.published_at,url=excluded.url,raw_json=excluded.raw_json,
				listed_at=COALESCE(x_statuses.listed_at,excluded.listed_at),updated_at=excluded.updated_at`,
			p.ID, account, nullable(name), p.Text, nullable(p.PublishedAt), p.URL, string(raw), now, now, now); err != nil {
			return out, ErrSourceWrite
		}
		ids = append(ids, p.ID)
		out.Listed++
	}
	if len(ids) > 0 {
		changes, reconcileErr := reconcileXStatusChanges(ctx, tx, accounts, ids, now)
		if reconcileErr != nil {
			return out, ErrSourceWrite
		}
		out.Promoted, out.Demoted = changes.promoted, changes.demoted
		if changes.any() {
			if err = bumpRevision(ctx, tx, now); err != nil {
				return out, ErrSourceWrite
			}
		}
	}
	if err = writeXRelatedState(ctx, tx, account, result, success, now); err != nil {
		return out, ErrSourceWrite
	}
	if err = tx.Commit(); err != nil {
		return out, ErrSourceWrite
	}
	return out, nil
}

// XRelatedWrite counts what a related account's read stored: the statuses
// its pages listed, and those it promoted or demoted.
type XRelatedWrite struct {
	Listed, Promoted, Demoted int
}

// writeXRelatedState replaces a related account's x-related row: a cursor
// that moves forward only after a successful read, and the timeline gap.
func writeXRelatedState(ctx context.Context, tx *sql.Tx, account string, result xsource.Result, success bool, now string) error {
	key := strings.ToLower(account)
	metadata := map[string]any{"handle": account, "discovered": result.DiscoveredCount, "listed": len(result.Posts)}
	if result.Discovery != "" {
		metadata["discovery"] = result.Discovery
	}
	if result.Gap != nil {
		metadata["timelineGap"] = result.Gap
	}
	if !success && len(result.Errors) > 0 {
		metadata["lastError"] = result.Errors[0]
	}
	body, _ := json.Marshal(metadata)
	var oldID, oldTime string
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(cursor_id,''),COALESCE(cursor_time,'') FROM sync_state WHERE source=? AND account_id=?`, XRelatedSource, key).Scan(&oldID, &oldTime)
	cursorID, cursorTime := oldID, oldTime
	oldInstant, _ := time.Parse(time.RFC3339Nano, oldTime)
	newInstant, _ := time.Parse(time.RFC3339Nano, result.NextCursorTime)
	if success && result.NextCursor != "" && (oldTime == "" || newInstant.After(oldInstant)) {
		cursorID, cursorTime = result.NextCursor, result.NextCursorTime
	}
	var lastSuccess any
	if success {
		lastSuccess = now
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO sync_state(source,account_id,cursor_id,cursor_time,last_success_at,metadata_json) VALUES(?,?,?,?,?,?)
		ON CONFLICT(source,account_id) DO UPDATE SET cursor_id=excluded.cursor_id,cursor_time=excluded.cursor_time,
			last_success_at=COALESCE(excluded.last_success_at,sync_state.last_success_at),metadata_json=excluded.metadata_json`,
		XRelatedSource, key, nullable(cursorID), nullable(cursorTime), lastSuccess, string(body))
	return err
}

type xStatusChanges struct{ promoted, demoted, shown int }

func (c xStatusChanges) any() bool { return c.promoted+c.demoted+c.shown > 0 }

// reconcileXStatuses evaluates the reasons of the given pooled statuses, or
// of every one when ids is nil, and reports whether a post changed.
func reconcileXStatuses(ctx context.Context, tx *sql.Tx, accounts []XRelatedAccount, ids []string, now string) (bool, error) {
	changes, err := reconcileXStatusChanges(ctx, tx, accounts, ids, now)
	return changes.any(), err
}

// reconcileXStatusChanges applies the promotion rules:
//
//   - keyword holds for a related account's status whose text contains one of
//     the account's rules;
//   - an operator's decision (manual_decision promote or demote) is a durable
//     lock that outranks it;
//   - promotion writes the status to posts from its stored raw_json with
//     promoted_at, registering its images but no schedule candidate;
//   - demotion sets posts.hidden_at and keeps the row, its media and events.
//     An automatic demotion records demoted_at, so only a hide the pool made
//     is undone by a later automatic promotion; an operator's hide in Posts
//     stays. Showing a promoted post in Posts records a manual promote.
//
// A pooled status that is an own post is never touched.
func reconcileXStatusChanges(ctx context.Context, tx *sql.Tx, accounts []XRelatedAccount, ids []string, now string) (xStatusChanges, error) {
	changes := xStatusChanges{}
	query := `SELECT s.id,s.author_handle,s.text,s.keyword,COALESCE(s.manual_decision,''),s.demoted_at IS NOT NULL,COALESCE(s.raw_json,''),
		p.id IS NOT NULL,p.promoted_at IS NOT NULL,p.hidden_at IS NOT NULL
		FROM x_statuses s LEFT JOIN posts p ON p.id=s.id`
	args := []any{}
	if ids != nil {
		if len(ids) == 0 {
			return changes, nil
		}
		query += ` WHERE s.id IN (` + placeholders(len(ids)) + `)`
		args = idArgs(ids)
	}
	type status struct {
		id, author, text, decision, raw    string
		keyword, autoDemoted               bool
		stored, promoted, hidden, matching bool
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return changes, err
	}
	found := []status{}
	rules := relatedKeywords(accounts)
	for rows.Next() {
		var st status
		if err = rows.Scan(&st.id, &st.author, &st.text, &st.keyword, &st.decision, &st.autoDemoted, &st.raw, &st.stored, &st.promoted, &st.hidden); err != nil {
			rows.Close()
			return changes, err
		}
		keywords, related := rules[strings.ToLower(st.author)]
		st.matching = related && matchesXKeyword(st.text, keywords)
		found = append(found, st)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return changes, err
	}
	for _, st := range found {
		if st.matching != st.keyword {
			if _, err = tx.ExecContext(ctx, `UPDATE x_statuses SET keyword=?,updated_at=? WHERE id=?`, st.matching, now, st.id); err != nil {
				return changes, err
			}
		}
		// An own post keeps its own life; the pool only translates around it.
		if st.stored && !st.promoted {
			continue
		}
		wanted := st.decision == xDecisionPromote || (st.decision == "" && st.matching)
		switch {
		case wanted && !st.stored:
			promoted, promoteErr := promoteXStatus(ctx, tx, st.id, st.author, st.raw, now)
			if promoteErr != nil {
				return changes, promoteErr
			}
			if promoted {
				changes.promoted++
			}
		case wanted && st.hidden && st.autoDemoted:
			if _, err = tx.ExecContext(ctx, `UPDATE posts SET hidden_at=NULL WHERE id=?`, st.id); err != nil {
				return changes, err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE x_statuses SET demoted_at=NULL,updated_at=? WHERE id=?`, now, st.id); err != nil {
				return changes, err
			}
			changes.shown++
		case !wanted && st.stored && !st.hidden:
			if _, err = tx.ExecContext(ctx, `UPDATE posts SET hidden_at=? WHERE id=?`, now, st.id); err != nil {
				return changes, err
			}
			if st.decision == "" {
				if _, err = tx.ExecContext(ctx, `UPDATE x_statuses SET demoted_at=?,updated_at=? WHERE id=?`, now, now, st.id); err != nil {
					return changes, err
				}
			}
			changes.demoted++
		}
	}
	return changes, nil
}

// promoteXStatus writes a pooled status to posts from its stored raw_json,
// under its author's handle and marked promoted. It reports false when the
// stored payload has no usable status.
func promoteXStatus(ctx context.Context, tx *sql.Tx, id, author, raw, now string) (bool, error) {
	var payload map[string]any
	if raw == "" || json.Unmarshal([]byte(raw), &payload) != nil {
		return false, nil
	}
	post := xsource.Normalize(payload, id, author)
	if post == nil || post.ID != id {
		return false, nil
	}
	stored, err := writeXPostAs(ctx, tx, author, *post, now, false)
	if err != nil || !stored {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE posts SET promoted_at=COALESCE(promoted_at,?) WHERE id=?`, now, id); err != nil {
		return false, err
	}
	return true, nil
}

// Operator decisions on a pooled status.
const (
	xDecisionPromote   = "promote"
	xDecisionDemote    = "demote"
	xDecisionAutomatic = "automatic"
)

// SetXStatusDecision records an operator's promote or demote as a durable
// lock with its reason, or with "automatic" removes the lock so the rules
// decide again. A promote needs a related account's status and shows a
// promoted post hidden before; a demote hides the promoted post. The status's
// own post is never touched.
func (s *Store) SetXStatusDecision(ctx context.Context, id, decision, reason string) (AdminXStatus, error) {
	if !s.Writable() {
		return AdminXStatus{}, ErrReadOnly
	}
	reason = strings.TrimSpace(reason)
	switch decision {
	case xDecisionPromote, xDecisionDemote:
		if reason == "" || utf8.RuneCountInString(reason) > maxXDecisionReason {
			return AdminXStatus{}, fmt.Errorf("%w: a reason of at most %d characters is required", ErrXRelatedInvalid, maxXDecisionReason)
		}
	case xDecisionAutomatic:
		reason = ""
	default:
		return AdminXStatus{}, fmt.Errorf("%w: decision must be promote, demote or automatic", ErrXRelatedInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AdminXStatus{}, ErrUnavailable
	}
	defer tx.Rollback()
	var author, previous string
	var stored, promoted, hidden bool
	err = tx.QueryRowContext(ctx, `SELECT s.author_handle,COALESCE(s.manual_decision,''),p.id IS NOT NULL,p.promoted_at IS NOT NULL,p.hidden_at IS NOT NULL
		FROM x_statuses s LEFT JOIN posts p ON p.id=s.id WHERE s.id=?`, id).Scan(&author, &previous, &stored, &promoted, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return AdminXStatus{}, ErrXStatusNotFound
	}
	if err != nil {
		return AdminXStatus{}, dbErr(err)
	}
	if stored && !promoted {
		return AdminXStatus{}, fmt.Errorf("%w: the status is an own post", ErrXRelatedInvalid)
	}
	accounts, err := readXRelatedAccounts(ctx, tx)
	if err != nil {
		return AdminXStatus{}, dbErr(err)
	}
	if _, related := relatedKeywords(accounts)[strings.ToLower(author)]; decision == xDecisionPromote && !related {
		return AdminXStatus{}, fmt.Errorf("%w: only a related account's status can be promoted", ErrXRelatedInvalid)
	}
	now := ISOTime(time.Now())
	changed := false
	switch decision {
	case xDecisionAutomatic:
		// A demote lock lifted leaves its hide to the rules.
		demoted := any(nil)
		if previous == xDecisionDemote && hidden {
			demoted = now
		}
		_, err = tx.ExecContext(ctx, `UPDATE x_statuses SET manual_decision=NULL,manual_reason=NULL,manual_at=NULL,demoted_at=COALESCE(?,demoted_at),updated_at=? WHERE id=?`, demoted, now, id)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE x_statuses SET manual_decision=?,manual_reason=?,manual_at=?,demoted_at=NULL,updated_at=? WHERE id=?`, decision, reason, now, now, id)
	}
	if err != nil {
		return AdminXStatus{}, dbErr(err)
	}
	switch {
	case decision == xDecisionPromote && stored && hidden:
		if _, err = tx.ExecContext(ctx, `UPDATE posts SET hidden_at=NULL WHERE id=?`, id); err != nil {
			return AdminXStatus{}, dbErr(err)
		}
		changed = true
	case decision == xDecisionDemote && stored && !hidden:
		if _, err = tx.ExecContext(ctx, `UPDATE posts SET hidden_at=? WHERE id=?`, now, id); err != nil {
			return AdminXStatus{}, dbErr(err)
		}
		changed = true
	}
	more, err := reconcileXStatuses(ctx, tx, accounts, []string{id}, now)
	if err != nil {
		return AdminXStatus{}, dbErr(err)
	}
	if changed || more {
		if err = bumpRevision(ctx, tx, now); err != nil {
			return AdminXStatus{}, dbErr(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return AdminXStatus{}, ErrUnavailable
	}
	page, err := s.ListAdminXStatuses(ctx, AdminXStatusFilter{IDs: []string{id}})
	if err != nil {
		return AdminXStatus{}, err
	}
	if len(page.Items) == 0 {
		return AdminXStatus{}, ErrXStatusNotFound
	}
	return page.Items[0], nil
}

// AdminXStatus is a pooled status as the console's External posts view lists
// it. State is promoted (a shown promoted post), demoted (a hidden one),
// stored (pool only) or post (an own post the pool keeps for translation).
// Reasons lists keyword, manual, quoted and reposted, or none.
type AdminXStatus struct {
	ID             string   `json:"id"`
	AuthorHandle   string   `json:"authorHandle"`
	AuthorName     *string  `json:"authorName"`
	Text           string   `json:"text"`
	PublishedAt    *string  `json:"publishedAt"`
	URL            string   `json:"url"`
	Images         int      `json:"images"`
	Reasons        []string `json:"reasons"`
	State          string   `json:"state"`
	Related        bool     `json:"related"`
	Decision       *string  `json:"decision"`
	DecisionReason *string  `json:"decisionReason"`
	DecidedAt      *string  `json:"decidedAt"`
	QuotedAt       *string  `json:"quotedAt"`
	RepostedAt     *string  `json:"repostedAt"`
	ListedAt       *string  `json:"listedAt"`
	FirstSeenAt    string   `json:"firstSeenAt"`
	// QuotedBy and RepostedBy are the own posts that quote it (newest
	// first, at most five) and the own accounts that reposted it.
	QuotedBy   []string `json:"quotedBy"`
	RepostedBy []string `json:"repostedBy"`
	// Translation is the status's x_status translation summary, nil for a
	// status whose translations are its post's.
	Translation *AdminTranslation `json:"translation"`
}

// AdminXStatusFilter selects pooled statuses. Reason is keyword, manual,
// quoted, reposted or none; State promoted, demoted, stored or post.
// ExcludeOwn leaves out every own post and the statuses of the Own handles,
// as the External posts view lists the pool.
type AdminXStatusFilter struct {
	Page, PageSize        int
	Author, Reason, State string
	Search                string
	IDs                   []string
	ExcludeOwn            bool
	Own                   []string
}

// xStatusStateSQL is a pooled status's state over x_statuses s joined with
// posts p.
const xStatusStateSQL = `CASE WHEN p.id IS NULL THEN 'stored' WHEN p.promoted_at IS NULL THEN 'post' WHEN p.hidden_at IS NULL THEN 'promoted' ELSE 'demoted' END`

// ListAdminXStatuses pages the pool, newest status first.
func (s *Store) ListAdminXStatuses(ctx context.Context, f AdminXStatusFilter) (AdminPage[AdminXStatus], error) {
	page, pageSize := boundPage(f.Page, f.PageSize, 100, 25)
	clauses := []string{"1=1"}
	args := []any{}
	if author := strings.TrimPrefix(strings.TrimSpace(f.Author), "@"); author != "" {
		clauses = append(clauses, "lower(s.author_handle)=lower(?)")
		args = append(args, author)
	}
	switch f.Reason {
	case "keyword":
		clauses = append(clauses, "s.keyword=1")
	case "manual":
		clauses = append(clauses, "s.manual_decision='promote'")
	case "quoted":
		clauses = append(clauses, "s.quoted_at IS NOT NULL")
	case "reposted":
		clauses = append(clauses, "s.reposted_at IS NOT NULL")
	case "none":
		clauses = append(clauses, "s.keyword=0 AND COALESCE(s.manual_decision,'')<>'promote' AND s.quoted_at IS NULL AND s.reposted_at IS NULL")
	}
	switch f.State {
	case "promoted", "demoted", "stored", "post":
		clauses = append(clauses, xStatusStateSQL+"=?")
		args = append(args, f.State)
	}
	if search := strings.TrimSpace(f.Search); search != "" {
		clauses = append(clauses, "(s.text LIKE ? ESCAPE '\\' OR s.id=?)")
		args = append(args, likePattern(search), search)
	}
	if f.ExcludeOwn {
		clauses = append(clauses, "(p.id IS NULL OR p.promoted_at IS NOT NULL)")
		for _, handle := range f.Own {
			if handle = strings.TrimPrefix(strings.TrimSpace(handle), "@"); handle != "" {
				clauses = append(clauses, "lower(s.author_handle)<>lower(?)")
				args = append(args, handle)
			}
		}
	}
	if f.IDs != nil {
		if len(f.IDs) == 0 {
			return AdminPage[AdminXStatus]{Items: []AdminXStatus{}, Page: 1, PageSize: pageSize, TotalPages: 1}, nil
		}
		clauses = append(clauses, "s.id IN ("+placeholders(len(f.IDs))+")")
		args = append(args, idArgs(f.IDs)...)
	}
	where := strings.Join(clauses, " AND ")
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM x_statuses s LEFT JOIN posts p ON p.id=s.id WHERE `+where, args...).Scan(&total); err != nil {
		return AdminPage[AdminXStatus]{}, dbErr(err)
	}
	totalPages := pageCount(total, pageSize)
	page = safePage(page, totalPages)
	rows, err := s.db.QueryContext(ctx, `SELECT s.id,s.author_handle,s.author_name,s.text,s.published_at,s.url,
		COALESCE(json_array_length(CASE WHEN json_valid(s.raw_json) THEN json_extract(s.raw_json,'$.mediaURLs') END),0),
		s.keyword,s.manual_decision,s.manual_reason,s.manual_at,s.quoted_at,s.reposted_at,s.listed_at,s.first_seen_at,`+xStatusStateSQL+`
		FROM x_statuses s LEFT JOIN posts p ON p.id=s.id WHERE `+where+`
		ORDER BY length(s.id) DESC,s.id DESC LIMIT ? OFFSET ?`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return AdminPage[AdminXStatus]{}, dbErr(err)
	}
	items := []AdminXStatus{}
	for rows.Next() {
		var item AdminXStatus
		var name, published, decision, reason, decided, quoted, reposted, listed sql.NullString
		var keyword bool
		if err = rows.Scan(&item.ID, &item.AuthorHandle, &name, &item.Text, &published, &item.URL, &item.Images,
			&keyword, &decision, &reason, &decided, &quoted, &reposted, &listed, &item.FirstSeenAt, &item.State); err != nil {
			rows.Close()
			return AdminPage[AdminXStatus]{}, dbErr(err)
		}
		item.AuthorName, item.PublishedAt = nullString(name), nullString(published)
		item.Decision, item.DecisionReason, item.DecidedAt = nullString(decision), nullString(reason), nullString(decided)
		item.QuotedAt, item.RepostedAt, item.ListedAt = nullString(quoted), nullString(reposted), nullString(listed)
		item.Reasons = []string{}
		if keyword {
			item.Reasons = append(item.Reasons, "keyword")
		}
		if decision.String == xDecisionPromote {
			item.Reasons = append(item.Reasons, "manual")
		}
		if quoted.Valid {
			item.Reasons = append(item.Reasons, "quoted")
		}
		if reposted.Valid {
			item.Reasons = append(item.Reasons, "reposted")
		}
		if len(item.Reasons) == 0 {
			item.Reasons = append(item.Reasons, "none")
		}
		item.QuotedBy, item.RepostedBy = []string{}, []string{}
		items = append(items, item)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return AdminPage[AdminXStatus]{}, dbErr(err)
	}
	accounts, err := readXRelatedAccounts(ctx, s.db)
	if err != nil {
		return AdminPage[AdminXStatus]{}, dbErr(err)
	}
	rules := relatedKeywords(accounts)
	if err = s.xStatusLinks(ctx, items); err != nil {
		return AdminPage[AdminXStatus]{}, err
	}
	pooled := []string{}
	for i := range items {
		item := &items[i]
		_, item.Related = rules[strings.ToLower(item.AuthorHandle)]
		if item.State == "stored" {
			pooled = append(pooled, item.ID)
		}
	}
	translations, err := s.AdminTranslations(ctx, TranslationOwnerXStatus, pooled)
	if err != nil {
		return AdminPage[AdminXStatus]{}, err
	}
	for i := range items {
		if translation, ok := translations[items[i].ID]; ok {
			items[i].Translation = &translation
		}
	}
	return AdminPage[AdminXStatus]{Items: items, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

// xStatusLinks fills, for a page of pooled statuses, the own posts that quote
// each (newest first, at most five) and the own accounts that reposted it.
// One scan of the posts' quotes serves the whole page.
func (s *Store) xStatusLinks(ctx context.Context, items []AdminXStatus) error {
	if len(items) == 0 {
		return nil
	}
	index := make(map[string]*AdminXStatus, len(items))
	ids := make([]string, 0, len(items))
	for i := range items {
		index[items[i].ID] = &items[i]
		ids = append(ids, items[i].ID)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT quoted, id FROM (
		SELECT json_extract(quote_json,'$.id') AS quoted, id,
			ROW_NUMBER() OVER (PARTITION BY json_extract(quote_json,'$.id') ORDER BY julianday(published_at) DESC,id) AS n
		FROM posts WHERE source='x' AND promoted_at IS NULL AND json_valid(quote_json)
			AND json_extract(quote_json,'$.id') IN (`+placeholders(len(ids))+`))
		WHERE n<=5 ORDER BY quoted,n`, idArgs(ids)...)
	if err != nil {
		return dbErr(err)
	}
	for rows.Next() {
		var quoted, id string
		if err = rows.Scan(&quoted, &id); err != nil {
			rows.Close()
			return dbErr(err)
		}
		if item := index[quoted]; item != nil {
			item.QuotedBy = append(item.QuotedBy, id)
		}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return dbErr(err)
	}
	rows, err = s.db.QueryContext(ctx, `SELECT status_id, account_handle FROM x_reposts
		WHERE status_id IN (`+placeholders(len(ids))+`) ORDER BY status_id, account_handle`, idArgs(ids)...)
	if err != nil {
		return dbErr(err)
	}
	for rows.Next() {
		var status, account string
		if err = rows.Scan(&status, &account); err != nil {
			rows.Close()
			return dbErr(err)
		}
		if item := index[status]; item != nil {
			item.RepostedBy = append(item.RepostedBy, account)
		}
	}
	return dbErr(errors.Join(rows.Err(), rows.Close()))
}

// XRelatedStatus is a related account's read as the Sources page shows it.
type XRelatedStatus struct {
	Handle        string  `json:"handle"`
	LastSuccessAt *string `json:"lastSuccessAt"`
	LastError     *string `json:"lastError"`
	Statuses      int     `json:"statuses"`
	Promoted      int     `json:"promoted"`
}

// XRelatedStatuses reports each related account's last read and pool size.
func (s *Store) XRelatedStatuses(ctx context.Context, accounts []XRelatedAccount) ([]XRelatedStatus, error) {
	out := make([]XRelatedStatus, 0, len(accounts))
	for _, account := range accounts {
		status := XRelatedStatus{Handle: account.Handle}
		var success, meta sql.NullString
		err := s.db.QueryRowContext(ctx, `SELECT last_success_at,metadata_json FROM sync_state WHERE source=? AND account_id=?`, XRelatedSource, strings.ToLower(account.Handle)).Scan(&success, &meta)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, dbErr(err)
		}
		status.LastSuccessAt = nullString(success)
		var data struct {
			LastError string `json:"lastError"`
		}
		if json.Unmarshal([]byte(meta.String), &data) == nil && data.LastError != "" {
			status.LastError = &data.LastError
		}
		if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN p.promoted_at IS NOT NULL AND p.hidden_at IS NULL THEN 1 ELSE 0 END),0)
			FROM x_statuses s LEFT JOIN posts p ON p.id=s.id WHERE lower(s.author_handle)=lower(?)`, account.Handle).Scan(&status.Statuses, &status.Promoted); err != nil {
			return nil, dbErr(err)
		}
		out = append(out, status)
	}
	return out, nil
}

func objectValue(v any) map[string]any { m, _ := v.(map[string]any); return m }
