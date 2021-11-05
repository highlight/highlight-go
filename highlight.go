package highlight

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hasura/go-graphql-client"
	"github.com/pkg/errors"
)

var (
	errorChan            chan BackendErrorObjectInput
	flushInterval        int
	client               *graphql.Client
	interruptChan        chan bool
	signalChan           chan os.Signal
	wg                   sync.WaitGroup
	graphqlClientAddress string
)

// contextKey represents the keys that highlight may store in the users' context
// we append every contextKey with Highlight to avoid collisions
type contextKey string

const (
	Highlight       contextKey = "highlight"
	RequestID                  = Highlight + "RequestID"
	SessionSecureID            = Highlight + "SessionSecureID"
)

var (
	ContextKeys = struct {
		RequestID       contextKey
		SessionSecureID contextKey
	}{
		RequestID:       RequestID,
		SessionSecureID: SessionSecureID,
	}
)

// appState is used for keeping track of the current state of the app
// this can determine whether to accept new errors
type appState byte

const (
	idle appState = iota
	started
	stopped
)

var (
	state appState // 0 is idle, 1 is started, 2 is stopped
)

const (
	consumeErrorSessionIDMissing = "context does not contain highlightSessionSecureID; context must have injected values from highlight.InterceptRequest"
	consumeErrorRequestIDMissing = "context does not contain highlightRequestID; context must have injected values from highlight.InterceptRequest"
	consumeErrorWorkerStopped    = "highlight worker stopped"
)

// Logger is an interface that implements Log and Logf
type Logger interface {
	Error(v ...interface{})
	Errorf(format string, v ...interface{})
}

// log is this packages logger
var logger struct {
	Logger
}

// noop default logger
type deadLog struct{}

func (d deadLog) Error(v ...interface{})                 {}
func (d deadLog) Errorf(format string, v ...interface{}) {}

// Requester is used for making graphql requests
// in testing, a mock requester with an overwritten trigger function may be used
type Requester interface {
	trigger([]*BackendErrorObjectInput) error
}

var (
	requester Requester
)

type graphqlRequester struct{}

func (g graphqlRequester) trigger(errorsInput []*BackendErrorObjectInput) error {
	if len(errorsInput) < 1 {
		return nil
	}
	var mutation struct {
		PushBackendPayload string `graphql:"pushBackendPayload(errors: $errors)"`
	}
	variables := map[string]interface{}{
		"errors": errorsInput,
	}

	err := client.Mutate(context.Background(), &mutation, variables)
	if err != nil {
		return err
	}
	return nil
}

type mockRequester struct{}

func (m mockRequester) trigger(errorsInput []*BackendErrorObjectInput) error {
	// NOOP
	_ = errorsInput
	return nil
}

type BackendErrorObjectInput struct {
	SessionSecureID graphql.String  `json:"session_secure_id"`
	RequestID       graphql.String  `json:"request_id"`
	Event           graphql.String  `json:"event"`
	Type            graphql.String  `json:"type"`
	URL             graphql.String  `json:"url"`
	Source          graphql.String  `json:"source"`
	StackTrace      graphql.String  `json:"stackTrace"`
	Timestamp       time.Time       `json:"timestamp"`
	Payload         *graphql.String `json:"payload"`
}

// init gets called once when you import the package
func init() {
	errorChan = make(chan BackendErrorObjectInput, 128)
	interruptChan = make(chan bool, 1)
	signalChan = make(chan os.Signal, 1)

	signal.Notify(signalChan, syscall.SIGABRT, syscall.SIGTERM, syscall.SIGINT)
	SetGraphqlClientAddress("https://pub.highlight.run")
	SetFlushInterval(10)
	SetDebugMode(deadLog{})

	requester = graphqlRequester{}
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
	client = graphql.NewClient(graphqlClientAddress, nil)
	state = started
	go func() {
		for {
			select {
			case <-time.After(time.Duration(flushInterval) * time.Second):
				wg.Add(1)
				flushedErrors := flush()
				wg.Done()
				_ = requester.trigger(flushedErrors)
			case <-interruptChan:
				shutdown()
				return
			case <-signalChan:
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

// SetGraphqlClientAddress allows you to override the graphql client address,
// in case you are running Highlight on-prem, and need to point to your on-prem instance.
func SetGraphqlClientAddress(newGraphqlClientAddress string) {
	graphqlClientAddress = newGraphqlClientAddress
}

func SetDebugMode(l Logger) {
	logger.Logger = l
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
	ctx = context.WithValue(ctx, ContextKeys.SessionSecureID, ids[0])
	ctx = context.WithValue(ctx, ContextKeys.RequestID, ids[1])
	return ctx
}

// ConsumeError adds an error to the queue of errors to be sent to our backend.
// the provided context must have the injected highlight keys from InterceptRequestWithContext.
func ConsumeError(ctx context.Context, errorInput interface{}, tags ...string) error {
	if state == stopped {
		return fmt.Errorf(consumeErrorWorkerStopped)
	}
	defer wg.Done()
	wg.Add(1)
	timestamp := time.Now()
	sessionSecureID := ctx.Value(ContextKeys.SessionSecureID)
	if sessionSecureID == nil {
		return fmt.Errorf(consumeErrorSessionIDMissing)
	}
	requestID := ctx.Value(ContextKeys.RequestID)
	if requestID == nil {
		return fmt.Errorf(consumeErrorRequestIDMissing)
	}

	tagsBytes, err := json.Marshal(tags)
	if err != nil {
		return err
	}
	tagsString := string(tagsBytes)
	convertedError := BackendErrorObjectInput{
		SessionSecureID: graphql.String(fmt.Sprintf("%v", sessionSecureID)),
		RequestID:       graphql.String(fmt.Sprintf("%v", requestID)),
		Type:            "BACKEND",
		Timestamp:       timestamp,
		Payload:         (*graphql.String)(&tagsString),
	}

	switch e := errorInput.(type) {
	case stackTracer:
		stack := e.StackTrace()
		if len(stack) < 1 {
			return fmt.Errorf("no stack frames in stack trace for stackTracer errors")
		}
		var stackFrames []string
		for _, frame := range stack {
			frameBytes, err := frame.MarshalText()
			if err != nil {
				return err
			}
			stackFrames = append(stackFrames, string(frameBytes))
		}
		convertedError.Event = graphql.String(fmt.Sprintf("%v", e.Error()))
		stackFramesBytes, err := json.Marshal(stackFrames)
		if err != nil {
			return err
		}
		convertedError.StackTrace = graphql.String(stackFramesBytes)
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

// stackTracer implements the errors.StackTrace() interface function
type stackTracer interface {
	StackTrace() errors.StackTrace
	Error() string
}

func flush() []*BackendErrorObjectInput {
	tempChanSize := len(errorChan)
	var flushedErrors []*BackendErrorObjectInput
	for i := 0; i < tempChanSize; i++ {
		e := <-errorChan
		if e == (BackendErrorObjectInput{}) {
			continue
		}
		flushedErrors = append(flushedErrors, &e)
	}
	return flushedErrors
}

func shutdown() {
	if state == stopped || state == idle {
		return
	}
	state = stopped
	wg.Wait()
	close(errorChan)
	close(interruptChan)
	close(signalChan)
}
