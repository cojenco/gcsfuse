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

package monitoring

import (
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/googlecloudplatform/gcsfuse/v3/tools/integration_tests/util/mounting"
	"github.com/googlecloudplatform/gcsfuse/v3/tools/integration_tests/util/setup"
	"github.com/googlecloudplatform/gcsfuse/v3/tools/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

const (
	testHNSBucket  = "gcsfuse_monitoring_test_bucket"
	testFlatBucket = "gcsfuse_monitoring_test_bucket_flat"
)

type PromTest struct {
	suite.Suite
	// Path to the gcsfuse binary.
	gcsfusePath string

	// A temporary directory into which a file system may be mounted. Removed in
	// TearDown.
	mountPoint string
}

func (testSuite *PromTest) SetupSuite() {
	setup.IgnoreTestIfIntegrationTestFlagIsNotSet(testSuite.T())
	err := setup.SetUpTestDir()
	require.NoErrorf(testSuite.T(), err, "error while building GCSFuse: %p", err)
}

func (testSuite *PromTest) SetupTest() {
	var err error
	testSuite.gcsfusePath = setup.BinFile()
	testSuite.mountPoint, err = os.MkdirTemp("", "gcsfuse_monitoring_tests")
	require.NoError(testSuite.T(), err)
	setPrometheusPort(testSuite.T())

	setup.SetLogFile(fmt.Sprintf("%s%s.txt", "/tmp/gcsfuse_monitoring_test_", strings.ReplaceAll(testSuite.T().Name(), "/", "_")))
	err = testSuite.mount(getBucket(testSuite.T()))
	require.NoError(testSuite.T(), err)
}

func (testSuite *PromTest) TearDownTest() {
	if err := util.Unmount(testSuite.mountPoint); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: unmount failed: %v\n", err)
	}
	// require.True(testSuite.T(), isPortOpen(prometheusPort))

	err := os.Remove(testSuite.mountPoint)
	assert.NoError(testSuite.T(), err)
}

func (testSuite *PromTest) mount(bucketName string) error {
	testSuite.T().Helper()
	if portAvailable := isPortOpen(prometheusPort); !portAvailable {
		require.Failf(testSuite.T(), "prometheus port is not available.", "port: %d", int64(prometheusPort))
	}
	cacheDir, err := os.MkdirTemp("", "gcsfuse-cache")
	require.NoError(testSuite.T(), err)
	testSuite.T().Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	flags := []string{fmt.Sprintf("--prometheus-port=%d", prometheusPort), "--cache-dir", cacheDir}
	args := append(flags, bucketName, testSuite.mountPoint)

	if err := mounting.MountGcsfuse(testSuite.gcsfusePath, args); err != nil {
		return err
	}
	return nil
}

func (testSuite *PromTest) TestStatMetrics() {
	_, err := os.Stat(path.Join(testSuite.mountPoint, "hello/hello.txt"))

	require.NoError(testSuite.T(), err)
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "LookUpInode")
	assertNonZeroHistogramMetric(testSuite.T(), "fs_ops_latency", "fs_op", "LookUpInode")
	assertNonZeroCountMetric(testSuite.T(), "gcs_request_count", "gcs_method", "StatObject")
	assertNonZeroHistogramMetric(testSuite.T(), "gcs_request_latencies", "gcs_method", "StatObject")
}

func (testSuite *PromTest) TestFsOpsErrorMetrics() {
	_, err := os.Stat(path.Join(testSuite.mountPoint, "non_existent_path.txt"))
	require.Error(testSuite.T(), err)

	assertNonZeroCountMetric(testSuite.T(), "fs_ops_error_count", "fs_op", "LookUpInode")
	assertNonZeroHistogramMetric(testSuite.T(), "fs_ops_latency", "fs_op", "LookUpInode")
}

func (testSuite *PromTest) TestListMetrics() {
	_, err := os.ReadDir(path.Join(testSuite.mountPoint, "hello"))

	require.NoError(testSuite.T(), err)
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "ReadDir")
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "OpenDir")
	assertNonZeroCountMetric(testSuite.T(), "gcs_request_count", "gcs_method", "ListObjects")
	assertNonZeroHistogramMetric(testSuite.T(), "gcs_request_latencies", "gcs_method", "ListObjects")
}

func (testSuite *PromTest) TestReadMetrics() {
	_, err := os.ReadFile(path.Join(testSuite.mountPoint, "hello/hello.txt"))

	require.NoError(testSuite.T(), err)
	assertNonZeroCountMetric(testSuite.T(), "file_cache_read_bytes_count", "read_type", "Sequential")
	assertNonZeroCountMetric(testSuite.T(), "file_cache_read_count", "cache_hit", "false")
	assertNonZeroCountMetric(testSuite.T(), "file_cache_read_count", "read_type", "Sequential")
	assertNonZeroHistogramMetric(testSuite.T(), "file_cache_read_latencies", "cache_hit", "false")
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "OpenFile")
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "ReadFile")
	assertNonZeroCountMetric(testSuite.T(), "fs_ops_count", "fs_op", "ReadFile")
	assertNonZeroCountMetric(testSuite.T(), "gcs_request_count", "gcs_method", "NewReader")
	assertNonZeroCountMetric(testSuite.T(), "gcs_reader_count", "io_method", "opened")
	assertNonZeroCountMetric(testSuite.T(), "gcs_reader_count", "io_method", "closed")
	assertNonZeroCountMetric(testSuite.T(), "gcs_read_count", "read_type", "Parallel")
	assertNonZeroCountMetric(testSuite.T(), "gcs_download_bytes_count", "", "")
	assertNonZeroCountMetric(testSuite.T(), "gcs_read_bytes_count", "", "")
	assertNonZeroHistogramMetric(testSuite.T(), "gcs_request_latencies", "gcs_method", "NewReader")
	assertNonZeroHistogramMetric(testSuite.T(), "gcs_request_latencies", "gcs_method", "NewReader")
}

func TestPromOTELSuite(t *testing.T) {
	suite.Run(t, new(PromTest))
}
