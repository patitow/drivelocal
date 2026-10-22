package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"strings"
)

// ---------- favoritos, ocultos, progresso, preferências ----------

func (l *Library) SetFavorite(ctx context.Context, userID string, mediaID int64, on bool) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		if on {
			_, err := t.Exec(`INSERT OR IGNORE INTO favorites(user_id, media_id, created_at) VALUES (?,?,?)`, userID, mediaID, nowUnix())
			return err
		}
		_, err := t.Exec(`DELETE FROM favorites WHERE user_id=? AND media_id=?`, userID, mediaID)
		return err
	})
}

func (l *Library) SetFavoriteTopic(ctx context.Context, userID string, topicID int64, on bool) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		if on {
			_, err := t.Exec(`INSERT OR IGNORE INTO favorite_topics(user_id, topic_id, created_at) VALUES (?,?,?)`, userID, topicID, nowUnix())
			return err
		}
		_, err := t.Exec(`DELETE FROM favorite_topics WHERE user_id=? AND topic_id=?`, userID, topicID)
		return err
	})
}

func (l *Library) SetHidden(ctx context.Context, userID string, mediaID int64, on bool) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		if on {
			_, err := t.Exec(`INSERT OR IGNORE INTO user_hidden(user_id, media_id, created_at) VALUES (?,?,?)`, userID, mediaID, nowUnix())
			return err
		}
		_, err := t.Exec(`DELETE FROM user_hidden WHERE user_id=? AND media_id=?`, userID, mediaID)
		return err
	})
}

type ProgressInput struct {
	PositionS     float64 `json:"positionS"`
	DurationS     float64 `json:"durationS"`
	Completed     *bool   `json:"completed,omitempty"`
	AudioTrack    string  `json:"audioTrack,omitempty"`
	SubtitleTrack string  `json:"subtitleTrack,omitempty"`
}

// Limiares de "continuar assistindo".
const (
	progressStartFrac = 0.03
	progressDoneFrac  = 0.92
)

func (l *Library) SaveProgress(ctx context.Context, userID string, mediaID int64, in ProgressInput) (*ProgressDTO, error) {
	if in.PositionS < 0 {
		in.PositionS = 0
	}
	completed := false
	if in.Completed != nil {
		completed = *in.Completed
	} else if in.DurationS > 0 && in.PositionS/in.DurationS >= progressDoneFrac {
		completed = true
	}
	now := nowUnix()
	err := l.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.Exec(`INSERT INTO watch_progress(user_id, media_id, position_s, duration_s, completed, audio_track, subtitle_track, updated_at)
			VALUES (?,?,?,?,?,NULLIF(?,''),NULLIF(?,''),?)
			ON CONFLICT(user_id, media_id) DO UPDATE SET position_s=excluded.position_s,
			  duration_s=CASE WHEN excluded.duration_s > 0 THEN excluded.duration_s ELSE watch_progress.duration_s END,
			  completed=excluded.completed,
			  audio_track=COALESCE(excluded.audio_track, watch_progress.audio_track),
			  subtitle_track=COALESCE(excluded.subtitle_track, watch_progress.subtitle_track),
			  updated_at=excluded.updated_at`,
			userID, mediaID, in.PositionS, in.DurationS, boolInt(completed), in.AudioTrack, in.SubtitleTrack, now); err != nil {
			return err
		}
		if in.DurationS > 0 {
			if _, err := t.Exec(`UPDATE media SET duration_s=? WHERE id=? AND (duration_s IS NULL OR abs(duration_s - ?) > 1)`, in.DurationS, mediaID, in.DurationS); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ProgressDTO{PositionS: in.PositionS, DurationS: in.DurationS, Completed: completed, AudioTrack: in.AudioTrack, SubtitleTrack: in.SubtitleTrack, UpdatedAt: now}, nil
}

// TrackPrefs: a última faixa de áudio e de legenda que o usuário usou na mesma série (tópico)
// ou na mesma pasta, para o próximo episódio já abrir com a dublagem/legenda escolhida.
type TrackPrefs struct {
	Audio string `json:"audio,omitempty"`
	Sub   string `json:"sub,omitempty"`
}

func (l *Library) TrackPrefs(ctx context.Context, userID string, mediaID int64, rel string) TrackPrefs {
	dir := path.Dir(rel) + "/"
	rows, err := l.r.QueryContext(ctx, `SELECT COALESCE(p.audio_track,''), COALESCE(p.subtitle_track,'')
		FROM watch_progress p JOIN media m ON m.id = p.media_id
		WHERE p.user_id = ? AND (p.audio_track IS NOT NULL OR p.subtitle_track IS NOT NULL) AND (
		  p.media_id = ?
		  OR p.media_id IN (SELECT o.media_id FROM topic_items ti JOIN topics t ON t.id = ti.topic_id AND t.kind = 'series'
		                    JOIN topic_items o ON o.topic_id = ti.topic_id WHERE ti.media_id = ?)
		  OR (substr(m.path, 1, length(?)) = ? AND instr(substr(m.path, length(?) + 1), '/') = 0))
		ORDER BY p.updated_at DESC LIMIT 50`,
		userID, mediaID, mediaID, dir, dir, dir)
	var out TrackPrefs
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() && (out.Audio == "" || out.Sub == "") {
		var a, s string
		if rows.Scan(&a, &s) != nil {
			break
		}
		if out.Audio == "" {
			out.Audio = a
		}
		if out.Sub == "" {
			out.Sub = s
		}
	}
	return out
}

func (l *Library) ClearProgress(ctx context.Context, userID string, mediaID int64) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`DELETE FROM watch_progress WHERE user_id=? AND media_id=?`, userID, mediaID)
		return err
	})
}

// ContinueWatching: vídeos começados e não terminados, mais recentes primeiro.
func (l *Library) ContinueWatching(ctx context.Context, v Viewer, limit int) ([]ItemDTO, error) {
	limit = clampLimit(limit, 20, 100)
	vis, args := v.visibleSQL(false)
	q := `SELECT ` + itemCols + ` FROM watch_progress wp JOIN media m ON m.id = wp.media_id
		WHERE wp.user_id = ? AND wp.completed = 0 AND m.kind = 'video'
		  AND wp.position_s >= 10
		  AND (wp.duration_s <= 0 OR (wp.position_s >= wp.duration_s * ? AND wp.position_s < wp.duration_s * ?))
		  AND NOT EXISTS (SELECT 1 FROM continue_dismissed cd WHERE cd.user_id = wp.user_id AND cd.media_id = wp.media_id AND cd.dismissed_at >= wp.updated_at)
		  AND ` + vis + ` ORDER BY wp.updated_at DESC LIMIT ?`
	all := append([]any{v.UserID, progressStartFrac, progressDoneFrac}, args...)
	all = append(all, limit)
	rows, err := l.r.QueryContext(ctx, q, all...)
	if err != nil {
		return nil, err
	}
	items := []ItemDTO{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	rows.Close()
	return items, l.hydrate(ctx, v, items)
}

// DismissContinue tira o item do "continuar assistindo" (hidden_from_continue)
// sem apagar progresso, favorito ou tags: ele volta quando houver progresso
// novo (updated_at depois do dispensar). mediaID 0 = todos os itens atuais.
func (l *Library) DismissContinue(ctx context.Context, userID string, mediaID int64) (int, error) {
	now := nowUnix()
	var res sql.Result
	var err error
	if mediaID > 0 {
		res, err = l.w.ExecContext(ctx, `INSERT INTO continue_dismissed(user_id, media_id, dismissed_at) VALUES (?,?,?)
			ON CONFLICT(user_id, media_id) DO UPDATE SET dismissed_at = excluded.dismissed_at`, userID, mediaID, now)
	} else {
		res, err = l.w.ExecContext(ctx, `INSERT INTO continue_dismissed(user_id, media_id, dismissed_at)
			SELECT user_id, media_id, ? FROM watch_progress WHERE user_id = ? AND completed = 0 AND true
			ON CONFLICT(user_id, media_id) DO UPDATE SET dismissed_at = excluded.dismissed_at`, now, userID)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	l.invalidateHome(userID)
	return int(n), nil
}

type UserPrefs struct {
	MaxMaturity string `json:"maxMaturity"`
	AutoResume  bool   `json:"autoResume"`
	Autoplay    bool   `json:"autoplay"`
	// RestrictedMode: o usuário quer ver conteúdo explícito; só vale se RestrictedAllowed.
	RestrictedMode    *bool `json:"restrictedMode,omitempty"`
	RestrictedAllowed bool  `json:"restrictedAllowed"`
}

func (l *Library) GetPrefs(ctx context.Context, userID string) UserPrefs {
	p := UserPrefs{MaxMaturity: "geral", AutoResume: true, Autoplay: true}
	var m, ar, ap, restricted int
	restricted = 1
	err := l.r.QueryRowContext(ctx, `SELECT max_maturity, auto_resume, autoplay, restricted_mode FROM user_prefs WHERE user_id=?`, userID).Scan(&m, &ar, &ap, &restricted)
	if err == nil {
		p.MaxMaturity = maturityName(m)
		p.AutoResume = ar == 1
		p.Autoplay = ap == 1
	}
	if u := getUserByID(userID); u != nil && u.Role == "admin" {
		p.MaxMaturity = "explicito"
	}
	p.RestrictedAllowed = p.MaxMaturity == "explicito"
	on := restricted == 1 && p.RestrictedAllowed
	p.RestrictedMode = &on
	return p
}

// SetRestrictedMode liga/desliga o modo restrito do próprio usuário.
func (l *Library) SetRestrictedMode(ctx context.Context, userID string, on bool) error {
	err := l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`INSERT INTO user_prefs(user_id, max_maturity, restricted_mode, updated_at) VALUES (?,?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET restricted_mode=excluded.restricted_mode, updated_at=excluded.updated_at`,
			userID, MaturityGeral, boolInt(on), nowUnix())
		return err
	})
	if err == nil {
		l.invalidateHome(userID)
	}
	return err
}

// SavePrefs grava as preferências do próprio usuário (não mexe na maturidade).
func (l *Library) SavePrefs(ctx context.Context, userID string, autoResume, autoplay bool) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`INSERT INTO user_prefs(user_id, max_maturity, auto_resume, autoplay, updated_at) VALUES (?,?,?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET auto_resume=excluded.auto_resume, autoplay=excluded.autoplay, updated_at=excluded.updated_at`,
			userID, MaturityGeral, boolInt(autoResume), boolInt(autoplay), nowUnix())
		return err
	})
}

// SetMaxMaturity: só admin chama.
func (l *Library) SetMaxMaturity(ctx context.Context, userID string, m int) error {
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`INSERT INTO user_prefs(user_id, max_maturity, updated_at) VALUES (?,?,?)
			ON CONFLICT(user_id) DO UPDATE SET max_maturity=excluded.max_maturity, updated_at=excluded.updated_at`, userID, m, nowUnix())
		return err
	})
}

// ---------- playlists ----------

type Playlist struct {
	ID          int64      `json:"id"`
	OwnerID     string     `json:"ownerId"`
	OwnerName   string     `json:"ownerName,omitempty"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Visibility  string     `json:"visibility"`
	Smart       *ItemQuery `json:"smart,omitempty"`
	ItemCount   int        `json:"itemCount"`
	DurationS   float64    `json:"durationS"`
	CanEdit     bool       `json:"canEdit"`
	CreatedAt   int64      `json:"createdAt"`
	UpdatedAt   int64      `json:"updatedAt"`
}

func validVisibility(s string) bool { return s == "private" || s == "shared" || s == "global" }

func (l *Library) playlistVisibleSQL(v Viewer) (string, []any) {
	if v.IsAdmin {
		return "1=1", nil
	}
	return "(p.owner_id = ? OR p.visibility IN ('shared','global'))", []any{v.UserID}
}

func (l *Library) ListPlaylists(ctx context.Context, v Viewer) ([]Playlist, error) {
	cond, args := l.playlistVisibleSQL(v)
	vis, vargs := v.visibleSQL(false)
	q := `SELECT p.id, p.owner_id, p.name, p.description, p.visibility, p.smart_query_json, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM playlist_items pi JOIN media m ON m.id = pi.media_id WHERE pi.playlist_id = p.id AND ` + vis + `),
		(SELECT COALESCE(SUM(m.duration_s),0) FROM playlist_items pi JOIN media m ON m.id = pi.media_id WHERE pi.playlist_id = p.id AND ` + vis + `)
		FROM playlists p WHERE ` + cond + ` ORDER BY p.updated_at DESC`
	all := append(append(append([]any{}, vargs...), vargs...), args...)
	rows, err := l.r.QueryContext(ctx, q, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Playlist{}
	for rows.Next() {
		p, err := scanPlaylist(rows, v)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPlaylist(sc interface{ Scan(...any) error }, v Viewer) (Playlist, error) {
	var p Playlist
	var smart sql.NullString
	if err := sc.Scan(&p.ID, &p.OwnerID, &p.Name, &p.Description, &p.Visibility, &smart, &p.CreatedAt, &p.UpdatedAt, &p.ItemCount, &p.DurationS); err != nil {
		return p, err
	}
	if smart.Valid && smart.String != "" {
		var q ItemQuery
		if json.Unmarshal([]byte(smart.String), &q) == nil {
			p.Smart = &q
		}
	}
	if u := getUserByID(p.OwnerID); u != nil {
		p.OwnerName = u.Username
	}
	p.CanEdit = v.IsAdmin || p.OwnerID == v.UserID
	return p, nil
}

func (l *Library) GetPlaylist(ctx context.Context, v Viewer, id int64) (*Playlist, error) {
	cond, args := l.playlistVisibleSQL(v)
	vis, vargs := v.visibleSQL(false)
	q := `SELECT p.id, p.owner_id, p.name, p.description, p.visibility, p.smart_query_json, p.created_at, p.updated_at,
		(SELECT COUNT(*) FROM playlist_items pi JOIN media m ON m.id = pi.media_id WHERE pi.playlist_id = p.id AND ` + vis + `),
		(SELECT COALESCE(SUM(m.duration_s),0) FROM playlist_items pi JOIN media m ON m.id = pi.media_id WHERE pi.playlist_id = p.id AND ` + vis + `)
		FROM playlists p WHERE p.id = ? AND ` + cond
	all := append(append(append(append([]any{}, vargs...), vargs...), id), args...)
	p, err := scanPlaylist(l.r.QueryRowContext(ctx, q, all...), v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

type PlaylistInput struct {
	Name        *string    `json:"name"`
	Description *string    `json:"description"`
	Visibility  *string    `json:"visibility"`
	Smart       *ItemQuery `json:"smart"`
	ClearSmart  bool       `json:"clearSmart"`
}

var errForbidden = errors.New("sem permissão")
var errInvalid = errors.New("dados inválidos")

func (l *Library) CreatePlaylist(ctx context.Context, v Viewer, in PlaylistInput) (*Playlist, error) {
	if in.Name == nil || strings.TrimSpace(*in.Name) == "" {
		return nil, errInvalid
	}
	vis := "private"
	if in.Visibility != nil {
		vis = *in.Visibility
	}
	if !validVisibility(vis) || (vis == "global" && !v.IsAdmin) {
		return nil, errInvalid
	}
	desc := ""
	if in.Description != nil {
		desc = *in.Description
	}
	var smart any
	if in.Smart != nil {
		b, _ := json.Marshal(in.Smart)
		smart = string(b)
	}
	var id int64
	now := nowUnix()
	err := l.tx(ctx, func(t *sql.Tx) error {
		res, err := t.Exec(`INSERT INTO playlists(owner_id, name, description, visibility, smart_query_json, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			v.UserID, strings.TrimSpace(*in.Name), desc, vis, smart, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return l.GetPlaylist(ctx, v, id)
}

func (l *Library) canEditPlaylist(ctx context.Context, v Viewer, id int64) error {
	var owner string
	if err := l.r.QueryRowContext(ctx, `SELECT owner_id FROM playlists WHERE id=?`, id).Scan(&owner); err != nil {
		return errNotFound
	}
	if !v.IsAdmin && owner != v.UserID {
		return errForbidden
	}
	return nil
}

func (l *Library) UpdatePlaylist(ctx context.Context, v Viewer, id int64, in PlaylistInput) (*Playlist, error) {
	if err := l.canEditPlaylist(ctx, v, id); err != nil {
		return nil, err
	}
	sets := []string{"updated_at=?"}
	args := []any{nowUnix()}
	if in.Name != nil {
		if strings.TrimSpace(*in.Name) == "" {
			return nil, errInvalid
		}
		sets = append(sets, "name=?")
		args = append(args, strings.TrimSpace(*in.Name))
	}
	if in.Description != nil {
		sets = append(sets, "description=?")
		args = append(args, *in.Description)
	}
	if in.Visibility != nil {
		if !validVisibility(*in.Visibility) || (*in.Visibility == "global" && !v.IsAdmin) {
			return nil, errInvalid
		}
		sets = append(sets, "visibility=?")
		args = append(args, *in.Visibility)
	}
	if in.Smart != nil {
		b, _ := json.Marshal(in.Smart)
		sets = append(sets, "smart_query_json=?")
		args = append(args, string(b))
	} else if in.ClearSmart {
		sets = append(sets, "smart_query_json=NULL")
	}
	err := l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`UPDATE playlists SET `+strings.Join(sets, ", ")+` WHERE id=?`, append(args, id)...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return l.GetPlaylist(ctx, v, id)
}

func (l *Library) DeletePlaylist(ctx context.Context, v Viewer, id int64) error {
	if err := l.canEditPlaylist(ctx, v, id); err != nil {
		return err
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		_, err := t.Exec(`DELETE FROM playlists WHERE id=?`, id)
		return err
	})
}

// PlaylistItems devolve os itens visíveis na ordem (manual) ou da consulta (inteligente).
func (l *Library) PlaylistItems(ctx context.Context, v Viewer, id int64) ([]ItemDTO, error) {
	p, err := l.GetPlaylist(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if p.Smart != nil {
		q := *p.Smart
		if q.Limit <= 0 {
			q.Limit = 200
		}
		page, err := l.QueryItems(ctx, v, q)
		if err != nil {
			return nil, err
		}
		return page.Items, nil
	}
	vis, args := v.visibleSQL(false)
	rows, err := l.r.QueryContext(ctx, `SELECT `+itemCols+` FROM playlist_items pi JOIN media m ON m.id = pi.media_id
		WHERE pi.playlist_id = ? AND `+vis+` ORDER BY pi.pos`, append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	items := []ItemDTO{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, it)
	}
	rows.Close()
	return items, l.hydrate(ctx, v, items)
}

// AddPlaylistItems acrescenta no fim (ignora repetidos).
func (l *Library) AddPlaylistItems(ctx context.Context, v Viewer, id int64, mediaIDs []int64) (int, error) {
	if err := l.canEditPlaylist(ctx, v, id); err != nil {
		return 0, err
	}
	added := 0
	err := l.tx(ctx, func(t *sql.Tx) error {
		var last sql.NullString
		t.QueryRow(`SELECT MAX(pos) FROM playlist_items WHERE playlist_id=?`, id).Scan(&last)
		cur := last.String
		for _, mid := range mediaIDs {
			next := keyBetween(cur, "")
			res, err := t.Exec(`INSERT OR IGNORE INTO playlist_items(playlist_id, media_id, pos, added_at) VALUES (?,?,?,?)`, id, mid, next, nowUnix())
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				added++
				cur = next
			}
		}
		_, err := t.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, nowUnix(), id)
		return err
	})
	return added, err
}

func (l *Library) RemovePlaylistItem(ctx context.Context, v Viewer, id, mediaID int64) error {
	if err := l.canEditPlaylist(ctx, v, id); err != nil {
		return err
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		if _, err := t.Exec(`DELETE FROM playlist_items WHERE playlist_id=? AND media_id=?`, id, mediaID); err != nil {
			return err
		}
		_, err := t.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, nowUnix(), id)
		return err
	})
}

// MovePlaylistItem coloca mediaID logo depois de afterID (0 = no início).
func (l *Library) MovePlaylistItem(ctx context.Context, v Viewer, id, mediaID, afterID int64) error {
	if err := l.canEditPlaylist(ctx, v, id); err != nil {
		return err
	}
	return l.tx(ctx, func(t *sql.Tx) error {
		var exists int
		if err := t.QueryRow(`SELECT COUNT(*) FROM playlist_items WHERE playlist_id=? AND media_id=?`, id, mediaID).Scan(&exists); err != nil || exists == 0 {
			return errNotFound
		}
		var lo, hi sql.NullString
		if afterID > 0 {
			if err := t.QueryRow(`SELECT pos FROM playlist_items WHERE playlist_id=? AND media_id=?`, id, afterID).Scan(&lo); err != nil {
				return errNotFound
			}
			t.QueryRow(`SELECT MIN(pos) FROM playlist_items WHERE playlist_id=? AND pos > ? AND media_id <> ?`, id, lo.String, mediaID).Scan(&hi)
		} else {
			t.QueryRow(`SELECT MIN(pos) FROM playlist_items WHERE playlist_id=? AND media_id <> ?`, id, mediaID).Scan(&hi)
		}
		pos := keyBetween(lo.String, hi.String)
		if _, err := t.Exec(`UPDATE playlist_items SET pos=? WHERE playlist_id=? AND media_id=?`, pos, id, mediaID); err != nil {
			return err
		}
		_, err := t.Exec(`UPDATE playlists SET updated_at=? WHERE id=?`, nowUnix(), id)
		return err
	})
}

// ---------- índice fracionário (base 62, sem zeros finais) ----------

const fiDigits = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// keyBetween devolve uma chave estritamente entre a e b ("" = sem limite).
// Requer a < b quando ambos existem.
func keyBetween(a, b string) string {
	if b != "" && a >= b {
		// Defesa: ordem inválida; põe depois de a.
		b = ""
	}
	return fiMidpoint(a, b)
}

func fiMidpoint(a, b string) string {
	if b != "" {
		n := 0
		for n < len(b) {
			ca := byte('0')
			if n < len(a) {
				ca = a[n]
			}
			if ca != b[n] {
				break
			}
			n++
		}
		if n > 0 {
			ra := ""
			if n < len(a) {
				ra = a[n:]
			}
			return b[:n] + fiMidpoint(ra, b[n:])
		}
	}
	digitA := 0
	if a != "" {
		digitA = strings.IndexByte(fiDigits, a[0])
	}
	digitB := len(fiDigits)
	if b != "" {
		digitB = strings.IndexByte(fiDigits, b[0])
	}
	if digitB-digitA > 1 {
		return string(fiDigits[(digitA+digitB+1)/2])
	}
	if b != "" && len(b) > 1 {
		return b[:1]
	}
	rest := ""
	if len(a) > 1 {
		rest = a[1:]
	}
	return string(fiDigits[digitA]) + fiMidpoint(rest, "")
}
