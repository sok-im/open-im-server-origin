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

package servererrs

import "github.com/openimsdk/tools/errs"

var (
	ErrDatabase         = errs.NewCodeError(DatabaseError, "DatabaseError")
	ErrNetwork          = errs.NewCodeError(NetworkError, "NetworkError")
	ErrCallback         = errs.NewCodeError(CallbackError, "CallbackError")
	ErrCallbackContinue = errs.NewCodeError(CallbackError, "ErrCallbackContinue")

	ErrInternalServer = errs.NewCodeError(ServerInternalError, "ServerInternalError")
	ErrArgs           = errs.NewCodeError(ArgsError, "ArgsError")
	ErrNoPermission   = errs.NewCodeError(NoPermissionError, "NoPermissionError")
	ErrDuplicateKey   = errs.NewCodeError(DuplicateKeyError, "DuplicateKeyError")
	ErrRecordNotFound = errs.NewCodeError(RecordNotFoundError, "RecordNotFoundError")

	ErrUserIDNotFound  = errs.NewCodeError(UserIDNotFoundError, "UserIDNotFoundError")
	ErrUserBlocked     = errs.NewCodeError(UserBlockedError, "UserBlockedError")
	ErrGroupIDNotFound = errs.NewCodeError(GroupIDNotFoundError, "GroupIDNotFoundError")
	ErrGroupIDExisted  = errs.NewCodeError(GroupIDExisted, "GroupIDExisted")

	ErrNotInGroupYet       = errs.NewCodeError(NotInGroupYetError, "NotInGroupYetError")
	ErrDismissedAlready    = errs.NewCodeError(DismissedAlreadyError, "DismissedAlreadyError")
	ErrRegisteredAlready   = errs.NewCodeError(RegisteredAlreadyError, "RegisteredAlreadyError")
	ErrGroupTypeNotSupport = errs.NewCodeError(GroupTypeNotSupport, "")
	ErrGroupRequestHandled = errs.NewCodeError(GroupRequestHandled, "GroupRequestHandled")

	ErrData             = errs.NewCodeError(DataError, "DataError")
	ErrTokenExpired     = errs.NewCodeError(TokenExpiredError, "TokenExpiredError")
	ErrTokenInvalid     = errs.NewCodeError(TokenInvalidError, "TokenInvalidError")         //
	ErrTokenMalformed   = errs.NewCodeError(TokenMalformedError, "TokenMalformedError")     //
	ErrTokenNotValidYet = errs.NewCodeError(TokenNotValidYetError, "TokenNotValidYetError") //
	ErrTokenUnknown     = errs.NewCodeError(TokenUnknownError, "TokenUnknownError")         //
	ErrTokenKicked      = errs.NewCodeError(TokenKickedError, "TokenKickedError")
	ErrTokenNotExist    = errs.NewCodeError(TokenNotExistError, "TokenNotExistError") //

	ErrMessageHasReadDisable = errs.NewCodeError(MessageHasReadDisable, "MessageHasReadDisable")

	ErrCanNotAddYourself    = errs.NewCodeError(CanNotAddYourselfError, "CanNotAddYourselfError")
	ErrBlockedByPeer        = errs.NewCodeError(BlockedByPeer, "BlockedByPeer")
	ErrNotPeersFriend       = errs.NewCodeError(NotPeersFriend, "NotPeersFriend")
	ErrRelationshipAlready  = errs.NewCodeError(RelationshipAlreadyError, "RelationshipAlreadyError")
	ErrFriendRequestHandled = errs.NewCodeError(FriendRequestHandled, "FriendRequestHandled")

	ErrMutedInGroup       = errs.NewCodeError(MutedInGroup, "MutedInGroup")
	ErrMutedGroup         = errs.NewCodeError(MutedGroup, "MutedGroup")
	ErrMsgAlreadyRevoke   = errs.NewCodeError(MsgAlreadyRevoke, "MsgAlreadyRevoke")
	ErrMsgReceiveNotAllowed = errs.NewCodeError(MsgReceiveNotAllowed, "MsgReceiveNotAllowed")
	ErrGroupMessageBlocked  = errs.NewCodeError(GroupMessageBlocked, "GroupMessageBlocked")

	ErrConnOverMaxNumLimit = errs.NewCodeError(ConnOverMaxNumLimit, "ConnOverMaxNumLimit")

	ErrConnArgsErr          = errs.NewCodeError(ConnArgsErr, "args err, need token, sendID, platformID")
	ErrPushMsgErr           = errs.NewCodeError(PushMsgErr, "push msg err")
	ErrIOSBackgroundPushErr = errs.NewCodeError(IOSBackgroundPushErr, "ios background push err")

	ErrFileUploadedExpired = errs.NewCodeError(FileUploadedExpiredError, "FileUploadedExpiredError")

	ErrAllUserBusy = errs.NewCodeError(AllUserBusyError, "AllUserBusyError")

	ErrCallE2EERequiredUnsupported     = errs.NewCodeError(CallE2EERequiredUnsupportedError, "CALL_E2EE_REQUIRED_UNSUPPORTED")
	ErrCallE2EEConversationNotReady    = errs.NewCodeError(CallE2EEConversationNotReadyError, "CALL_E2EE_CONVERSATION_NOT_READY")
	ErrCallE2EEGroupMembershipInvalid  = errs.NewCodeError(CallE2EEGroupMembershipInvalidError, "CALL_E2EE_GROUP_MEMBERSHIP_INVALID")
	ErrCallE2EEProtocolVersionMismatch = errs.NewCodeError(CallE2EEProtocolVersionMismatchError, "CALL_E2EE_PROTOCOL_VERSION_MISMATCH")
	ErrCallE2EETokenDenied             = errs.NewCodeError(CallE2EETokenDeniedError, "CALL_E2EE_TOKEN_DENIED")
	ErrMLSEpochConflict                = errs.NewCodeError(MLSEpochConflictError, "epoch conflict")

	// TOTP / MFA errors.
	ErrTotpAlreadyBound   = errs.NewCodeError(TotpAlreadyBoundError, "user already has TOTP bound, please unbind first")
	ErrTotpCodeInvalid    = errs.NewCodeError(TotpCodeInvalidError, "TOTP or recovery code is incorrect")
	ErrTotpSecretExpired  = errs.NewCodeError(TotpSecretExpiredError, "temporary secret not found or expired, please restart binding")
	ErrMfaTokenExpired    = errs.NewCodeError(MfaTokenExpiredError, "MFA token not found or expired, please login again")
	ErrTotpNotBound       = errs.NewCodeError(TotpNotBoundError, "user has not bound TOTP")
	ErrTotpRecoveryExhausted = errs.NewCodeError(TotpRecoveryExhausted, "all recovery codes have been used, please contact support")
	ErrTotpTooManyErrors  = errs.NewCodeError(TotpVerifyTooManyErrors, "too many failed attempts, please retry later")
)
