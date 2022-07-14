package highlight

// This schema/arch is taken from: https://github.com/99designs/gqlgen/blob/master/graphql/handler/apollotracing/tracer.go

import (
	"context"
	"fmt"
	"github.com/99designs/gqlgen/graphql"
)

type GraphqlTracer interface {
	graphql.HandlerExtension
	graphql.ResponseInterceptor
	graphql.FieldInterceptor
}

type Tracer struct {
	graphName string
}

func NewGraphqlTracer(graphName string) GraphqlTracer {
	return Tracer{graphName: graphName}
}

func (t Tracer) ExtensionName() string {
	return "HighlightTracer"
}

func (t Tracer) Validate(graphql.ExecutableSchema) error {
	return nil
}

func (t Tracer) InterceptField(ctx context.Context, next graphql.Resolver) (interface{}, error) {
	fc := graphql.GetFieldContext(ctx)
	name := fmt.Sprintf("operation.field.%s", fc.Field.Name)

	start := graphql.Now()
	res, err := next(ctx)
	end := graphql.Now()

	RecordMetric(ctx, name+".duration", float64(end.Sub(start)))
	return res, err
}

func (t Tracer) InterceptResponse(ctx context.Context, next graphql.ResponseHandler) *graphql.Response {
	rc := graphql.GetOperationContext(ctx)
	opName := "undefined"
	if rc != nil && rc.Operation != nil {
		opName = rc.OperationName
	}
	name := fmt.Sprintf("graphql.operation.%s", opName)
	RecordMetric(ctx, name+".size", float64(len(rc.RawQuery)))

	start := graphql.Now()
	resp := next(ctx)
	end := graphql.Now()

	RecordMetric(ctx, name+".duration", float64(end.Sub(start)))
	if resp.Errors != nil {
		RecordMetric(ctx, name+".errorsCount", float64(len(resp.Errors)))
	}
	return resp
}
