package video

import (
	"context"
	"fmt"
	"strings"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Pencarian playlist (langkah 22c, tab Playlist di halaman pencarian).
//
// Hanya playlist PUBLIC yang berisi minimal satu video READY: playlist
// kosong tidak berguna di hasil pencarian. Judul dicocokkan dengan
// LIKE '%kata%' untuk setiap kata, bukan inverted index seperti video,
// karena yang dicari hanya judul pendek (maksimal 100 huruf).

// SearchPlaylists mencari playlist publik. Halaman memakai offset dan
// token yang sama dengan SearchVideos (maksimal 10 halaman).
func (s *Service) SearchPlaylists(ctx context.Context, query string, pageSize int, pageToken string) ([]Playlist, string, error) {
	if pageSize <= 0 {
		pageSize = defaultPageSize
	}
	if pageSize > maxPageSize {
		return nil, "", &ValidationError{"page_size", fmt.Sprintf("maksimal %d", maxPageSize)}
	}
	page := 0
	if pageToken != "" {
		p, err := decodeSearchPage(pageToken)
		if err != nil {
			return nil, "", &ValidationError{"page_token", "tidak valid"}
		}
		page = p
	}
	terms, err := ParseSearchQuery(query)
	if err != nil {
		return nil, "", err
	}
	list, err := s.repo.SearchPlaylists(ctx, terms, page*pageSize, pageSize+1)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > pageSize {
		list = list[:pageSize]
		if page+1 < searchMaxPages {
			next = encodeSearchPage(page + 1)
		}
	}
	if len(list) == 0 {
		return list, next, nil
	}
	ids := make([]int64, len(list))
	for i, p := range list {
		ids[i] = p.ID
	}
	stats, err := s.repo.PlaylistStats(ctx, ids)
	if err != nil {
		return nil, "", err
	}
	for i := range list {
		st := stats[list[i].ID]
		list[i].VideoCount = st.VideoCount
		list[i].ThumbnailKey = st.ThumbnailKey
	}
	return list, next, nil
}

// SearchPlaylists menyusun query seperti ini (satu baris AND per kata):
//
//	SELECT ... FROM playlists p
//	 WHERE p.visibility = 'PUBLIC'
//	   AND LOWER(p.title) LIKE '%belajar%'
//	   AND EXISTS (video READY di playlist ini)
//	 ORDER BY <judul diawali kata pertama dulu>, p.updated_at DESC, p.id DESC
//	OFFSET ? ROWS FETCH NEXT ? ROWS ONLY
//
// Nama kolom diberi alias huruf kecil dalam tanda kutip (AS "title"),
// sama seperti query Raw lain di package ini: tanpa itu Oracle menjawab
// nama kolom huruf besar dan sebagian field tidak terisi saat Scan.
//
// Kata dari ParseSearchQuery hanya berisi huruf dan angka, jadi aman
// dipakai di pola LIKE tanpa escape untuk % dan _.
func (r *OracleRepository) SearchPlaylists(ctx context.Context, terms []string, offset, limit int) ([]Playlist, error) {
	if len(terms) == 0 {
		return nil, nil
	}
	var sql strings.Builder
	sql.WriteString(`SELECT p.id AS "id", p.owner_id AS "owner_id", p.title AS "title", p.description AS "description", p.visibility AS "visibility", p.created_at AS "created_at", p.updated_at AS "updated_at"
  FROM playlists p
 WHERE p.visibility = ?`)
	args := []any{VisibilityPublic}
	for _, t := range terms {
		sql.WriteString("\n   AND LOWER(p.title) LIKE ?")
		args = append(args, "%"+t+"%")
	}
	sql.WriteString(`
   AND EXISTS (SELECT 1 FROM playlist_items pi JOIN videos v ON v.id = pi.video_id
                WHERE pi.playlist_id = p.id AND v.status = ? AND v.deleted_at IS NULL)
 ORDER BY CASE WHEN LOWER(p.title) LIKE ? THEN 0 ELSE 1 END, p.updated_at DESC, p.id DESC
OFFSET ? ROWS FETCH NEXT ? ROWS ONLY`)
	args = append(args, StatusReady, terms[0]+"%", offset, limit)

	var rows []playlistRow
	if err := r.db.WithContext(ctx).Raw(sql.String(), args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("search playlists: %w", err)
	}
	out := make([]Playlist, len(rows))
	for i, row := range rows {
		out[i] = row.toPlaylist()
	}
	return out, nil
}

// SearchPlaylists: handler gRPC.
func (s *GRPCServer) SearchPlaylists(ctx context.Context, req *videov1.SearchPlaylistsRequest) (*videov1.SearchPlaylistsResponse, error) {
	list, next, err := s.svc.SearchPlaylists(ctx, req.GetQuery(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.SearchPlaylistsResponse{NextPageToken: next, Playlists: make([]*videov1.Playlist, len(list))}
	for i, p := range list {
		resp.Playlists[i] = s.toProtoPlaylist(p)
	}
	return resp, nil
}
