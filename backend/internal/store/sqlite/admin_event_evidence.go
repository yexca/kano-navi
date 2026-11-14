package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Evidence roles: a post the entry was extracted from, a post of an entry
// merged into it, and the post that suggested its cancellation.
const (
	EvidenceSource       = "source"
	EvidenceMerged       = "merged"
	EvidenceCancellation = "cancellation"
)

const (
	// evidenceNearbyLimit bounds the context posts, nearest first.
	evidenceNearbyLimit = 20
	// evidenceNearbyWindow is how far before the first and after the last
	// evidence post the same accounts' posts are read.
	evidenceNearbyWindow = 24 * time.Hour
	// evidenceLeadWindow is how far before the start of an entry without a
	// stored post the own accounts' posts are read.
	evidenceLeadWindow = 72 * time.Hour
)

// AdminEventEvidence is what an operator reads to judge an entry: the stored
// posts it was extracted from, its merged duplicates' posts and the post that
// suggested its cancellation (Posts, oldest first), the links of evidence
// that is not a stored post (Links), and the same accounts' other posts
// around them (Nearby, oldest first).
type AdminEventEvidence struct {
	Event  AdminEvent          `json:"event"`
	Posts  []AdminEvidencePost `json:"posts"`
	Links  []AdminEvidenceLink `json:"links"`
	Nearby []AdminPost         `json:"nearby"`
}

// AdminEvidencePost is a stored post with what it says about the entry.
type AdminEvidencePost struct {
	AdminPost
	Roles []string `json:"roles"`
}

// AdminEvidenceLink is evidence without a stored post, such as a YouTube
// reservation, by its source and link.
type AdminEvidenceLink struct {
	Source string `json:"source"`
	URL    string `json:"url"`
	Role   string `json:"role"`
}

// AdminEventEvidence reads the evidence of the event id, deleted or merged
// ones included; a missing event is sql.ErrNoRows.
func (s *Store) AdminEventEvidence(ctx context.Context, id string) (AdminEventEvidence, error) {
	event, err := s.event(ctx, id)
	if err != nil {
		return AdminEventEvidence{}, err
	}
	out := AdminEventEvidence{Event: event, Posts: []AdminEvidencePost{}, Links: []AdminEvidenceLink{}, Nearby: []AdminPost{}}

	roles := map[string][]string{}
	postIDs := []string{}
	addPost := func(postID, role string) {
		if _, seen := roles[postID]; !seen {
			postIDs = append(postIDs, postID)
		}
		if !slices.Contains(roles[postID], role) {
			roles[postID] = append(roles[postID], role)
		}
	}
	rows, err := s.db.QueryContext(ctx, `WITH q(id) AS (SELECT ?)
 SELECT es.event_id,es.source,es.source_item_id,COALESCE(es.url,''),EXISTS (SELECT 1 FROM posts p WHERE p.id=es.source_item_id AND p.source=es.source)
 FROM q, `+eventEvidenceSQL("q", "es")+` ORDER BY es.id`, id)
	if err != nil {
		return AdminEventEvidence{}, fmt.Errorf("%w: event evidence", ErrUnavailable)
	}
	for rows.Next() {
		var owner, source, itemID, link string
		var stored int
		if err := rows.Scan(&owner, &source, &itemID, &link, &stored); err != nil {
			rows.Close()
			return AdminEventEvidence{}, fmt.Errorf("%w: event evidence", ErrUnavailable)
		}
		role := EvidenceSource
		if owner != id {
			role = EvidenceMerged
		}
		if stored != 0 {
			addPost(itemID, role)
		} else if link != "" && !slices.ContainsFunc(out.Links, func(l AdminEvidenceLink) bool { return l.URL == link }) {
			out.Links = append(out.Links, AdminEvidenceLink{Source: source, URL: link, Role: role})
		}
	}
	if err := closeEvidenceRows(rows); err != nil {
		return AdminEventEvidence{}, err
	}
	if cancelled := event.CancellationSourceItemID; cancelled != nil && *cancelled != "" {
		var stored int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM posts WHERE id=?)`, *cancelled).Scan(&stored); err != nil {
			return AdminEventEvidence{}, fmt.Errorf("%w: event evidence", ErrUnavailable)
		}
		if stored != 0 {
			addPost(*cancelled, EvidenceCancellation)
		}
	}

	posts, err := s.adminPostsByID(ctx, postIDs)
	if err != nil {
		return AdminEventEvidence{}, err
	}
	for _, post := range posts {
		out.Posts = append(out.Posts, AdminEvidencePost{AdminPost: post, Roles: roles[post.ID]})
	}

	nearbyIDs, err := s.evidenceNearbyIDs(ctx, event, posts)
	if err != nil {
		return AdminEventEvidence{}, err
	}
	if out.Nearby, err = s.adminPostsByID(ctx, nearbyIDs); err != nil {
		return AdminEventEvidence{}, err
	}
	return out, nil
}

// closeEvidenceRows closes rows and reports a read error as unavailable.
func closeEvidenceRows(rows *sql.Rows) error {
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("%w: event evidence", ErrUnavailable)
	}
	return rows.Close()
}

// adminPostsByID reads the console posts with these IDs, oldest first.
func (s *Store) adminPostsByID(ctx context.Context, ids []string) ([]AdminPost, error) {
	if len(ids) == 0 {
		return []AdminPost{}, nil
	}
	page, err := s.ListAdminPosts(ctx, AdminPostFilter{IDs: ids, PageSize: len(ids)})
	if err != nil {
		return nil, err
	}
	items := page.Items
	slices.Reverse(items)
	return items, nil
}

// evidenceNearbyIDs picks the context posts of an entry: the same accounts'
// posts around its evidence posts, or, for an entry without one, the own
// accounts' posts in the days before it starts. Nearest come first, up to
// evidenceNearbyLimit, and evidence posts are left out.
func (s *Store) evidenceNearbyIDs(ctx context.Context, event AdminEvent, evidence []AdminPost) ([]string, error) {
	var from, to, anchor time.Time
	clauses := []string{}
	args := []any{}
	accounts := map[string]bool{}
	for _, post := range evidence {
		published, err := time.Parse(time.RFC3339Nano, post.PublishedAt)
		if err != nil {
			continue
		}
		if anchor.IsZero() || published.Before(anchor) {
			anchor = published
		}
		if published.After(to) {
			to = published
		}
		handle := ""
		if post.AccountHandle != nil {
			handle = *post.AccountHandle
		}
		if key := post.Source + "\x00" + handle; !accounts[key] {
			accounts[key] = true
			clauses = append(clauses, "(p.source=? AND COALESCE(p.account_handle,'')=?)")
			args = append(args, post.Source, handle)
		}
	}
	if !anchor.IsZero() {
		from, to = anchor.Add(-evidenceNearbyWindow), to.Add(evidenceNearbyWindow)
	} else {
		start := event.StartsOn + "T12:00:00+09:00"
		if event.StartsAt != nil && *event.StartsAt != "" {
			start = *event.StartsAt
		}
		parsed, err := time.Parse(time.RFC3339Nano, start)
		if err != nil {
			return []string{}, nil
		}
		anchor, from, to = parsed, parsed.Add(-evidenceLeadWindow), parsed
		clauses = append(clauses, "(p.source IN ('x','bilibili') AND p.promoted_at IS NULL)")
	}
	where := "p.deleted_at IS NULL AND julianday(p.published_at) BETWEEN julianday(?) AND julianday(?) AND (" + strings.Join(clauses, " OR ") + ")"
	args = append([]any{from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339)}, args...)
	if len(evidence) > 0 {
		where += " AND p.id NOT IN (" + placeholders(len(evidence)) + ")"
		for _, post := range evidence {
			args = append(args, post.ID)
		}
	}
	args = append(args, anchor.UTC().Format(time.RFC3339), evidenceNearbyLimit)
	rows, err := s.db.QueryContext(ctx, `SELECT p.id FROM posts p WHERE `+where+` ORDER BY ABS(julianday(p.published_at)-julianday(?)),p.id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: event evidence", ErrUnavailable)
	}
	ids := []string{}
	for rows.Next() {
		var postID string
		if err := rows.Scan(&postID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: event evidence", ErrUnavailable)
		}
		ids = append(ids, postID)
	}
	if err := closeEvidenceRows(rows); err != nil {
		return nil, err
	}
	return ids, nil
}
