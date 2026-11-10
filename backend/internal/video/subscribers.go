package video

import (
	"context"
	"time"

	userv1 "github.com/farhantaufikrahman/ice-stream/backend/gen/user/v1"
)

// GRPCSubscriberSource mengambil id subscriber dari UserService lewat gRPC.
// Tabel subscriptions ada di schema AUTH_SVC, jadi VideoService tidak boleh
// membacanya langsung; ia harus bertanya ke pemilik datanya.
type GRPCSubscriberSource struct {
	client userv1.UserServiceClient
}

// NewGRPCSubscriberSource membuat GRPCSubscriberSource.
func NewGRPCSubscriberSource(client userv1.UserServiceClient) *GRPCSubscriberSource {
	return &GRPCSubscriberSource{client: client}
}

var _ SubscriberSource = (*GRPCSubscriberSource)(nil)

// subscriberCallTimeout membatasi satu panggilan ke UserService. Satu
// halaman (1000 id) cukup dibaca dari index, jadi biasanya hanya beberapa
// milidetik.
const subscriberCallTimeout = 10 * time.Second

// ListSubscriberIDs memanggil UserService.ListSubscriberIds.
func (s *GRPCSubscriberSource) ListSubscriberIDs(ctx context.Context, channelID, afterID int64, limit int) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, subscriberCallTimeout)
	defer cancel()
	resp, err := s.client.ListSubscriberIds(ctx, &userv1.ListSubscriberIdsRequest{
		ChannelId:         channelID,
		AfterSubscriberId: afterID,
		PageSize:          int32(limit),
	})
	if err != nil {
		return nil, err
	}
	return resp.GetSubscriberIds(), nil
}
