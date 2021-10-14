package highlight

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/hasura/go-graphql-client"
)

var (
	errorChan     chan BackendErrorObjectInput
	flushInterval int
	client        *graphql.Client

	ContextKeys = struct {
		HighlightRequestID string
		HighlightSessionID string
	}{
		HighlightRequestID: HighlightRequestID,
		HighlightSessionID: HighlightSessionID,
	}
)

const (
	HighlightRequestID = "highlightRequestID"
	HighlightSessionID = "highlightSessionID"
)

type BackendErrorObjectInput struct {
	SessionID  graphql.String  `json:"session_id"`
	RequestID  graphql.String  `json:"request_id"`
	Event      graphql.String  `json:"event"`
	Type       graphql.String  `json:"type"`
	URL        graphql.String  `json:"url"`
	Source     graphql.String  `json:"source"`
	StackTrace graphql.String  `json:"stackTrace"`
	Timestamp  time.Time       `json:"timestamp"`
	Payload    *graphql.String `json:"payload"`
}

// init gets called once when you import the package
func init() {
	errorChan = make(chan BackendErrorObjectInput, 128)
	client = graphql.NewClient("https://pub.highlight.run", nil)
	SetFlushInterval(10)
}

// Start is used to start the Highlight client's collection service.
// To use it, run `go highlight.Start()` once in your app.
func Start() {
	go func() {
		for {
			time.Sleep(time.Duration(flushInterval) * time.Second)
			tempChanSize := len(errorChan)
			fmt.Println("flushing: ", tempChanSize)
			var flushedErrors []*BackendErrorObjectInput
			for i := 0; i < tempChanSize; i++ {
				e := <-errorChan
				flushedErrors = append(flushedErrors, &e)
			}
			makeRequest(flushedErrors)
		}
	}()
}

// SetFlushInterval allows you to override the amount of time in which the
// Highlight client will collect errors before sending them to our backend.
// - newFlushInterval is an integer representing seconds
func SetFlushInterval(newFlushInterval int) {
	flushInterval = newFlushInterval
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
	ctx = context.WithValue(ctx, ContextKeys.HighlightSessionID, ids[0])
	ctx = context.WithValue(ctx, ContextKeys.HighlightRequestID, ids[1])
	return ctx
}

// ConsumeError adds an error to the queue of errors to be sent to our backend.
// the provided context must have the injected highlight keys from InterceptRequestWithContext.
func ConsumeError(ctx context.Context, errorInput interface{}, tags ...string) error {
	timestamp := time.Now()
	sessionID := ctx.Value(ContextKeys.HighlightSessionID)
	if sessionID == nil {
		return fmt.Errorf("context does not contain highlightSessionID; context must have injected values from highlight.InterceptRequest")
	}
	requestID := ctx.Value(ContextKeys.HighlightRequestID)
	if requestID == nil {
		return fmt.Errorf("context does not contain highlightRequestID; context must have injected values from highlight.InterceptRequest")
	}

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	tagsString := string(tagsBytes)
	convertedError := BackendErrorObjectInput{
		SessionID: graphql.String(fmt.Sprintf("%v", sessionID)),
		RequestID: graphql.String(fmt.Sprintf("%v", requestID)),
		Type:      "BACKEND",
		Timestamp: timestamp,
		Payload:   (*graphql.String)(&tagsString),
	}
	switch e := errorInput.(type) {
	case error:
		convertedError.Event = graphql.String(e.Error())
		convertedError.StackTrace = graphql.String(e.Error())
	default:
		convertedError.Event = graphql.String(fmt.Sprintf("%v", e))
		convertedError.StackTrace = graphql.String(fmt.Sprintf("%v", e))
	}
	errorChan <- convertedError
	return nil
}

func makeRequest(errorsInput []*BackendErrorObjectInput) {
	if len(errorsInput) < 1 {
		return
	}
	var mutation struct {
		PushBackendPayload string `graphql:"pushBackendPayload(errors: $errors)"`
	}
	variables := map[string]interface{}{
		"errors": errorsInput,
	}

	err := client.Mutate(context.Background(), &mutation, variables)
	if err != nil {
		return
	}
}
