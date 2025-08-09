package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	// Adjust this path if your gcsfuse binary is located elsewhere
	gcsfuseBin    = "../main.go"
	testBucketEnv = "GCSFUSE_TEST_BUCKET"
	projectIDEnv  = "GCP_PROJECT_ID"
	mountTimeout  = 15 * time.Second
	runDuration   = 90 * time.Second // How long to keep GCSFuse running
)

// waitForMount polls the mount point until it's ready or times out.
func waitForMount(mountPoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, err := os.ReadDir(mountPoint)
		if err == nil {
			log.Printf("Mount point %s is ready.", mountPoint)
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("mount point %s not ready after %v", mountPoint, timeout)
}

func main() {
	bucketName := os.Getenv(testBucketEnv)
	if bucketName == "" {
		log.Printf("Environment variable %s not set.", testBucketEnv)
		bucketName = "test-fuse-metrics66"
		log.Printf("Set bucketName to %s", bucketName)
	}

	projectID := os.Getenv(projectIDEnv)
	if projectID == "" {
		log.Printf("Environment variable %s not set.", projectIDEnv)
		projectID = "storage-sdks-cathyo"
		log.Printf("Set projectID to %s", projectID)
	}

	log.Printf("Using bucket: %s, Project: %s", bucketName, projectID)

	// Create a temporary directory for the mount point
	mountPoint, err := os.MkdirTemp("", "gcsfuse-gcm-test-")
	if err != nil {
		log.Fatalf("Failed to create temp dir for mount point: %v", err)
	}
	defer os.RemoveAll(mountPoint)
	log.Printf("Mount point: %s", mountPoint)

	// Create a temporary directory for logs
	logDir, err := os.MkdirTemp("", "gcsfuse-gcm-logs-")
	if err != nil {
		log.Fatalf("Failed to create temp dir for logs: %v", err)
	}
	defer os.RemoveAll(logDir)
	logFile := filepath.Join(logDir, "gcsfuse.log")
	log.Printf("GCSFuse logs will be in: %s", logFile)

	ctx, cancel := context.WithTimeout(context.Background(), runDuration+mountTimeout+10*time.Second)
	defer cancel()

	// Command to run GCSFuse with Cloud Monitoring export enabled
	cmd := exec.CommandContext(ctx, gcsfuseBin,
		"--foreground",
		"--implicit-dirs",
		"--cloud-metrics-export-interval-secs=15", // Enable GCM export, every 15s
		"--log-file", logFile,
		"--log-format", "text",
		// "--debug_gcs", // Uncomment for more detailed GCS interaction logs
		bucketName,
		mountPoint,
	)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout

	log.Printf("Starting GCSFuse: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start GCSFuse: %v", err)
	}

	// Cleanup: Ensure GCSFuse process is terminated
	defer func() {
		log.Println("Tearing down GCSFuse...")
		if cmd.Process != nil {
			cmd.Process.Signal(os.Interrupt)
			err := cmd.Wait()
			log.Printf("GCSFuse exited: %v", err)
		}
		// Fallback unmount
		exec.Command("fusermount", "-u", mountPoint).Run()
		log.Println("Cleanup finished.")
	}()

	if err := waitForMount(mountPoint, mountTimeout); err != nil {
		log.Fatalf("GCSFuse mount failed: %v", err)
	}

	// Perform some file operations to generate metrics
	testFile := "gcm_export_test.txt"
	filePath := filepath.Join(mountPoint, testFile)

	log.Println("Writing file...")
	if err := os.WriteFile(filePath, []byte("test data"), 0644); err != nil {
		log.Printf("Failed to write file: %v", err)
	}

	log.Println("Reading file...")
	if _, err := os.ReadFile(filePath); err != nil {
		log.Printf("Failed to read file: %v", err)
	}

	log.Println("Listing directory...")
	if _, err := os.ReadDir(mountPoint); err != nil {
		log.Printf("Failed to list directory: %v", err)
	}

	// Keep GCSFuse running for a while to allow metric exports
	log.Printf("GCSFuse is running. Waiting for %s to allow metric export to GCM...", runDuration)
	select {
	case <-time.After(runDuration):
		log.Println("Finished waiting.")
	case <-ctx.Done():
		log.Println("Context done, terminating.")
	}
}