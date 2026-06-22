package user

import (
	"context"

	"github.com/openimsdk/open-im-server/v3/pkg/authverify"
	tablerelation "github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	pbuser "github.com/openimsdk/protocol/user"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

func (s *userServer) setNotificationSwitch(ctx context.Context, userID, dbField string, enable bool) error {
	if userID == "" {
		return errs.ErrArgs.WrapMsg("userID is required")
	}
	if err := authverify.CheckAccessV3(ctx, userID, s.config.Share.IMAdminUserID); err != nil {
		log.ZWarn(ctx, "setNotificationSwitch: access denied", err,
			"opUserID", mcontext.GetOpUserID(ctx), "targetUserID", userID, "field", dbField)
		return err
	}
	if _, err := s.db.FindWithError(ctx, []string{userID}); err != nil {
		log.ZError(ctx, "setNotificationSwitch: user not found or db error", err,
			"opUserID", mcontext.GetOpUserID(ctx), "targetUserID", userID, "field", dbField)
		return err
	}
	if err := s.db.UpdateByMap(ctx, userID, map[string]any{
		dbField: tablerelation.BoolToNotificationSwitch(enable),
	}); err != nil {
		log.ZError(ctx, "setNotificationSwitch: UpdateByMap failed", err,
			"opUserID", mcontext.GetOpUserID(ctx), "targetUserID", userID,
			"field", dbField, "enable", enable)
		return err
	}
	return nil
}

// SetMsgNotificationSwitch 设置消息通知开关。
func (s *userServer) SetMsgNotificationSwitch(ctx context.Context, req *pbuser.SetMsgNotificationSwitchReq) (*pbuser.SetMsgNotificationSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "msg_notification", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetMsgNotificationSwitchResp{}, nil
}

// SetSokimPaymentNotificationSwitch 设置 sokim 支付通知开关。
func (s *userServer) SetSokimPaymentNotificationSwitch(ctx context.Context, req *pbuser.SetSokimPaymentNotificationSwitchReq) (*pbuser.SetSokimPaymentNotificationSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "sokim_payment_notification", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetSokimPaymentNotificationSwitchResp{}, nil
}

// SetSokimServiceNotificationSwitch 设置 sokim 服务通知开关。
func (s *userServer) SetSokimServiceNotificationSwitch(ctx context.Context, req *pbuser.SetSokimServiceNotificationSwitchReq) (*pbuser.SetSokimServiceNotificationSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "sokim_service_notification", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetSokimServiceNotificationSwitchResp{}, nil
}

// SetAvNotificationSwitch 设置音视频通知开关。
func (s *userServer) SetAvNotificationSwitch(ctx context.Context, req *pbuser.SetAvNotificationSwitchReq) (*pbuser.SetAvNotificationSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "av_notification", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetAvNotificationSwitchResp{}, nil
}

// SetAvCallRingtoneSwitch 设置音视频来电铃声开关。
func (s *userServer) SetAvCallRingtoneSwitch(ctx context.Context, req *pbuser.SetAvCallRingtoneSwitchReq) (*pbuser.SetAvCallRingtoneSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "av_call_ringtone", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetAvCallRingtoneSwitchResp{}, nil
}

// SetPlayCalleeRingtoneOnAnswerSwitch 设置接听时播放对方铃声开关。
func (s *userServer) SetPlayCalleeRingtoneOnAnswerSwitch(ctx context.Context, req *pbuser.SetPlayCalleeRingtoneOnAnswerSwitchReq) (*pbuser.SetPlayCalleeRingtoneOnAnswerSwitchResp, error) {
	if err := s.setNotificationSwitch(ctx, req.UserID, "play_callee_ringtone_on_answer", req.Enable); err != nil {
		return nil, err
	}
	return &pbuser.SetPlayCalleeRingtoneOnAnswerSwitchResp{}, nil
}

// GetUserNotificationSettings 返回当前登录用户（ctx opUserID）的通知相关开关。
func (s *userServer) GetUserNotificationSettings(ctx context.Context, req *pbuser.GetUserNotificationSettingsReq) (*pbuser.GetUserNotificationSettingsResp, error) {
	userID := mcontext.GetOpUserID(ctx)
	if userID == "" {
		return nil, errs.ErrArgs.WrapMsg("opUserID is required")
	}
	users, err := s.db.FindWithError(ctx, []string{userID})
	if err != nil {
		log.ZError(ctx, "GetUserNotificationSettings: user not found or db error", err, "opUserID", userID)
		return nil, err
	}
	u := users[0]
	return &pbuser.GetUserNotificationSettingsResp{
		MsgNotification:            tablerelation.NotificationSwitchToBool(u.MsgNotification),
		SokimPaymentNotification:   tablerelation.NotificationSwitchToBool(u.SokimPaymentNotification),
		SokimServiceNotification:   tablerelation.NotificationSwitchToBool(u.SokimServiceNotification),
		AvNotification:             tablerelation.NotificationSwitchToBool(u.AvNotification),
		AvCallRingtone:             tablerelation.NotificationSwitchToBool(u.AvCallRingtone),
		PlayCalleeRingtoneOnAnswer: tablerelation.NotificationSwitchToBool(u.PlayCalleeRingtoneOnAnswer),
	}, nil
}
