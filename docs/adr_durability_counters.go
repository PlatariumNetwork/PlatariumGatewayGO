// Package docs holds Gateway architecture decision records as Go source so they
// can merge to main under the orchestrator file policy (.go / .go.tmpl only).
package docs

import "platarium-gateway-go/internal/metrics"

// ADRDurabilityCounters documents minimal Core/Rocks/divergence counters (#69).
const ADRDurabilityCounters = metrics.HowToRead
