package contentadmin

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/yexca/kano-navi/backend/internal/httpx"
	"github.com/yexca/kano-navi/backend/internal/mediaformat"
	"github.com/yexca/kano-navi/backend/internal/store/sqlite"
)

var uploadContentTypePattern = regexp.MustCompile(`(?i)^(?:image/|application/octet-stream(?:;|$))`)

type profileItemResponse struct {
	Item *ProfileMedia `json:"item"`
	*profileMediaPayload
}

type profileDiscoverResponse struct {
	Slot       string         `json:"slot,omitempty"`
	Source     string         `json:"source"`
	SourceURL  string         `json:"sourceUrl"`
	Candidates []ProfileMedia `json:"candidates"`
	*profileMediaPayload
}

func (m *Module) listProfileMediaRoute(w http.ResponseWriter, r *http.Request) {
	payload, err := m.profilePayload(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

// writeProfileItem answers with the changed candidate and the refreshed list.
func (m *Module) writeProfileItem(w http.ResponseWriter, r *http.Request, status int, id string) {
	item, err := getProfileMedia(r.Context(), m.db, id)
	if err == nil && item == nil {
		err = failure("profile media candidate not found")
	}
	if err != nil {
		writeFailure(w, err)
		return
	}
	payload, err := m.profilePayload(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, status, profileItemResponse{Item: item, profileMediaPayload: payload})
}

// discoverySlotAndSource validates the shared slot/source pair. The slot is
// optional and stays empty when omitted; uploads are never a remote source.
func discoverySlotAndSource(slotText, sourceText *string) (string, string, error) {
	slot, source := "", ""
	if slotText != nil {
		if slot = normalizedProfileSlot(*slotText); slot == "" {
			return "", "", httpx.Invalid("slot or source is invalid")
		}
	}
	if sourceText != nil {
		source = normalizedProfileSource(*sourceText)
	}
	if source == "" || source == "upload" {
		return "", "", httpx.Invalid("slot or source is invalid")
	}
	return slot, source, nil
}

// optionalSlot reads an optional slot; empty means the caller decides.
func optionalSlot(value string) (string, error) {
	text, err := textValue(value, "slot", false, 16)
	if err != nil || text == nil {
		return "", err
	}
	slot := normalizedProfileSlot(*text)
	if slot == "" {
		return "", httpx.Invalid("slot is invalid")
	}
	return slot, nil
}

func (m *Module) addProfileMedia(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	slotText, err := textField(body, "slot", false, 16)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	sourceText, err := textField(body, "source", true, 16)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	slot, source, err := discoverySlotAndSource(slotText, sourceText)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if slot == "" {
		slot = "avatar"
	}
	rawURL, err := textValue(jsString(body["sourceUrl"]), "url", false, 2000)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if rawURL == nil {
		httpx.WriteError(w, httpx.Invalid("sourceUrl is required"))
		return
	}
	sourceURL, parsed := normalizeSourceURL(*rawURL)
	if parsed == nil {
		message := "url is invalid"
		if scheme := schemeOf(*rawURL); scheme != "" && scheme != "http" && scheme != "https" {
			message = "url must use HTTP or HTTPS"
		}
		httpx.WriteError(w, httpx.Invalid("%s", message))
		return
	}
	sourceRef, err := textField(body, "sourceRef", false, 2000)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	id, err := upsertProfileCandidate(r.Context(), m.db, profileCandidate{Slot: slot, Source: source, SourceRef: sourceRef, SourceURL: &sourceURL}, m.timestamp())
	if err != nil {
		writeFailure(w, err)
		return
	}
	m.writeProfileItem(w, r, http.StatusCreated, id)
}

var schemePattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)

func schemeOf(value string) string {
	match := schemePattern.FindStringSubmatch(value)
	if match == nil {
		return ""
	}
	return asciiLower(match[1])
}

func asciiLower(value string) string {
	out := []byte(value)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 'a' - 'A'
		}
	}
	return string(out)
}

func (m *Module) discoverProfileMediaRoute(w http.ResponseWriter, r *http.Request) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	slotText, err := textField(body, "slot", false, 16)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	sourceText, err := textField(body, "source", true, 16)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	slot, source, err := discoverySlotAndSource(slotText, sourceText)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := m.discoverProfileMedia(r.Context(), slot, source)
	if err != nil {
		writeFailure(w, err)
		return
	}
	payload, err := m.profilePayload(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	result.profileMediaPayload = payload
	httpx.WriteJSON(w, http.StatusOK, result)
}

// discoverProfileMedia reads one allow-listed public profile page and records
// the image URLs it references. Without a slot it reads both the avatar and the
// banner keys, and each URL keeps the first slot that found it. It runs only
// for an explicit operator action and holds no transaction while the page is
// fetched.
func (m *Module) discoverProfileMedia(ctx context.Context, slot, source string) (*profileDiscoverResponse, error) {
	var xURL, youtubeURL, avatarURL, bannerURL sql.NullString
	err := m.db.QueryRowContext(ctx, `SELECT x_url, youtube_url, avatar_url, banner_url FROM profiles ORDER BY id LIMIT 1`).Scan(&xURL, &youtubeURL, &avatarURL, &bannerURL)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	pageURL := defaultXProfilePage
	if source == "x" && xURL.String != "" {
		pageURL = xURL.String
	}
	if source == "youtube" {
		pageURL = defaultYouTubePage
		if youtubeURL.String != "" {
			pageURL = youtubeURL.String
		}
	}
	page, err := m.fetchAllowed(ctx, pageURL, pageFetch)
	if err != nil {
		return nil, err
	}
	if !page.ok() {
		return nil, httpStatusFailure(pageFetch, page.status)
	}
	slots := []string{slot}
	if slot == "" {
		slots = []string{"avatar", "banner"}
	}
	existing := map[string]string{"avatar": avatarURL.String, "banner": bannerURL.String}
	seen := map[string]bool{}
	candidates := []profileCandidate{}
	// A page can contain unrelated social images; those are skipped safely.
	for _, found := range slots {
		for _, value := range append(discoveredImageURLs(string(page.body), found), existing[found]) {
			imageURL, ok := profileImageURL(value)
			if !ok || seen[imageURL] {
				continue
			}
			seen[imageURL] = true
			candidates = append(candidates, profileCandidate{Slot: found, Source: source, SourceRef: &page.url, SourceURL: &imageURL})
		}
	}
	now := m.timestamp()
	err = m.inTx(ctx, func(tx *sql.Tx) error {
		for _, candidate := range candidates {
			if _, upsertErr := upsertProfileCandidate(ctx, tx, candidate, now); upsertErr != nil {
				return upsertErr
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	listed, err := listProfileMedia(ctx, m.db, slot)
	if err != nil {
		return nil, err
	}
	return &profileDiscoverResponse{Slot: slot, Source: source, SourceURL: page.url, Candidates: listed}, nil
}

func (m *Module) uploadProfileMedia(w http.ResponseWriter, r *http.Request, slotParam string) {
	contentType := r.Header.Get("Content-Type")
	var content []byte
	// Only image and octet-stream bodies are read, bounded like express.raw.
	if uploadContentTypePattern.MatchString(contentType) && r.Body != nil {
		if r.ContentLength > maxProfileImageBytes {
			httpx.WriteCode(w, http.StatusRequestEntityTooLarge, "request_too_large")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxProfileImageBytes))
		if err != nil {
			var large *http.MaxBytesError
			if errors.As(err, &large) {
				httpx.WriteCode(w, http.StatusRequestEntityTooLarge, "request_too_large")
				return
			}
			httpx.WriteCode(w, http.StatusBadRequest, "invalid_request")
			return
		}
		content = body
	}
	slot, err := optionalSlot(slotParam)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if slot == "" {
		slot = "avatar"
	}
	if len(content) == 0 {
		httpx.WriteError(w, httpx.Invalid("image body is required"))
		return
	}
	// The original bytes are verified and stored before any row exists, so a
	// rejected upload leaves no candidate behind.
	image, err := m.storeProfileImage("upload", content, normalizeMediaMIME(contentType), "uploaded file is not a supported image")
	if err != nil {
		writeFailure(w, err)
		return
	}
	now, id, sourceRef := m.timestamp(), "", "admin-upload"
	err = m.inTx(r.Context(), func(tx *sql.Tx) error {
		var upsertErr error
		if id, upsertErr = upsertProfileCandidate(r.Context(), tx, profileCandidate{Slot: slot, Source: "upload", SourceRef: &sourceRef}, now); upsertErr != nil {
			return upsertErr
		}
		return markProfileMediaReady(r.Context(), tx, id, image, now)
	})
	if err != nil {
		writeFailure(w, err)
		return
	}
	m.writeProfileItem(w, r, http.StatusCreated, id)
}

func (m *Module) downloadProfileMediaRoute(w http.ResponseWriter, r *http.Request, id string) {
	if err := m.downloadProfileMedia(r.Context(), id); err != nil {
		writeFailure(w, err)
		return
	}
	m.writeProfileItem(w, r, http.StatusOK, id)
}

// downloadProfileMedia fetches one candidate after an explicit request. A
// failure records a fixed description and keeps any earlier ready file.
func (m *Module) downloadProfileMedia(ctx context.Context, id string) error {
	candidate, err := getProfileMedia(ctx, m.db, id)
	if err != nil {
		return err
	}
	if candidate == nil {
		return failure("profile media candidate not found")
	}
	if candidate.SourceURL == nil || *candidate.SourceURL == "" {
		return failure("profile media candidate has no source URL")
	}
	image, err := m.fetchProfileImage(ctx, candidate)
	if err != nil {
		message := "profile media download failed"
		var op *opError
		if errors.As(err, &op) {
			message = op.message
		}
		// The outcome is recorded even when the operator's request was cancelled.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if markErr := markProfileMediaFailed(recordCtx, m.db, candidate.ID, message, m.timestamp()); markErr != nil {
			return markErr
		}
		return failure(message)
	}
	return markProfileMediaReady(ctx, m.db, candidate.ID, image, m.timestamp())
}

func (m *Module) fetchProfileImage(ctx context.Context, candidate *ProfileMedia) (*storedImage, error) {
	response, err := m.fetchAllowed(ctx, *candidate.SourceURL, imageFetch)
	if err != nil {
		return nil, err
	}
	if !response.ok() {
		return nil, httpStatusFailure(imageFetch, response.status)
	}
	return m.storeProfileImage(candidate.Source, response.body, normalizeMediaMIME(response.contentType), "profile response is not a supported image")
}

func (m *Module) selectProfileMediaRoute(w http.ResponseWriter, r *http.Request, id string) {
	body, err := httpx.Body(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	slot, err := optionalSlot(jsString(body["slot"]))
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	if err := m.selectProfileMedia(r.Context(), id, slot); err != nil {
		writeFailure(w, err)
		return
	}
	m.writeProfileItem(w, r, http.StatusOK, id)
}

// selectProfileMedia copies a ready candidate to data/avatar/<slot>.<ext> and
// makes it the active image for that slot, or for its own slot when none is
// given. A candidate selected for the other slot moves there, so a slot it was
// active for falls back to the built-in image. The copied bytes are
// re-verified so the public reader's hash and MIME checks agree with the
// stored metadata.
func (m *Module) selectProfileMedia(ctx context.Context, id, slot string) error {
	m.selectMu.Lock()
	defer m.selectMu.Unlock()
	candidate, err := getProfileMedia(ctx, m.db, id)
	if err != nil {
		return err
	}
	if candidate == nil || candidate.Status != "ready" {
		return failure("profile media is not ready")
	}
	if candidate.CachePath == nil || candidate.SHA256 == nil || candidate.MIMEType == nil {
		return failure("profile media cache path is invalid")
	}
	sourceFile, err := m.containedMediaFile(*candidate.CachePath, m.namespaceRoot(mediaNamespaceForSource(candidate.Source)), m.namespaceRoot("legacy"))
	if err != nil {
		return failure("profile media cache path is outside profile storage")
	}
	content, err := readBoundedFile(sourceFile)
	if err != nil {
		return failure("profile media cache file does not exist")
	}
	mimeType := mediaformat.Sniff(content)
	extension := mediaformat.Extension(mimeType)
	if extension == "" || mimeType != *candidate.MIMEType || sha256Hex(content) != *candidate.SHA256 {
		return failure("profile media cache file does not match its metadata")
	}
	if slot == "" {
		slot = candidate.Slot
	}
	if slot != "avatar" && slot != "banner" {
		return failure("invalid profile media slot")
	}
	destination := "avatar/" + slot + "." + extension
	if err = m.writeRootFile(destination, content, false); err != nil {
		return failure("profile media destination is invalid")
	}
	now := m.timestamp()
	return m.inTx(ctx, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx, `UPDATE profile_media SET is_active=0, updated_at=? WHERE slot=? AND is_active=1 AND id<>?`, now, slot, candidate.ID); execErr != nil {
			return execErr
		}
		if _, execErr := tx.ExecContext(ctx, `UPDATE profile_media SET slot=?, is_active=1, active_cache_path=?, active_sha256=?, active_mime_type=?, updated_at=? WHERE id=?`,
			slot, destination, *candidate.SHA256, mimeType, now, candidate.ID); execErr != nil {
			return execErr
		}
		return sqlite.BumpRevisionTx(ctx, tx, now)
	})
}

// deleteProfileMediaRoute removes one candidate row. Its cached and selected
// files stay on disk. Deleting the active candidate leaves its slot on the
// built-in image, and discovery may record a deleted URL again.
func (m *Module) deleteProfileMediaRoute(w http.ResponseWriter, r *http.Request, id string) {
	m.selectMu.Lock()
	defer m.selectMu.Unlock()
	candidate, err := getProfileMedia(r.Context(), m.db, id)
	if err != nil {
		writeFailure(w, err)
		return
	}
	if candidate == nil {
		httpx.WriteCode(w, http.StatusNotFound, "profile_media_not_found")
		return
	}
	now := m.timestamp()
	err = m.inTx(r.Context(), func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(r.Context(), `DELETE FROM profile_media WHERE id=?`, candidate.ID); execErr != nil {
			return execErr
		}
		if !candidate.IsActive {
			return nil
		}
		return sqlite.BumpRevisionTx(r.Context(), tx, now)
	})
	if err != nil {
		writeFailure(w, err)
		return
	}
	payload, err := m.profilePayload(r.Context())
	if err != nil {
		writeFailure(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, payload)
}

func (m *Module) previewProfileMedia(w http.ResponseWriter, r *http.Request, id string) {
	item, err := getProfileMedia(r.Context(), m.db, id)
	if err != nil {
		writeFailure(w, err)
		return
	}
	if item == nil || item.Status != "ready" || item.CachePath == nil || *item.CachePath == "" {
		httpx.WriteCode(w, http.StatusNotFound, "profile_media_not_ready")
		return
	}
	notFound := func() { httpx.WriteCode(w, http.StatusNotFound, "profile_media_not_found") }
	filename, err := m.containedMediaFile(*item.CachePath, m.allMediaRoots()...)
	if err != nil {
		notFound()
		return
	}
	content, err := readBoundedFile(filename)
	if err != nil {
		notFound()
		return
	}
	// The stored type must be a cacheable image and agree with the bytes, so a
	// replaced file can never be served as active same-origin content.
	mimeType := ""
	if item.MIMEType != nil {
		mimeType = normalizeMediaMIME(*item.MIMEType)
	}
	if mimeType == "" || mediaformat.Sniff(content) != mimeType {
		notFound()
		return
	}
	header := w.Header()
	header.Set("Content-Type", mimeType)
	header.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	header.Set("X-Content-Type-Options", "nosniff")
	if header.Get("Cache-Control") == "" {
		header.Set("Cache-Control", "no-store, no-transform")
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(content))
}
