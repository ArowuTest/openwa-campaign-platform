package importer

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	sharedcrypto "campaign-platform/internal/shared/crypto"
)

func TestLargeAudienceTwoMillionCSVStreamingProfile(t *testing.T) {
	if os.Getenv("RUN_LARGE_AUDIENCE_BENCHMARK") != "1" {
		t.Skip("set RUN_LARGE_AUDIENCE_BENCHMARK=1 to run the 2M-row profile")
	}
	const rows = 2_000_000

	protector, err := sharedcrypto.NewMSISDNProtector(bytes.Repeat([]byte{61}, 32), bytes.Repeat([]byte{62}, 32))
	if err != nil {
		t.Fatal(err)
	}
	reader, writer := io.Pipe()
	var generatedBytes atomic.Int64
	generatorDone := make(chan error, 1)
	go func() {
		buffered := bufio.NewWriterSize(writer, 256<<10)
		counting := &countingBenchmarkWriter{Writer: buffered, Count: &generatedBytes}
		if _, err := io.WriteString(counting, "msisdn,country\n"); err != nil {
			_ = writer.CloseWithError(err)
			generatorDone <- err
			return
		}
		for i := 0; i < rows; i++ {
			// 08000000000..08001999999: 11-digit Nigerian national format,
			// normalised server-side to +234 + 10 national digits.
			if _, err := fmt.Fprintf(counting, "0%d,NG\n", int64(8_000_000_000)+int64(i)); err != nil {
				_ = writer.CloseWithError(err)
				generatorDone <- err
				return
			}
		}
		if err := buffered.Flush(); err != nil {
			_ = writer.CloseWithError(err)
			generatorDone <- err
			return
		}
		generatorDone <- writer.Close()
	}()

	var baseline runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&baseline)
	var peakHeap atomic.Uint64
	peakHeap.Store(baseline.HeapAlloc)
	sampleStop := make(chan struct{})
	sampleDone := make(chan struct{})
	go func() {
		defer close(sampleDone)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sampleStop:
				return
			case <-ticker.C:
				var stats runtime.MemStats
				runtime.ReadMemStats(&stats)
				for {
					current := peakHeap.Load()
					if stats.HeapAlloc <= current || peakHeap.CompareAndSwap(current, stats.HeapAlloc) {
						break
					}
				}
			}
		}
	}()

	started := time.Now()
	var consumed int64
	result, processErr := ProcessCSV(context.Background(), reader, PreviewOptions{
		DefaultCountryISO2: "NG",
		MaxRows:            rows,
		MaxIssues:          1000,
		MaxCandidateSample: 1,
		Protector:          protector,
		Mapping:            ColumnMapping{MSISDN: "msisdn", Country: "country"},
	}, func(_ context.Context, candidate ContactCandidate) (bool, error) {
		if candidate.E164 != "" {
			return false, fmt.Errorf("raw E.164 crossed protected consumer boundary at row %d", candidate.RowNumber)
		}
		if len(candidate.EncryptedMSISDN) == 0 || len(candidate.LookupHMAC) == 0 {
			return false, fmt.Errorf("row %d lacks protected MSISDN material", candidate.RowNumber)
		}
		consumed++
		return false, nil
	})
	elapsed := time.Since(started)
	close(sampleStop)
	<-sampleDone
	generatorErr := <-generatorDone
	if generatorErr != nil {
		t.Fatalf("generate source: %v", generatorErr)
	}
	if processErr != nil {
		t.Fatalf("process 2M source: %v", processErr)
	}
	if result.UploadedRows != rows || result.ValidRows != rows || result.InvalidRows != 0 || result.DuplicateRows != 0 || consumed != rows {
		t.Fatalf("accounting uploaded=%d valid=%d invalid=%d duplicate=%d consumed=%d", result.UploadedRows, result.ValidRows, result.InvalidRows, result.DuplicateRows, consumed)
	}
	if len(result.Candidates) != 0 || len(result.Samples) != 0 {
		t.Fatalf("full processing retained browser-style candidate samples: candidates=%d samples=%d", len(result.Candidates), len(result.Samples))
	}
	peak := peakHeap.Load()
	var peakDelta uint64
	if peak > baseline.HeapAlloc {
		peakDelta = peak - baseline.HeapAlloc
	}
	const maxHeapDelta = 256 << 20
	if peakDelta > maxHeapDelta {
		t.Fatalf("2M streaming peak heap delta=%d exceeds bounded acceptance threshold=%d", peakDelta, maxHeapDelta)
	}
	rowsPerSecond := float64(rows) / elapsed.Seconds()
	t.Logf("2M streaming result: rows=%d bytes=%d elapsed=%s rows_per_second=%.0f baseline_heap=%d peak_heap=%d peak_delta=%d", rows, generatedBytes.Load(), elapsed, rowsPerSecond, baseline.HeapAlloc, peak, peakDelta)
}

type countingBenchmarkWriter struct {
	io.Writer
	Count *atomic.Int64
}

func (w *countingBenchmarkWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.Count.Add(int64(n))
	return n, err
}
