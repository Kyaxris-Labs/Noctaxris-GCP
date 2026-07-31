package restlab

import (
	"net/http"
	"sync"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

// LocationOperationGetHook handles GET .../locations/{loc}/operations/{operation} for a
// specific lab product (for example Managed Kafka). Return true when the hook wrote a response.
type LocationOperationGetHook func(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, opID string) bool

var (
	locationOpGetHooksMu sync.RWMutex
	locationOpGetHooks   []LocationOperationGetHook
)

// RegisterLocationOperationGetHook adds a shared location Operations.get handler.
// Memorystore registers the mux route once; hooks extend polling for other products.
func RegisterLocationOperationGetHook(h LocationOperationGetHook) {
	locationOpGetHooksMu.Lock()
	defer locationOpGetHooksMu.Unlock()
	locationOpGetHooks = append(locationOpGetHooks, h)
}

// DispatchLocationOperationGetHooks runs registered hooks in registration order.
func DispatchLocationOperationGetHooks(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, opID string) bool {
	locationOpGetHooksMu.RLock()
	hooks := append([]LocationOperationGetHook(nil), locationOpGetHooks...)
	locationOpGetHooksMu.RUnlock()
	for _, h := range hooks {
		if h(w, r, p, project, location, opID) {
			return true
		}
	}
	return false
}

// ClearLocationOperationGetHooks removes registered hooks (unit tests only).
func ClearLocationOperationGetHooks() {
	locationOpGetHooksMu.Lock()
	defer locationOpGetHooksMu.Unlock()
	locationOpGetHooks = nil
}
