// Licensed to Elasticsearch B.V. under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Elasticsearch B.V. licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package spanmetricsconnectorv2 // import "github.com/elastic/opentelemetry-collector-components/connector/spanmetricsconnectorv2"

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/ptrace"
)

const scopeName = "otelcol/spanmetricsconnectorv2"

type spanMetrics struct {
	component.StartFunc
	component.ShutdownFunc

	next            consumer.Metrics
	spansMetricDefs map[string]metricDef
}

func (sm *spanMetrics) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (sm *spanMetrics) ConsumeTraces(ctx context.Context, td ptrace.Traces) error {
	var multiError error
	processedMetrics := pmetric.NewMetrics()
	processedMetrics.ResourceMetrics().EnsureCapacity(td.ResourceSpans().Len())
	for i := 0; i < td.ResourceSpans().Len(); i++ {
		resourceSpan := td.ResourceSpans().At(i)
		// TODO (lahsivjar): add support for exponential histogram and summary
		spansHist := newExplicitHistogram(sm.spansMetricDefs)

		for j := 0; j < resourceSpan.ScopeSpans().Len(); j++ {
			scopeSpan := resourceSpan.ScopeSpans().At(j)

			for k := 0; k < scopeSpan.Spans().Len(); k++ {
				span := scopeSpan.Spans().At(k)
				var duration time.Duration
				startTime := span.StartTimestamp()
				endTime := span.EndTimestamp()
				if endTime > startTime {
					duration = time.Duration(endTime - startTime)
				}
				multiError = errors.Join(multiError, spansHist.update(ctx, span.Attributes(), duration))
			}
		}

		if len(spansHist.counts) == 0 {
			continue // don't add an empty resource
		}

		processedResource := processedMetrics.ResourceMetrics().AppendEmpty()
		resourceSpan.Resource().Attributes().CopyTo(processedResource.Resource().Attributes())

		processedScope := processedResource.ScopeMetrics().AppendEmpty()
		processedScope.Scope().SetName(scopeName)

		spansHist.appendMetricsTo(processedScope.Metrics())
	}
	if multiError != nil {
		return multiError
	}
	return sm.next.ConsumeMetrics(ctx, processedMetrics)
}
