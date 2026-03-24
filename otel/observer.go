package otel

import (
	"context"
	"time"

	"github.com/ChiragRayani/resilix"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// Observer implements resilix.Observer using OpenTelemetry metrics and traces.
//
// It records:
//   - resilix.executions (counter): total calls, labelled by policy, success/failure
//   - resilix.rejections (counter): calls rejected without execution
//   - resilix.latency (histogram): execution latency per policy
//   - resilix.retries (counter): retry attempts per policy
//   - resilix.state_changes (counter): circuit breaker transitions
type Observer struct {
	tracer      trace.Tracer
	executions  metric.Int64Counter
	rejections  metric.Int64Counter
	latency     metric.Float64Histogram
	retries     metric.Int64Counter
	stateChange metric.Int64Counter
}

// NewObserver creates an OTel observer. Pass the meter and tracer from your
// global OTel provider.
func NewObserver(meter metric.Meter, tracer trace.Tracer) (*Observer, error) {
	executions, err := meter.Int64Counter("resilix.executions",
		metric.WithDescription("Total policy executions"))
	if err != nil {
		return nil, err
	}

	rejections, err := meter.Int64Counter("resilix.rejections",
		metric.WithDescription("Calls rejected without execution"))
	if err != nil {
		return nil, err
	}

	lat, err := meter.Float64Histogram("resilix.latency",
		metric.WithDescription("Execution latency in seconds"),
		metric.WithUnit("s"))
	if err != nil {
		return nil, err
	}

	retries, err := meter.Int64Counter("resilix.retries",
		metric.WithDescription("Retry attempts"))
	if err != nil {
		return nil, err
	}

	sc, err := meter.Int64Counter("resilix.state_changes",
		metric.WithDescription("Circuit breaker state transitions"))
	if err != nil {
		return nil, err
	}

	return &Observer{
		tracer:      tracer,
		executions:  executions,
		rejections:  rejections,
		latency:     lat,
		retries:     retries,
		stateChange: sc,
	}, nil
}

func (o *Observer) OnStateChange(policy string, from, to resilix.State) {
	o.stateChange.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("policy", policy),
			attribute.String("from", from.String()),
			attribute.String("to", to.String()),
		))
}

func (o *Observer) OnSuccess(policy string, latency time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("policy", policy),
		attribute.String("result", "success"),
	)
	o.executions.Add(context.Background(), 1, attrs)
	o.latency.Record(context.Background(), latency.Seconds(), attrs)
}

func (o *Observer) OnFailure(policy string, _ error, latency time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("policy", policy),
		attribute.String("result", "failure"),
	)
	o.executions.Add(context.Background(), 1, attrs)
	o.latency.Record(context.Background(), latency.Seconds(), attrs)
}

func (o *Observer) OnRejected(policy string) {
	o.rejections.Add(context.Background(), 1,
		metric.WithAttributes(attribute.String("policy", policy)))
}

func (o *Observer) OnRetry(policy string, attempt int, _ error) {
	o.retries.Add(context.Background(), 1,
		metric.WithAttributes(
			attribute.String("policy", policy),
			attribute.Int("attempt", attempt),
		))
}
