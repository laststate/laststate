// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Last State contributors

// Package delivery posts stored events to Trace with retries and batching.
package delivery

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/laststate/relay/internal/store"
	"github.com/laststate/relay/internal/traceapi"
)

// RuntimeDestination holds resolved credentials and batch preferences for one Trace peer.
type RuntimeDestination struct {
	Token          string
	Client         *http.Client
	BatchEnabled   bool
	MaxBatchEvents int
	MaxBatchBytes  int64
	// PreferBinary requests application/vnd.laststate.batch.v1 when the peer advertises binary_batch.
	PreferBinary bool
	// PreferZstd requests Content-Encoding: zstd when the peer lists "zstd" in capabilities.
	PreferZstd bool
}

// Worker claims pending deliveries and posts them to Trace with retry/circuit-breaker policy.
type Worker struct {
	Store              *store.Store
	Client             *http.Client
	Destinations       map[string]RuntimeDestination
	Transformers       map[string]func([]byte) ([]byte, error)
	Concurrency        int
	MinDelay, MaxDelay time.Duration
	Multiplier         float64
	Jitter             bool
	MaxAttempts        int
	FailureThreshold   int
	CircuitOpenFor     time.Duration
	Hooks              Hooks

	mu           sync.Mutex
	circuits     map[string]*circuit
	capabilities map[string]capabilityCache
	rng          *rand.Rand // per-worker random source for jitter
}

type Hooks struct {
	OnDelivered func(delivery store.PendingDelivery, statusCode int)
	OnRetry     func(delivery store.PendingDelivery, statusCode int, failure string, retryIn time.Duration)
	OnDead      func(delivery store.PendingDelivery, statusCode int, failure string)
}

type circuit struct {
	Failures int
	OpenTill time.Time
}

type Capabilities struct {
	APIVersion     string   `json:"api_version"`
	LEPVersions    []int    `json:"lep_versions"`
	MaxEventSize   int64    `json:"max_event_size"`
	MaxBatchEvents int      `json:"max_batch_events"`
	MaxBatchBytes  int64    `json:"max_batch_bytes"`
	Compression    []string `json:"compression"`
	BatchIngest    bool     `json:"batch_ingest"`
	BinaryBatch    bool     `json:"binary_batch"`
	ArtifactUpload bool     `json:"artifact_upload"`
}

type capabilityCache struct {
	Value     Capabilities
	ExpiresAt time.Time
	Legacy    bool
}

func (worker *Worker) defaults() {
	if worker.Client == nil {
		worker.Client = defaultHTTPClient()
	}
	if worker.Concurrency <= 0 {
		worker.Concurrency = 4
	}
	if worker.MinDelay <= 0 {
		worker.MinDelay = time.Second
	}
	if worker.MaxDelay <= 0 {
		worker.MaxDelay = 15 * time.Minute
	}
	if worker.Multiplier < 1 {
		worker.Multiplier = 2
	}
	if worker.MaxAttempts <= 0 {
		worker.MaxAttempts = 50
	}
	if worker.FailureThreshold <= 0 {
		worker.FailureThreshold = 5
	}
	if worker.CircuitOpenFor <= 0 {
		worker.CircuitOpenFor = 30 * time.Second
	}
	if worker.circuits == nil {
		worker.circuits = map[string]*circuit{}
	}
	if worker.capabilities == nil {
		worker.capabilities = map[string]capabilityCache{}
	}
	if worker.rng == nil {
		worker.rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			// Never forward bearer credentials to a redirected host. Trace
			// endpoints are expected to be stable and redirects are suspicious.
			return http.ErrUseLastResponse
		},
	}
}

func (worker *Worker) Run(ctx context.Context) error {
	worker.defaults()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := worker.Flush(ctx); err != nil && ctx.Err() == nil {
			return err
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (worker *Worker) Flush(ctx context.Context) error {
	worker.defaults()
	claimLimit := worker.Concurrency
	for _, destination := range worker.Destinations {
		if destination.BatchEnabled {
			maxEvents := destination.MaxBatchEvents
			if maxEvents <= 0 {
				maxEvents = 100
			}
			if candidate := worker.Concurrency * maxEvents; candidate > claimLimit {
				claimLimit = candidate
			}
		}
	}
	if claimLimit > 1000 {
		claimLimit = 1000
	}
	deliveries, err := worker.Store.ClaimPendingDeliveries(ctx, claimLimit, 2*time.Minute)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}

	groups := make(map[string][]store.PendingDelivery)
	for _, delivery := range deliveries {
		groups[delivery.DestinationID] = append(groups[delivery.DestinationID], delivery)
	}

	semaphore := make(chan struct{}, worker.Concurrency)
	var group sync.WaitGroup
	errorsCh := make(chan error, len(groups))
	for _, destinationDeliveries := range groups {
		if ctx.Err() != nil {
			break
		}
		group.Add(1)
		go func(items []store.PendingDelivery) {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			if err := worker.deliverGroup(ctx, items); err != nil {
				errorsCh <- err
			}
		}(destinationDeliveries)
	}
	group.Wait()
	close(errorsCh)
	var failures []error
	for err := range errorsCh {
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

func (worker *Worker) deliverGroup(ctx context.Context, deliveries []store.PendingDelivery) error {
	if len(deliveries) == 0 {
		return nil
	}
	runtime := worker.Destinations[deliveries[0].DestinationID]
	if !runtime.BatchEnabled || len(deliveries) == 1 {
		return worker.deliverIndividually(ctx, deliveries)
	}
	capabilities, err := worker.getCapabilities(ctx, deliveries[0])
	if err != nil || !capabilities.BatchIngest {
		return worker.deliverIndividually(ctx, deliveries)
	}
	return worker.deliverBatches(ctx, deliveries, capabilities, runtime)
}

func (worker *Worker) deliverIndividually(ctx context.Context, deliveries []store.PendingDelivery) error {
	var failures []error
	for _, delivery := range deliveries {
		if ctx.Err() != nil {
			break
		}
		if err := worker.deliver(ctx, delivery); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (worker *Worker) deliver(ctx context.Context, delivery store.PendingDelivery) error {
	if until := worker.circuitOpenUntil(delivery.DestinationID); until.After(time.Now()) {
		if err := worker.Store.ReleaseDelivery(ctx, delivery, until, "destination circuit breaker is open"); err != nil {
			return err
		}
		return nil
	}

	raw, err := os.ReadFile(delivery.RawObjectPath)
	if err != nil {
		return worker.recordPermanent(ctx, delivery, 0, fmt.Sprintf("read event object: %v", err))
	}
	if transform := worker.Transformers[delivery.DestinationID]; transform != nil {
		raw, err = transform(raw)
		if err != nil {
			return worker.recordPermanent(ctx, delivery, 0, "privacy transform: "+err.Error())
		}
	}
	capabilities, err := worker.getCapabilities(ctx, delivery)
	if err == nil && capabilities.MaxEventSize > 0 && int64(len(raw)) > capabilities.MaxEventSize {
		return worker.recordPermanent(ctx, delivery, http.StatusRequestEntityTooLarge, fmt.Sprintf("event is %d bytes; destination limit is %d", len(raw), capabilities.MaxEventSize))
	}

	endpoint, err := ingestURL(delivery.URL)
	if err != nil {
		return worker.recordPermanent(ctx, delivery, 0, err.Error())
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return worker.recordPermanent(ctx, delivery, 0, err.Error())
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Idempotency-Key", delivery.EventID)
	request.Header.Set("X-Last-State-Event-ID", delivery.EventID)
	if token := worker.token(delivery); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	started := time.Now()
	response, err := worker.client(delivery).Do(request)
	if err != nil {
		worker.markFailure(delivery.DestinationID)
		return worker.retry(ctx, delivery, 0, err.Error(), time.Time{}, false)
	}
	defer response.Body.Close()
	receiptBytes, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if readErr != nil {
		worker.markFailure(delivery.DestinationID)
		return worker.retry(ctx, delivery, response.StatusCode, "read destination response: "+readErr.Error(), time.Time{}, false)
	}
	receipt := strings.TrimSpace(string(receiptBytes))
	_ = started // retained for future latency metrics

	if isIngestSuccess(response.StatusCode, receipt) {
		worker.markSuccess(delivery.DestinationID)
		if err := worker.Store.RecordDelivery(ctx, delivery, response.StatusCode, receipt, "", time.Time{}, false); err != nil {
			return err
		}
		if worker.Hooks.OnDelivered != nil {
			worker.Hooks.OnDelivered(delivery, response.StatusCode)
		}
		return nil
	}

	failure := destinationFailure(response.StatusCode, receipt)
	retryAt := parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	permanent := isPermanent(response.StatusCode) || isIngestConflict(response.StatusCode, receipt)
	if !permanent {
		worker.markFailure(delivery.DestinationID)
	}
	return worker.retry(ctx, delivery, response.StatusCode, failure, retryAt, permanent)
}

type preparedDelivery struct {
	Delivery store.PendingDelivery
	Payload  []byte
}

func (worker *Worker) deliverBatches(ctx context.Context, deliveries []store.PendingDelivery, capabilities Capabilities, runtime RuntimeDestination) error {
	if until := worker.circuitOpenUntil(deliveries[0].DestinationID); until.After(time.Now()) {
		var failures []error
		for _, delivery := range deliveries {
			if err := worker.Store.ReleaseDelivery(ctx, delivery, until, "destination circuit breaker is open"); err != nil {
				failures = append(failures, err)
			}
		}
		return errors.Join(failures...)
	}

	maxEvents := runtime.MaxBatchEvents
	if maxEvents <= 0 {
		maxEvents = 100
	}
	if capabilities.MaxBatchEvents > 0 && capabilities.MaxBatchEvents < maxEvents {
		maxEvents = capabilities.MaxBatchEvents
	}
	if maxEvents < 1 {
		maxEvents = 1
	}
	maxBytes := runtime.MaxBatchBytes
	if maxBytes <= 0 {
		maxBytes = 4 << 20
	}
	if capabilities.MaxBatchBytes > 0 && capabilities.MaxBatchBytes < maxBytes {
		maxBytes = capabilities.MaxBatchBytes
	}

	prepared := make([]preparedDelivery, 0, len(deliveries))
	var failures []error
	for _, delivery := range deliveries {
		raw, err := os.ReadFile(delivery.RawObjectPath)
		if err != nil {
			failures = append(failures, worker.recordPermanent(ctx, delivery, 0, fmt.Sprintf("read event object: %v", err)))
			continue
		}
		if transform := worker.Transformers[delivery.DestinationID]; transform != nil {
			raw, err = transform(raw)
			if err != nil {
				failures = append(failures, worker.recordPermanent(ctx, delivery, 0, "privacy transform: "+err.Error()))
				continue
			}
		}
		if capabilities.MaxEventSize > 0 && int64(len(raw)) > capabilities.MaxEventSize {
			failures = append(failures, worker.recordPermanent(ctx, delivery, http.StatusRequestEntityTooLarge,
				fmt.Sprintf("event is %d bytes; destination limit is %d", len(raw), capabilities.MaxEventSize)))
			continue
		}
		prepared = append(prepared, preparedDelivery{Delivery: delivery, Payload: raw})
	}

	for len(prepared) > 0 {
		count := 0
		var estimated int64
		for count < len(prepared) && count < maxEvents {
			// JSON base64 expands binary payloads by roughly 4/3. Include a small
			// fixed allowance for the event identifier and JSON structure.
			itemBytes := int64((len(prepared[count].Payload)+2)/3*4 + len(prepared[count].Delivery.EventID) + 64)
			if count > 0 && estimated+itemBytes > maxBytes {
				break
			}
			if count == 0 && itemBytes > maxBytes {
				failures = append(failures, worker.recordPermanent(ctx, prepared[count].Delivery, http.StatusRequestEntityTooLarge,
					fmt.Sprintf("encoded event exceeds batch limit of %d bytes", maxBytes)))
				prepared = prepared[1:]
				continue
			}
			estimated += itemBytes
			count++
		}
		if count == 0 {
			continue
		}
		chunk := append([]preparedDelivery(nil), prepared[:count]...)
		prepared = prepared[count:]
		if err := worker.sendBatch(ctx, chunk); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (worker *Worker) sendBatch(ctx context.Context, items []preparedDelivery) error {
	if len(items) == 0 {
		return nil
	}
	endpoint, err := batchURL(items[0].Delivery.URL)
	if err != nil {
		return worker.permanentBatch(ctx, items, 0, err.Error())
	}
	batch := traceapi.BatchRequest{Events: make([]traceapi.BatchEvent, 0, len(items))}
	for _, item := range items {
		batch.Events = append(batch.Events, traceapi.BatchEvent{EventID: item.Delivery.EventID, Payload: item.Payload})
	}

	capabilities, _ := worker.getCapabilities(ctx, items[0].Delivery)
	runtime := worker.Destinations[items[0].Delivery.DestinationID]
	contentType := "application/json"
	var body []byte
	if runtime.PreferBinary && capabilities.BinaryBatch {
		body, err = traceapi.EncodeBinaryBatch(batch.Events)
		if err != nil {
			return worker.permanentBatch(ctx, items, 0, "encode binary batch: "+err.Error())
		}
		contentType = "application/vnd.laststate.batch.v1"
	} else {
		body, err = json.Marshal(batch)
		if err != nil {
			return worker.permanentBatch(ctx, items, 0, "encode batch: "+err.Error())
		}
	}

	contentEncoding := ""
	if runtime.PreferZstd && supportsCompression(capabilities.Compression, "zstd") {
		compressed, compressErr := zstdCompress(body)
		if compressErr == nil && len(compressed) < len(body) {
			body = compressed
			contentEncoding = "zstd"
		}
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return worker.permanentBatch(ctx, items, 0, err.Error())
	}
	request.Header.Set("Content-Type", contentType)
	if contentEncoding != "" {
		request.Header.Set("Content-Encoding", contentEncoding)
	}
	request.Header.Set("X-Last-State-Batch-Count", strconv.Itoa(len(items)))
	if token := worker.token(items[0].Delivery); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := worker.client(items[0].Delivery).Do(request)
	if err != nil {
		worker.markFailure(items[0].Delivery.DestinationID)
		return worker.retryBatch(ctx, items, 0, err.Error(), time.Time{}, false)
	}
	defer response.Body.Close()
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if readErr != nil {
		worker.markFailure(items[0].Delivery.DestinationID)
		return worker.retryBatch(ctx, items, response.StatusCode, "read batch response: "+readErr.Error(), time.Time{}, false)
	}

	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		worker.disableBatchCapability(items[0].Delivery.DestinationID)
		return worker.deliverIndividually(ctx, extractDeliveries(items))
	}
	if response.StatusCode == http.StatusRequestEntityTooLarge && len(items) > 1 {
		middle := len(items) / 2
		return errors.Join(worker.sendBatch(ctx, items[:middle]), worker.sendBatch(ctx, items[middle:]))
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		failure := destinationFailure(response.StatusCode, strings.TrimSpace(string(responseBody)))
		retryAt := parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		permanent := isPermanent(response.StatusCode)
		if !permanent {
			worker.markFailure(items[0].Delivery.DestinationID)
		}
		return worker.retryBatch(ctx, items, response.StatusCode, failure, retryAt, permanent)
	}

	var result traceapi.BatchResponse
	if err := json.Unmarshal(responseBody, &result); err != nil {
		worker.markFailure(items[0].Delivery.DestinationID)
		return worker.retryBatch(ctx, items, response.StatusCode, "decode batch response: "+err.Error(), time.Time{}, false)
	}
	worker.markSuccess(items[0].Delivery.DestinationID)
	byID := make(map[string]preparedDelivery, len(items))
	for _, item := range items {
		byID[item.Delivery.EventID] = item
	}
	processed := make(map[string]struct{}, len(items))
	var failures []error
	for _, accepted := range append(result.Accepted, result.Duplicates...) {
		item, ok := byID[accepted.EventID]
		if !ok {
			continue
		}
		processed[accepted.EventID] = struct{}{}
		if err := worker.Store.RecordDelivery(ctx, item.Delivery, response.StatusCode, accepted.Receipt, "", time.Time{}, false); err != nil {
			failures = append(failures, err)
			continue
		}
		if worker.Hooks.OnDelivered != nil {
			worker.Hooks.OnDelivered(item.Delivery, response.StatusCode)
		}
	}
	for _, rejected := range result.Rejected {
		item, ok := byID[rejected.EventID]
		if !ok {
			continue
		}
		processed[rejected.EventID] = struct{}{}
		failure := rejected.Code
		if rejected.Message != "" {
			failure += ": " + rejected.Message
		}
		if err := worker.retry(ctx, item.Delivery, response.StatusCode, failure, time.Time{}, !rejected.Retryable); err != nil {
			failures = append(failures, err)
		}
	}
	for _, item := range items {
		if _, ok := processed[item.Delivery.EventID]; ok {
			continue
		}
		if err := worker.retry(ctx, item.Delivery, response.StatusCode, "batch response omitted event result", time.Time{}, false); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func extractDeliveries(items []preparedDelivery) []store.PendingDelivery {
	deliveries := make([]store.PendingDelivery, 0, len(items))
	for _, item := range items {
		deliveries = append(deliveries, item.Delivery)
	}
	return deliveries
}

func (worker *Worker) retryBatch(ctx context.Context, items []preparedDelivery, status int, failure string, retryAt time.Time, permanent bool) error {
	var failures []error
	for _, item := range items {
		if err := worker.retry(ctx, item.Delivery, status, failure, retryAt, permanent); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (worker *Worker) permanentBatch(ctx context.Context, items []preparedDelivery, status int, failure string) error {
	return worker.retryBatch(ctx, items, status, failure, time.Time{}, true)
}

func (worker *Worker) disableBatchCapability(destinationID string) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	cached := worker.capabilities[destinationID]
	cached.Value.BatchIngest = false
	cached.Legacy = true
	cached.ExpiresAt = time.Now().Add(10 * time.Minute)
	worker.capabilities[destinationID] = cached
}

func (worker *Worker) client(delivery store.PendingDelivery) *http.Client {
	if runtime, ok := worker.Destinations[delivery.DestinationID]; ok && runtime.Client != nil {
		return runtime.Client
	}
	return worker.Client
}

func (worker *Worker) token(delivery store.PendingDelivery) string {
	if runtime, ok := worker.Destinations[delivery.DestinationID]; ok {
		return runtime.Token
	}
	return ""
}

func (worker *Worker) retry(ctx context.Context, delivery store.PendingDelivery, status int, failure string, retryAt time.Time, permanent bool) error {
	if delivery.AttemptCount+1 >= worker.MaxAttempts {
		permanent = true
		failure = fmt.Sprintf("retry budget exhausted after %d attempts: %s", delivery.AttemptCount+1, failure)
	}
	if retryAt.IsZero() {
		delay := worker.backoff(delivery.AttemptCount)
		retryAt = time.Now().Add(delay)
	}
	if permanent {
		if err := worker.Store.RecordDelivery(ctx, delivery, status, "", failure, time.Time{}, true); err != nil {
			return err
		}
		if worker.Hooks.OnDead != nil {
			worker.Hooks.OnDead(delivery, status, failure)
		}
		return nil
	}
	if err := worker.Store.RecordDelivery(ctx, delivery, status, "", failure, retryAt, false); err != nil {
		return err
	}
	if worker.Hooks.OnRetry != nil {
		worker.Hooks.OnRetry(delivery, status, failure, time.Until(retryAt))
	}
	return nil
}

func (worker *Worker) recordPermanent(ctx context.Context, delivery store.PendingDelivery, status int, failure string) error {
	return worker.retry(ctx, delivery, status, failure, time.Time{}, true)
}

func (worker *Worker) backoff(attempt int) time.Duration {
	delay := time.Duration(float64(worker.MinDelay) * math.Pow(worker.Multiplier, float64(attempt)))
	if delay > worker.MaxDelay {
		delay = worker.MaxDelay
	}
	if worker.Jitter && delay > 0 {
		// Full jitter in [50%, 100%] avoids synchronized retry storms while
		// preventing unexpectedly tiny retry intervals.
		factor := 0.5 + worker.rng.Float64()*0.5
		delay = time.Duration(float64(delay) * factor)
	}
	return delay
}

func (worker *Worker) markFailure(destination string) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	state := worker.circuits[destination]
	if state == nil {
		state = &circuit{}
		worker.circuits[destination] = state
	}
	state.Failures++
	if state.Failures >= worker.FailureThreshold {
		state.OpenTill = time.Now().Add(worker.CircuitOpenFor)
	}
}

func (worker *Worker) markSuccess(destination string) {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	worker.circuits[destination] = &circuit{}
}

func (worker *Worker) circuitOpenUntil(destination string) time.Time {
	worker.mu.Lock()
	defer worker.mu.Unlock()
	if state := worker.circuits[destination]; state != nil {
		return state.OpenTill
	}
	return time.Time{}
}

func (worker *Worker) getCapabilities(ctx context.Context, delivery store.PendingDelivery) (Capabilities, error) {
	worker.mu.Lock()
	cached, ok := worker.capabilities[delivery.DestinationID]
	worker.mu.Unlock()
	if ok && cached.ExpiresAt.After(time.Now()) {
		if cached.Legacy {
			return Capabilities{}, nil
		}
		return cached.Value, nil
	}
	endpoint, err := capabilitiesURL(delivery.URL)
	if err != nil {
		return Capabilities{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Capabilities{}, err
	}
	if token := worker.token(delivery); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := worker.client(delivery).Do(request)
	if err != nil {
		return Capabilities{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusMethodNotAllowed {
		worker.mu.Lock()
		worker.capabilities[delivery.DestinationID] = capabilityCache{Legacy: true, ExpiresAt: time.Now().Add(10 * time.Minute)}
		worker.mu.Unlock()
		return Capabilities{}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Capabilities{}, fmt.Errorf("capabilities endpoint returned HTTP %d", response.StatusCode)
	}
	var value Capabilities
	decoder := json.NewDecoder(io.LimitReader(response.Body, 256<<10))
	if err := decoder.Decode(&value); err != nil {
		return Capabilities{}, fmt.Errorf("decode capabilities: %w", err)
	}
	if value.APIVersion != "" && value.APIVersion != "1" {
		return Capabilities{}, fmt.Errorf("unsupported destination API version %s", value.APIVersion)
	}
	worker.mu.Lock()
	worker.capabilities[delivery.DestinationID] = capabilityCache{Value: value, ExpiresAt: time.Now().Add(10 * time.Minute)}
	worker.mu.Unlock()
	return value, nil
}

func destinationFailure(status int, body string) string {
	if body == "" {
		return fmt.Sprintf("destination returned HTTP %d", status)
	}
	if len(body) > 1024 {
		body = body[:1024] + "…"
	}
	return fmt.Sprintf("destination returned HTTP %d: %s", status, body)
}

func isPermanent(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooEarly, http.StatusTooManyRequests:
		return false
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity, http.StatusConflict:
		return true
	}
	return status >= 400 && status < 500
}

// isIngestSuccess reports whether Trace accepted the event (including true
// duplicates). HTTP 409 alone is NOT success: Trace uses 409/422 for
// same-event_id / different-payload conflicts. Legacy peers that returned 409
// for duplicates are accepted only when the body marks the event as duplicate.
func isIngestSuccess(status int, body string) bool {
	if status >= 200 && status < 300 {
		return true
	}
	if status != http.StatusConflict {
		return false
	}
	return bodyIndicatesDuplicate(body)
}

// isIngestConflict is a same event_id / different hash rejection (permanent).
func isIngestConflict(status int, body string) bool {
	if status == http.StatusUnprocessableEntity || status == http.StatusConflict {
		if bodyIndicatesDuplicate(body) {
			return false
		}
		// Prefer explicit error code when present.
		var envelope struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
			Code   string `json:"code"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal([]byte(body), &envelope); err == nil {
			code := envelope.Error.Code
			if code == "" {
				code = envelope.Code
			}
			if code == "conflict" || envelope.Status == "conflict" {
				return true
			}
		}
		// 422 without a parseable body is permanent via isPermanent; 409 without
		// duplicate markers is treated as conflict (not silent success).
		return status == http.StatusConflict || strings.Contains(strings.ToLower(body), "conflict")
	}
	return false
}

func bodyIndicatesDuplicate(body string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		// Non-JSON legacy body: only accept explicit duplicate wording.
		lower := strings.ToLower(body)
		return strings.Contains(lower, `"duplicate":true`) || strings.Contains(lower, "status\":\"duplicate")
	}
	if v, ok := payload["duplicate"].(bool); ok && v {
		return true
	}
	if s, ok := payload["status"].(string); ok && strings.EqualFold(s, "duplicate") {
		return true
	}
	if code, ok := payload["code"].(string); ok && strings.EqualFold(code, "duplicate") {
		return true
	}
	if errObj, ok := payload["error"].(map[string]any); ok {
		if code, ok := errObj["code"].(string); ok && strings.EqualFold(code, "duplicate") {
			return true
		}
	}
	return false
}

func parseRetryAfter(value string, now time.Time) time.Time {
	if value == "" {
		return time.Time{}
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && seconds >= 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if parsed, err := http.ParseTime(value); err == nil && parsed.After(now) {
		return parsed
	}
	return time.Time{}
}

func ingestURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported destination URL scheme")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("destination URL must not contain credentials")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/ingest"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func batchURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported destination URL scheme")
	}
	if parsed.User != nil {
		return "", fmt.Errorf("destination URL must not contain credentials")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/events:batch"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func supportsCompression(advertised []string, codec string) bool {
	for _, item := range advertised {
		if strings.EqualFold(strings.TrimSpace(item), codec) {
			return true
		}
	}
	return false
}

func zstdCompress(input []byte) ([]byte, error) {
	encoder, err := zstd.NewWriter(nil)
	if err != nil {
		return nil, err
	}
	defer encoder.Close()
	return encoder.EncodeAll(input, nil), nil
}

func capabilitiesURL(base string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported destination URL scheme")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/relay/capabilities"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
