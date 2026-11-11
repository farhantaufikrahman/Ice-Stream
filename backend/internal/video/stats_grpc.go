package video

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	videov1 "github.com/farhantaufikrahman/ice-stream/backend/gen/video/v1"
)

// GetChannelStats: handler gRPC untuk dasbor Creator Studio (stats.go).
func (s *GRPCServer) GetChannelStats(ctx context.Context, req *videov1.GetChannelStatsRequest) (*videov1.GetChannelStatsResponse, error) {
	st, err := s.svc.GetChannelStats(ctx, req.GetRequesterId(), int(req.GetDays()))
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	resp := &videov1.GetChannelStatsResponse{
		Daily:         make([]*videov1.DailyViews, len(st.Daily)),
		Current:       toProtoTotals(st.Current),
		Previous:      toProtoTotals(st.Previous),
		PreviousStart: timestamppb.New(st.PreviousStart),
		CurrentStart:  timestamppb.New(st.CurrentStart),
		End:           timestamppb.New(st.End),
	}
	for i, d := range st.Daily {
		resp.Daily[i] = &videov1.DailyViews{Date: d.Date, Views: d.Views}
	}
	return resp, nil
}

func toProtoTotals(t ChannelTotals) *videov1.ChannelTotals {
	return &videov1.ChannelTotals{Views: t.Views, Danmaku: t.Danmaku, Comments: t.Comments}
}
