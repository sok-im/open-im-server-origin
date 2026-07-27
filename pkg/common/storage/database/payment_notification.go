package database

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/pagination"
)

type PaymentNotification interface {
	Create(ctx context.Context, n *model.PaymentNotification) error
	FindPage(ctx context.Context, sendUserID string, pagination pagination.Pagination) (total int64, list []*model.PaymentNotification, err error)
}
