package lansync

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Append-only suffix delta.
//
// Codex/Claude session files are append-only logs: when a session continues, its
// file grows by new trailing bytes while the existing prefix is unchanged. That
// makes a true byte-level "send only what's new" delta possible and safe, *as long
// as it is fully verified*: the receiver must prove its current file is exactly the
// sender's prefix before appending, and prove the appended result matches the
// sender's whole-file hash afterwards. The sender can establish the precondition
// using only the (size, sha256) the receiver already advertises in its manifest —
// no extra round-trip — because if sha256(senderFile[:receiverSize]) == receiverSHA
// then the receiver's file is byte-identical to the sender's prefix.
//
// This file is the pure, unit-tested core of that scheme. It is deliberately
// transport-agnostic so it can be verified in-process. The live LAN transfer keeps
// using the bundle+import(--merge) path (which additionally inherits checksums,
// conflict handling, cwd remapping, mtime preservation and the Claude layout); this
// suffix codec is the foundation for a future wire-level optimization and is what a
// continuous daemon would use for the common "a session just grew" case.

// DeltaPlan describes how to bring a receiver's copy of one session up to date.
type DeltaPlan struct {
	// Append is true when the receiver holds an exact prefix and only the trailing
	// Suffix bytes need to be sent and appended. When false the whole file must be
	// sent (the receiver lacks it, or its copy diverged and is not a clean prefix).
	Append bool
	Suffix []byte // bytes to append (Append==true)
	Full   []byte // whole file to send (Append==false)
	// SHA256 is the sender's whole-file hash, which the receiver verifies after
	// applying, so a corrupt or mis-targeted transfer can never be committed.
	SHA256 string
}

// PlanDelta computes how to update a receiver, given the sender's current file
// bytes and the receiver's advertised (size, sha256). receiverSize<0 (or an empty
// receiverSHA) means the receiver does not have the session at all.
func PlanDelta(senderFile []byte, receiverSize int64, receiverSHA string) DeltaPlan {
	full := sha256Hex(senderFile)
	// Receiver has a clean, strictly-shorter prefix -> append only the new tail.
	if receiverSHA != "" && receiverSize > 0 && int64(len(senderFile)) > receiverSize {
		if sha256Hex(senderFile[:receiverSize]) == receiverSHA {
			return DeltaPlan{Append: true, Suffix: senderFile[receiverSize:], SHA256: full}
		}
	}
	// Otherwise send the whole thing (new session, equal, shorter, or diverged).
	return DeltaPlan{Append: false, Full: senderFile, SHA256: full}
}

// ApplyDelta reconstructs the sender's file on the receiver. For an append it
// requires current to be exactly the known prefix; in every case it verifies the
// result matches the sender's advertised SHA before returning it, so a bad delta
// is rejected rather than written.
func ApplyDelta(current []byte, plan DeltaPlan) ([]byte, error) {
	var result []byte
	if plan.Append {
		result = make([]byte, 0, len(current)+len(plan.Suffix))
		result = append(result, current...)
		result = append(result, plan.Suffix...)
	} else {
		result = plan.Full
	}
	if got := sha256Hex(result); got != plan.SHA256 {
		return nil, fmt.Errorf("delta verification failed: reconstructed sha %s != expected %s", got, plan.SHA256)
	}
	return result, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
