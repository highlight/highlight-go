package highlight

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hasura/go-graphql-client"
)

var (
	errorChan     chan BackendErrorObjectInput
	flushInterval int
	client        *graphql.Client
	interruptChan chan bool
	wg            sync.WaitGroup
	state         appState // 0 is idle, 1 is started, 2 is stopped

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

type appState byte

const (
	idle    appState = 0
	started appState = 1
	stopped appState = 2
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
	if state == started {
		return
	}
	state = started
	go func() {
		for {
			select {
			case <-time.After(time.Duration(flushInterval) * time.Second):
				time.Sleep(time.Duration(flushInterval) * time.Second)
				wg.Add(1)
				tempChanSize := len(errorChan)
				var flushedErrors []*BackendErrorObjectInput
				for i := 0; i < tempChanSize; i++ {
					e := <-errorChan
					if e == (BackendErrorObjectInput{}) {
						continue
					}
					flushedErrors = append(flushedErrors, &e)
				}
				wg.Done()
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
	if state == stopped || state == idle {
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
	if state == stopped {
		return fmt.Errorf("highlight worker stopped")
	}
	defer wg.Done()
	wg.Add(1)
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

func shutdown() {
	if state == stopped || state == idle {
		return
	}
	state = stopped
	wg.Wait()
	close(errorChan)
	close(interruptChan)
}
