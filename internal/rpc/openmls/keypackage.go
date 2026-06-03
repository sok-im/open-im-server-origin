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

import "github.com/openimsdk/tools/errs"

// This file implements a minimal, read-only decoder for the RFC 9420 (MLS)
// KeyPackage TLS serialization. It does NOT validate MLS semantics — the
// authoritative MLS logic lives in the client (OpenMLS/Rust). The server only
// needs to reach two fields in order to verify the server-issued credential:
//
//	KeyPackage.leaf_node.signature_key  — the leaf's Ed25519 public key
//	KeyPackage.leaf_node.credential     — a BasicCredential whose identity bytes
//	                                      carry the credential envelope we issued
//
// Variable-length vectors use the RFC 9420 §2.1.2 length prefix, which is the
// QUIC variable-length integer encoding (RFC 9000 §16): the two most
// significant bits of the first byte select a 1/2/4/8-byte length field.

const credentialTypeBasic = 1

var errKeyPackageTruncated = errs.New("keyPackage TLS data is truncated").Wrap()

// readVarint decodes an RFC 9000 variable-length integer at b[off], returning
// the value and the number of bytes it occupied.
func readVarint(b []byte, off int) (val uint64, n int, err error) {
	if off < 0 || off >= len(b) {
		return 0, 0, errKeyPackageTruncated
	}
	n = 1 << (b[off] >> 6) // 1, 2, 4 or 8 bytes
	if off+n > len(b) {
		return 0, 0, errKeyPackageTruncated
	}
	val = uint64(b[off] & 0x3f)
	for i := 1; i < n; i++ {
		val = (val << 8) | uint64(b[off+i])
	}
	return val, n, nil
}

// readOpaque decodes a variable-length opaque<V> vector at b[off], returning its
// contents and the offset immediately after the vector.
func readOpaque(b []byte, off int) (data []byte, newOff int, err error) {
	length, n, err := readVarint(b, off)
	if err != nil {
		return nil, 0, err
	}
	start := off + n
	end := start + int(length)
	if end < start || end > len(b) {
		return nil, 0, errKeyPackageTruncated
	}
	return b[start:end], end, nil
}

// extractLeafCredential parses just enough of a TLS-serialized KeyPackage to
// return the ciphersuite, leaf signature public key, and BasicCredential identity
// bytes.
//
// KeyPackage layout (RFC 9420 §10):
//
//	uint16 version
//	uint16 cipher_suite
//	opaque init_key<V>
//	LeafNode leaf_node {
//	    opaque encryption_key<V>
//	    opaque signature_key<V>
//	    Credential credential { uint16 credential_type; opaque identity<V> (basic) }
//	    ... (capabilities, lifetime, extensions, signature — not parsed)
//	}
func extractLeafCredential(kp []byte) (ciphersuite uint16, signatureKey, credentialIdentity []byte, err error) {
	off := 0
	// version (uint16) + cipher_suite (uint16)
	if off+4 > len(kp) {
		return 0, nil, nil, errKeyPackageTruncated
	}
	ciphersuite = uint16(kp[2])<<8 | uint16(kp[3])
	off += 4
	// init_key
	if _, off, err = readOpaque(kp, off); err != nil {
		return 0, nil, nil, err
	}
	// leaf_node.encryption_key
	if _, off, err = readOpaque(kp, off); err != nil {
		return 0, nil, nil, err
	}
	// leaf_node.signature_key
	if signatureKey, off, err = readOpaque(kp, off); err != nil {
		return 0, nil, nil, err
	}
	// leaf_node.credential.credential_type (uint16)
	if off+2 > len(kp) {
		return 0, nil, nil, errKeyPackageTruncated
	}
	credType := uint16(kp[off])<<8 | uint16(kp[off+1])
	off += 2
	if credType != credentialTypeBasic {
		return 0, nil, nil, errs.New("unsupported MLS credential type (only basic is supported)").Wrap()
	}
	// BasicCredential.identity
	if credentialIdentity, _, err = readOpaque(kp, off); err != nil {
		return 0, nil, nil, err
	}
	return ciphersuite, signatureKey, credentialIdentity, nil
}
