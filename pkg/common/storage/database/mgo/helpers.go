// Copyright © 2023 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mgo

import (
	"context"
	"errors"

	"github.com/openimsdk/tools/errs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func IsNotFound(err error) bool {
	return errs.Unwrap(err) == mongo.ErrNoDocuments
}

// ensureUniqueIndex migrates a legacy non-unique index to unique only when needed.
// Unconditionally Drop+Create on every process start races across msg/msgtransfer and
// aborts in-progress builds (IndexBuildAborted / dropIndexes).
func ensureUniqueIndex(ctx context.Context, coll *mongo.Collection, name string, keys bson.D) error {
	hasUnique, nonUniqueNames, err := inspectIndexes(ctx, coll, keys)
	if err != nil {
		return err
	}
	if hasUnique {
		return nil
	}
	for _, indexName := range nonUniqueNames {
		if _, err := coll.Indexes().DropOne(ctx, indexName); err != nil {
			if !isIndexNotFound(err) {
				return err
			}
		}
	}
	_, err = coll.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    keys,
		Options: options.Index().SetUnique(true).SetName(name),
	})
	if err == nil {
		return nil
	}
	// Sibling process may have created the same unique index concurrently.
	hasUnique, _, checkErr := inspectIndexes(ctx, coll, keys)
	if checkErr == nil && hasUnique {
		return nil
	}
	return err
}

type mongoIndexInfo struct {
	Name   string `bson:"name"`
	Unique bool   `bson:"unique"`
	Key    bson.D `bson:"key"`
}

func inspectIndexes(ctx context.Context, coll *mongo.Collection, keys bson.D) (hasUnique bool, nonUniqueNames []string, err error) {
	cursor, err := coll.Indexes().List(ctx)
	if err != nil {
		return false, nil, err
	}
	defer cursor.Close(ctx)

	var indexes []mongoIndexInfo
	if err := cursor.All(ctx, &indexes); err != nil {
		return false, nil, err
	}
	for _, idx := range indexes {
		if idx.Name == "_id_" || !indexKeysEqual(idx.Key, keys) {
			continue
		}
		if idx.Unique {
			hasUnique = true
			continue
		}
		nonUniqueNames = append(nonUniqueNames, idx.Name)
	}
	return hasUnique, nonUniqueNames, nil
}

func indexKeysEqual(a, b bson.D) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Key != b[i].Key {
			return false
		}
		if toInt64(a[i].Value) != toInt64(b[i].Value) {
			return false
		}
	}
	return true
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int32:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func isIndexNotFound(err error) bool {
	var cmdErr mongo.CommandError
	if errors.As(err, &cmdErr) {
		// 27 = IndexNotFound
		return cmdErr.Code == 27 || cmdErr.Name == "IndexNotFound"
	}
	return false
}
