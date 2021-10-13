package errors

import (
	"context"
	"time"

	"github.com/hasura/go-graphql-client"
)

var (
	flushInterval int
	client        *graphql.Client
)

type BackendErrorObjectInput struct {
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

func SetFlushInterval(newFlushInterval int) {
	flushInterval = newFlushInterval
}

func Start(errorChan <-chan BackendErrorObjectInput) {
	for {
		time.Sleep(time.Duration(flushInterval) * time.Second)
		tempChanSize := len(errorChan)
		var flushedErrors []BackendErrorObjectInput
		for i := 0; i < tempChanSize; i++ {
			flushedErrors = append(flushedErrors, <-errorChan)
		}
		makeRequest(flushedErrors)
	}
}

func init() {
	client = graphql.NewClient("https://pub.highlight.run", nil)
}

func makeRequest(errorsInput []BackendErrorObjectInput) {
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
