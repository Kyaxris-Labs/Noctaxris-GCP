package store

import "sync"

var (
	httpCatcherMu   sync.Mutex
	httpCatcherMsgs []string
)

// RecordHTTPCatcher appends a lab HTTP catcher delivery
// (Pub/Sub push, Eventarc, Scheduler, Cloud Tasks, or POST accept).
func RecordHTTPCatcher(body string) {
	httpCatcherMu.Lock()
	defer httpCatcherMu.Unlock()
	httpCatcherMsgs = append(httpCatcherMsgs, body)
}

// ListHTTPCatcher returns a copy of recorded catcher payloads.
func ListHTTPCatcher() []string {
	httpCatcherMu.Lock()
	defer httpCatcherMu.Unlock()
	out := make([]string, len(httpCatcherMsgs))
	copy(out, httpCatcherMsgs)
	return out
}

// ClearHTTPCatcher resets catcher state (tests).
func ClearHTTPCatcher() {
	httpCatcherMu.Lock()
	defer httpCatcherMu.Unlock()
	httpCatcherMsgs = nil
}
