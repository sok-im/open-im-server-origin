package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/openimsdk/open-im-server/v3/pkg/rpcli"
	pbAuth "github.com/openimsdk/protocol/auth"
	pbcaptcha "github.com/openimsdk/protocol/captcha"
	"github.com/openimsdk/protocol/conversation"
	pbcrypto "github.com/openimsdk/protocol/crypto"
	"github.com/openimsdk/protocol/group"
	"github.com/openimsdk/protocol/msg"
	pbopenmls "github.com/openimsdk/protocol/openmls"
	pbredpacket "github.com/openimsdk/protocol/redpacket"
	"github.com/openimsdk/protocol/relation"
	"github.com/openimsdk/protocol/rtc"
	"github.com/openimsdk/protocol/third"
	pbtotp "github.com/openimsdk/protocol/totp"
	"github.com/openimsdk/protocol/user"
	pbvirgil "github.com/openimsdk/protocol/virgilsecurity"

	"github.com/openimsdk/open-im-server/v3/internal/api/jssdk"
	relationrpc "github.com/openimsdk/open-im-server/v3/internal/rpc/relation"

	"github.com/gin-contrib/gzip"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/openimsdk/open-im-server/v3/pkg/common/prommetrics"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/cache/redis"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/controller"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	"github.com/openimsdk/protocol/constant"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mw"
)

const (
	NoCompression      = -1
	DefaultCompression = 0
	BestCompression    = 1
	BestSpeed          = 2
)

func prommetricsGin() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		path := c.FullPath()
		if c.Writer.Status() == http.StatusNotFound {
			prommetrics.HttpCall("<404>", c.Request.Method, c.Writer.Status())
		} else {
			prommetrics.HttpCall(path, c.Request.Method, c.Writer.Status())
		}
		if resp := apiresp.GetGinApiResponse(c); resp != nil {
			prommetrics.APICall(path, c.Request.Method, resp.ErrCode)
		}
	}
}

func newGinRouter(ctx context.Context, client discovery.SvcDiscoveryRegistry, config *Config) (*gin.Engine, error) {
	mgocli, err := mongoutil.NewMongoDB(ctx, config.MongodbConfig.Build())
	if err != nil {
		return nil, err
	}
	userGlobalBlackDB, err := mgo.NewUserGlobalBlackMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	userDB, err := mgo.NewUserMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	phoneSNDB, err := mgo.NewPhoneSNMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	totpDB, err := mgo.NewUserTotpMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	totpRecoveryDB, err := mgo.NewUserTotpRecoveryMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	friendDB, err := mgo.NewFriendMongo(mgocli.GetDB())
	if err != nil {
		return nil, err
	}
	rdb, err := redisutil.NewRedisClient(ctx, config.RedisConfig.Build())
	if err != nil {
		return nil, err
	}
	userCache := redis.NewUserCacheRedis(rdb, &config.LocalCacheConfig, userDB, redis.GetRocksCacheOptions())
	userCtrl := controller.NewUserDatabase(userDB, userCache, mgocli.GetTx())
	blacklistCtrl := controller.NewUserGlobalBlackDatabase(userGlobalBlackDB)

	authConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Auth)
	if err != nil {
		return nil, err
	}
	userConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.User)
	if err != nil {
		return nil, err
	}
	groupConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Group)
	if err != nil {
		return nil, err
	}
	friendConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Friend)
	if err != nil {
		return nil, err
	}
	conversationConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Conversation)
	if err != nil {
		return nil, err
	}
	thirdConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Third)
	if err != nil {
		return nil, err
	}
	msgConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Msg)
	if err != nil {
		return nil, err
	}
	captchaConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Captcha)
	if err != nil {
		return nil, err
	}
	rtcConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Rtc)
	if err != nil {
		return nil, err
	}
	cryptoConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Crypto)
	if err != nil {
		return nil, err
	}
	redpacketConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.RedPacket)
	if err != nil {
		return nil, err
	}
	virgilSecurityConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.VirgilSecurity)
	if err != nil {
		return nil, err
	}
	openMLSConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.OpenMLS)
	if err != nil {
		return nil, err
	}
	totpConn, err := client.GetConn(ctx, config.Share.RpcRegisterName.Totp)
	if err != nil {
		return nil, err
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("required_if", RequiredIf)
	}
	switch config.API.Api.CompressionLevel {
	case NoCompression:
	case DefaultCompression:
		r.Use(gzip.Gzip(gzip.DefaultCompression))
	case BestCompression:
		r.Use(gzip.Gzip(gzip.BestCompression))
	case BestSpeed:
		r.Use(gzip.Gzip(gzip.BestSpeed))
	}
	r.Use(prommetricsGin(), gin.RecoveryWithWriter(gin.DefaultErrorWriter, mw.GinPanicErr), mw.CorsHandler(), mw.GinParseOperationID(), GinParseToken(rpcli.NewAuthClient(authConn)))
	u := NewUserApi(user.NewUserClient(userConn), client, config.Share.RpcRegisterName, config.Share.IMAdminUserID)
	m := NewMessageApi(msg.NewMsgClient(msgConn), rpcli.NewUserClient(userConn), config.Share.IMAdminUserID)
	cp := NewCaptchaApi(pbcaptcha.NewCaptchaClient(captchaConn))
	bl := NewUserGlobalBlackApi(blacklistCtrl, userDB, config.Share.IMAdminUserID, rpcli.NewAuthClient(authConn))
	friendNotifier := relationrpc.NewFriendNotificationSender(&config.NotificationConfig, rpcli.NewMsgClient(msgConn))
	du := NewDeleteUserApi(userCtrl, friendDB, phoneSNDB, totpDB, totpRecoveryDB, rpcli.NewAuthClient(authConn), group.NewGroupClient(groupConn), relation.NewFriendClient(friendConn), friendNotifier, config.Share.IMAdminUserID)
	phoneSN := NewPhoneSNApi(phoneSNDB)
	userRouterGroup := r.Group("/user")
	{
		userRouterGroup.POST("/user_register", u.UserRegister)
		userRouterGroup.POST("/update_user_info", u.UpdateUserInfo)
		userRouterGroup.POST("/update_user_info_ex", u.UpdateUserInfoEx)
		userRouterGroup.POST("/set_global_msg_recv_opt", u.SetGlobalRecvMessageOpt)
		userRouterGroup.POST("/get_users_info", u.GetUsersPublicInfo)
		userRouterGroup.POST("/get_all_users_uid", u.GetAllUsersID)
		userRouterGroup.POST("/account_check", u.AccountCheck)
		userRouterGroup.POST("/get_users", u.GetUsers)
		userRouterGroup.POST("/get_users_online_status", u.GetUsersOnlineStatus)
		userRouterGroup.POST("/get_users_online_token_detail", u.GetUsersOnlineTokenDetail)
		userRouterGroup.POST("/get_self_login_platforms", u.GetSelfLoginPlatforms)
		userRouterGroup.POST("/subscribe_users_status", u.SubscriberStatus)
		userRouterGroup.POST("/get_users_status", u.GetUserStatus)
		userRouterGroup.POST("/get_subscribe_users_status", u.GetSubscribeUsersStatus)

		userRouterGroup.POST("/process_user_command_add", u.ProcessUserCommandAdd)
		userRouterGroup.POST("/process_user_command_delete", u.ProcessUserCommandDelete)
		userRouterGroup.POST("/process_user_command_update", u.ProcessUserCommandUpdate)
		userRouterGroup.POST("/process_user_command_get", u.ProcessUserCommandGet)
		userRouterGroup.POST("/process_user_command_get_all", u.ProcessUserCommandGetAll)

		userRouterGroup.POST("/add_notification_account", u.AddNotificationAccount)
		userRouterGroup.POST("/update_notification_account", u.UpdateNotificationAccountInfo)
		userRouterGroup.POST("/search_notification_account", u.SearchNotificationAccount)

		// 手机号可见性设置（所有人/仅好友/隐藏）
		userRouterGroup.POST("/set_phone_visibility", u.SetPhoneVisibility)
		userRouterGroup.POST("/set_call_accept_setting", u.SetCallAcceptSetting)
		userRouterGroup.POST("/set_msg_receive_setting", u.SetMsgReceiveSetting)
		userRouterGroup.POST("/set_group_invite_setting", u.SetGroupInviteSetting)
		// 设置用户全局阅后即焚时长（秒），0 表示关闭
		userRouterGroup.POST("/set_user_msg_burn_duration", u.SetUserMsgBurnDuration)
		// 设置删除账号等待间隔（秒），0 表示使用系统默认（18个月）
		userRouterGroup.POST("/set_delete_account_interval", u.SetDeleteAccountInterval)
		// 批量查询阅后即焚、手机号可见性、音视频接收、全局/会话消息接收、群邀请等设置
		userRouterGroup.POST("/get_user_privacy_settings", u.GetUserPrivacySettings)
		// 用户通知相关开关
		userRouterGroup.POST("/set_msg_notification_switch", u.SetMsgNotificationSwitch)
		userRouterGroup.POST("/set_sokim_payment_notification_switch", u.SetSokimPaymentNotificationSwitch)
		userRouterGroup.POST("/set_sokim_service_notification_switch", u.SetSokimServiceNotificationSwitch)
		userRouterGroup.POST("/set_av_notification_switch", u.SetAvNotificationSwitch)
		userRouterGroup.POST("/set_av_call_ringtone_switch", u.SetAvCallRingtoneSwitch)
		userRouterGroup.POST("/set_play_callee_ringtone_on_answer_switch", u.SetPlayCalleeRingtoneOnAnswerSwitch)
		userRouterGroup.POST("/get_user_notification_settings", u.GetUserNotificationSettings)
		// 根据手机号精确查找用户（phoneSearchVisibility=true 时遵守 phone_visibility 设置）
		userRouterGroup.POST("/get_user_by_phone", u.GetUserByPhone)
		// 根据昵称精确查询用户（可多结果，与 getPaginationUsers 模糊搜索不同）
		userRouterGroup.POST("/get_users_by_nickname", u.GetUsersByNickname)
		// 检查昵称是否已被占用（精确匹配，可选 excludeUserID 排除本人）
		userRouterGroup.POST("/check_nickname", u.CheckNickname)
		// 检查指定 userID 的用户是否存在
		userRouterGroup.POST("/check_user_exist", u.CheckUserExist)

		// 全局黑名单管理（仅管理员）
		userRouterGroup.POST("/add_global_blacklist", bl.AddGlobalBlacklist)
		userRouterGroup.POST("/remove_global_blacklist", bl.RemoveGlobalBlacklist)
		userRouterGroup.POST("/get_global_blacklist", bl.GetGlobalBlacklist)

		userRouterGroup.POST("/delete_user", du.DeleteUser)
	}
	// friend routing group
	{
		f := NewFriendApi(relation.NewFriendClient(friendConn))
		friendRouterGroup := r.Group("/friend")
		friendRouterGroup.POST("/delete_friend", f.DeleteFriend)
		friendRouterGroup.POST("/delete_friend_oneway", f.DeleteFriendOneway)
		friendRouterGroup.POST("/get_friend_apply_list", f.GetFriendApplyList)
		friendRouterGroup.POST("/get_designated_friend_apply", f.GetDesignatedFriendsApply)
		friendRouterGroup.POST("/get_self_friend_apply_list", f.GetSelfApplyList)
		friendRouterGroup.POST("/get_friend_list", f.GetFriendList)
		friendRouterGroup.POST("/get_designated_friends", f.GetDesignatedFriends)
		friendRouterGroup.POST("/add_friend", f.AddOnewayFriend)
		friendRouterGroup.POST("/add_friend_response", f.RespondFriendApply)
		friendRouterGroup.POST("/set_friend_remark", f.SetFriendRemark)
		friendRouterGroup.POST("/add_black", f.AddBlack)
		friendRouterGroup.POST("/get_black_list", f.GetPaginationBlacks)
		friendRouterGroup.POST("/get_specified_blacks", f.GetSpecifiedBlacks)
		friendRouterGroup.POST("/remove_black", f.RemoveBlack)
		friendRouterGroup.POST("/get_incremental_blacks", f.GetIncrementalBlacks)
		friendRouterGroup.POST("/import_friend", f.ImportFriends)
		friendRouterGroup.POST("/is_friend", f.IsFriend)
		friendRouterGroup.POST("/get_friend_id", f.GetFriendIDs)
		friendRouterGroup.POST("/get_specified_friends_info", f.GetSpecifiedFriendsInfo)
		friendRouterGroup.POST("/get_phone", f.GetFriendPhone)
		friendRouterGroup.POST("/update_friends", f.UpdateFriends)
		friendRouterGroup.POST("/get_incremental_friends", f.GetIncrementalFriends)
		friendRouterGroup.POST("/get_full_friend_user_ids", f.GetFullFriendUserIDs)
		friendRouterGroup.POST("/get_self_unhandled_apply_count", f.GetSelfUnhandledApplyCount)
		friendRouterGroup.POST("/get_pinned_friend_ids", f.GetPinnedFriendIDs)
		friendRouterGroup.POST("/add_oneway_friend", f.AddOnewayFriend)
		friendRouterGroup.POST("/pin", f.PinFriend)
		friendRouterGroup.POST("/unpin", f.UnpinFriend)
	}

	g := NewGroupApi(group.NewGroupClient(groupConn))
	{
		groupRouterGroup := r.Group("/group")
		groupRouterGroup.POST("/create_group", g.CreateGroup)
		groupRouterGroup.POST("/set_group_info", g.SetGroupInfo)
		groupRouterGroup.POST("/set_group_info_ex", g.SetGroupInfoEx)
		groupRouterGroup.POST("/set_send_message_setting", g.SetSendMessageSetting)
		groupRouterGroup.POST("/set_invite_setting", g.SetInviteSetting)
		groupRouterGroup.POST("/set_invite_link_setting", g.SetInviteLinkSetting)
		groupRouterGroup.POST("/set_pin_setting", g.SetPinSetting)
		groupRouterGroup.POST("/set_edit_setting", g.SetEditSetting)
		groupRouterGroup.POST("/set_burn_setting", g.SetBurnSetting)
		groupRouterGroup.POST("/get_group_setting", g.GetGroupSetting)
		groupRouterGroup.POST("/set_msg_burn_duration", g.SetMsgBurnDuration)
		groupRouterGroup.POST("/get_msg_burn_duration", g.GetMsgBurnDuration)
		groupRouterGroup.POST("/set_group_announcement", g.SetGroupAnnouncement)
		groupRouterGroup.POST("/get_group_announcement", g.GetGroupAnnouncement)
		groupRouterGroup.POST("/join_group", g.JoinGroup)
		groupRouterGroup.POST("/quit_group", g.QuitGroup)
		groupRouterGroup.POST("/group_application_response", g.ApplicationGroupResponse)
		groupRouterGroup.POST("/transfer_group", g.TransferGroupOwner)
		groupRouterGroup.POST("/get_recv_group_applicationList", g.GetRecvGroupApplicationList)
		groupRouterGroup.POST("/get_user_req_group_applicationList", g.GetUserReqGroupApplicationList)
		groupRouterGroup.POST("/get_group_users_req_application_list", g.GetGroupUsersReqApplicationList)
		groupRouterGroup.POST("/get_specified_user_group_request_info", g.GetSpecifiedUserGroupRequestInfo)
		groupRouterGroup.POST("/get_groups_info", g.GetGroupsInfo)
		groupRouterGroup.POST("/kick_group", g.KickGroupMember)
		groupRouterGroup.POST("/get_group_members_info", g.GetGroupMembersInfo)
		groupRouterGroup.POST("/get_group_member_list", g.GetGroupMemberList)
		groupRouterGroup.POST("/invite_user_to_group", g.InviteUserToGroup)
		groupRouterGroup.POST("/get_joined_group_list", g.GetJoinedGroupList)
		groupRouterGroup.POST("/dismiss_group", g.DismissGroup) //
		groupRouterGroup.POST("/mute_group_member", g.MuteGroupMember)
		groupRouterGroup.POST("/cancel_mute_group_member", g.CancelMuteGroupMember)
		groupRouterGroup.POST("/mute_group", g.MuteGroup)
		groupRouterGroup.POST("/cancel_mute_group", g.CancelMuteGroup)
		groupRouterGroup.POST("/set_group_member_info", g.SetGroupMemberInfo)
		groupRouterGroup.POST("/get_group_abstract_info", g.GetGroupAbstractInfo)
		groupRouterGroup.POST("/get_groups", g.GetGroups)
		groupRouterGroup.POST("/get_group_member_user_id", g.GetGroupMemberUserIDs)
		groupRouterGroup.POST("/get_incremental_join_groups", g.GetIncrementalJoinGroup)
		groupRouterGroup.POST("/get_incremental_group_members", g.GetIncrementalGroupMember)
		groupRouterGroup.POST("/get_incremental_group_members_batch", g.GetIncrementalGroupMemberBatch)
		groupRouterGroup.POST("/get_full_group_member_user_ids", g.GetFullGroupMemberUserIDs)
		groupRouterGroup.POST("/get_full_join_group_ids", g.GetFullJoinGroupIDs)
		groupRouterGroup.POST("/get_group_application_unhandled_count", g.GetGroupApplicationUnhandledCount)
		groupRouterGroup.POST("/get_common_groups_with_friend", g.GetCommonGroupsWithFriend)
		groupRouterGroup.POST("/pin_group_message", g.PinGroupMessage)
		groupRouterGroup.POST("/unpin_group_message", g.UnpinGroupMessage)
		groupRouterGroup.POST("/get_group_pinned_messages", g.GetGroupPinnedMessages)
		groupRouterGroup.POST("/set_mute", g.SetGroupMute)
		groupRouterGroup.POST("/get_mute", g.GetGroupMute)
		groupRouterGroup.POST("/pin", g.PinGroup)
		groupRouterGroup.POST("/unpin", g.UnpinGroup)

		// 群邀请链接
		groupRouterGroup.POST("/create_invite_link", g.CreateGroupInviteLink)
		groupRouterGroup.POST("/get_invite_link", g.GetGroupInviteLink)
		groupRouterGroup.POST("/join_by_invite_link", g.JoinGroupByInviteLink)
		groupRouterGroup.POST("/revoke_invite_link", g.RevokeGroupInviteLink)
		groupRouterGroup.POST("/list_invite_links", g.ListGroupInviteLinks)
	}
	// certificate
	{
		a := NewAuthApi(pbAuth.NewAuthClient(authConn))
		authRouterGroup := r.Group("/auth")
		authRouterGroup.POST("/get_admin_token", a.GetAdminToken)
		authRouterGroup.POST("/get_user_token", a.GetUserToken)
		authRouterGroup.POST("/parse_token", a.ParseToken)
		authRouterGroup.POST("/force_logout", a.ForceLogout)
		authRouterGroup.POST("/get_active_devices", a.GetActiveDevices)
		authRouterGroup.POST("/kick_device", a.KickDevice)
	}
	// Third service
	{
		t := NewThirdApi(third.NewThirdClient(thirdConn), config.API.Prometheus.GrafanaURL)
		thirdGroup := r.Group("/third")
		thirdGroup.GET("/prometheus", t.GetPrometheus)
		thirdGroup.POST("/fcm_update_token", t.FcmUpdateToken)
		thirdGroup.POST("/set_app_badge", t.SetAppBadge)

		logs := thirdGroup.Group("/logs")
		logs.POST("/upload", t.UploadLogs)
		logs.POST("/delete", t.DeleteLogs)
		logs.POST("/search", t.SearchLogs)

		objectGroup := r.Group("/object")

		objectGroup.POST("/part_limit", t.PartLimit)
		objectGroup.POST("/part_size", t.PartSize)
		objectGroup.POST("/initiate_multipart_upload", t.InitiateMultipartUpload)
		objectGroup.POST("/auth_sign", t.AuthSign)
		objectGroup.POST("/complete_multipart_upload", t.CompleteMultipartUpload)
		objectGroup.POST("/access_url", t.AccessURL)
		objectGroup.POST("/initiate_form_data", t.InitiateFormData)
		objectGroup.POST("/complete_form_data", t.CompleteFormData)
		objectGroup.GET("/*name", t.ObjectRedirect)
	}
	// Message
	{
		msgGroup := r.Group("/msg")
		msgGroup.POST("/newest_seq", m.GetSeq)
		msgGroup.POST("/search_msg", m.SearchMsg)
		msgGroup.POST("/send_msg", m.SendMessage)
		msgGroup.POST("/send_business_notification", m.SendBusinessNotification)
		msgGroup.POST("/send_service_notification", m.SendServiceNotification)
		msgGroup.POST("/batch_send_service_notification", m.BatchSendServiceNotification)
		msgGroup.POST("/send_payment_notification", m.SendPaymentNotification)
		msgGroup.POST("/pull_msg_by_seq", m.PullMsgBySeqs)
		msgGroup.POST("/revoke_msg", m.RevokeMsg)
		msgGroup.POST("/mark_msgs_as_read", m.MarkMsgsAsRead)
		msgGroup.POST("/mark_conversation_as_read", m.MarkConversationAsRead)
		msgGroup.POST("/mark_group_msgs_as_read", m.MarkGroupMsgsAsRead)
		msgGroup.POST("/get_conversations_has_read_and_max_seq", m.GetConversationsHasReadAndMaxSeq)
		msgGroup.POST("/set_conversation_has_read_seq", m.SetConversationHasReadSeq)

		msgGroup.POST("/clear_conversation_msg", m.ClearConversationsMsg)
		msgGroup.POST("/user_clear_all_msg", m.UserClearAllMsg)
		msgGroup.POST("/delete_msgs", m.DeleteMsgs)
		msgGroup.POST("/delete_msg_phsical_by_seq", m.DeleteMsgPhysicalBySeq)
		msgGroup.POST("/delete_msg_physical", m.DeleteMsgPhysical)

		msgGroup.POST("/batch_send_msg", m.BatchSendMsg)
		msgGroup.POST("/check_msg_is_send_success", m.CheckMsgIsSendSuccess)
		msgGroup.POST("/get_server_time", m.GetServerTime)
		msgGroup.POST("/report_spam", m.ReportSpam)
		msgGroup.POST("/get_spam_reports", m.GetSpamReports)
		msgGroup.POST("/handle_spam_report", m.HandleSpamReport)
	}
	// Conversation
	{
		c := NewConversationApi(conversation.NewConversationClient(conversationConn))
		conversationGroup := r.Group("/conversation")
		conversationGroup.POST("/get_sorted_conversation_list", c.GetSortedConversationList)
		conversationGroup.POST("/get_all_conversations", c.GetAllConversations)
		conversationGroup.POST("/get_conversation", c.GetConversation)
		conversationGroup.POST("/get_conversations", c.GetConversations)
		conversationGroup.POST("/set_conversations", c.SetConversations)
		conversationGroup.POST("/get_conversation_offline_push_user_ids", c.GetConversationOfflinePushUserIDs)
		conversationGroup.POST("/get_full_conversation_ids", c.GetFullOwnerConversationIDs)
		conversationGroup.POST("/get_incremental_conversations", c.GetIncrementalConversation)
		conversationGroup.POST("/get_owner_conversation", c.GetOwnerConversation)
		conversationGroup.POST("/get_not_notify_conversation_ids", c.GetNotNotifyConversationIDs)
		conversationGroup.POST("/get_pinned_conversation_ids", c.GetPinnedConversationIDs)
		conversationGroup.POST("/set_mute", c.SetMute)
		conversationGroup.POST("/set_burn", c.SetBurn)
	}

	{
		captchaGroup := r.Group("/captcha")
		captchaGroup.POST("/generate", cp.GenerateCaptcha)
		captchaGroup.POST("/verify", cp.VerifyCaptcha)
		captchaGroup.POST("/click_generate", cp.GenerateClickCaptcha)
		captchaGroup.POST("/click_verify", cp.VerifyClickCaptcha)
	}

	// TOTP / MFA
	{
		tp := NewTotpApi(pbtotp.NewTotpClient(totpConn))
		totpGroup := r.Group("/totp")
		// Endpoints below require a valid login token; userID is injected from the token.
		totpGroup.POST("/secret", tp.GetSecret)
		totpGroup.POST("/bind", tp.BindTotp)
		totpGroup.POST("/status", tp.GetStatus)
		totpGroup.POST("/unbind", tp.UnbindTotp)
		// /totp/verify is token-free: user is mid-login and authenticates via mfaToken.
		totpGroup.POST("/verify", tp.VerifyTotp)
	}

	{
		phoneGroup := r.Group("/phone")
		phoneGroup.POST("/get_sn_info", phoneSN.GetSNInfo)
		phoneGroup.POST("/set_sn_info", phoneSN.SetSNInfo)
	}
	{
		rc := NewRtcApi(rtc.NewRtcServiceClient(rtcConn))
		rtcGroup := r.Group("/rtc")
		rtcGroup.POST("/signal_message_assemble", rc.SignalMessageAssemble)
		rtcGroup.POST("/signal_get_room_by_group_id", rc.SignalGetRoomByGroupID)
		rtcGroup.POST("/signal_get_token_by_room_id", rc.SignalGetTokenByRoomID)
		rtcGroup.POST("/signal_get_rooms", rc.SignalGetRooms)
		rtcGroup.POST("/get_signal_invitation_info", rc.GetSignalInvitationInfo)
		rtcGroup.POST("/get_signal_invitation_info_start_app", rc.GetSignalInvitationInfoStartApp)
		rtcGroup.POST("/signal_send_custom_signal", rc.SignalSendCustomSignal)
		rtcGroup.POST("/signal_notify_group_call_ended", rc.SignalNotifyGroupCallEnded)
		rtcGroup.POST("/get_signal_invitation_records", rc.GetSignalInvitationRecords)
		rtcGroup.POST("/delete_signal_records", rc.DeleteSignalRecords)
	}

	// Crypto / E2EE
	{
		cr := NewCryptoApi(pbcrypto.NewCryptoServiceClient(cryptoConn))
		cryptoGroup := r.Group("/crypto")
		cryptoGroup.POST("/register_device", cr.RegisterDevice)
		cryptoGroup.POST("/get_devices", cr.GetDevices)
		cryptoGroup.POST("/revoke_device", cr.RevokeDevice)
		cryptoGroup.POST("/get_virgil_jwt", cr.GetVirgilJWT)
		cryptoGroup.POST("/get_group_key_version", cr.GetGroupKeyVersion)
		cryptoGroup.POST("/get_group_key_events", cr.GetGroupKeyEvents)
		cryptoGroup.POST("/security_precheck", cr.SecurityPrecheck)
		cryptoGroup.POST("/integrity_report", cr.IntegrityReport)
	}

	// VirgilSecurity 1v1 E2EE (按 virgil-1v1-e2ee-openapi.yaml 暴露 /api/im-e2ee/v1/* 路径)
	{
		vs := NewVirgilSecurityApi(pbvirgil.NewVirgilSecurityServiceClient(virgilSecurityConn))
		e2ee := r.Group("/virgil/v1")
		e2ee.POST("/jwt", vs.IssueVirgilJWT)
		e2ee.POST("/devices/register", vs.RegisterDevice)
		e2ee.POST("/devices", vs.GetDevices)
		e2ee.POST("/devices/revoke", vs.RevokeDevice)
		e2ee.POST("/conversations/ensure-1v1", vs.EnsureConversation)
		e2ee.POST("/events/subscribe", vs.SubscribeEvents)
		e2ee.POST("/files/upload-url", vs.CreateUploadURL)
	}

	// OpenMLS E2EE — MLS Delivery Service (e2ee-openmls-design.md §5)
	{
		omls := NewOpenMLSApi(pbopenmls.NewOpenMLSServiceClient(openMLSConn))
		mlsGroup := r.Group("/openmls/v1")
		mlsGroup.POST("/key_packages/upload", omls.UploadKeyPackage)
		mlsGroup.POST("/key_packages/fetch", omls.GetKeyPackages)
		mlsGroup.POST("/key_packages/count", omls.GetKeyPackageCount)
		mlsGroup.POST("/key_packages/refresh", omls.RefreshKeyPackages)
		mlsGroup.POST("/groups/commit", omls.SubmitCommit)
		mlsGroup.POST("/groups/commits", omls.GetCommits)
		mlsGroup.POST("/groups/welcome", omls.SendWelcome)
		mlsGroup.POST("/groups/state", omls.GetGroupState)
		mlsGroup.POST("/groups/delete", omls.DeleteGroup)

		cryptoMLS := r.Group("/crypto/v1")
		cryptoMLS.POST("/credential/issue", omls.IssueCredential)
		cryptoMLS.POST("/credential/verify", omls.VerifyCredential)
		cryptoMLS.POST("/root_public_key", omls.GetRootPublicKey)
	}

	// RedPacket
	{
		rp := NewRedPacketApi(pbredpacket.NewRedPacketClient(redpacketConn))
		redpacketGroup := r.Group("/redpacket")
		redpacketGroup.POST("/create_order", rp.CreateOrder)
		redpacketGroup.POST("/created_callback", rp.CreatedCallback)
		redpacketGroup.POST("/detail", rp.GetDetail)
		redpacketGroup.POST("/issue_claim_sign", rp.IssueClaimSign)
		redpacketGroup.POST("/claim_result", rp.ClaimResult)
		redpacketGroup.POST("/request_refund", rp.RequestRefund)
		redpacketGroup.POST("/get_refund", rp.GetRefund)
		redpacketGroup.POST("/wallet_bind/challenge", rp.IssueWalletBindChallenge)
		redpacketGroup.POST("/wallet_bind/confirm", rp.ConfirmWalletBind)
		redpacketGroup.POST("/wallet_bind/detail", rp.GetWalletBinding)

		adminGroup := redpacketGroup.Group("/admin")
		adminGroup.POST("/set_signer", rp.AdminSetSigner)
		adminGroup.POST("/set_token", rp.AdminSetToken)
		adminGroup.POST("/set_expiry", rp.AdminSetExpiry)
		adminGroup.POST("/set_allow_all_tokens", rp.AdminSetAllowAllTokens)
		adminGroup.POST("/set_native_token_enabled", rp.AdminSetNativeTokenEnabled)
		adminGroup.POST("/parse_tx_events", rp.AdminParseTxEvents)
	}

	{
		statisticsGroup := r.Group("/statistics")
		statisticsGroup.POST("/user/register", u.UserRegisterCount)
		statisticsGroup.POST("/user/online", u.GetOnlineUserCount)
		statisticsGroup.POST("/user/active", m.GetActiveUser)
		statisticsGroup.POST("/group/create", g.GroupCreateCount)
		statisticsGroup.POST("/group/active", m.GetActiveGroup)
	}

	{
		j := jssdk.NewJSSdkApi(rpcli.NewUserClient(userConn), rpcli.NewRelationClient(friendConn),
			rpcli.NewGroupClient(groupConn), rpcli.NewConversationClient(conversationConn), rpcli.NewMsgClient(msgConn))
		jssdk := r.Group("/jssdk")
		jssdk.POST("/get_conversations", j.GetConversations)
		jssdk.POST("/get_active_conversations", j.GetActiveConversations)
	}
	{
		pd := NewPrometheusDiscoveryApi(config, client)
		proDiscoveryGroup := r.Group("/prometheus_discovery", pd.Enable)
		proDiscoveryGroup.GET("/api", pd.Api)
		proDiscoveryGroup.GET("/user", pd.User)
		proDiscoveryGroup.GET("/group", pd.Group)
		proDiscoveryGroup.GET("/msg", pd.Msg)
		proDiscoveryGroup.GET("/friend", pd.Friend)
		proDiscoveryGroup.GET("/conversation", pd.Conversation)
		proDiscoveryGroup.GET("/third", pd.Third)
		proDiscoveryGroup.GET("/auth", pd.Auth)
		proDiscoveryGroup.GET("/push", pd.Push)
		proDiscoveryGroup.GET("/msg_gateway", pd.MessageGateway)
		proDiscoveryGroup.GET("/msg_transfer", pd.MessageTransfer)
		proDiscoveryGroup.GET("/rtc", pd.Rtc)
	}
	return r, nil
}

func GinParseToken(authClient *rpcli.AuthClient) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost:
			for _, wApi := range Whitelist {
				if strings.HasPrefix(c.Request.URL.Path, wApi) {
					c.Next()
					return
				}
			}

			token := c.Request.Header.Get(constant.Token)
			if token == "" {
				log.ZWarn(c, "header get token error", servererrs.ErrArgs.WrapMsg("header must have token"))
				apiresp.GinError(c, servererrs.ErrArgs.WrapMsg("header must have token"))
				c.Abort()
				return
			}
			resp, err := authClient.ParseToken(c, token)
			if err != nil {
				apiresp.GinError(c, err)
				c.Abort()
				return
			}
			c.Set(constant.OpUserPlatform, constant.PlatformIDToName(int(resp.PlatformID)))
			c.Set(constant.OpUserID, resp.UserID)
			c.Next()
		}
	}
}

// Whitelist api not parse token
var Whitelist = []string{
	"/auth/get_admin_token",
	"/auth/parse_token",
	"/captcha",
	"/phone/get_sn_info",
	"/group/get_invite_link",
	"/totp/verify", // second-factor login step; user authenticates via mfaToken, not a login token
}
