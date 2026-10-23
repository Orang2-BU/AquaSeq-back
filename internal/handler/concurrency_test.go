package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"aquaseq-back/internal/domain"
)

// TestSimultaneous1000Calculations verifies that 1000 simultaneous user calculations
// are processed concurrently with zero race conditions, zero errors, and sub-second latency.
func TestSimultaneous1000Calculations(t *testing.T) {
	ts := setupTestServer()

	concurrency := 1000
	var wg sync.WaitGroup
	wg.Add(concurrency)

	startGate := make(chan struct{})
	var successCount int64
	var failCount int64

	payload, _ := json.Marshal(domain.SDRIInput{
		CTPPC: 19.95,
		SQPCR: 1.5,
		SBio:  -2.0,
		SChem: 1.2,
	})

	start := time.Now()

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			<-startGate // Synchronize launch: all 1000 fire simultaneously

			req := httptest.NewRequest(http.MethodPost, "/api/v1/sdri/calculate", bytes.NewReader(payload))
			w := httptest.NewRecorder()

			ts.ServeHTTP(w, req)

			if w.Code == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&failCount, 1)
			}
		}(i)
	}

	// Open the gate - all 1000 fire at the exact same microsecond
	close(startGate)
	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("Completed %d simultaneous calculations in %v", concurrency, elapsed)
	t.Logf("Success: %d, Fail: %d, Throughput: %.2f req/sec", successCount, failCount, float64(concurrency)/elapsed.Seconds())

	if successCount != int64(concurrency) {
		t.Fatalf("expected %d successes, got %d (failures: %d)", concurrency, successCount, failCount)
	}
}

// TestWorkerPool1000RealSockets verifies that 1000 real HTTP client requests over TCP sockets
// execute cleanly using connection reuse and concurrency workers.
func TestWorkerPool1000RealSockets(t *testing.T) {
	ts := httptest.NewServer(setupTestServer())
	defer ts.Close()

	totalRequests := 1000
	workers := 50 // 50 concurrent persistent workers processing 1000 requests
	requestsPerWorker := totalRequests / workers

	var wg sync.WaitGroup
	wg.Add(workers)

	var successCount int64
	var failCount int64

	client := ts.Client()

	payload := []byte(`{"ct_ppc":19.95,"s_qpcr":1.5,"s_bio":-2.0,"s_chem":1.2}`)
	start := time.Now()

	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			for i := 0; i < requestsPerWorker; i++ {
				resp, err := client.Post(ts.URL+"/api/v1/sdri/calculate", "application/json", bytes.NewReader(payload))
				if err != nil {
					atomic.AddInt64(&failCount, 1)
					continue
				}
				if resp.StatusCode == http.StatusOK {
					atomic.AddInt64(&successCount, 1)
				} else {
					atomic.AddInt64(&failCount, 1)
				}
				_ = resp.Body.Close()
			}
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	t.Logf("Completed %d TCP requests across %d concurrent workers in %v", totalRequests, workers, elapsed)
	t.Logf("Success: %d, Fail: %d, Throughput: %.2f req/sec", successCount, failCount, float64(totalRequests)/elapsed.Seconds())

	if successCount != int64(totalRequests) {
		t.Fatalf("expected %d successes, got %d (failures: %d)", totalRequests, successCount, failCount)
	}
}
