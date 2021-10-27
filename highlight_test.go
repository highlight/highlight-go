package highlight

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/pkg/errors"
)

// TestConsumeError tests every case for ConsumeError
func TestConsumeError(t *testing.T) {
	requester = mockRequester{}
	ctx := context.Background()
	ctx = context.WithValue(ctx, ContextKeys.SessionSecureID, "0")
	ctx = context.WithValue(ctx, ContextKeys.RequestID, "0")
	tests := map[string]struct {
		errorInput         interface{}
		contextInput       context.Context
		tags               []string
		expectedEvent      string
		expectedStackTrace string
		expectedError      error
	}{
		"test builtin error":                                {contextInput: ctx, errorInput: fmt.Errorf("error here"), expectedEvent: "error here", expectedStackTrace: "error here"},
		"test builtin error with invalid context":           {contextInput: context.Background(), errorInput: fmt.Errorf("error here"), expectedError: fmt.Errorf(consumeErrorSessionIDMissing)},
		"test simple github.com/pkg/errors error":           {contextInput: ctx, errorInput: errors.New("error here"), expectedEvent: "error here", expectedStackTrace: "[github.com/highlight-run/highlight-go.TestConsumeError /Users/cameronbrill/Projects/work/Highlight/highlight-go/highlight_test.go:27 testing.tRunner /usr/local/opt/go/libexec/src/testing/testing.go:1259 runtime.goexit /usr/local/opt/go/libexec/src/runtime/asm_amd64.s:1581]"},
		"test github.com/pkg/errors error with stack trace": {contextInput: ctx, errorInput: errors.Wrap(errors.New("error here"), "error there"), expectedEvent: "error there: error here", expectedStackTrace: "[github.com/highlight-run/highlight-go.TestConsumeError /Users/cameronbrill/Projects/work/Highlight/highlight-go/highlight_test.go:28 testing.tRunner /usr/local/opt/go/libexec/src/testing/testing.go:1259 runtime.goexit /usr/local/opt/go/libexec/src/runtime/asm_amd64.s:1581]"},
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			Start()
			err := ConsumeError(input.contextInput, input.errorInput, input.tags...)
			if err != nil {
				if input.expectedError == nil || err.Error() != input.expectedError.Error() {
					t.Errorf("received error not equal to expected error: %v != %v", err, input.expectedError)
				}
				return
			}
			a := flush()
			if len(a) != 1 {
				t.Errorf("flush returned the wrong number of errors")
				return
			}
			if string(a[0].Event) != input.expectedEvent {
				t.Errorf("event not equal to expected event: %v != %v", a[0].Event, input.expectedEvent)
			}
			if string(a[0].StackTrace) != input.expectedStackTrace && !strings.Contains(string(a[0].StackTrace), "highlight_test.go") {
				t.Errorf("stack trace not equal to expected stack trace: %v != %v", a[0].StackTrace, input.expectedStackTrace)
			}
		})
	}
	Stop()
}
