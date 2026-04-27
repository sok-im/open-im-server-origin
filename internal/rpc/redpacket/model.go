package redpacket

import (
	"time"

	pbredpacket "github.com/openimsdk/protocol/redpacket"
)

const (
	collRedPacket = "redpacket"
	collClaims    = "redpacket_claims"
	collClaimAuth = "redpacket_claim_auth"
	collCursor    = "redpacket_cursor"

	statusPending   = "PENDING"
	statusActive    = "ACTIVE"
	statusConfirmed = "CONFIRMED"
	statusRefunded  = "REFUNDED"
)

type redPacketDoc struct {
	BizID           string    `bson:"biz_id"`
	PacketID        string    `bson:"packet_id"`
	ChainID         int64     `bson:"chain_id"`
	ContractAddress string    `bson:"contract_address"`
	CreatorUserID   string    `bson:"creator_user_id"`
	CreatorWallet   string    `bson:"creator_wallet"`
	PacketType      int       `bson:"packet_type"`
	Token           string    `bson:"token"`
	TotalAmount     string    `bson:"total_amount"`
	TotalShares     int       `bson:"total_shares"`
	ExpiryAt        int64     `bson:"expiry_at"`
	TxHash          string    `bson:"tx_hash"`
	Status          string    `bson:"status"`
	Chain           string    `bson:"chain"`
	CreatedAt       time.Time `bson:"created_at"`
	UpdatedAt       time.Time `bson:"updated_at"`
}

type claimDoc struct {
	PacketID      string    `bson:"packet_id"`
	ClaimerWallet string    `bson:"claimer_wallet"`
	AuthNonce     string    `bson:"auth_nonce"`
	ClaimTxHash   string    `bson:"claim_tx_hash"`
	ClaimedAmount string    `bson:"claimed_amount"`
	BlockNumber   int64     `bson:"block_number"`
	Status        string    `bson:"status"`
	CreatedAt     time.Time `bson:"created_at"`
	UpdatedAt     time.Time `bson:"updated_at"`
}

type claimAuthDoc struct {
	PacketID      string    `bson:"packet_id"`
	ClaimerWallet string    `bson:"claimer_wallet"`
	UserID        string    `bson:"user_id"`
	AuthNonce     string    `bson:"auth_nonce"`
	RandomSeed    string    `bson:"random_seed"`
	Deadline      int64     `bson:"deadline"`
	Signature     string    `bson:"signature"`
	CreatedAt     time.Time `bson:"created_at"`
}

type cursorDoc struct {
	ID          string `bson:"_id"`
	LastBlock   int64  `bson:"last_block"`
}

func redPacketDocToPb(d *redPacketDoc) *pbredpacket.RedPacketRecord {
	return &pbredpacket.RedPacketRecord{
		BizId:           d.BizID,
		PacketId:        d.PacketID,
		ChainId:         d.ChainID,
		ContractAddress: d.ContractAddress,
		CreatorUserId:   d.CreatorUserID,
		CreatorWallet:   d.CreatorWallet,
		PacketType:      int32(d.PacketType),
		Token:           d.Token,
		TotalAmount:     d.TotalAmount,
		TotalShares:     int32(d.TotalShares),
		ExpiryAt:        d.ExpiryAt,
		TxHash:          d.TxHash,
		Status:          d.Status,
		Chain:           d.Chain,
		CreatedAt:       d.CreatedAt.Unix(),
		UpdatedAt:       d.UpdatedAt.Unix(),
	}
}

func claimDocToPb(d *claimDoc) *pbredpacket.ClaimRecord {
	return &pbredpacket.ClaimRecord{
		PacketId:      d.PacketID,
		ClaimerWallet: d.ClaimerWallet,
		AuthNonce:     d.AuthNonce,
		ClaimTxHash:   d.ClaimTxHash,
		ClaimedAmount: d.ClaimedAmount,
		BlockNumber:   d.BlockNumber,
		Status:        d.Status,
		CreatedAt:     d.CreatedAt.Unix(),
		UpdatedAt:     d.UpdatedAt.Unix(),
	}
}
