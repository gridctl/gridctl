package tracing

import (
	"log/slog"
	"strings"
	"sync/atomic"

	"go.opentelemetry.io/otel/propagation"
)

// BaggageMaxBytes is the cap on a forwarded baggage value. Over the cap the
// whole value is dropped. Baggage is never copied into span attributes, logs,
// run records, or the trace buffer.
const BaggageMaxBytes = 8192

const baggageKey = "baggage"

var baggageDrops atomic.Uint64

// BaggageDrops reports how many oversized baggage values have been dropped
// in this process. Tests and the debug log share the count.
func BaggageDrops() uint64 {
	return baggageDrops.Load()
}

// NoteBaggageDrop records one oversized drop and logs the byte count, never
// the value.
func NoteBaggageDrop(n int) {
	baggageDrops.Add(1)
	slog.Debug("dropped oversized baggage", "component", "tracing", "bytes", n)
}

// CapBaggage returns value unchanged when it is within the cap. An oversized
// value is dropped (empty string, dropped=true) and counted.
func CapBaggage(value string) (string, bool) {
	if len(value) <= BaggageMaxBytes {
		return value, false
	}
	NoteBaggageDrop(len(value))
	return "", true
}

// CappedCarrier wraps a TextMapCarrier and drops an oversized baggage value
// on Set. Other keys pass through.
type CappedCarrier struct {
	inner propagation.TextMapCarrier
}

// NewCappedCarrier wraps inner. A nil inner is not useful; callers pass a
// header or meta carrier.
func NewCappedCarrier(inner propagation.TextMapCarrier) CappedCarrier {
	return CappedCarrier{inner: inner}
}

// Get returns the inner value.
func (c CappedCarrier) Get(key string) string {
	if c.inner == nil {
		return ""
	}
	return c.inner.Get(key)
}

// Set stores the pair, dropping baggage that exceeds BaggageMaxBytes.
func (c CappedCarrier) Set(key, value string) {
	if c.inner == nil {
		return
	}
	if strings.EqualFold(key, baggageKey) {
		capped, dropped := CapBaggage(value)
		if dropped {
			return
		}
		value = capped
	}
	c.inner.Set(key, value)
}

// Keys returns the inner keys.
func (c CappedCarrier) Keys() []string {
	if c.inner == nil {
		return nil
	}
	return c.inner.Keys()
}
