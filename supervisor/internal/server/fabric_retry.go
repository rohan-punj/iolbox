package server

import (
	"log"
	"strings"
	"time"
)

// Bounded retries for genuinely transient kernel netdev timing (robustness
// item 7). This is defense-in-depth ONLY, layered on top of the ownership and
// serialization fixes (items 1-2) — never a substitute for them.
//
// Scope is deliberately narrow. It wraps `ip tuntap add` (EnsureTap) and
// `ip link set <tap> master <br>` (Attach), where an EBUSY/EAGAIN genuinely
// does clear on its own: the kernel's teardown of a netdev that was just
// deleted or just left another bridge is asynchronous, so an immediate
// recreate/re-enslave of the same name can transiently fail while nothing is
// actually wrong. It is NOT wrapped around iouyap's openTap/TUNSETIFF, which
// is the path where EBUSY means a live pump still owns the tap — retrying
// there would mask exactly the ownership bug finding #1 fixed, and would never
// succeed inside any sane retry budget anyway.
const fabricRetryAttempts = 3

// fabricRetryBaseDelay is the first backoff step; it doubles per attempt, so
// the worst-case added latency for a call that ultimately fails is
// 25ms + 50ms = 75ms. That ceiling matters because these calls run
// synchronously inside request handlers under the lab mutex, once per tap and
// per link — an unbounded or generous budget would turn a large lab's start
// into a visible hang.
const fabricRetryBaseDelay = 25 * time.Millisecond

// retryTransientFabric runs op, retrying only errors isTransientFabricError
// classifies as transient, up to fabricRetryAttempts times with exponential
// backoff. A non-transient error returns on the first attempt so real failures
// surface at full speed.
func retryTransientFabric(op func() error) error {
	var err error
	for attempt := 0; attempt < fabricRetryAttempts; attempt++ {
		err = op()
		if err == nil {
			if attempt > 0 {
				log.Printf("fabric: transient error cleared after %d retries", attempt)
			}
			return nil
		}
		if !isTransientFabricError(err) || attempt == fabricRetryAttempts-1 {
			return err
		}
		time.Sleep(fabricRetryBaseDelay << attempt)
	}
	return err
}

// isTransientFabricError classifies an `ip`/netlink failure as retry-worthy.
// The fabric manager shells out to `ip`, so the errno arrives as text in the
// command's stderr rather than as a wrapped syscall.Errno — hence the string
// match on the three strerror spellings the kernel produces for EBUSY and
// EAGAIN.
func isTransientFabricError(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "device or resource busy") ||
		strings.Contains(s, "resource temporarily unavailable") ||
		strings.Contains(s, "try again")
}
