// Command loadtest drives a simple HTTP workload and reports latency
// percentiles. Used to validate the control-plane overhead through Envoy.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type headerFlags []string

func (h *headerFlags) String() string { return strings.Join(*h, ",") }
func (h *headerFlags) Set(v string) error {
	*h = append(*h, v)
	return nil
}

type result struct {
	Status  int
	Err     error
	Latency time.Duration
}

func main() {
	url := flag.String("url", "http://127.0.0.1:18000/accounts/A123", "target URL")
	method := flag.String("method", http.MethodGet, "HTTP method")
	body := flag.String("body", "", "request body")
	duration := flag.Duration("duration", 10*time.Second, "test duration")
	concurrency := flag.Int("concurrency", 50, "concurrent workers")
	rps := flag.Int("rps", 0, "target requests per second (0 = unthrottled)")
	timeout := flag.Duration("timeout", 5*time.Second, "per-request timeout")
	expect := flag.Int("expect", 200, "expected status (0 disables)")
	maxP99 := flag.Duration("max-p99", 0, "fail if p99 exceeds this (0 disables)")
	var headers headerFlags
	flag.Var(&headers, "H", "header in 'Key: Value' form (repeatable)")
	flag.Parse()

	reqHeaders := map[string]string{}
	for _, raw := range headers {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "invalid header %q\n", raw)
			os.Exit(2)
		}
		reqHeaders[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	client := &http.Client{
		Timeout: *timeout,
		Transport: &http.Transport{
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 1000,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()

	var (
		mu        sync.Mutex
		latencies []time.Duration
		statuses  = map[int]int{}
		errs      int
	)
	record := func(r result) {
		mu.Lock()
		defer mu.Unlock()
		if r.Err != nil {
			errs++
			return
		}
		statuses[r.Status]++
		if *expect == 0 || r.Status == *expect {
			latencies = append(latencies, r.Latency)
		}
	}

	do := func() {
		start := time.Now()
		req, err := http.NewRequest(*method, *url, strings.NewReader(*body))
		if err != nil {
			record(result{Err: err})
			return
		}
		for k, v := range reqHeaders {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			record(result{Err: err})
			return
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		record(result{Status: resp.StatusCode, Latency: time.Since(start)})
	}

	start := time.Now()
	var wg sync.WaitGroup
	if *rps > 0 {
		jobs := make(chan struct{}, *concurrency*2)
		for i := 0; i < *concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for range jobs {
					do()
				}
			}()
		}
		interval := time.Second / time.Duration(*rps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
	dispatch:
		for {
			select {
			case <-ctx.Done():
				break dispatch
			case <-ticker.C:
				select {
				case jobs <- struct{}{}:
				case <-ctx.Done():
					break dispatch
				}
			}
		}
		close(jobs)
		wg.Wait()
	} else {
		for i := 0; i < *concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-ctx.Done():
						return
					default:
						do()
					}
				}
			}()
		}
		wg.Wait()
	}
	elapsed := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	pct := func(p float64) time.Duration {
		if len(latencies) == 0 {
			return 0
		}
		idx := int(p * float64(len(latencies)))
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		return latencies[idx]
	}
	out := map[string]any{
		"elapsedSeconds": elapsed.Seconds(),
		"requests":       len(latencies) + errs,
		"successful":     len(latencies),
		"errors":         errs,
		"rps":            float64(len(latencies)+errs) / elapsed.Seconds(),
		"p50Ms":          float64(pct(0.50).Microseconds()) / 1000,
		"p95Ms":          float64(pct(0.95).Microseconds()) / 1000,
		"p99Ms":          float64(pct(0.99).Microseconds()) / 1000,
		"statuses":       statuses,
	}
	raw, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(raw))

	if errs > 0 {
		fmt.Fprintf(os.Stderr, "%d requests failed\n", errs)
		os.Exit(1)
	}
	for status := range statuses {
		if *expect != 0 && status != *expect {
			fmt.Fprintf(os.Stderr, "unexpected status %d\n", status)
			os.Exit(1)
		}
	}
	if *maxP99 > 0 && pct(0.99) > *maxP99 {
		fmt.Fprintf(os.Stderr, "p99 %s exceeds %s\n", pct(0.99), *maxP99)
		os.Exit(1)
	}
}
