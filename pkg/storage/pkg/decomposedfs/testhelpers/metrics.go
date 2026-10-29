// Copyright 2026 OpenCloud GmbH <mail@opencloud.eu>
// SPDX-License-Identifier: Apache-2.0

package helpers

import (
	"github.com/prometheus/client_golang/prometheus"
)

// MetricValue returns the value of a metric registered with the default registry, or the number of
// observations of a histogram. The labels are given as pairs of name and value, the first metric having
// all of them is used. It returns 0 if there is none.
func MetricValue(name string, labels ...string) (float64, error) {
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return 0, err
	}
	want := map[string]string{}
	for i := 0; i+1 < len(labels); i += 2 {
		want[labels[i]] = labels[i+1]
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			matched := 0
			for _, l := range m.GetLabel() {
				if v, ok := want[l.GetName()]; ok && v == l.GetValue() {
					matched++
				}
			}
			if matched != len(want) {
				continue
			}
			switch {
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue(), nil
			case m.GetGauge() != nil:
				return m.GetGauge().GetValue(), nil
			case m.GetHistogram() != nil:
				return float64(m.GetHistogram().GetSampleCount()), nil
			}
		}
	}
	return 0, nil
}
