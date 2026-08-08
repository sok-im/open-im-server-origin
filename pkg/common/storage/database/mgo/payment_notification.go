package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/pagination"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewPaymentNotificationMongo(db *mongo.Database) (database.PaymentNotification, error) {
	coll := db.Collection(database.PaymentNotificationName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys: bson.D{
			{Key: "send_user_id", Value: 1},
			{Key: "create_time", Value: -1},
		},
	})
	if err != nil {
		return nil, err
	}
	return &PaymentNotificationMgo{coll: coll}, nil
}

type PaymentNotificationMgo struct {
	coll *mongo.Collection
}

func (p *PaymentNotificationMgo) Create(ctx context.Context, n *model.PaymentNotification) error {
	if n.CreateTime.IsZero() {
		n.CreateTime = time.Now()
	}
	return mongoutil.InsertOne(ctx, p.coll, n)
}

func (p *PaymentNotificationMgo) FindPage(ctx context.Context, sendUserID string, pagination pagination.Pagination) (int64, []*model.PaymentNotification, error) {
	filter := bson.M{"send_user_id": sendUserID}
	return mongoutil.FindPage[*model.PaymentNotification](ctx, p.coll, filter, pagination,
		options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}}))
}
