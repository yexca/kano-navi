package schedule

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

// This file holds the part of a scan that the run and the queue preview
// share: the ordered candidate queue, the stop rule, and the judgement of
// one post. Judging reads nothing and writes nothing; the run carries a
// decision out through execute, the preview only reports it.

type candidate struct {
	post          post
	detectionType string
	force         bool
	attemptedAt   int64
}

// scanQueue is one scan's ordered candidates and what selecting them cost.
type scanQueue struct {
	config       scheduleConfig
	boardEnabled bool
	// disabled is a scan with both detection stages off and no request.
	disabled  bool
	queue     []candidate
	requested int
	// postsRead counts the rows the detection stages scanned, each stage
	// reading every shown X and Bilibili post.
	postsRead int
	elapsed   time.Duration
	// states is every post_llm_states row as the queue was built, keyed by
	// post; it orders the candidates and serves the preview's judging.
	states map[string]*postState
}

// buildQueue selects and orders a scan's candidates: operator reprocess
// requests (at most scanLimit, shown posts only) first, then the keyword
// candidates never attempted first, then the least recently attempted,
// newer posts first among equals.
func (e *Extractor) buildQueue(ctx context.Context) (scanQueue, error) {
	config, err := e.loadConfig(ctx)
	if err != nil {
		return scanQueue{}, err
	}
	started := time.Now()
	scan := scanQueue{config: config, boardEnabled: config.keywordEnabled && config.visionEnabled}
	requestedPosts, err := e.listRequestedPosts(ctx, scanLimit)
	if err != nil {
		return scanQueue{}, err
	}
	if !scan.boardEnabled && !config.messageEnabled && len(requestedPosts) == 0 {
		scan.disabled = true
		scan.elapsed = time.Since(started)
		return scan, nil
	}
	// A post selected by the board path is not sent again through the message
	// path in the same scan.
	keyword := []candidate{}
	keywordType := map[string]string{}
	for _, stage := range []struct {
		enabled       bool
		detectionType string
	}{{scan.boardEnabled, "board"}, {config.messageEnabled, "message"}} {
		if !stage.enabled {
			continue
		}
		posts, read, err := e.listCandidatePosts(ctx, config.keywords, stage.detectionType, config.messageAllPosts)
		scan.postsRead += read
		if err != nil {
			return scanQueue{}, err
		}
		for _, item := range posts {
			if _, known := keywordType[item.id]; !known {
				keywordType[item.id] = stage.detectionType
				keyword = append(keyword, candidate{post: item, detectionType: stage.detectionType})
			}
		}
	}
	// Operator reprocess requests run before keyword candidates so a full scan
	// limit cannot starve them, and they bypass the keyword heuristic.
	queue := []candidate{}
	requested := map[string]bool{}
	for _, item := range requestedPosts {
		queue = append(queue, candidate{post: item, detectionType: requestedDetectionType(item, keywordType[item.id]), force: true})
		requested[item.id] = true
	}
	states, err := listPostStates(ctx, e.db)
	if err != nil {
		return scanQueue{}, err
	}
	scan.states = states
	rest := []candidate{}
	for _, item := range keyword {
		if state := states[item.post.id]; state != nil {
			item.attemptedAt = instantMillis(state.lastAttemptAt)
		}
		if !requested[item.post.id] {
			rest = append(rest, item)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].attemptedAt != rest[j].attemptedAt {
			return rest[i].attemptedAt < rest[j].attemptedAt
		}
		return instantMillis(rest[i].post.publishedAt) > instantMillis(rest[j].post.publishedAt)
	})
	scan.queue = append(queue, rest...)
	scan.requested = len(requestedPosts)
	scan.elapsed = time.Since(started)
	return scan, nil
}

// walkQueue visits the queue in order until scanLimit visited posts took a
// place (postResult.takesSlot), and returns the index of the first post it
// did not visit. A cancelled context stops it before the next visit.
func walkQueue(ctx context.Context, queue []candidate, visit func(candidate) (postResult, error)) (int, error) {
	used := 0
	for i, item := range queue {
		if used >= scanLimit {
			return i, nil
		}
		if err := ctx.Err(); err != nil {
			return i, err
		}
		result, err := visit(item)
		if err != nil {
			return i, err
		}
		if result.takesSlot() {
			used++
		}
	}
	return len(queue), nil
}

// candidateInput is everything judging one post needs: the post with its
// shown quote, its post_llm_states row (nil without one), the state of its
// images, and the latest stored extraction of its current input, loaded
// only when the post is neither forced nor short of content.
type candidateInput struct {
	post   post
	state  *postState
	media  postImages
	latest *extractionRow
}

// candidateReads is where judging reads a post's stored input: its
// post_llm_states row, its linked post images, their media assets and the
// latest extraction of an input. The run reads the snapshot one query at a
// time (liveReads), since it writes between posts; the preview, which writes
// nothing, reads each table once (previewReads).
type candidateReads interface {
	postState(ctx context.Context, postID string) (*postState, error)
	imageLinks(ctx context.Context, postID string) ([]string, error)
	mediaAsset(ctx context.Context, id string) (*mediaAsset, error)
	latestExtraction(ctx context.Context, source, sourceItemID, fingerprint, versionPrefix string) (*extractionRow, error)
}

type liveReads struct{ q queryer }

func (r liveReads) postState(ctx context.Context, postID string) (*postState, error) {
	return getPostState(ctx, r.q, postID)
}

func (r liveReads) imageLinks(ctx context.Context, postID string) ([]string, error) {
	return postImageLinks(ctx, r.q, postID)
}

func (r liveReads) mediaAsset(ctx context.Context, id string) (*mediaAsset, error) {
	return getMediaAsset(ctx, r.q, id)
}

func (r liveReads) latestExtraction(ctx context.Context, source, sourceItemID, fingerprint, versionPrefix string) (*extractionRow, error) {
	return latestExtraction(ctx, r.q, source, sourceItemID, fingerprint, versionPrefix)
}

// previewReads answers the same reads from tables read once, in the order
// and with the matching rules of the live queries. Its extraction rows carry
// no result_json: judging reads only their status.
type previewReads struct {
	states      map[string]*postState
	links       map[string][]string
	assets      map[string]*mediaAsset
	extractions map[extractionKey][]versionedExtraction
}

type extractionKey struct{ source, sourceItemID, fingerprint string }

// versionedExtraction is one stored extraction with its extractor version,
// listed newest first per extractionKey.
type versionedExtraction struct {
	version string
	row     extractionRow
}

// loadPreviewReads reads the post images, their assets and the stored
// extractions once; states are the ones the queue was built with.
func loadPreviewReads(ctx context.Context, q queryer, states map[string]*postState) (previewReads, error) {
	reads := previewReads{states: states, links: map[string][]string{},
		assets: map[string]*mediaAsset{}, extractions: map[extractionKey][]versionedExtraction{}}
	rows, err := q.QueryContext(ctx, `SELECT owner_id, media_id FROM media_links
		WHERE owner_type = 'post' AND role = 'post-image'
		ORDER BY owner_id, position, id`)
	if err != nil {
		return previewReads{}, err
	}
	for rows.Next() {
		var owner, media string
		if err = rows.Scan(&owner, &media); err != nil {
			rows.Close()
			return previewReads{}, err
		}
		reads.links[owner] = append(reads.links[owner], media)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return previewReads{}, err
	}
	rows, err = q.QueryContext(ctx, `SELECT id, cache_path, mime_type, sha256, status FROM media_assets
		WHERE id IN (SELECT media_id FROM media_links WHERE owner_type = 'post' AND role = 'post-image')`)
	if err != nil {
		return previewReads{}, err
	}
	for rows.Next() {
		var id string
		var cachePath, mimeType, hash, status sql.NullString
		if err = rows.Scan(&id, &cachePath, &mimeType, &hash, &status); err != nil {
			rows.Close()
			return previewReads{}, err
		}
		reads.assets[id] = &mediaAsset{id, cachePath.String, mimeType.String, hash.String, status.String}
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return previewReads{}, err
	}
	rows, err = q.QueryContext(ctx, `SELECT source, source_item_id, content_fingerprint, extractor_version,
		id, model, status, updated_at
		FROM schedule_extractions ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return previewReads{}, err
	}
	for rows.Next() {
		var key extractionKey
		var stored versionedExtraction
		var model, status, updatedAt sql.NullString
		if err = rows.Scan(&key.source, &key.sourceItemID, &key.fingerprint, &stored.version,
			&stored.row.id, &model, &status, &updatedAt); err != nil {
			rows.Close()
			return previewReads{}, err
		}
		stored.row.model, stored.row.status, stored.row.updatedAt = model.String, status.String, updatedAt.String
		reads.extractions[key] = append(reads.extractions[key], stored)
	}
	if err = errors.Join(rows.Err(), rows.Close()); err != nil {
		return previewReads{}, err
	}
	return reads, nil
}

func (r previewReads) postState(_ context.Context, postID string) (*postState, error) {
	return r.states[postID], nil
}

func (r previewReads) imageLinks(_ context.Context, postID string) ([]string, error) {
	return append([]string{}, r.links[postID]...), nil
}

func (r previewReads) mediaAsset(_ context.Context, id string) (*mediaAsset, error) {
	if !mediaIDPattern.MatchString(id) {
		return nil, nil
	}
	return r.assets[id], nil
}

func (r previewReads) latestExtraction(_ context.Context, source, sourceItemID, fingerprint, versionPrefix string) (*extractionRow, error) {
	for _, stored := range r.extractions[extractionKey{source, sourceItemID, fingerprint}] {
		if strings.HasPrefix(stored.version, versionPrefix) {
			row := stored.row
			return &row, nil
		}
	}
	return nil, nil
}

// loadCandidate reads one post's candidate input.
func (e *Extractor) loadCandidate(ctx context.Context, reads candidateReads, item post, detectionType string, force bool) (candidateInput, error) {
	state, err := reads.postState(ctx, item.id)
	if err != nil {
		return candidateInput{}, err
	}
	media, err := e.postImageState(ctx, reads, item)
	if err != nil {
		return candidateInput{}, err
	}
	input := candidateInput{post: item, state: state, media: media}
	inputMode := inputModeOf(item, media)
	if effectiveForce(force, state) || inputMode == "" || (media.hasImages && !media.complete) {
		return input, nil
	}
	input.latest, err = reads.latestExtraction(ctx, item.extractionSource(), item.id,
		fingerprintOf(item, detectionType, inputMode, media), extractorVersionPrefix(detectionType, inputMode))
	return input, err
}

// judgeCandidate loads and judges one queued post: the step the run and the
// preview share. A settled post is the cache hit its stored state records,
// decided without loading or judging it: the stamp shows that its input,
// detection stage and extractor version are those of that result.
func (e *Extractor) judgeCandidate(ctx context.Context, reads candidateReads, item candidate, targets targetLookup, now time.Time) (candidateInput, decision, error) {
	if item.post.settled {
		return candidateInput{post: item.post}, decision{disposition: dispositionCached, settled: true}, nil
	}
	input, err := e.loadCandidate(ctx, reads, item.post, item.detectionType, item.force)
	if err != nil {
		return input, decision{}, err
	}
	decided, err := judge(input, item.detectionType, item.force, targets, now)
	return input, decided, err
}

// effectiveForce: a stored operator request forces the post like a request
// listed for this scan.
func effectiveForce(force bool, state *postState) bool {
	return force || (state != nil && state.reprocessRequested)
}

// inputModeOf is what the model is given: text (the post's own and its
// quote's), images, both, or "" for neither.
func inputModeOf(item post, media postImages) string {
	hasText := jsTrim(item.matchText()) != ""
	switch {
	case hasText && media.hasImages:
		return "text_image"
	case hasText:
		return "text"
	case media.hasImages:
		return "image"
	}
	return ""
}

// fingerprintOf is the content fingerprint of a post's extraction input.
func fingerprintOf(item post, detectionType, inputMode string, media postImages) string {
	hashes := make([]any, len(media.images))
	for i, image := range media.images {
		hashes[i] = image.sha256
	}
	identity := obj("detectionType", detectionType, "inputMode", inputMode, "text", item.text)
	if item.publishedAt != "" {
		identity.set("publishedAt", item.publishedAt)
	}
	identity.set("url", item.url)
	identity.set("images", hashes)
	// A post without a shown quote keeps the fingerprint it always had.
	if item.quote.text != "" {
		identity.set("quote", obj("author", item.quote.author, "text", item.quote.text))
	}
	return sha256Hex(stringify(identity))
}

// Dispositions of a judged post.
const (
	dispositionCall         = "call"
	dispositionCached       = "cached"
	dispositionBackoff      = "backoff"
	dispositionRetryStopped = "retryStopped"
	dispositionSkip         = "skip"
)

// decision is how a scan treats one post.
type decision struct {
	disposition string
	// reason is a skip's code: no_content, media_pending, or why no queue
	// target fits.
	reason string
	// force is the effective operator request, which bypasses the cache and
	// the failure backoff.
	force       bool
	fingerprint string
	inputMode   string
	failureKey  string
	// providers are the targets a call tries in order.
	providers []provider
	// repeated: the post's stored failures belong to this input and these
	// targets.
	repeated bool
	// retryAt is when a held-back post's backoff ends.
	retryAt time.Time
	cached  *extractionRow
	// settled: a cache hit of a settled post, decided without loading it;
	// its state already records the result.
	settled bool
}

// result is the scan outcome a decision stands for before it is carried
// out; a call stands for any provider outcome.
func (d decision) result() postResult {
	switch d.disposition {
	case dispositionSkip:
		return postResult{status: "skipped", reason: d.reason}
	case dispositionCall:
		return postResult{status: dispositionCall}
	}
	return postResult{status: d.disposition}
}

// targetLookup returns the usable text or image extraction queue targets of an
// input mode, or none with the reason (configuredProviders).
type targetLookup func(inputMode string) ([]provider, string, error)

// liveTargets reads the queue each time it is asked, as the run does for
// every post that reaches a model call.
func (e *Extractor) liveTargets(ctx context.Context) targetLookup {
	return func(inputMode string) ([]provider, string, error) {
		return e.configuredProviders(ctx, inputMode)
	}
}

// memoTargets reads the queue once per input mode, for a preview that
// judges every candidate against one picture of the queue.
func (e *Extractor) memoTargets(ctx context.Context) targetLookup {
	type answer struct {
		providers []provider
		reason    string
	}
	known := map[string]answer{}
	return func(inputMode string) ([]provider, string, error) {
		if hit, ok := known[inputMode]; ok {
			return hit.providers, hit.reason, nil
		}
		providers, reason, err := e.configuredProviders(ctx, inputMode)
		if err == nil {
			known[inputMode] = answer{providers, reason}
		}
		return providers, reason, err
	}
}

// judge decides how a scan treats one post at now without reading or
// writing the snapshot; only targets is asked for the queue targets, and
// only when the post gets that far.
func judge(input candidateInput, detectionType string, force bool, targets targetLookup, now time.Time) (decision, error) {
	item, previous, media := input.post, input.state, input.media
	decided := decision{force: effectiveForce(force, previous)}
	decided.inputMode = inputModeOf(item, media)
	if decided.inputMode == "" {
		decided.disposition, decided.reason = dispositionSkip, "no_content"
		return decided, nil
	}
	if media.hasImages && !media.complete {
		decided.disposition, decided.reason = dispositionSkip, "media_pending"
		return decided, nil
	}
	decided.fingerprint = fingerprintOf(item, detectionType, decided.inputMode, media)

	// A success or uncertain result stands for this input whichever queue
	// target produced it, so a queue change sends no post again. A newer
	// failed attempt, a changed input or an operator request does.
	if !decided.force {
		if latest := input.latest; latest != nil && (latest.status == "success" || latest.status == "uncertain") {
			decided.disposition, decided.cached = dispositionCached, latest
			return decided, nil
		}
	}
	providers, reason, err := targets(decided.inputMode)
	if err != nil {
		return decision{}, err
	}
	if len(providers) == 0 {
		decided.disposition, decided.reason = dispositionSkip, reason
		return decided, nil
	}
	decided.providers = providers

	// The backoff belongs to this input and these targets: new content, a
	// different queue or model, or an operator request starts it over.
	keys := []any{decided.fingerprint}
	for _, target := range providers {
		keys = append(keys, extractorVersionFor(target, detectionType, decided.inputMode))
	}
	decided.failureKey = sha256Hex(stringify(keys))
	decided.repeated = !decided.force && previous != nil && previous.failureCount > 0 && previous.failureFingerprint == decided.failureKey
	if decided.repeated {
		if previous.nextAttemptAt == "" {
			decided.disposition = dispositionRetryStopped
			return decided, nil
		}
		if retryAt, ok := parseInstant(previous.nextAttemptAt); ok && now.Before(retryAt) {
			decided.disposition, decided.retryAt = dispositionBackoff, retryAt
			return decided, nil
		}
	}
	decided.disposition = dispositionCall
	return decided, nil
}

// ---- queue preview ----

// QueuePreview is what a schedule run started now would do: the posts it
// would process, in order, and counts of the rest. Its size does not grow
// with the number of posts.
type QueuePreview struct {
	GeneratedAt string `json:"generatedAt"`
	// Disabled: both detection stages are off and nothing is requested.
	Disabled       bool `json:"disabled"`
	BoardEnabled   bool `json:"boardEnabled"`
	MessageEnabled bool `json:"messageEnabled"`
	AllPosts       bool `json:"allPosts"`
	// Limit is how many posts one run processes.
	Limit int `json:"limit"`
	// PostsRead, Candidates and CandidateMs are the run's candidate phase
	// counts; ElapsedMs is the whole preview.
	PostsRead   int   `json:"postsRead"`
	Candidates  int   `json:"candidates"`
	Requested   int   `json:"requested"`
	CandidateMs int64 `json:"candidateMs"`
	ElapsedMs   int64 `json:"elapsedMs"`
	// Run lists the posts this run processes, in order: model calls and
	// skips, each taking one of the Limit places.
	Run []QueueEntry `json:"run"`
	// Later counts the posts left for later runs that would take a place.
	Later        int       `json:"later"`
	Cached       int       `json:"cached"`
	Backoff      QueueHold `json:"backoff"`
	RetryStopped QueueHold `json:"retryStopped"`
}

// QueueHold counts held-back posts; NextAt is the earliest time one of them
// is due again, empty when none is.
type QueueHold struct {
	Count  int    `json:"count"`
	NextAt string `json:"nextAt,omitempty"`
}

// QueueEntry is one post a run would process.
type QueueEntry struct {
	PostID string `json:"postId"`
	Source string `json:"source"`
	// Route is board or message.
	Route string `json:"route"`
	// Outcome is call (a model call) or skip.
	Outcome string `json:"outcome"`
	// Reason is why a call happens (requested, new, changed, backoffDue) or a
	// skip's code.
	Reason        string `json:"reason"`
	InputMode     string `json:"inputMode,omitempty"`
	PublishedAt   string `json:"publishedAt,omitempty"`
	LastAttemptAt string `json:"lastAttemptAt,omitempty"`
	URL           string `json:"url,omitempty"`
	Excerpt       string `json:"excerpt"`
}

// callReason says why a post gets a model call: an operator request, a post
// whose backoff is over, a post never attempted, or one attempted before
// without a stored result for its current input.
func callReason(input candidateInput, decided decision) string {
	switch {
	case decided.force:
		return "requested"
	case decided.repeated:
		return "backoffDue"
	case input.state == nil || input.state.lastAttemptAt == "":
		return "new"
	}
	return "changed"
}

const excerptRunes = 140

func excerptOf(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if runes := []rune(text); len(runes) > excerptRunes {
		return string(runes[:excerptRunes]) + "…"
	}
	return text
}

// PreviewQueue judges the scan a run started now would make, in the run's
// order and with its stop rule. It writes nothing and calls no model; the
// queue targets are read once per input mode.
func (e *Extractor) PreviewQueue(ctx context.Context) (QueuePreview, error) {
	if e == nil || e.db == nil {
		return QueuePreview{}, ErrStore
	}
	started := time.Now()
	now := e.now()
	scan, err := e.buildQueue(ctx)
	if err != nil {
		return QueuePreview{}, previewError(ctx)
	}
	out := QueuePreview{
		GeneratedAt: sqlite.ISOTime(now), Disabled: scan.disabled,
		BoardEnabled: scan.boardEnabled, MessageEnabled: scan.config.messageEnabled,
		AllPosts: scan.config.messageAllPosts, Limit: scanLimit,
		PostsRead: scan.postsRead, Candidates: len(scan.queue), Requested: scan.requested,
		CandidateMs: scan.elapsed.Milliseconds(), Run: []QueueEntry{},
	}
	targets := e.memoTargets(ctx)
	reads, err := loadPreviewReads(ctx, e.db, scan.states)
	if err != nil {
		return QueuePreview{}, previewError(ctx)
	}
	var nextBackoff time.Time
	tally := func(decided decision) {
		switch decided.disposition {
		case dispositionCached:
			out.Cached++
		case dispositionBackoff:
			out.Backoff.Count++
			if nextBackoff.IsZero() || decided.retryAt.Before(nextBackoff) {
				nextBackoff = decided.retryAt
			}
		case dispositionRetryStopped:
			out.RetryStopped.Count++
		}
	}
	judged := func(item candidate) (candidateInput, decision, error) {
		return e.judgeCandidate(ctx, reads, item, targets, now)
	}
	next, err := walkQueue(ctx, scan.queue, func(item candidate) (postResult, error) {
		input, decided, err := judged(item)
		if err != nil {
			return postResult{}, err
		}
		tally(decided)
		result := decided.result()
		if result.takesSlot() {
			out.Run = append(out.Run, previewEntry(item, input, decided))
		}
		return result, nil
	})
	if err != nil {
		return QueuePreview{}, previewError(ctx)
	}
	for _, item := range scan.queue[next:] {
		_, decided, err := judged(item)
		if err != nil {
			return QueuePreview{}, previewError(ctx)
		}
		tally(decided)
		if decided.result().takesSlot() {
			out.Later++
		}
	}
	if !nextBackoff.IsZero() {
		out.Backoff.NextAt = sqlite.ISOTime(nextBackoff)
	}
	out.ElapsedMs = time.Since(started).Milliseconds()
	return out, nil
}

func previewError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrStore
}

func previewEntry(item candidate, input candidateInput, decided decision) QueueEntry {
	entry := QueueEntry{
		PostID: item.post.id, Source: item.post.source, Route: item.detectionType,
		Outcome: decided.disposition, Reason: decided.reason, InputMode: decided.inputMode,
		PublishedAt: item.post.publishedAt, URL: item.post.url, Excerpt: excerptOf(item.post.matchText()),
	}
	if decided.disposition == dispositionCall {
		entry.Reason = callReason(input, decided)
	}
	if input.state != nil {
		entry.LastAttemptAt = input.state.lastAttemptAt
	}
	return entry
}
