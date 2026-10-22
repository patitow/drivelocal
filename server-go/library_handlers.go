package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// registerLibraryRoutes liga a API da biblioteca no subrouter protegido.
// Chamado por main.go; o banco pode não existir (biblioteca desativada → 503).
func registerLibraryRoutes(protected *mux.Router) {
	h := func(fn func(*Library, http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			l := getLibrary()
			if l == nil {
				libError(w, http.StatusServiceUnavailable, "biblioteca indisponível")
				return
			}
			// Escritas mudam a home: a do próprio usuário, ou a de todos se for admin.
			if r.Method != http.MethodGet {
				if strings.Contains(r.URL.Path, "/admin/") {
					defer l.invalidateHome("")
				} else {
					defer l.invalidateHome(r.Header.Get("X-User-ID"))
				}
			}
			fn(l, w, r)
		}
	}
	p := protected
	p.HandleFunc("/catalog/items", h(libItemsHandler)).Methods("GET")
	p.HandleFunc("/art/{file}", h(libArtFileHandler)).Methods("GET")
	p.HandleFunc("/catalog/search", h(libSearchHandler)).Methods("GET")
	p.HandleFunc("/catalog/home", h(libHomeHandler)).Methods("GET")
	p.HandleFunc("/catalog/feed", h(libFeedHandler)).Methods("GET")
	p.HandleFunc("/catalog/mangas", h(libMangasHandler)).Methods("GET")
	p.HandleFunc("/catalog/tv-series", h(libTVSeriesHandler)).Methods("GET")
	p.HandleFunc("/catalog/franchises", h(libFranchisesHandler)).Methods("GET")
	p.HandleFunc("/catalog/facets", h(libFacetsHandler)).Methods("GET")
	p.HandleFunc("/catalog/lookup", h(libLookupHandler)).Methods("GET")
	p.HandleFunc("/catalog/items/{id:[0-9]+}", h(libItemHandler)).Methods("GET")
	p.HandleFunc("/catalog/items/{id:[0-9]+}", h(libItemPatchHandler)).Methods("PATCH")
	p.HandleFunc("/catalog/items/{id:[0-9]+}/tags", h(libItemTagsHandler)).Methods("POST")

	p.HandleFunc("/tags", h(libTagsHandler)).Methods("GET")
	p.HandleFunc("/topics", h(libTopicsHandler)).Methods("GET")
	p.HandleFunc("/meta", h(libMetaHandler)).Methods("GET")
	p.HandleFunc("/meta/art", h(libArtHandler)).Methods("GET")
	p.HandleFunc("/topics/{id:[0-9]+}", h(libTopicHandler)).Methods("GET")
	p.HandleFunc("/topics/{id:[0-9]+}/items", h(libTopicItemsHandler)).Methods("GET")
	p.HandleFunc("/topics/{id:[0-9]+}/suggest", h(libTopicSuggestHandler)).Methods("POST")

	p.HandleFunc("/me/favorites", h(libFavoritesHandler)).Methods("GET")
	p.HandleFunc("/me/favorites/{id:[0-9]+}", h(libFavoriteSetHandler)).Methods("PUT", "DELETE")
	p.HandleFunc("/me/favorite-topics/{id:[0-9]+}", h(libFavoriteTopicSetHandler)).Methods("PUT", "DELETE")
	p.HandleFunc("/me/hidden", h(libHiddenListHandler)).Methods("GET")
	p.HandleFunc("/me/hidden/{id:[0-9]+}", h(libHiddenSetHandler)).Methods("PUT", "DELETE")
	p.HandleFunc("/me/progress", h(libProgressByPathHandler)).Methods("GET", "PUT")
	p.HandleFunc("/me/progress/{id:[0-9]+}", h(libProgressHandler)).Methods("PUT", "DELETE")
	p.HandleFunc("/me/continue", h(libContinueHandler)).Methods("GET", "DELETE")
	p.HandleFunc("/me/continue/{id:[0-9]+}", h(libContinueDismissHandler)).Methods("DELETE")
	p.HandleFunc("/me/prefs", h(libPrefsHandler)).Methods("GET", "PUT")
	p.HandleFunc("/me/manga-progress", h(libMangaProgressHandler)).Methods("GET", "PUT")
	p.HandleFunc("/me/creator-news", h(libCreatorNewsHandler)).Methods("GET", "POST")

	p.HandleFunc("/playlists", h(libPlaylistsHandler)).Methods("GET", "POST")
	p.HandleFunc("/playlists/{id:[0-9]+}", h(libPlaylistHandler)).Methods("GET", "PATCH", "DELETE")
	p.HandleFunc("/playlists/{id:[0-9]+}/items", h(libPlaylistAddHandler)).Methods("POST")
	p.HandleFunc("/playlists/{id:[0-9]+}/items/{mediaId:[0-9]+}", h(libPlaylistRemoveHandler)).Methods("DELETE")
	p.HandleFunc("/playlists/{id:[0-9]+}/move", h(libPlaylistMoveHandler)).Methods("POST")

	a := protected.PathPrefix("/admin/library").Subrouter()
	a.Use(requireRole("admin"))
	a.HandleFunc("/stats", h(libStatsHandler)).Methods("GET")
	a.HandleFunc("/art", h(libArtStoreHandler)).Methods("POST")
	a.HandleFunc("/art/frame", h(libArtFrameHandler)).Methods("GET")
	a.HandleFunc("/policies", h(libPoliciesHandler)).Methods("GET", "PUT")
	a.HandleFunc("/users", h(libUsersHandler)).Methods("GET")
	a.HandleFunc("/users/{id}", h(libUserSetHandler)).Methods("PUT")
	a.HandleFunc("/scan", h(libScanHandler)).Methods("GET", "POST")
	a.HandleFunc("/tags", h(libTagCreateHandler)).Methods("POST")
	a.HandleFunc("/tags/{id:[0-9]+}", h(libTagEditHandler)).Methods("PATCH", "DELETE")
	a.HandleFunc("/tags/{id:[0-9]+}/merge", h(libTagMergeHandler)).Methods("POST")
	a.HandleFunc("/topics", h(libTopicCreateHandler)).Methods("POST")
	a.HandleFunc("/topics/{id:[0-9]+}", h(libTopicEditHandler)).Methods("PATCH", "DELETE")
	a.HandleFunc("/topics/{id:[0-9]+}/merge", h(libTopicMergeHandler)).Methods("POST")
	a.HandleFunc("/topics/{id:[0-9]+}/dissolve", h(libTopicDissolveHandler)).Methods("POST")
	a.HandleFunc("/topic-blocks", h(libTopicBlocksHandler)).Methods("GET")
	a.HandleFunc("/topic-blocks/restore", h(libTopicRestoreHandler)).Methods("POST")
	a.HandleFunc("/topic-flags", h(libTopicFlagsHandler)).Methods("GET")
	a.HandleFunc("/topic-flags/{id:[0-9]+}/dismiss", h(libTopicFlagDismissHandler)).Methods("POST")
	a.HandleFunc("/topic-dupes", h(libTopicDupesHandler)).Methods("GET")
	a.HandleFunc("/topic-dupes/scan", h(libTopicDupesScanHandler)).Methods("POST")
	a.HandleFunc("/topic-dupes/accept", h(libTopicDupesAcceptHandler)).Methods("POST")
	a.HandleFunc("/topic-dupes/reject", h(libTopicDupesRejectHandler)).Methods("POST")
	a.HandleFunc("/topic-merges", h(libTopicMergesHandler)).Methods("GET")
	a.HandleFunc("/topic-merges/{batch}/undo", h(libTopicMergeUndoHandler)).Methods("POST")
	a.HandleFunc("/file-dupes", h(libFileDupesHandler)).Methods("GET", "POST")
	a.HandleFunc("/durations", h(libDurationsHandler)).Methods("GET", "POST")
	a.HandleFunc("/dark-thumbs", h(libDarkThumbsHandler)).Methods("GET", "POST")
	a.HandleFunc("/rules", h(libRulesHandler)).Methods("GET", "POST")
	a.HandleFunc("/rules/apply", h(libRulesApplyHandler)).Methods("POST")
	a.HandleFunc("/rules/{id:[0-9]+}", h(libRuleHandler)).Methods("PUT", "DELETE")
	a.HandleFunc("/batch", h(libBatchHandler)).Methods("POST")
	a.HandleFunc("/changes", h(libChangesHandler)).Methods("GET")
	a.HandleFunc("/changes/{batch}/undo", h(libUndoHandler)).Methods("POST")
	a.HandleFunc("/tokens", h(libTokensHandler)).Methods("GET", "POST")
	a.HandleFunc("/tokens/{id:[0-9]+}", h(libTokenRevokeHandler)).Methods("DELETE")
	a.HandleFunc("/export", h(libExportHandler)).Methods("GET")
	a.HandleFunc("/backup", h(libBackupHandler)).Methods("POST")
	a.HandleFunc("/review", h(libReviewListHandler)).Methods("GET")
	a.HandleFunc("/review", h(libReviewHandler)).Methods("POST")
	a.HandleFunc("/ai/status", h(libAIStatusHandler)).Methods("GET")
	a.HandleFunc("/ai/enqueue", h(libAIEnqueueHandler)).Methods("POST")
	a.HandleFunc("/ai/run", h(libAIRunHandler)).Methods("POST")
	a.HandleFunc("/ai/pause", h(libAIPauseHandler)).Methods("POST")
	a.HandleFunc("/ai/clear", h(libAIClearHandler)).Methods("POST")
	a.HandleFunc("/golden", h(libGoldenListHandler)).Methods("GET")
	a.HandleFunc("/golden/sample", h(libGoldenSampleHandler)).Methods("POST")
	a.HandleFunc("/golden/{id:[0-9]+}", h(libGoldenLabelHandler)).Methods("PUT")
	a.HandleFunc("/eval", h(libEvalHandler)).Methods("GET")
	a.HandleFunc("/entries", h(libEntriesHandler)).Methods("GET", "POST", "PUT", "DELETE")
	a.HandleFunc("/image-folders", h(libImageFoldersHandler)).Methods("GET")
	a.HandleFunc("/meta/search", h(libMetaSearchHandler)).Methods("GET")
}

// ---------- utilitários ----------

func libError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func libFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errNotFound):
		libError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errForbidden):
		libError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, errInvalid):
		libError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errScanRunning):
		libError(w, http.StatusConflict, err.Error())
	default:
		logError("biblioteca: %v", err)
		libError(w, http.StatusBadRequest, err.Error())
	}
}

func libJSON(w http.ResponseWriter, v any) { jsonResponse(w, v) }

func pathID(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(mux.Vars(r)[key], 10, 64)
	return n
}

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 4<<20))
	if err := dec.Decode(v); err != nil {
		return errInvalid
	}
	return nil
}

func qInt(r *http.Request, key string) int {
	n, _ := strconv.Atoi(r.URL.Query().Get(key))
	return n
}

func qBool(r *http.Request, key string) bool {
	v := r.URL.Query().Get(key)
	return v == "1" || v == "true"
}

func actorOf(r *http.Request) string {
	if u := r.Header.Get("X-Username"); u != "" {
		return u
	}
	return r.Header.Get("X-User-ID")
}

func itemQueryFromRequest(r *http.Request) ItemQuery {
	q := r.URL.Query()
	iq := ItemQuery{
		Q: q.Get("q"), Kind: q.Get("kind"), Root: q.Get("root"), PathPrefix: q.Get("pathPrefix"),
		Maturity: q.Get("maturity"), Category: q.Get("category"), Sort: q.Get("sort"),
		Favorites: qBool(r, "favorites"), Unwatched: qBool(r, "unwatched"), OnlyHidden: qBool(r, "onlyHidden"),
		Limit: qInt(r, "limit"), Offset: qInt(r, "offset"),
	}
	if t := q.Get("tags"); t != "" {
		for _, s := range strings.Split(t, ",") {
			if s = strings.TrimSpace(s); s != "" {
				iq.Tags = append(iq.Tags, s)
			}
		}
	}
	iq.TopicID, _ = strconv.ParseInt(q.Get("topicId"), 10, 64)
	iq.MaxDurS, _ = strconv.ParseFloat(q.Get("maxDurationS"), 64)
	iq.MinDurS, _ = strconv.ParseFloat(q.Get("minDurationS"), 64)
	iq.Since, _ = strconv.ParseInt(q.Get("since"), 10, 64)
	iq.InProgress = qBool(r, "inProgress")
	return iq
}

// ---------- catálogo ----------

func libItemsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	page, err := l.QueryItems(r.Context(), l.viewer(r), itemQueryFromRequest(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, page)
}

func libFacetsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	f, err := l.Facets(r.Context(), l.viewer(r), itemQueryFromRequest(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, f)
}

// libLookupHandler: GET /catalog/lookup?path=... devolve o item (cataloga na hora se preciso).
func libLookupHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	rel, ok := l.resolveVisible(v, decodeMediaPath(r.URL.Query().Get("path")))
	if !ok || rel == "" {
		libError(w, http.StatusNotFound, "não encontrado")
		return
	}
	id, err := l.EnsureMediaForPath(r.Context(), rel)
	if err != nil {
		libFail(w, err)
		return
	}
	it, err := l.GetItem(r.Context(), v, id)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, it)
}

func libItemHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	it, err := l.GetItem(r.Context(), l.viewer(r), pathID(r, "id"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, it)
}

// PATCH /catalog/items/{id} (admin): título, maturidade, categoria, tópico.
func libItemPatchHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	if !v.IsAdmin {
		libError(w, http.StatusForbidden, "só administradores")
		return
	}
	var acts Actions
	if err := decodeBody(r, &acts); err != nil {
		libFail(w, err)
		return
	}
	id := pathID(r, "id")
	res, err := l.ApplyActions(r.Context(), []int64{id}, acts, "manual", actorOf(r), "edição manual", false)
	if err != nil {
		libFail(w, err)
		return
	}
	it, _ := l.GetItem(r.Context(), v, id)
	libJSON(w, map[string]any{"result": res, "item": it})
}

// POST /catalog/items/{id}/tags {add:[], remove:[]} — qualquer usuário que vê o item.
func libItemTagsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetItem(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	var body struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	res, err := l.ApplyActions(r.Context(), []int64{id}, Actions{AddTags: body.Add, RemoveTags: body.Remove}, "manual", actorOf(r), "tags", false)
	if err != nil {
		libFail(w, err)
		return
	}
	it, _ := l.GetItem(r.Context(), v, id)
	libJSON(w, map[string]any{"result": res, "item": it})
}

// ---------- tags e tópicos ----------

func libTagsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	tags, err := l.ListTags(r.Context(), l.viewer(r), r.URL.Query().Get("q"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"tags": tags})
}

func libTopicsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, err := l.ListTopics(r.Context(), l.viewer(r), TopicQuery{Kind: q.Get("kind"), Q: q.Get("q"), Favorites: qBool(r, "favorites"), Limit: qInt(r, "limit"), Offset: qInt(r, "offset")})
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"topics": list, "total": total})
}

func libTopicHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	t, err := l.GetTopic(r.Context(), l.viewer(r), pathID(r, "id"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, t)
}

func libTopicItemsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := itemQueryFromRequest(r)
	q.TopicID = pathID(r, "id")
	if q.Sort == "" {
		q.Sort = "episode"
	}
	page, err := l.QueryItems(r.Context(), l.viewer(r), q)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, page)
}

func libTagCreateHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var in TagInput
	if err := decodeBody(r, &in); err != nil {
		libFail(w, err)
		return
	}
	id, err := l.CreateTag(r.Context(), in)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"id": id})
}

func libTagEditHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var err error
	if r.Method == http.MethodDelete {
		err = l.DeleteTag(r.Context(), id)
	} else {
		var in TagInput
		if err = decodeBody(r, &in); err == nil {
			err = l.UpdateTag(r.Context(), id, in)
		}
	}
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libTagMergeHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Into int64 `json:"into"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	if err := l.MergeTags(r.Context(), pathID(r, "id"), body.Into); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libTopicCreateHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var in TopicInput
	if err := decodeBody(r, &in); err != nil {
		libFail(w, err)
		return
	}
	id, err := l.CreateTopic(r.Context(), in)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"id": id})
}

func libTopicEditHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	var err error
	if r.Method == http.MethodDelete {
		err = l.DeleteTopic(r.Context(), id)
	} else {
		var in TopicInput
		if err = decodeBody(r, &in); err == nil {
			err = l.UpdateTopic(r.Context(), id, in)
		}
	}
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libTopicMergeHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Into int64 `json:"into"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	if err := l.MergeTopics(r.Context(), pathID(r, "id"), body.Into); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libTopicDissolveHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	b, err := l.DissolveTopic(r.Context(), pathID(r, "id"), actorOf(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, b)
}

func libTopicBlocksHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.ListTopicBlocks(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"blocks": list})
}

func libTopicRestoreHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind string `json:"kind"`
		Slug string `json:"slug"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	id, err := l.RestoreTopic(r.Context(), body.Kind, body.Slug)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"id": id})
}

func libTopicFlagsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.TopicFlags(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"flags": list})
}

func libTopicFlagDismissHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if err := l.DismissTopicFlags(r.Context(), pathID(r, "id"), actorOf(r)); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

// libTopicSuggestHandler: POST /topics/{id}/suggest {action: merge|invalid, into}
func libTopicSuggestHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
		Into   int64  `json:"into"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	if err := l.SuggestTopic(r.Context(), l.viewer(r), pathID(r, "id"), actorOf(r), body.Action, body.Into); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

// libSearchHandler: GET /catalog/search?q=...&group=series agrupa por obra;
// sem group devolve a página de itens como /catalog/items.
func libSearchHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := itemQueryFromRequest(r)
	if r.URL.Query().Get("group") != "series" {
		libItemsHandler(l, w, r)
		return
	}
	res, err := l.SearchGrouped(r.Context(), l.viewer(r), q)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

func libHomeHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	home, err := l.Home(r.Context(), l.viewer(r))
	if err != nil {
		libFail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=30")
	libJSON(w, home)
}

func libFeedHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	seed, _ := strconv.ParseInt(q.Get("seed"), 10, 64)
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	page, err := l.Feed(r.Context(), l.viewer(r), q.Get("scope"), seed, offset, limit)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, page)
}

func libMangasHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	works, err := l.Mangas(r.Context(), l.viewer(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"works": works})
}

// ---------- tópicos duplicados ----------

func libTopicDupesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	groups, err := l.TopicDupGroups(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"groups": groups, "threshold": topicDupReview})
}

func libTopicDupesScanHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	rep, err := l.DetectTopicDuplicates(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, rep)
}

func libTopicDupesAcceptHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		CanonicalID int64   `json:"canonicalId"`
		MemberIDs   []int64 `json:"memberIds"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	if body.CanonicalID <= 0 || len(body.MemberIDs) == 0 {
		libFail(w, errInvalid)
		return
	}
	res, err := l.AcceptTopicMerge(r.Context(), body.CanonicalID, body.MemberIDs, actorOf(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

func libTopicDupesRejectHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	n, err := l.RejectTopicMerge(r.Context(), body.IDs, actorOf(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true, "rejected": n})
}

func libTopicMergesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.ListTopicMerges(r.Context(), qInt(r, "limit"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"merges": list})
}

func libTopicMergeUndoHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	n, err := l.UndoTopicMerge(r.Context(), mux.Vars(r)["batch"])
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true, "restored": n})
}

// ---------- usuário ----------

func libFavoritesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := itemQueryFromRequest(r)
	q.Favorites = true
	if q.Sort == "" {
		q.Sort = "title"
	}
	page, err := l.QueryItems(r.Context(), l.viewer(r), q)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, page)
}

func libFavoriteSetHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetItem(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	on := r.Method == http.MethodPut
	if err := l.SetFavorite(r.Context(), v.UserID, id, on); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"favorite": on})
}

func libFavoriteTopicSetHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetTopic(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	on := r.Method == http.MethodPut
	if err := l.SetFavoriteTopic(r.Context(), v.UserID, id, on); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"favorite": on})
}

func libHiddenListHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := itemQueryFromRequest(r)
	q.OnlyHidden = true
	page, err := l.QueryItems(r.Context(), l.viewer(r), q)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, page)
}

func libHiddenSetHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetItem(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	on := r.Method == http.MethodPut
	if err := l.SetHidden(r.Context(), v.UserID, id, on); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"hidden": on})
}

func libProgressHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetItem(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	if r.Method == http.MethodDelete {
		if err := l.ClearProgress(r.Context(), v.UserID, id); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ok": true})
		return
	}
	var in ProgressInput
	if err := decodeBody(r, &in); err != nil {
		libFail(w, err)
		return
	}
	p, err := l.SaveProgress(r.Context(), v.UserID, id, in)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, p)
}

// /me/progress?path=... — usado pela ponte do player, que só conhece o caminho.
// GET devolve {mediaId, progress, prefs, tracks}; PUT grava o progresso (e as faixas escolhidas).
func libProgressByPathHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	rel, ok := l.resolveVisible(v, decodeMediaPath(r.URL.Query().Get("path")))
	if !ok || rel == "" {
		libError(w, http.StatusNotFound, "não encontrado")
		return
	}
	id, err := l.EnsureMediaForPath(r.Context(), rel)
	if err != nil {
		libFail(w, err)
		return
	}
	if r.Method == http.MethodPut {
		var in ProgressInput
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		p, err := l.SaveProgress(r.Context(), v.UserID, id, in)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"mediaId": id, "progress": p})
		return
	}
	it, err := l.GetItem(r.Context(), v, id)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"mediaId": id, "progress": it.Progress, "favorite": it.Favorite, "prefs": l.GetPrefs(r.Context(), v.UserID),
		"tracks": l.TrackPrefs(r.Context(), v.UserID, id, rel)})
}

func libContinueHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete { // "limpar tudo"
		n, err := l.DismissContinue(r.Context(), l.viewer(r).UserID, 0)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ok": true, "dismissed": n})
		return
	}
	items, err := l.ContinueWatching(r.Context(), l.viewer(r), qInt(r, "limit"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"items": items})
}

// libContinueDismissHandler: DELETE /me/continue/{id} tira o item do
// "continuar assistindo" (o progresso fica; volta com progresso novo).
func libContinueDismissHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	if _, err := l.GetItem(r.Context(), v, id); err != nil {
		libFail(w, err)
		return
	}
	if _, err := l.DismissContinue(r.Context(), v.UserID, id); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libPrefsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	uid := r.Header.Get("X-User-ID")
	if r.Method == http.MethodPut {
		var in UserPrefs
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		if in.RestrictedMode != nil {
			// só o modo restrito (o botão da barra lateral manda só ele)
			if err := l.SetRestrictedMode(r.Context(), uid, *in.RestrictedMode); err != nil {
				libFail(w, err)
				return
			}
		} else if err := l.SavePrefs(r.Context(), uid, in.AutoResume, in.Autoplay); err != nil {
			libFail(w, err)
			return
		}
	}
	libJSON(w, l.GetPrefs(r.Context(), uid))
}

// ---------- playlists ----------

func libPlaylistsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	if r.Method == http.MethodPost {
		var in PlaylistInput
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		p, err := l.CreatePlaylist(r.Context(), v, in)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, p)
		return
	}
	list, err := l.ListPlaylists(r.Context(), v)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"playlists": list})
}

func libPlaylistHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	id := pathID(r, "id")
	switch r.Method {
	case http.MethodDelete:
		if err := l.DeletePlaylist(r.Context(), v, id); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ok": true})
	case http.MethodPatch:
		var in PlaylistInput
		if err := decodeBody(r, &in); err != nil {
			libFail(w, err)
			return
		}
		p, err := l.UpdatePlaylist(r.Context(), v, id, in)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, p)
	default:
		p, err := l.GetPlaylist(r.Context(), v, id)
		if err != nil {
			libFail(w, err)
			return
		}
		items, err := l.PlaylistItems(r.Context(), v, id)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"playlist": p, "items": items})
	}
}

func libPlaylistAddHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	v := l.viewer(r)
	var body struct {
		IDs   []int64  `json:"ids"`
		Paths []string `json:"paths"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	ids := []int64{}
	for _, id := range body.IDs {
		if _, err := l.GetItem(r.Context(), v, id); err == nil {
			ids = append(ids, id)
		}
	}
	for _, p := range body.Paths {
		rel, ok := l.resolveVisible(v, p)
		if !ok || rel == "" {
			continue
		}
		if id, err := l.EnsureMediaForPath(r.Context(), rel); err == nil {
			ids = append(ids, id)
		}
	}
	n, err := l.AddPlaylistItems(r.Context(), v, pathID(r, "id"), ids)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"added": n})
}

func libPlaylistRemoveHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if err := l.RemovePlaylistItem(r.Context(), l.viewer(r), pathID(r, "id"), pathID(r, "mediaId")); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libPlaylistMoveHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		MediaID int64 `json:"mediaId"`
		AfterID int64 `json:"afterId"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	if err := l.MovePlaylistItem(r.Context(), l.viewer(r), pathID(r, "id"), body.MediaID, body.AfterID); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

// ---------- admin ----------

type LibraryStats struct {
	Media       int            `json:"media"`
	Missing     int            `json:"missing"`
	ByKind      map[string]int `json:"byKind"`
	ByMaturity  map[string]int `json:"byMaturity"`
	ByRoot      map[string]int `json:"byRoot"`
	Tags        int            `json:"tags"`
	Topics      int            `json:"topics"`
	Playlists   int            `json:"playlists"`
	Pending     int            `json:"pendingSuggestions"`
	ScanRunning bool           `json:"scanRunning"`
	LastScan    *ScanReport    `json:"lastScan,omitempty"`
	DBPath      string         `json:"dbPath"`
}

func (l *Library) Stats(ctx context.Context) LibraryStats {
	s := LibraryStats{ByKind: map[string]int{}, ByMaturity: map[string]int{}, ByRoot: map[string]int{}, DBPath: l.path}
	l.r.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(missing),0) FROM media`).Scan(&s.Media, &s.Missing)
	group := func(q string, dst map[string]int, mat bool) {
		rows, err := l.r.QueryContext(ctx, q)
		if err != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			var n int
			rows.Scan(&k, &n)
			if mat {
				m, _ := strconv.Atoi(k)
				k = maturityName(m)
			}
			dst[k] = n
		}
	}
	group(`SELECT kind, COUNT(*) FROM media WHERE missing=0 GROUP BY kind`, s.ByKind, false)
	group(`SELECT CAST(maturity AS TEXT), COUNT(*) FROM media WHERE missing=0 GROUP BY maturity`, s.ByMaturity, true)
	group(`SELECT root, COUNT(*) FROM media WHERE missing=0 GROUP BY root`, s.ByRoot, false)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM tags`).Scan(&s.Tags)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM topics`).Scan(&s.Topics)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlists`).Scan(&s.Playlists)
	l.r.QueryRowContext(ctx, `SELECT COUNT(*) FROM suggestions WHERE status='pending'`).Scan(&s.Pending)
	s.ScanRunning, s.LastScan = l.ScanStatus()
	return s
}

func libStatsHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	libJSON(w, l.Stats(r.Context()))
}

func libPoliciesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPut {
		var body struct {
			Prefix   string  `json:"prefix"`
			Maturity *string `json:"maturity"` // null/"" = herda
			Hidden   bool    `json:"hidden"`
			Note     string  `json:"note"`
		}
		if err := decodeBody(r, &body); err != nil {
			libFail(w, err)
			return
		}
		var m *int
		if body.Maturity != nil && *body.Maturity != "" {
			mv, ok := parseMaturity(*body.Maturity)
			if !ok {
				libFail(w, errInvalid)
				return
			}
			m = &mv
		}
		if err := l.SetPolicy(r.Context(), body.Prefix, m, body.Hidden, body.Note); err != nil {
			libFail(w, err)
			return
		}
	}
	list := l.listPolicies()
	out := make([]folderPolicyJSON, 0, len(list))
	for _, p := range list {
		out = append(out, p.toJSON())
	}
	libJSON(w, map[string]any{"policies": out})
}

type libUserRow struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Role        string `json:"role"`
	MaxMaturity string `json:"maxMaturity"`
}

func libUsersHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	usersMutex.RLock()
	list := make([]libUserRow, 0, len(users))
	for _, u := range users {
		if !u.Active {
			continue
		}
		list = append(list, libUserRow{ID: u.ID, Username: u.Username, Role: u.Role})
	}
	usersMutex.RUnlock()
	for i := range list {
		if list[i].Role == "admin" {
			list[i].MaxMaturity = "explicito"
		} else {
			list[i].MaxMaturity = maturityName(func() int { m, _ := l.userMaturity(list[i].ID); return m }())
		}
	}
	libJSON(w, map[string]any{"users": list})
}

func libUserSetHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if getUserByID(id) == nil {
		libFail(w, errNotFound)
		return
	}
	var body struct {
		MaxMaturity string `json:"maxMaturity"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	m, ok := parseMaturity(body.MaxMaturity)
	if !ok || body.MaxMaturity == "" {
		libFail(w, errInvalid)
		return
	}
	if err := l.SetMaxMaturity(r.Context(), id, m); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libScanHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		go l.scanAndProbe()
		time.Sleep(50 * time.Millisecond)
	}
	running, last := l.ScanStatus()
	libJSON(w, map[string]any{"running": running, "last": last})
}

func libRulesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var rule Rule
		if err := decodeBody(r, &rule); err != nil {
			libFail(w, err)
			return
		}
		rule.ID = 0
		if err := l.SaveRule(r.Context(), &rule); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, rule)
		return
	}
	rules, err := l.ListRules(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"rules": rules})
}

func libRuleHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	id := pathID(r, "id")
	if r.Method == http.MethodDelete {
		if err := l.DeleteRule(r.Context(), id); err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"ok": true})
		return
	}
	var rule Rule
	if err := decodeBody(r, &rule); err != nil {
		libFail(w, err)
		return
	}
	rule.ID = id
	if err := l.SaveRule(r.Context(), &rule); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, rule)
}

func libRulesApplyHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		RuleID int64 `json:"ruleId"`
		DryRun bool  `json:"dryRun"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	res, err := l.ApplyRules(r.Context(), body.RuleID, actorOf(r), body.DryRun)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

func libBatchHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ops    []BatchOp `json:"ops"`
		DryRun bool      `json:"dryRun"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	res, err := l.ApplyBatch(r.Context(), body.Ops, actorOf(r), body.DryRun)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

type ChangeBatch struct {
	BatchID   string         `json:"batchId"`
	Actor     string         `json:"actor"`
	Reason    string         `json:"reason"`
	Count     int            `json:"count"`
	Undone    bool           `json:"undone"`
	CreatedAt int64          `json:"createdAt"`
	ByOp      map[string]int `json:"byOp"`
}

func (l *Library) ListChanges(ctx context.Context, limit int) ([]ChangeBatch, error) {
	limit = clampLimit(limit, 50, 500)
	rows, err := l.r.QueryContext(ctx, `SELECT batch_id, actor, COALESCE(reason,''), COUNT(*), MIN(undone), MIN(created_at),
		group_concat(op, ',') FROM changes GROUP BY batch_id ORDER BY MAX(id) DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChangeBatch{}
	for rows.Next() {
		var c ChangeBatch
		var undone int
		var ops string
		if err := rows.Scan(&c.BatchID, &c.Actor, &c.Reason, &c.Count, &undone, &c.CreatedAt, &ops); err != nil {
			return nil, err
		}
		c.Undone = undone == 1
		c.ByOp = map[string]int{}
		for _, o := range strings.Split(ops, ",") {
			c.ByOp[o]++
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func libChangesHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.ListChanges(r.Context(), qInt(r, "limit"))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"batches": list})
}

func libUndoHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	res, err := l.Undo(r.Context(), mux.Vars(r)["batch"], actorOf(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

func libTokensHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var body struct {
			Name   string `json:"name"`
			UserID string `json:"userId"`
		}
		if err := decodeBody(r, &body); err != nil {
			libFail(w, err)
			return
		}
		if body.UserID == "" {
			body.UserID = r.Header.Get("X-User-ID")
		}
		raw, tok, err := l.CreateAPIToken(r.Context(), body.UserID, body.Name)
		if err != nil {
			libFail(w, err)
			return
		}
		libJSON(w, map[string]any{"token": raw, "info": tok})
		return
	}
	list, err := l.ListAPITokens(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"tokens": list})
}

func libTokenRevokeHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if err := l.RevokeAPIToken(r.Context(), pathID(r, "id")); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

// libExportHandler: JSON Lines com o catálogo curado (tags, tópicos, maturidade).
func libExportHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="drivelocal-catalog.jsonl"`)
	admin := Viewer{UserID: r.Header.Get("X-User-ID"), IsAdmin: true, MaxMaturity: MaturityExplicito}
	enc := json.NewEncoder(w)
	for off := 0; ; off += 500 {
		page, err := l.QueryItems(r.Context(), admin, ItemQuery{Limit: 500, Offset: off, Sort: "path"})
		if err != nil || len(page.Items) == 0 {
			return
		}
		for _, it := range page.Items {
			enc.Encode(map[string]any{"path": it.Path, "title": it.Title, "maturity": it.Maturity, "category": it.Category, "tags": it.Tags, "topics": it.Topics})
		}
		if off+500 >= page.Total {
			return
		}
	}
}

func libBackupHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	if err := l.Backup(); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libReviewListHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	minConf, _ := strconv.ParseFloat(q.Get("minConfidence"), 64)
	list, total, err := l.ListSuggestions(r.Context(), ReviewQuery{Status: q.Get("status"), Kind: q.Get("kind"), PathPrefix: q.Get("pathPrefix"), MinConf: minConf, Limit: qInt(r, "limit"), Offset: qInt(r, "offset")})
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"suggestions": list, "total": total})
}

func libReviewHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs    []int64 `json:"ids"`
		Accept bool    `json:"accept"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	res, err := l.ReviewSuggestions(r.Context(), body.IDs, body.Accept, actorOf(r))
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, res)
}

func libAIStatusHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	libJSON(w, l.AIStatus(r.Context()))
}

func libAIEnqueueHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var req EnqueueRequest
	if err := decodeBody(r, &req); err != nil {
		libFail(w, err)
		return
	}
	n, err := l.AIEnqueue(r.Context(), req)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"queued": n})
}

func libAIRunHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Max int `json:"max"`
	}
	decodeBody(r, &body)
	if err := AIRunNow(clampLimit(body.Max, 20, 1000)); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, l.AIStatus(r.Context()))
}

func libAIPauseHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paused bool `json:"paused"`
	}
	if err := decodeBody(r, &body); err != nil {
		libFail(w, err)
		return
	}
	AISetPaused(body.Paused)
	libJSON(w, l.AIStatus(r.Context()))
}

func libAIClearHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	n, err := l.AIClearQueue(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"removed": n})
}

func libGoldenListHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	list, err := l.ListGolden(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"items": list})
}

func libGoldenSampleHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var body struct {
		N int `json:"n"`
	}
	decodeBody(r, &body)
	n, err := l.GoldenSample(r.Context(), body.N)
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"added": n})
}

func libGoldenLabelHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	var lab GoldenLabels
	if err := decodeBody(r, &lab); err != nil {
		libFail(w, err)
		return
	}
	if err := l.LabelGolden(r.Context(), pathID(r, "id"), lab, actorOf(r)); err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, map[string]any{"ok": true})
}

func libEvalHandler(l *Library, w http.ResponseWriter, r *http.Request) {
	rep, err := l.Eval(r.Context())
	if err != nil {
		libFail(w, err)
		return
	}
	libJSON(w, rep)
}

// ---------- controle de acesso nas rotas existentes ----------

// libraryAccessMiddleware aplica maturidade/ocultação às rotas antigas sem
// editar handlers.go: nega mídia que o usuário não pode ver, filtra a
// listagem de /api/files e bloqueia pastas com conteúdo proibido nas rotas
// que agregam o conteúdo da pasta (capa de pasta, download, zip). Admin passa
// direto. Se o banco da biblioteca falhou ao abrir, é fail-closed: não-admin
// recebe 503 nas rotas de mídia.
func libraryAccessMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("X-User-ID")
		l := getLibrary()
		if l == nil {
			if libraryFailed.Load() && uid != "" && isMediaRoute(r) && !isAdminUser(uid) {
				logWarn("Biblioteca indisponível: negando %s %s para usuário não-admin (fail-closed)", r.Method, r.URL.Path)
				libError(w, http.StatusServiceUnavailable, "biblioteca indisponível")
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if uid == "" {
			next.ServeHTTP(w, r)
			return
		}
		v := l.viewerFor(uid)
		if v.IsAdmin {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/files") {
			if !l.canSeePath(v, decodeMediaPath(r.URL.Query().Get("path"))) {
				libError(w, http.StatusNotFound, "Directory not found")
				return
			}
			filterFilesResponse(l, v, w, r, next)
			return
		}
		// Toda rota com {path} serve mídia (stream, download, media-info,
		// subtitle, media-font, keyframe, thumbnail e as futuras): mesmo gate,
		// com o mesmo valor que o handler vai resolver. Pasta com qualquer item
		// proibido também é negada (a capa de pasta e downloads de pasta
		// agregariam esses arquivos).
		if raw, ok := mux.Vars(r)["path"]; ok {
			rel, ok := l.resolveVisible(v, decodeMediaPath(raw))
			if !ok || (isMediaDir(rel) && l.dirHasBlocked(v, rel)) {
				libError(w, http.StatusNotFound, "File not found")
				return
			}
		}
		if (p == "/api/download-multiple" || p == "/api/thumbnails") && r.Method == http.MethodPost {
			body, err := readLimited(r, 1<<20)
			if err != nil {
				libError(w, http.StatusBadRequest, "Invalid request")
				return
			}
			// Mesmo struct (e mesma regra case-insensitive/última-chave-vence)
			// que os handlers usam; reserializado só com as chaves canônicas,
			// então nenhuma variante ("Files", chave duplicada) escapa do filtro.
			var req struct {
				Files []string `json:"files,omitempty"`
				Paths []string `json:"paths,omitempty"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				libError(w, http.StatusBadRequest, "Invalid request")
				return
			}
			for _, list := range []*[]string{&req.Files, &req.Paths} {
				kept := make([]string, 0, len(*list))
				for _, f := range *list {
					rel, ok := l.resolveVisible(v, decodeMediaPath(f))
					if !ok {
						libError(w, http.StatusNotFound, "File not found")
						return
					}
					// pasta com conteúdo proibido some da lista: nunca entra no zip/capa
					if isMediaDir(rel) && l.dirHasBlocked(v, rel) {
						continue
					}
					kept = append(kept, f)
				}
				*list = kept
			}
			body, _ = json.Marshal(req)
			r.Body = nopCloser{bytes.NewReader(body)}
			r.ContentLength = int64(len(body))
		}
		next.ServeHTTP(w, r)
	})
}

// isMediaRoute: rotas que expõem nomes ou conteúdo do acervo.
func isMediaRoute(r *http.Request) bool {
	p := r.URL.Path
	if _, ok := mux.Vars(r)["path"]; ok {
		return true
	}
	return strings.HasPrefix(p, "/api/files") || p == "/api/download-multiple" || p == "/api/thumbnails"
}

func isAdminUser(uid string) bool {
	u := getUserByID(uid)
	return u != nil && u.Role == "admin"
}

// isMediaDir diz se o caminho relativo canônico é uma pasta no disco.
func isMediaDir(rel string) bool {
	info, err := os.Stat(filepath.Join(filepath.Clean(MEDIA_PATH), filepath.FromSlash(rel)))
	return err == nil && info.IsDir()
}

type nopCloser struct{ *bytes.Reader }

func (nopCloser) Close() error { return nil }

func readLimited(r *http.Request, n int64) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(http.MaxBytesReader(nil, r.Body, n))
	return buf.Bytes(), err
}

type captureWriter struct {
	h      http.Header
	status int
	buf    bytes.Buffer
}

func (c *captureWriter) Header() http.Header         { return c.h }
func (c *captureWriter) Write(b []byte) (int, error) { return c.buf.Write(b) }
func (c *captureWriter) WriteHeader(s int)           { c.status = s }

// filterFilesResponse roda o filesHandler num buffer e tira os itens proibidos.
func filterFilesResponse(l *Library, v Viewer, w http.ResponseWriter, r *http.Request, next http.Handler) {
	cw := &captureWriter{h: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(cw, r)
	for k, vals := range cw.h {
		for _, val := range vals {
			w.Header().Add(k, val)
		}
	}
	if cw.status != http.StatusOK {
		w.WriteHeader(cw.status)
		w.Write(cw.buf.Bytes())
		return
	}
	// Fail-closed: resposta que não dá para filtrar não é repassada.
	w.Header().Del("Content-Length")
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(cw.buf.Bytes(), &resp); err != nil {
		libError(w, http.StatusInternalServerError, "listagem indisponível")
		return
	}
	var items []map[string]any
	if raw, ok := resp["items"]; ok {
		if err := json.Unmarshal(raw, &items); err != nil {
			libError(w, http.StatusInternalServerError, "listagem indisponível")
			return
		}
	}
	kept := make([]map[string]any, 0, len(items))
	for _, it := range items {
		p, _ := it["path"].(string)
		if p != "" && l.canSeePath(v, filepath.ToSlash(p)) {
			kept = append(kept, it)
		}
	}
	resp["items"], _ = json.Marshal(kept)
	out, _ := json.Marshal(resp)
	w.Write(out)
}
