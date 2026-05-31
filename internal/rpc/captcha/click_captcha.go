package captcha

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	pbcaptcha "github.com/openimsdk/protocol/captcha"
	"github.com/openimsdk/tools/log"
	"github.com/wenlng/go-captcha/v2/base/option"
	"github.com/wenlng/go-captcha/v2/click"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type clickCaptchaDoc struct {
	CaptchaID  string     `bson:"captcha_id"`
	Dots       []clickDot `bson:"dots"`
	ExpiredAt  time.Time  `bson:"expired_at"`
	CreateTime time.Time  `bson:"create_time"`
	VerifyTime time.Time  `bson:"verify_time,omitempty"`
}

type clickDot struct {
	Index  int `bson:"index"`
	X      int `bson:"x"`
	Y      int `bson:"y"`
	Width  int `bson:"width"`
	Height int `bson:"height"`
}

func (s *server) GenerateClickCaptcha(ctx context.Context, _ *pbcaptcha.GenerateClickCaptchaReq) (*pbcaptcha.GenerateClickCaptchaResp, error) {
	captData, err := s.clickCapt.Generate()
	if err != nil {
		log.ZError(ctx, "click captcha generate failed", err)
		return nil, err
	}
	dots := captData.GetData()
	if len(dots) == 0 {
		log.ZError(ctx, "click captcha generate empty dots", nil)
		return nil, servererrs.ErrInternalServer.WrapMsg("click captcha generate empty dots")
	}
	masterImage, err := captData.GetMasterImage().ToBase64DataWithQuality(option.QualityNone)
	if err != nil {
		log.ZError(ctx, "click captcha encode master image failed", err)
		return nil, err
	}
	thumbImage, err := captData.GetThumbImage().ToBase64Data()
	if err != nil {
		log.ZError(ctx, "click captcha encode thumb image failed", err)
		return nil, err
	}
	id := uuid.NewString()
	now := time.Now()
	expiredAt := now.Add(time.Duration(s.conf.ExpireSeconds) * time.Second)
	docDots := make([]clickDot, 0, len(dots))
	for i := 0; i < len(dots); i++ {
		dot := dots[i]
		docDots = append(docDots, clickDot{
			Index:  dot.Index,
			X:      dot.X,
			Y:      dot.Y,
			Width:  dot.Width,
			Height: dot.Height,
		})
	}
	_, err = s.clickCollection.InsertOne(ctx, clickCaptchaDoc{
		CaptchaID:  id,
		Dots:       docDots,
		ExpiredAt:  expiredAt,
		CreateTime: now,
	})
	if err != nil {
		log.ZError(ctx, "click captcha insert mongodb failed", err, "captchaID", id)
		return nil, err
	}
	return &pbcaptcha.GenerateClickCaptchaResp{
		CaptchaID:   id,
		MasterImage: masterImage,
		ThumbImage:  thumbImage,
		ExpireAt:    expiredAt.Unix(),
	}, nil
}

func (s *server) VerifyClickCaptcha(ctx context.Context, req *pbcaptcha.VerifyClickCaptchaReq) (*pbcaptcha.VerifyClickCaptchaResp, error) {
	now := time.Now()
	filter := bson.M{
		"captcha_id":  req.CaptchaID,
		"verify_time": bson.M{"$exists": false},
	}
	update := bson.M{
		"$set": bson.M{
			"verify_time": now,
		},
	}
	var doc clickCaptchaDoc
	err := s.clickCollection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetReturnDocument(options.Before),
	).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			log.ZWarn(ctx, "click captcha not found or already verified", err, "captchaID", req.CaptchaID)
			return nil, servererrs.ErrRecordNotFound.WrapMsg("click captcha not found, expired, or already verified", "captchaID", req.CaptchaID)
		}
		log.ZError(ctx, "click captcha verify query failed", err, "captchaID", req.CaptchaID)
		return nil, servererrs.ErrDatabase.WrapMsg("verify click captcha query failed", "captchaID", req.CaptchaID)
	}
	if now.After(doc.ExpiredAt) {
		log.ZWarn(ctx, "click captcha expired", nil, "captchaID", req.CaptchaID, "expiredAt", doc.ExpiredAt.Unix())
		return nil, servererrs.ErrFileUploadedExpired.WrapMsg("click captcha expired", "captchaID", req.CaptchaID)
	}
	success := validateClickDots(req.GetDots(), doc.Dots, s.conf.VerifyPadding)
	if !success {
		log.ZError(ctx, "click captcha validate failed", nil, "captchaID", req.CaptchaID, "dotCount", len(req.GetDots()), "expectedCount", len(doc.Dots))
	}
	return &pbcaptcha.VerifyClickCaptchaResp{Success: success}, nil
}

func validateClickDots(clicks []*pbcaptcha.ClickPoint, expected []clickDot, padding int) bool {
	if len(clicks) != len(expected) {
		return false
	}
	for i, dot := range expected {
		clickPoint := clicks[i]
		if !click.Validate(int(clickPoint.GetX()), int(clickPoint.GetY()), dot.X, dot.Y, dot.Width, dot.Height, padding) {
			return false
		}
	}
	return true
}
