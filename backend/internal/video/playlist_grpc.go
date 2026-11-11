package video

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// Handler gRPC untuk playlist. Isinya hanya menerjemahkan pesan gRPC ke
// pemanggilan Service (playlist.go) dan sebaliknya.

func (s *GRPCServer) CreatePlaylist(ctx context.Context, req *videov1.CreatePlaylistRequest) (*videov1.CreatePlaylistResponse, error) {
	p, err := s.svc.CreatePlaylist(ctx, CreatePlaylistInput{
		RequesterID: req.GetRequesterId(),
		Title:       req.GetTitle(),
		Description: req.GetDescription(),
		Visibility:  fromProtoVisibility(req.GetVisibility()),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.CreatePlaylistResponse{Playlist: s.toProtoPlaylist(p)}, nil
}

func (s *GRPCServer) UpdatePlaylist(ctx context.Context, req *videov1.UpdatePlaylistRequest) (*videov1.UpdatePlaylistResponse, error) {
	in := UpdatePlaylistInput{
		RequesterID: req.GetRequesterId(),
		PlaylistID:  req.GetPlaylistId(),
		Title:       req.Title,
		Description: req.Description,
	}
	if v := fromProtoVisibility(req.GetVisibility()); v != "" {
		in.Visibility = &v
	}
	p, err := s.svc.UpdatePlaylist(ctx, in)
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.UpdatePlaylistResponse{Playlist: s.toProtoPlaylist(p)}, nil
}

func (s *GRPCServer) DeletePlaylist(ctx context.Context, req *videov1.DeletePlaylistRequest) (*videov1.DeletePlaylistResponse, error) {
	if err := s.svc.DeletePlaylist(ctx, req.GetRequesterId(), req.GetPlaylistId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.DeletePlaylistResponse{}, nil
}

func (s *GRPCServer) GetPlaylist(ctx context.Context, req *videov1.GetPlaylistRequest) (*videov1.GetPlaylistResponse, error) {
	p, err := s.svc.GetPlaylist(ctx, req.GetViewerId(), req.GetPlaylistId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.GetPlaylistResponse{Playlist: s.toProtoPlaylist(p)}, nil
}

func (s *GRPCServer) ListPlaylists(ctx context.Context, req *videov1.ListPlaylistsRequest) (*videov1.ListPlaylistsResponse, error) {
	list, next, err := s.svc.ListPlaylists(ctx, ListPlaylistsInput{
		ViewerID:  req.GetViewerId(),
		OwnerID:   req.GetOwnerId(),
		VideoID:   req.GetVideoId(),
		PageSize:  int(req.GetPageSize()),
		PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListPlaylistsResponse{NextPageToken: next}
	for _, p := range list {
		resp.Playlists = append(resp.Playlists, s.toProtoPlaylist(p))
	}
	return resp, nil
}

func (s *GRPCServer) AddToPlaylist(ctx context.Context, req *videov1.AddToPlaylistRequest) (*videov1.AddToPlaylistResponse, error) {
	addedAt, err := s.svc.AddToPlaylist(ctx, req.GetRequesterId(), req.GetPlaylistId(), req.GetVideoId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.AddToPlaylistResponse{AddedAt: timestamppb.New(addedAt)}, nil
}

func (s *GRPCServer) RemoveFromPlaylist(ctx context.Context, req *videov1.RemoveFromPlaylistRequest) (*videov1.RemoveFromPlaylistResponse, error) {
	if err := s.svc.RemoveFromPlaylist(ctx, req.GetRequesterId(), req.GetPlaylistId(), req.GetVideoId()); err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &videov1.RemoveFromPlaylistResponse{}, nil
}

func (s *GRPCServer) ListPlaylistVideos(ctx context.Context, req *videov1.ListPlaylistVideosRequest) (*videov1.ListPlaylistVideosResponse, error) {
	entries, next, err := s.svc.ListPlaylistVideos(ctx, req.GetViewerId(), req.GetPlaylistId(), int(req.GetPageSize()), req.GetPageToken())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.ListPlaylistVideosResponse{NextPageToken: next}
	for _, e := range entries {
		resp.Entries = append(resp.Entries, &videov1.PlaylistEntry{
			Video:   s.toProtoVideo(e.Video),
			AddedAt: timestamppb.New(e.AddedAt),
		})
	}
	return resp, nil
}

func (s *GRPCServer) toProtoPlaylist(p Playlist) *videov1.Playlist {
	return &videov1.Playlist{
		Id:            p.ID,
		OwnerId:       p.OwnerID,
		Title:         p.Title,
		Description:   p.Description,
		Visibility:    toProtoVisibility(p.Visibility),
		VideoCount:    p.VideoCount,
		ThumbnailUrl:  s.svc.MediaURL(p.ThumbnailKey),
		CreatedAt:     timestamppb.New(p.CreatedAt),
		UpdatedAt:     timestamppb.New(p.UpdatedAt),
		ContainsVideo: p.ContainsVideo,
	}
}

func toProtoVisibility(v string) videov1.PlaylistVisibility {
	switch v {
	case VisibilityPublic:
		return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC
	case VisibilityPrivate:
		return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PRIVATE
	}
	return videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_UNSPECIFIED
}

// fromProtoVisibility: UNSPECIFIED menjadi "" (pakai bawaan / tidak diubah).
func fromProtoVisibility(v videov1.PlaylistVisibility) string {
	switch v {
	case videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PUBLIC:
		return VisibilityPublic
	case videov1.PlaylistVisibility_PLAYLIST_VISIBILITY_PRIVATE:
		return VisibilityPrivate
	}
	return ""
}
