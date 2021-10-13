package highlight

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	internal "github.com/highlight-run/highlight-go/internal/errors"
)

var (
	errorChan chan internal.BackendErrorObjectInput

	contextKeys = struct {
		HighlightRequestID string
		HighlightSessionID string
	}{
		HighlightRequestID: "highlightRequestID",
		HighlightSessionID: "highlightSessionID",
	}
)

func init() {
	errorChan = make(chan internal.BackendErrorObjectInput)
	SetFlushInterval(10)
}

// Start is used to start the Highlight client's collection service.
// To use it, run `go highlight.Start()` once in your app.
func Start() {
	internal.Start(errorChan)
}

// SetFlushInterval allows you to override the amount of time in which the
// Highlight client will collect errors before sending them to our backend.
// - newFlushInterval is an integer representing seconds
func SetFlushInterval(newFlushInterval int) {
	internal.SetFlushInterval(newFlushInterval)
}

// InterceptRequest calls InterceptRequestWithContext using the request object's context
func InterceptRequest(r *http.Request) context.Context {
	return InterceptRequestWithContext(r.Context(), r)
}

// InterceptRequestWithContext captures the highlight session and request ID
// for a particular request from the request headers, adding the values to the provided context.
func InterceptRequestWithContext(ctx context.Context, r *http.Request) context.Context {
	highlightReqDetails := r.Header.Get("X-Highlight-Request")
	ids := strings.Split(highlightReqDetails, "/")
	if len(ids) < 2 {
		return ctx
	}
	ctx = context.WithValue(ctx, contextKeys.HighlightSessionID, ids[0])
	ctx = context.WithValue(ctx, contextKeys.HighlightRequestID, ids[1])
	return ctx
}

// ConsumeError adds an error to the queue of errors to be sent to our backend.
// the provided context must have the injected highlight keys from InterceptRequestWithContext.
func ConsumeError(ctx context.Context, errorInput interface{}, tags ...string) error {
	timestamp := time.Now()
	sessionID := ctx.Value(contextKeys.HighlightSessionID)
	if sessionID == nil {
		return fmt.Errorf("context does not contain highlightSessionID; context must have injected values from highlight.InterceptRequest")
	}
	requestID := ctx.Value(contextKeys.HighlightRequestID)
	if requestID == nil {
		return fmt.Errorf("context does not contain highlightRequestID; context must have injected values from highlight.InterceptRequest")
	}

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	tagsString := string(tagsBytes)
	convertedError := internal.BackendErrorObjectInput{
		SessionID: fmt.Sprintf("%v", sessionID),
		RequestID: fmt.Sprintf("%v", requestID),
		Type:      "BACKEND",
		Timestamp: timestamp,
		Payload:   &tagsString,
	}
	switch e := errorInput.(type) {
	case error:
		convertedError.Event = e.Error()
		convertedError.StackTrace = e.Error()
	default:
		convertedError.Event = fmt.Sprintf("%v", e)
		convertedError.StackTrace = fmt.Sprintf("%v", e)
	}
	errorChan <- convertedError
	return nil
}
