package compute

import (
	"fmt"
	"os"
	"strings"
)

// EnvNestedEngineFailClosed opts into hard errors when Cloud SQL, Managed Kafka,
// or Memorystore Redis nested engine start fails (1/true).
// Default unset: soft-fail to theatre metadata (unit tests without DinD stay green).
// Distinct from EnvNestedInvokeFailClosed (Cloud Run :invoke only).
const EnvNestedEngineFailClosed = "NOCTAXRIS_GCP_NESTED_ENGINE_FAIL_CLOSED"

// NestedEngineFailClosed reports whether EnvNestedEngineFailClosed is truthy.
func NestedEngineFailClosed() bool {
	v := strings.TrimSpace(os.Getenv(EnvNestedEngineFailClosed))
	return v == "1" || strings.EqualFold(v, "true")
}

// NestedEngineFailClosedMessage builds a clear REST error message for create failures.
func NestedEngineFailClosedMessage(err error) string {
	if err == nil {
		return "nested engine start failed"
	}
	return fmt.Sprintf("nested engine start failed: %v", err)
}
