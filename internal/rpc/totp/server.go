package totp

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openimsdk/open-im-server/v3/pkg/common/config"
	"github.com/openimsdk/open-im-server/v3/pkg/common/servererrs"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/database/mgo"
	pbtotp "github.com/openimsdk/protocol/totp"
	"github.com/openimsdk/tools/db/mongoutil"
	"github.com/openimsdk/tools/db/redisutil"
	"github.com/openimsdk/tools/discovery"
	"github.com/openimsdk/tools/log"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"

	dbtotp "github.com/openimsdk/open-im-server/v3/pkg/common/storage/database"
	"github.com/openimsdk/open-im-server/v3/pkg/common/storage/model"
)

const (
	pendingSecretTTL = 10 * time.Minute
	mfaTokenTTL      = 5 * time.Minute
	replaySetTTL     = 90 * time.Second // 3 × 30s steps to safely cover ±1 step window
	maxFailCount     = 5
	failCountTTL     = 5 * time.Minute

	recoveryCodeCount  = 8
	recoveryCodeLength = 8  // chars, alphanumeric, displayed as XXXX-XXXX
	totpWindow         = 1  // ±1 time step tolerance
	totpPeriod         = 30 // seconds per TOTP step
	totpDigits         = 6

	keyPendingSecret = "totp:pending:%s" // value: Base32 secret
	keyMfaToken      = "totp:mfa:%s"     // value: userID
	keyReplay        = "totp:replay:%s"  // set of used TOTP codes per user; TTL replaySetTTL
	keyFailCount     = "totp:fail:%s"    // counter for failed attempts per mfaToken
)

// Config bundles all external dependencies for the TOTP service.
type Config struct {
	RpcConfig     config.Totp
	MongodbConfig config.Mongo
	RedisConfig   config.Redis
	Share         config.Share
	Discovery     config.Discovery
}

type totpServer struct {
	pbtotp.UnimplementedTotpServer
	cfg     config.Totp
	totpDB  dbtotp.UserTotp
	recovDB dbtotp.UserTotpRecovery
	rdb     redis.UniversalClient
}

func Start(ctx context.Context, cfg *Config, _ discovery.SvcDiscoveryRegistry, grpcServer *grpc.Server) error {
	mgocli, err := mongoutil.NewMongoDB(ctx, cfg.MongodbConfig.Build())
	if err != nil {
		log.ZError(ctx, "totp: connect mongodb failed", err)
		return err
	}
	db := mgocli.GetDB()

	totpDB, err := mgo.NewUserTotpMongo(db)
	if err != nil {
		log.ZError(ctx, "totp: create user_totp indexes failed", err)
		return err
	}
	recovDB, err := mgo.NewUserTotpRecoveryMongo(db)
	if err != nil {
		log.ZError(ctx, "totp: create user_totp_recovery indexes failed", err)
		return err
	}

	rdb, err := redisutil.NewRedisClient(ctx, cfg.RedisConfig.Build())
	if err != nil {
		log.ZError(ctx, "totp: connect redis failed", err)
		return err
	}

	s := &totpServer{
		cfg:     cfg.RpcConfig,
		totpDB:  totpDB,
		recovDB: recovDB,
		rdb:     rdb,
	}
	pbtotp.RegisterTotpServer(grpcServer, s)
	return nil
}

// ── GetSecret ────────────────────────────────────────────────────────────────

func (s *totpServer) GetSecret(ctx context.Context, req *pbtotp.GetSecretReq) (*pbtotp.GetSecretResp, error) {
	// Reject if user already has an active binding.
	_, err := s.totpDB.Get(ctx, req.UserID)
	if err == nil {
		return nil, servererrs.ErrTotpAlreadyBound.WrapMsg("userID", req.UserID)
	}
	if !isNotFound(err) {
		return nil, servererrs.ErrDatabase.WrapMsg("get user_totp failed", "userID", req.UserID)
	}

	secret, err := generateSecret(20)
	if err != nil {
		return nil, err
	}

	issuer := req.Issuer
	if issuer == "" {
		issuer = s.cfg.Issuer
	}
	if issuer == "" {
		issuer = "OpenIM"
	}
	account := req.AccountName
	if account == "" {
		account = req.UserID
	}

	otpAuthURL := fmt.Sprintf(
		"otpauth://totp/%s:%s?secret=%s&issuer=%s&algorithm=SHA1&digits=%d&period=%d",
		issuer, account, secret, issuer, totpDigits, totpPeriod,
	)

	redisKey := fmt.Sprintf(keyPendingSecret, req.UserID)
	if err := s.rdb.Set(ctx, redisKey, secret, pendingSecretTTL).Err(); err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("store pending secret failed", "userID", req.UserID)
	}

	expireAt := time.Now().Add(pendingSecretTTL).Unix()
	return &pbtotp.GetSecretResp{
		Secret:     secret,
		OtpAuthUrl: otpAuthURL,
		ExpireAt:   expireAt,
	}, nil
}

// ── BindTotp ─────────────────────────────────────────────────────────────────

func (s *totpServer) BindTotp(ctx context.Context, req *pbtotp.BindTotpReq) (*pbtotp.BindTotpResp, error) {
	redisKey := fmt.Sprintf(keyPendingSecret, req.UserID)
	secret, err := s.rdb.Get(ctx, redisKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, servererrs.ErrTotpSecretExpired.WrapMsg("userID", req.UserID)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get pending secret failed", "userID", req.UserID)
	}

	if !verifyTOTP(secret, req.TotpCode, totpWindow) {
		return nil, servererrs.ErrTotpCodeInvalid.WrapMsg("userID", req.UserID)
	}

	// Persist the binding.
	now := time.Now()
	if err := s.totpDB.Upsert(ctx, &model.UserTotp{
		UserID:  req.UserID,
		Secret:  secret,
		Enabled: true,
		BoundAt: now.Unix(),
	}); err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("upsert user_totp failed", "userID", req.UserID)
	}

	// Generate and store recovery codes.
	plains, docs, err := generateRecoveryCodes(req.UserID, recoveryCodeCount)
	if err != nil {
		return nil, err
	}
	if err := s.recovDB.InsertMany(ctx, docs); err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("insert recovery codes failed", "userID", req.UserID)
	}

	// Clean up temporary secret.
	_ = s.rdb.Del(ctx, redisKey).Err()

	return &pbtotp.BindTotpResp{RecoveryCodes: plains}, nil
}

// ── VerifyTotp ───────────────────────────────────────────────────────────────

func (s *totpServer) VerifyTotp(ctx context.Context, req *pbtotp.VerifyTotpReq) (*pbtotp.VerifyTotpResp, error) {
	mfaKey := fmt.Sprintf(keyMfaToken, req.MfaToken)
	userID, err := s.rdb.Get(ctx, mfaKey).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, servererrs.ErrMfaTokenExpired.WrapMsg("mfaToken", req.MfaToken)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get mfaToken failed")
	}

	// Check fail-count guard.
	failKey := fmt.Sprintf(keyFailCount, req.MfaToken)
	if err := s.checkAndIncrFailCount(ctx, failKey); err != nil {
		return nil, err
	}

	// Fetch the TOTP binding.
	rec, err := s.totpDB.Get(ctx, userID)
	if err != nil {
		if isNotFound(err) {
			return nil, servererrs.ErrTotpNotBound.WrapMsg("userID", userID)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get user_totp failed", "userID", userID)
	}

	code := req.TotpCode
	var verified bool

	if len(code) == totpDigits && isDigitsOnly(code) {
		// TOTP code path – check replay then verify.
		replayKey := fmt.Sprintf(keyReplay, userID)
		added, err := s.rdb.SAdd(ctx, replayKey, code).Result()
		if err != nil {
			return nil, servererrs.ErrDatabase.WrapMsg("check replay failed", "userID", userID)
		}
		s.rdb.Expire(ctx, replayKey, replaySetTTL)
		if added == 0 {
			// Code was already used in this window.
			return nil, servererrs.ErrTotpCodeInvalid.WrapMsg("replay detected", "userID", userID)
		}
		verified = verifyTOTP(rec.Secret, code, totpWindow)
	} else {
		// Recovery code path.
		unusedCodes, err := s.recovDB.FindUnused(ctx, userID)
		if err != nil {
			return nil, servererrs.ErrDatabase.WrapMsg("find recovery codes failed", "userID", userID)
		}
		if len(unusedCodes) == 0 {
			return nil, servererrs.ErrTotpRecoveryExhausted.WrapMsg("userID", userID)
		}
		normalised := strings.ReplaceAll(code, "-", "")
		for _, rc := range unusedCodes {
			if bcrypt.CompareHashAndPassword([]byte(rc.CodeHash), []byte(normalised)) == nil {
				if err := s.recovDB.MarkUsed(ctx, rc.ID, time.Now().Unix()); err != nil {
					log.ZWarn(ctx, "mark recovery code used failed", err, "userID", userID)
				}
				verified = true
				break
			}
		}
	}

	if !verified {
		return nil, servererrs.ErrTotpCodeInvalid.WrapMsg("userID", userID)
	}

	// Consume the mfaToken (one-time use).
	_ = s.rdb.Del(ctx, mfaKey).Err()
	_ = s.rdb.Del(ctx, failKey).Err()

	// Check remaining recovery codes.
	remaining, _ := s.recovDB.CountUnused(ctx, userID)
	recovLow := remaining < 3

	return &pbtotp.VerifyTotpResp{
		UserID:            userID,
		RecoveryCodesLow:  recovLow,
		RecoveryCodesLeft: int32(remaining),
	}, nil
}

// ── GetStatus ────────────────────────────────────────────────────────────────

func (s *totpServer) GetStatus(ctx context.Context, req *pbtotp.GetStatusReq) (*pbtotp.GetStatusResp, error) {
	rec, err := s.totpDB.Get(ctx, req.UserID)
	if err != nil {
		if isNotFound(err) {
			return &pbtotp.GetStatusResp{Enabled: false}, nil
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get user_totp failed", "userID", req.UserID)
	}

	remaining, err := s.recovDB.CountUnused(ctx, req.UserID)
	if err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("count recovery codes failed", "userID", req.UserID)
	}

	return &pbtotp.GetStatusResp{
		Enabled:                true,
		BoundAt:                rec.BoundAt,
		RecoveryCodesRemaining: int32(remaining),
	}, nil
}

// ── UnbindTotp ───────────────────────────────────────────────────────────────

func (s *totpServer) UnbindTotp(ctx context.Context, req *pbtotp.UnbindTotpReq) (*pbtotp.UnbindTotpResp, error) {
	rec, err := s.totpDB.Get(ctx, req.UserID)
	if err != nil {
		if isNotFound(err) {
			return nil, servererrs.ErrTotpNotBound.WrapMsg("userID", req.UserID)
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get user_totp failed", "userID", req.UserID)
	}

	code := req.TotpCode
	var verified bool

	if len(code) == totpDigits && isDigitsOnly(code) {
		verified = verifyTOTP(rec.Secret, code, totpWindow)
	} else {
		unusedCodes, err := s.recovDB.FindUnused(ctx, req.UserID)
		if err != nil {
			return nil, servererrs.ErrDatabase.WrapMsg("find recovery codes failed", "userID", req.UserID)
		}
		normalised := strings.ReplaceAll(code, "-", "")
		for _, rc := range unusedCodes {
			if bcrypt.CompareHashAndPassword([]byte(rc.CodeHash), []byte(normalised)) == nil {
				verified = true
				break
			}
		}
	}

	if !verified {
		return nil, servererrs.ErrTotpCodeInvalid.WrapMsg("userID", req.UserID)
	}

	s.totpDB.Delete(ctx, req.UserID)

	s.recovDB.DeleteByUser(ctx, req.UserID)

	return &pbtotp.UnbindTotpResp{}, nil
}

// ── CreateMfaToken ───────────────────────────────────────────────────────────

func (s *totpServer) CreateMfaToken(ctx context.Context, req *pbtotp.CreateMfaTokenReq) (*pbtotp.CreateMfaTokenResp, error) {
	token := uuid.NewString()
	key := fmt.Sprintf(keyMfaToken, token)
	if err := s.rdb.Set(ctx, key, req.UserID, mfaTokenTTL).Err(); err != nil {
		return nil, servererrs.ErrDatabase.WrapMsg("store mfaToken failed", "userID", req.UserID)
	}
	return &pbtotp.CreateMfaTokenResp{
		MfaToken: token,
		ExpireAt: time.Now().Add(mfaTokenTTL).Unix(),
	}, nil
}

// ── CheckTotpBound ───────────────────────────────────────────────────────────

func (s *totpServer) CheckTotpBound(ctx context.Context, req *pbtotp.CheckTotpBoundReq) (*pbtotp.CheckTotpBoundResp, error) {
	_, err := s.totpDB.Get(ctx, req.UserID)
	if err != nil {
		if isNotFound(err) {
			return &pbtotp.CheckTotpBoundResp{Bound: false}, nil
		}
		return nil, servererrs.ErrDatabase.WrapMsg("get user_totp failed", "userID", req.UserID)
	}
	return &pbtotp.CheckTotpBoundResp{Bound: true}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func isNotFound(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}

// generateSecret generates a random n-byte secret encoded as uppercase Base32.
func generateSecret(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return base32.StdEncoding.EncodeToString(b), nil
}

// verifyTOTP checks the 6-digit code against the Base32 secret within ±window steps.
func verifyTOTP(secret, code string, window int) bool {
	key, err := base32.StdEncoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return false
	}
	t := time.Now().Unix() / int64(totpPeriod)
	for i := -window; i <= window; i++ {
		if totp(key, t+int64(i)) == code {
			return true
		}
	}
	return false
}

// totp computes the TOTP code for the given key and time counter.
func totp(key []byte, counter int64) string {
	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(counter))
	mac := hmac.New(sha1.New, key)
	mac.Write(msg)
	h := mac.Sum(nil)
	offset := h[len(h)-1] & 0x0f
	code := int(h[offset]&0x7f)<<24 |
		int(h[offset+1]&0xff)<<16 |
		int(h[offset+2]&0xff)<<8 |
		int(h[offset+3]&0xff)
	code = code % int(math.Pow10(totpDigits))
	return fmt.Sprintf("%06d", code)
}

const recoveryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// generateRecoveryCodes creates n random recovery codes. Returns plain texts and hashed model docs.
func generateRecoveryCodes(userID string, n int) ([]string, []*model.UserTotpRecovery, error) {
	plains := make([]string, n)
	docs := make([]*model.UserTotpRecovery, n)
	for i := range plains {
		plain, err := randomString(recoveryAlphabet, recoveryCodeLength)
		if err != nil {
			return nil, nil, fmt.Errorf("generate recovery code: %w", err)
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, fmt.Errorf("hash recovery code: %w", err)
		}
		plains[i] = plain[:4] + "-" + plain[4:]
		docs[i] = &model.UserTotpRecovery{
			UserID:   userID,
			CodeHash: string(hash),
			Used:     false,
		}
	}
	return plains, docs, nil
}

func randomString(alphabet string, n int) (string, error) {
	b := make([]byte, n)
	max := big.NewInt(int64(len(alphabet)))
	for i := range b {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		b[i] = alphabet[idx.Int64()]
	}
	return string(b), nil
}

func isDigitsOnly(s string) bool {
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// checkAndIncrFailCount returns an error if the fail count already reached the limit,
// otherwise atomically increments it.
func (s *totpServer) checkAndIncrFailCount(ctx context.Context, failKey string) error {
	cnt, err := s.rdb.Get(ctx, failKey).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return servererrs.ErrDatabase.WrapMsg("check fail count failed")
	}
	if cnt >= maxFailCount {
		return servererrs.ErrTotpTooManyErrors
	}
	pipe := s.rdb.Pipeline()
	pipe.Incr(ctx, failKey)
	pipe.Expire(ctx, failKey, failCountTTL)
	_, _ = pipe.Exec(ctx)
	return nil
}
