package observability

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func Middleware() gin.HandlerFunc {
	tracer := otel.Tracer(InstrumentationName)

	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = c.Request.URL.Path
		}

		ctx, span := tracer.Start(
			c.Request.Context(),
			RouteSpanName(c.Request.Method, route),
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String(AttrHTTPMethod, c.Request.Method),
				attribute.String(AttrHTTPRoute, route),
				attribute.String(AttrHTTPPath, c.Request.URL.Path),
				attribute.String(AttrTraceRole, TraceRoleHTTPRequest),
			),
		)
		c.Request = c.Request.WithContext(ctx)
		c.Next()

		statusCode := c.Writer.Status()
		span.SetAttributes(attribute.Int(AttrHTTPStatusCode, statusCode))
		if len(c.Errors) > 0 {
			for _, handlerErr := range c.Errors {
				span.RecordError(handlerErr.Err)
			}
			span.SetStatus(codes.Error, c.Errors.Last().Error())
		} else if statusCode >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(statusCode))
		}
		span.End()
	}
}
