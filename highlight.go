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

func Start() {
	internal.Start(errorChan)
}

func SetFlushInterval(newFlushInterval int) {
	internal.SetFlushInterval(newFlushInterval)
}

func InterceptRequest(r *http.Request) context.Context {
	return InterceptRequestWithContext(r.Context(), r)
}

func InterceptRequestWithContext(ctx context.Context, r *http.Request) context.Context {
	highlightReqDetails := r.Header.Get("X-Highlight-Request")
	ids := strings.Split(highlightReqDetails, ":")
	if len(ids) < 2 {
		return ctx
	}
	ctx = context.WithValue(ctx, contextKeys.HighlightRequestID, ids[0])
	ctx = context.WithValue(ctx, contextKeys.HighlightSessionID, ids[1])
	return ctx
}

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
