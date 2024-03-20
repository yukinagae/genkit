// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package genkit

import (
	"context"
	"errors"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// A traceStoreExporter is an OpenTelemetry SpanExporter that
// writes spans to a TraceStore.
type traceStoreExporter struct {
	store TraceStore
}

func newTraceStoreExporter(store TraceStore) *traceStoreExporter {
	return &traceStoreExporter{store}
}

// ExportSpans implements [go.opentelemetry.io/otel/sdk/trace.SpanExporter.ExportSpans].
// It saves the spans to e's TraceStore.
// Saving is not atomic: it is possible that some but not all spans will be saved.
func (e *traceStoreExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	// Group spans by trace ID.
	spansByTrace := map[trace.TraceID][]sdktrace.ReadOnlySpan{}
	for _, span := range spans {
		tid := span.SpanContext().TraceID()
		spansByTrace[tid] = append(spansByTrace[tid], span)
	}

	// Convert each trace to our types and save it.
	for tid, spans := range spansByTrace {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		td, err := convertTrace(spans)
		if err != nil {
			return err
		}
		if err := e.store.Save(ctx, tid.String(), td); err != nil {
			return err
		}
	}
	return nil
}

// convertTrace converts a list of spans to a TraceData.
// The spans must all have the same trace ID.
func convertTrace(spans []sdktrace.ReadOnlySpan) (*TraceData, error) {
	td := &TraceData{Spans: map[string]*SpanData{}}
	for _, span := range spans {
		cspan := convertSpan(span)
		// The unique span with no parent determines
		// the TraceData fields.
		if cspan.ParentSpanID == "" {
			if td.DisplayName != "" {
				return nil, errors.New("more than one parentless span")
			}
			td.DisplayName = cspan.DisplayName
			td.StartTime = cspan.StartTime
			td.EndTime = cspan.EndTime
		}
		td.Spans[cspan.SpanID] = cspan
	}
	return td, nil
}

// convertSpan converts an OpenTelemetry span to a SpanData.
func convertSpan(span sdktrace.ReadOnlySpan) *SpanData {
	sc := span.SpanContext()
	sd := &SpanData{
		SpanID:      sc.SpanID().String(),
		TraceID:     sc.TraceID().String(),
		StartTime:   timeToMicroseconds(span.StartTime()),
		EndTime:     timeToMicroseconds(span.EndTime()),
		Attributes:  attributesToMap(span.Attributes()),
		DisplayName:  span.Name(),
		Links:  convertLinks(span.Links()),
		InstrumentationLibrary:  InstrumentationLibrary(span.InstrumentationLibrary()),
		SpanKind:  span.SpanKind().String(),
		SameProcessAsParentSpan: boolValue{!sc.IsRemote()},
		Status:                  convertStatus(span.Status()),
	}
	if p := span.Parent(); p.HasSpanID() {
		sd.ParentSpanID = p.SpanID().String()
	}
	sd.TimeEvents.TimeEvent = convertEvents(span.Events())
	return sd
}

func attributesToMap(attrs []attribute.KeyValue) map[string]any {
	m := map[string]any{}
	for _, a := range attrs {
		m[string(a.Key)] = a.Value.AsInterface()
	}
	return m
}

func convertLinks(links []sdktrace.Link) []*Link {
	var cls []*Link
	for _, l := range links {
		cl := &Link{
			SpanContext:            convertSpanContext(l.SpanContext),
			Attributes:             attributesToMap(l.Attributes),
			DroppedAttributesCount: l.DroppedAttributeCount,
		}
		cls = append(cls, cl)
	}
	return cls
}

func convertSpanContext(sc trace.SpanContext) SpanContext {
	return SpanContext{
		TraceID:    sc.TraceID().String(),
		SpanID:     sc.SpanID().String(),
		IsRemote:   sc.IsRemote(),
		TraceFlags: int(sc.TraceFlags()),
	}
}

func convertEvents(evs []sdktrace.Event) []TimeEvent {
	var tes []TimeEvent
	for _, e := range evs {
		tes = append(tes, TimeEvent{
			Time: timeToMicroseconds(e.Time),
			Annotation: annotation{
				Description: e.Name,
				Attributes:  attributesToMap(e.Attributes),
			},
		})
	}
	return tes
}

func convertStatus(s sdktrace.Status) Status {
	return Status{
		Code:        uint32(s.Code),
		Description: s.Description,
	}
}

// ExportSpans implements [go.opentelemetry.io/otel/sdk/trace.SpanExporter.Shutdown].
func (e *traceStoreExporter) Shutdown(ctx context.Context) error { return nil }