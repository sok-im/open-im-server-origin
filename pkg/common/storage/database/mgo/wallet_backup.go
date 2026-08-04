package mgo

import (
	"context"
	"time"

	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func NewWalletBackupInfoMongo(db *mongo.Database) (database.WalletBackupInfo, error) {
	coll := db.Collection(database.WalletBackupInfoName)
	_, err := coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "uid", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return &walletBackupInfoMgo{coll: coll}, nil
}

type walletBackupInfoMgo struct {
	coll *mongo.Collection
}

func (w *walletBackupInfoMgo) Upsert(ctx context.Context, info *model.WalletBackupInfo) error {
	if info == nil || info.UID == "" {
		return errs.ErrArgs.WrapMsg("uid is empty")
	}
	now := time.Now().UnixMilli()
	filter := bson.M{"uid": info.UID}
	update := bson.M{
		"$set": bson.M{
			"backup_time": info.BackupTime,
			"file_size":   info.FileSize,
			"name":        info.Name,
			"update_time": now,
		},
		"$setOnInsert": bson.M{"uid": info.UID},
	}
	_, err := w.coll.UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return errs.Wrap(err)
}

func (w *walletBackupInfoMgo) GetByUID(ctx context.Context, uid string) (*model.WalletBackupInfo, error) {
	if uid == "" {
		return nil, nil
	}
	doc, err := mongoutil.FindOne[*model.WalletBackupInfo](ctx, w.coll, bson.M{"uid": uid})
	if err != nil {
		if errs.ErrRecordNotFound.Is(err) {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}
