// Copyright © 2026 OpenIM. All rights reserved.
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

package openmls

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/mcontext"
)

// putVarint encodes an RFC 9000 variable-length integer (the inverse of
// readVarint) so tests can build TLS opaque<V> vectors.
func putVarint(v uint64) []byte {
	switch {
	case v < 1<<6:
		return []byte{byte(v)}
	case v < 1<<14:
		return []byte{0x40 | byte(v>>8), byte(v)}
	case v < 1<<30:
		return []byte{0x80 | byte(v>>24), byte(v >> 16), byte(v >> 8), byte(v)}
	default:
		return []byte{
			0xc0 | byte(v>>56), byte(v >> 48), byte(v >> 40), byte(v >> 32),
			byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v),
		}
	}
}

func putOpaque(b []byte) []byte {
	return append(putVarint(uint64(len(b))), b...)
}

// buildKeyPackage assembles a minimal RFC 9420 KeyPackage TLS blob carrying the
// given leaf signature key and BasicCredential identity bytes.
func buildKeyPackage(signatureKey, identity []byte) []byte {
	var out []byte
	out = append(out, 0x00, 0x01) // version
	out = append(out, 0x00, 0x01) // cipher_suite
	out = append(out, putOpaque([]byte("init-key"))...)
	out = append(out, putOpaque([]byte("encryption-key"))...) // leaf_node.encryption_key
	out = append(out, putOpaque(signatureKey)...)             // leaf_node.signature_key
	out = append(out, 0x00, credentialTypeBasic)              // credential_type = basic
	out = append(out, putOpaque(identity)...)                 // BasicCredential.identity
	out = append(out, 0xde, 0xad, 0xbe, 0xef)                 // trailing bytes (capabilities, etc.)
	return out
}

func newTestServer(t *testing.T) (*openMLSServer, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate signing key: %v", err)
	}
	cfg := &Config{}
	cfg.RpcConfig.SigningKey.Issuer = "test-issuer"
	cfg.RpcConfig.SigningKey.KeyID = "v1"
	s := &openMLSServer{
		config:     cfg,
		signingKey: priv,
		rootPubKey: base64.StdEncoding.EncodeToString(pub),
	}
	return s, priv
}

func TestExtractLeafCredential(t *testing.T) {
	sigKey := []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	identity := []byte("this-is-a-credential-identity-that-is-longer-than-sixty-four-bytes-to-exercise-2-byte-length")
	kp := buildKeyPackage(sigKey, identity)

	cs, gotSig, gotID, err := extractLeafCredential(kp)
	if err != nil {
		t.Fatalf("extractLeafCredential: %v", err)
	}
	// buildKeyPackage writes 0x00 0x01 for cipher_suite
	if cs != 0x0001 {
		t.Errorf("ciphersuite: got 0x%04x want 0x0001", cs)
	}
	if string(gotSig) != string(sigKey) {
		t.Errorf("signature key mismatch: got %q want %q", gotSig, sigKey)
	}
	if string(gotID) != string(identity) {
		t.Errorf("identity mismatch: got %q want %q", gotID, identity)
	}
}

func TestExtractLeafCredentialTruncated(t *testing.T) {
	if _, _, _, err := extractLeafCredential([]byte{0x00, 0x01}); err == nil {
		t.Fatal("expected error for truncated keyPackage, got nil")
	}
}

func issueTestCredential(t *testing.T, s *openMLSServer, userID, deviceID, platform string, leafPub []byte) string {
	t.Helper()
	ctx := mcontext.WithOpUserIDContext(context.Background(), userID)
	resp, err := s.IssueCredential(ctx, &pbopenmls.IssueCredentialReq{
		UserID:        userID,
		DeviceID:      deviceID,
		Platform:      platform,
		LeafPublicKey: base64.StdEncoding.EncodeToString(leafPub),
	})
	if err != nil {
		t.Fatalf("IssueCredential: %v", err)
	}
	return resp.Credential
}

func TestVerifyKeyPackageCredential(t *testing.T) {
	s, _ := newTestServer(t)
	leafPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	cred := issueTestCredential(t, s, "alice", "device-1", "iOS", leafPub)
	kp := buildKeyPackage(leafPub, []byte(cred))

	t.Run("valid", func(t *testing.T) {
		meta, err := s.verifyKeyPackageCredential(context.Background(), kp, "alice", "device-1")
		if err != nil {
			t.Fatalf("expected valid credential, got error: %v", err)
		}
		if meta.Platform != "iOS" {
			t.Errorf("expected platform iOS, got %q", meta.Platform)
		}
		if meta.Ciphersuite != "0x0001" {
			t.Errorf("expected ciphersuite 0x0001, got %q", meta.Ciphersuite)
		}
	})

	t.Run("wrong user", func(t *testing.T) {
		if _, err := s.verifyKeyPackageCredential(context.Background(), kp, "bob", "device-1"); err == nil {
			t.Fatal("expected identity mismatch error, got nil")
		}
	})

	t.Run("wrong device", func(t *testing.T) {
		if _, err := s.verifyKeyPackageCredential(context.Background(), kp, "alice", "device-2"); err == nil {
			t.Fatal("expected identity mismatch error, got nil")
		}
	})

	t.Run("leaf key mismatch", func(t *testing.T) {
		otherLeaf, _, _ := ed25519.GenerateKey(rand.Reader)
		kpBad := buildKeyPackage(otherLeaf, []byte(cred))
		if _, err := s.verifyKeyPackageCredential(context.Background(), kpBad, "alice", "device-1"); err == nil {
			t.Fatal("expected leaf key binding error, got nil")
		}
	})

	t.Run("tampered credential", func(t *testing.T) {
		tampered := []byte(cred)
		tampered[len(tampered)-2] ^= 0xff
		kpBad := buildKeyPackage(leafPub, tampered)
		if _, err := s.verifyKeyPackageCredential(context.Background(), kpBad, "alice", "device-1"); err == nil {
			t.Fatal("expected signature verification error, got nil")
		}
	})

	t.Run("signing disabled skips verification", func(t *testing.T) {
		disabled := &openMLSServer{config: &Config{}}
		if _, err := disabled.verifyKeyPackageCredential(context.Background(), []byte("not-a-keypackage"), "alice", "device-1"); err != nil {
			t.Fatalf("expected skip when signing key absent, got error: %v", err)
		}
	})
}
