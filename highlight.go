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
	errorChan     chan backendErrorObjectInput
	flushInterval int
	client        *graphql.Client
	interruptChan chan bool
	canceled      bool

	ContextKeys = struct {
		RequestID string
		SessionID string
	}{
		RequestID: RequestID,
		SessionID: SessionID,
	}
)

const (
	Highlight = "highlight"
	RequestID = Highlight + "RequestID"
	SessionID = Highlight + "SessionID"
)

type backendErrorObjectInput struct {
	SessionID  string    `json:"session_id"`
	RequestID  string    `json:"request_id"`
	Event      string    `json:"event"`
	Type       string    `json:"type"`
	URL        string    `json:"url"`
	Source     string    `json:"source"`
	StackTrace string    `json:"stackTrace"`
	Timestamp  time.Time `json:"timestamp"`
	Payload    *string   `json:"payload"`
}

// init gets called once when you import the package
func init() {
	errorChan = make(chan backendErrorObjectInput, 128)
	interruptChan = make(chan bool, 1)

	client = graphql.NewClient("https://pub.highlight.run", nil)
	SetFlushInterval(10)
}

// Start is used to start the Highlight client's collection service.
func Start() {
	StartWithContext(context.Background())
}

// StartWithContext is used to start the Highlight client's collection
// service, but allows the user to pass in their own context.Context.
// This allows the user kill the highlight worker by canceling their context.CancelFunc.
func StartWithContext(ctx context.Context) {
	go func() {
		for {
			select {
			case <-time.After(time.Duration(flushInterval) * time.Second):
				tempChanSize := len(errorChan)
				var flushedErrors []backendErrorObjectInput
				for i := 0; i < tempChanSize; i++ {
					flushedErrors = append(flushedErrors, <-errorChan)
				}
				makeRequest(flushedErrors)
			case <-interruptChan:
				shutdown()
				return
			case <-ctx.Done():
				shutdown()
				return
			}
		}
	}()
}

// Stop sends an interrupt signal to the main process, closing the channels and returning the goroutines.
func Stop() {
	if canceled {
		return
	}
	interruptChan <- true
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
	ctx = context.WithValue(ctx, ContextKeys.SessionID, ids[0])
	ctx = context.WithValue(ctx, ContextKeys.RequestID, ids[1])
	return ctx
}

// ConsumeError adds an error to the queue of errors to be sent to our backend.
// the provided context must have the injected highlight keys from InterceptRequestWithContext.
func ConsumeError(ctx context.Context, errorInput interface{}, tags ...string) error {
	timestamp := time.Now()
	sessionID := ctx.Value(ContextKeys.SessionID)
	if sessionID == nil {
		return fmt.Errorf("context does not contain highlightSessionID; context must have injected values from highlight.InterceptRequest")
	}
	requestID := ctx.Value(ContextKeys.RequestID)
	if requestID == nil {
		return fmt.Errorf("context does not contain highlightRequestID; context must have injected values from highlight.InterceptRequest")
	}

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	tagsString := string(tagsBytes)
	convertedError := backendErrorObjectInput{
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

func makeRequest(errorsInput []backendErrorObjectInput) {
	if len(errorsInput) < 1 {
		return
	}
	var mutation struct {
		PushBackendPayload struct {
		} `graphql:"pushBackendPayload(errors: $errors)"`
	}
	variables := map[string]interface{}{
		"errors": errorsInput,
	}

	err := client.Mutate(context.Background(), &mutation, variables)
	if err != nil {
		return
	}
}

func shutdown() {
	if canceled {
		return
	}
	close(errorChan)
	close(interruptChan)
	canceled = true
}
