package api

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
	"github.com/openimsdk/tools/apiresp"
	"github.com/openimsdk/tools/errs"
	"github.com/openimsdk/tools/log"
	"github.com/openimsdk/tools/mcontext"
)

type WalletBackupApi struct {
	db database.WalletBackupInfo
}

func NewWalletBackupApi(db database.WalletBackupInfo) *WalletBackupApi {
	return &WalletBackupApi{db: db}
}

type walletSetBackupInfoReq struct {
	UID        string `json:"uid" binding:"required"`
	BackupTime int64  `json:"backupTime"`
	FileSize   int64  `json:"fileSize"`
	Name       string `json:"name"`
}

type walletGetBackupInfoReq struct {
	UID string `json:"uid" binding:"required"`
}

type walletBackupInfoResp struct {
	UID        string `json:"uid"`
	BackupTime int64  `json:"backupTime"`
	FileSize   int64  `json:"fileSize"`
	Name       string `json:"name"`
}

func requireSelfUID(opUserID, uid string) error {
	if opUserID == "" || opUserID != uid {
		return errs.ErrNoPermission.WrapMsg("only self can access wallet backup info")
	}
	return nil
}

// isClearBackupInfo 表示客户端请求清空备份：BackupTime=0、FileSize=0、name 为空。
func isClearBackupInfo(name string, backupTime, fileSize int64) bool {
	return backupTime == 0 && fileSize == 0 && strings.TrimSpace(name) == ""
}

func validateSetBackupInfo(uid, name string, backupTime, fileSize int64) error {
	if strings.TrimSpace(uid) == "" {
		return errs.ErrArgs.WrapMsg("uid is empty")
	}
	if isClearBackupInfo(name, backupTime, fileSize) {
		return nil
	}
	if strings.TrimSpace(name) == "" {
		return errs.ErrArgs.WrapMsg("name is empty")
	}
	if backupTime <= 0 {
		return errs.ErrArgs.WrapMsg("backupTime must be > 0")
	}
	if fileSize < 0 {
		return errs.ErrArgs.WrapMsg("fileSize must be >= 0")
	}
	return nil
}

// SetBackupInfo POST /wallet/set_backup_info
// 当 BackupTime=0、FileSize=0、name="" 时删除该 uid 的备份记录。
func (a *WalletBackupApi) SetBackupInfo(c *gin.Context) {
	var req walletSetBackupInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	uid := strings.TrimSpace(req.UID)
	name := strings.TrimSpace(req.Name)
	if err := validateSetBackupInfo(uid, name, req.BackupTime, req.FileSize); err != nil {
		apiresp.GinError(c, err)
		return
	}
	if err := requireSelfUID(mcontext.GetOpUserID(c), uid); err != nil {
		apiresp.GinError(c, err)
		return
	}
	if isClearBackupInfo(name, req.BackupTime, req.FileSize) {
		if err := a.db.DeleteByUID(c, uid); err != nil {
			log.ZError(c, "SetBackupInfo delete", err, "uid", uid)
			apiresp.GinError(c, err)
			return
		}
		apiresp.GinSuccess(c, nil)
		return
	}
	if err := a.db.Upsert(c, &model.WalletBackupInfo{
		UID:        uid,
		BackupTime: req.BackupTime,
		FileSize:   req.FileSize,
		Name:       name,
	}); err != nil {
		log.ZError(c, "SetBackupInfo", err, "uid", uid)
		apiresp.GinError(c, err)
		return
	}
	apiresp.GinSuccess(c, nil)
}

// GetBackupInfo POST /wallet/get_backup_info
func (a *WalletBackupApi) GetBackupInfo(c *gin.Context) {
	var req walletGetBackupInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg(err.Error()))
		return
	}
	uid := strings.TrimSpace(req.UID)
	if uid == "" {
		apiresp.GinError(c, errs.ErrArgs.WrapMsg("uid is empty"))
		return
	}
	if err := requireSelfUID(mcontext.GetOpUserID(c), uid); err != nil {
		apiresp.GinError(c, err)
		return
	}
	info, err := a.db.GetByUID(c, uid)
	if err != nil {
		log.ZError(c, "GetBackupInfo", err, "uid", uid)
		apiresp.GinError(c, err)
		return
	}
	resp := walletBackupInfoResp{UID: uid}
	if info != nil {
		resp.BackupTime = info.BackupTime
		resp.FileSize = info.FileSize
		resp.Name = info.Name
	}
	apiresp.GinSuccess(c, resp)
}
