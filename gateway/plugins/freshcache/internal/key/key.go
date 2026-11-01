package key

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// lp length-prefixes a string to guarantee collision resistance across variable-length fields.
func lp(s string) string {
	return strconv.Itoa(len(s)) + ":" + s
}

// Compute produces a deterministic SHA-256 cache key including tenant isolation (ADR-006).
func Compute(tenantID, subject, intent string, windowDays int, promptVersion, model string) string {
	h := sha256.New()
	h.Write([]byte(lp(tenantID)))
	h.Write([]byte(lp(subject)))
	h.Write([]byte(lp(intent)))
	h.Write([]byte(lp(strconv.Itoa(windowDays))))
	h.Write([]byte(lp(promptVersion)))
	h.Write([]byte(lp(model)))
	return hex.EncodeToString(h.Sum(nil))
}
