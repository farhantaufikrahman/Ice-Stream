package user

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// SearchUsers palsu meniru urutan query Oracle: username sama persis,
// lalu diawali kata pertama, lalu subscriber terbanyak, lalu id.
func (f *fakeRepo) SearchUsers(_ context.Context, terms []string, offset, limit int) ([]Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rank := func(p Profile) int {
		name := strings.ToLower(p.DisplayName)
		switch {
		case p.Username == terms[0]:
			return 0
		case strings.HasPrefix(p.Username, terms[0]) || strings.HasPrefix(name, terms[0]):
			return 1
		}
		return 2
	}
	var out []Profile
	for _, p := range f.users {
		if p.Status == statusBanned {
			continue
		}
		name := strings.ToLower(p.DisplayName)
		ok := true
		for _, t := range terms {
			ok = ok && (strings.Contains(p.Username, t) || strings.Contains(name, t))
		}
		if ok {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b Profile) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		if a.SubscriberCount != b.SubscriberCount {
			return int(b.SubscriberCount - a.SubscriberCount)
		}
		return int(a.ID - b.ID)
	})
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func TestParseSearchTerms(t *testing.T) {
	terms, err := parseSearchTerms("  Budi_Santoso budi  a 100%")
	require.NoError(t, err)
	// "_" dan "%" menjadi pemisah, kata dobel dan kata 1 huruf dibuang.
	assert.Equal(t, []string{"budi", "santoso", "100"}, terms)

	var verr *ValidationError
	_, err = parseSearchTerms("a b !")
	assert.ErrorAs(t, err, &verr)
	_, err = parseSearchTerms(strings.Repeat("x", 101))
	assert.ErrorAs(t, err, &verr)
}

func TestSearchUsers(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := newTestService()
	srv := NewGRPCServer(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	repo.users[20] = Profile{ID: 20, Username: "dapurbudi", DisplayName: "Dapur Budi", Email: "d@example.com", Status: "ACTIVE", SubscriberCount: 500}
	repo.users[21] = Profile{ID: 21, Username: "masak", DisplayName: "Budi Masak", Status: "ACTIVE", SubscriberCount: 900}
	repo.users[22] = Profile{ID: 22, Username: "kebudidayaan", DisplayName: "Kebudidayaan", Status: "ACTIVE", SubscriberCount: 9999}

	resp, err := srv.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: "BUDI"})
	require.NoError(t, err)
	var names []string
	for _, p := range resp.GetProfiles() {
		names = append(names, p.GetUsername())
		assert.Empty(t, p.GetEmail(), "email tidak pernah ikut")
	}
	// budi (sama persis), lalu yang diawali "budi" (masak: "Budi Masak"),
	// lalu yang hanya berisi "budi" diurutkan subscriber terbanyak.
	// Akun "spam" yang diblokir tidak ikut walau cocok.
	assert.Equal(t, []string{"budi", "masak", "kebudidayaan", "dapurbudi"}, names)
	assert.Empty(t, resp.GetNextPageToken())

	// Dua kata: keduanya harus ada.
	resp, err = srv.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: "dapur budi"})
	require.NoError(t, err)
	require.Len(t, resp.GetProfiles(), 1)
	assert.Equal(t, "dapurbudi", resp.GetProfiles()[0].GetUsername())

	// Halaman demi halaman.
	resp, err = srv.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: "budi", PageSize: 3})
	require.NoError(t, err)
	assert.Len(t, resp.GetProfiles(), 3)
	require.NotEmpty(t, resp.GetNextPageToken())
	resp, err = srv.SearchUsers(ctx, &userv1.SearchUsersRequest{Query: "budi", PageSize: 3, PageToken: resp.GetNextPageToken()})
	require.NoError(t, err)
	require.Len(t, resp.GetProfiles(), 1)
	assert.Equal(t, "dapurbudi", resp.GetProfiles()[0].GetUsername())

	for name, req := range map[string]*userv1.SearchUsersRequest{
		"query kosong":      {Query: " "},
		"page_size besar":   {Query: "budi", PageSize: 51},
		"token rusak":       {Query: "budi", PageToken: "xyz"},
		"halaman ke-10":     {Query: "budi", PageToken: encodeSearchPage(10)},
		"page_size negatif": {Query: "budi", PageSize: -1},
	} {
		_, err := srv.SearchUsers(ctx, req)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), name)
	}
}
