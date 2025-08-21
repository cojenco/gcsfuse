// Copyright 2025 Google LLC
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

package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"testing"

	"github.com/googlecloudplatform/gcsfuse/v3/tools/integration_tests/util/client"
	"github.com/googlecloudplatform/gcsfuse/v3/tools/integration_tests/util/setup"
	promclient "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Shared constants and variables
var (
	portNonHNSRun = 9190
	portHNSRun    = 10190
)

var prometheusPort int


// setPrometheusPort sets a unique Prometheus port for each test run.
func setPrometheusPort(t *testing.T) {
	if isHNSTestRun(t) {
		prometheusPort = portHNSRun
		portHNSRun++
		return
	}
	prometheusPort = portNonHNSRun
	portNonHNSRun++
}

func getBucket(t *testing.T) string {
	if isHNSTestRun(t) {
		return testHNSBucket
	}
	return testFlatBucket
}

// isPortOpen checks if the specified port is not in use.
func isPortOpen(port int) bool {
	c := exec.Command("lsof", "-t", fmt.Sprintf("-i:%d", port))
	output, _ := c.CombinedOutput()
	return len(output) == 0
}

// isHNSTestRun checks if the bucket is a Hierarchical Namespace (HNS) bucket.
func isHNSTestRun(t *testing.T) bool {
	storageClient, err := client.CreateStorageClient(context.Background())
	require.NoError(t, err, "error while creating storage client")
	defer storageClient.Close()
	return setup.IsHierarchicalBucket(context.Background(), storageClient)
}

// parsePromFormat fetches and parses the Prometheus metrics.
func parsePromFormat(t *testing.T) (map[string]*promclient.MetricFamily, error) {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/metrics", prometheusPort))
	require.NoError(t, err) // Use t for require.NoError
	defer resp.Body.Close()
	var parser expfmt.TextParser
	return parser.TextToMetricFamilies(resp.Body)
}

// assertNonZeroCountMetric asserts that the specified count metric is present and is positive in the Prometheus export
func assertNonZeroCountMetric(t *testing.T, metricName, labelName, labelValue string) {
	t.Helper()
	mf, err := parsePromFormat(t) // Pass t to parsePromFormat
	require.NoError(t, err)      // Use t for require.NoError
	for k, v := range mf {
		if k != metricName || *v.Type != promclient.MetricType_COUNTER {
			continue
		}
		for _, m := range v.Metric {
			if *m.Counter.Value <= 0 {
				continue
			}
			if labelName == "" {
				return
			}
			for _, l := range m.GetLabel() {
				if *l.Name == labelName && *l.Value == labelValue {
					return
				}
			}
		}
	}
	assert.Fail(t, fmt.Sprintf("Didn't find the metric with name: %s, labelName: %s and labelValue: %s",
		metricName, labelName, labelValue))
}

// assertNonZeroHistogramMetric asserts that the specified histogram metric is present and is positive for at least one of the buckets in the Prometheus export.
func assertNonZeroHistogramMetric(t *testing.T, metricName, labelName, labelValue string) {
	t.Helper()
	mf, err := parsePromFormat(t) // Pass t to parsePromFormat
	require.NoError(t, err)      // Use t for require.NoError

	for k, v := range mf {
		if k != metricName || *v.Type != promclient.MetricType_HISTOGRAM {
			continue
		}
		for _, m := range v.Metric {
			for _, bkt := range m.GetHistogram().Bucket {
				if bkt.CumulativeCount == nil || *bkt.CumulativeCount == 0 {
					continue
				}
				if labelName == "" {
					return
				}
				for _, l := range m.GetLabel() {
					if *l.Name == labelName && *l.Value == labelValue {
						return
					}
				}
			}
		}
	}
}
