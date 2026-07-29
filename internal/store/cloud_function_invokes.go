package store

import "sync"

var (
	cloudFunctionInvokeMu   sync.Mutex
	cloudFunctionInvokeMsgs []cloudFunctionInvoke
)

type cloudFunctionInvoke struct {
	Function string
	Body     string
}

// RecordCloudFunctionInvoke appends an in-process Eventarc→Functions delivery.
func RecordCloudFunctionInvoke(functionName, body string) {
	cloudFunctionInvokeMu.Lock()
	defer cloudFunctionInvokeMu.Unlock()
	cloudFunctionInvokeMsgs = append(cloudFunctionInvokeMsgs, cloudFunctionInvoke{
		Function: functionName,
		Body:     body,
	})
}

// ListCloudFunctionInvokes returns recorded function names and payloads (tests).
func ListCloudFunctionInvokes() []struct{ Function, Body string } {
	cloudFunctionInvokeMu.Lock()
	defer cloudFunctionInvokeMu.Unlock()
	out := make([]struct{ Function, Body string }, len(cloudFunctionInvokeMsgs))
	for i, m := range cloudFunctionInvokeMsgs {
		out[i].Function = m.Function
		out[i].Body = m.Body
	}
	return out
}

// ClearCloudFunctionInvokes resets invoke theatre state (tests).
func ClearCloudFunctionInvokes() {
	cloudFunctionInvokeMu.Lock()
	defer cloudFunctionInvokeMu.Unlock()
	cloudFunctionInvokeMsgs = nil
}
