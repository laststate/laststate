// Package dna implements device fingerprinting and similarity detection.
// Each device has a unique "DNA" based on hardware characteristics that can
// detect clones, replacements, and defective batches.
//
// DNA components:
//   - Boot time variance (σ of boot time over N boots)
//   - Clock frequency micro-variations
//   - Flash wear indicators (program/erase cycles)
//   - Power consumption signature
//   - MCU unique ID + bootloader signature
//
// Configuration:
//
//	DNA_DETECTION_THRESHOLD=0.95  — similarity threshold for clone detection
package dna

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"
)

// DeviceDNA represents a device's hardware fingerprint.

// DeviceDNA represents a device's hardware fingerprint.
type DeviceDNA struct {
	DeviceID       string    `json:"device_id"`
	BootTimeAvg    float64   `json:"boot_time_avg_ms"`
	BootTimeStdDev float64   `json:"boot_time_stddev_ms"`
	ClockFreq      float64   `json:"clock_freq_mhz"`
	ClockStdDev    float64   `json:"clock_stddev_ppm"`
	FlashWear      int       `json:"flash_wear_percent"`
	PowerAvgmA     float64   `json:"power_avg_ma"`
	PowerStdDev    float64   `json:"power_stddev_ma"`
	MCUUID         string    `json:"mcu_uuid"`
	BootloaderSig  string    `json:"bootloader_signature"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Fingerprint    string    `json:"fingerprint"` // SHA256 hash of all fields
}

// SimilarityResult holds the result of comparing two device DNAs.
type SimilarityResult struct {
	Device1ID  string   `json:"device_1_id"`
	Device2ID  string   `json:"device_2_id"`
	Similarity float64  `json:"similarity"` // 0-1
	Match      bool     `json:"match"`      // true if similarity >= threshold
	Reasons    []string `json:"reasons"`
}

// DNAStore interface for persisting device DNAs.
type DNAStore interface {
	Save(ctx context.Context, dna DeviceDNA) error
	Get(ctx context.Context, deviceID string) (*DeviceDNA, error)
	ListAll(ctx context.Context) ([]DeviceDNA, error)
}

// Manager coordinates DNA capture, storage, and comparison.
type Manager struct {
	store     DNAStore
	threshold float64
	log       *slog.Logger
	mu        sync.RWMutex
	cache     map[string]*DeviceDNA
	cacheTTL  time.Duration
}

// NewManager creates a DNA manager.
func NewManager(store DNAStore, threshold float64, log *slog.Logger) *Manager {
	return &Manager{
		store:     store,
		threshold: threshold,
		log:       log,
		cache:     make(map[string]*DeviceDNA),
		cacheTTL:  5 * time.Minute,
	}
}

// CaptureDNA extracts and stores a device's DNA from runtime metrics.
func (m *Manager) CaptureDNA(ctx context.Context, deviceID string, bootTimes []float64, clockFreq float64, flashWear int, mcuUUID string, bootloaderSig string) (*DeviceDNA, error) {
	// Compute boot time statistics
	avgBoot, stddevBoot := meanStdDev(bootTimes)

	// Compute fingerprint
	dna := DeviceDNA{
		DeviceID:       deviceID,
		BootTimeAvg:    avgBoot,
		BootTimeStdDev: stddevBoot,
		ClockFreq:      clockFreq,
		ClockStdDev:    0, // would need multiple samples
		FlashWear:      flashWear,
		PowerAvgmA:     0, // would need power sensor data
		PowerStdDev:    0,
		MCUUID:         mcuUUID,
		BootloaderSig:  bootloaderSig,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	}
	dna.Fingerprint = computeFingerprint(dna)

	if err := m.store.Save(ctx, dna); err != nil {
		return nil, fmt.Errorf("saving DNA: %w", err)
	}

	m.mu.Lock()
	m.cache[deviceID] = &dna
	m.mu.Unlock()

	return &dna, nil
}

// GetDNA retrieves a device's DNA from cache or store.
func (m *Manager) GetDNA(ctx context.Context, deviceID string) (*DeviceDNA, error) {
	m.mu.RLock()
	if cached, ok := m.cache[deviceID]; ok && time.Since(cached.UpdatedAt) < m.cacheTTL {
		m.mu.RUnlock()
		return cached, nil
	}
	m.mu.RUnlock()

	dna, err := m.store.Get(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.cache[deviceID] = dna
	m.mu.Unlock()

	return dna, nil
}

// CompareDevices compares two devices and returns similarity.
func (m *Manager) CompareDevices(ctx context.Context, device1ID, device2ID string) (*SimilarityResult, error) {
	dna1, err := m.GetDNA(ctx, device1ID)
	if err != nil {
		return nil, fmt.Errorf("device 1: %w", err)
	}
	dna2, err := m.GetDNA(ctx, device2ID)
	if err != nil {
		return nil, fmt.Errorf("device 2: %w", err)
	}

	result := compareDNAs(dna1, dna2, m.threshold)
	return result, nil
}

// DetectClones finds all pairs of devices with similarity above threshold.
func (m *Manager) DetectClones(ctx context.Context) ([]*SimilarityResult, error) {
	allDNAs, err := m.store.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	var clones []*SimilarityResult
	for i := 0; i < len(allDNAs); i++ {
		for j := i + 1; j < len(allDNAs); j++ {
			result := compareDNAs(&allDNAs[i], &allDNAs[j], m.threshold)
			if result.Match {
				clones = append(clones, result)
			}
		}
	}
	return clones, nil
}

// compareDNAs computes similarity between two device DNAs. The pair is
// reported in lexicographic device-ID order so results are deterministic
// regardless of the underlying store's listing order.
func compareDNAs(dna1, dna2 *DeviceDNA, threshold float64) *SimilarityResult {
	first, second := dna1, dna2
	if second.DeviceID < first.DeviceID {
		first, second = second, first
	}
	result := &SimilarityResult{
		Device1ID:  first.DeviceID,
		Device2ID:  second.DeviceID,
		Similarity: 0,
		Reasons:    make([]string, 0),
	}

	// Boot time similarity (weight: 30%) — symmetric denominator
	bootSim := 1.0 - math.Abs(dna1.BootTimeAvg-dna2.BootTimeAvg)/(math.Max(dna1.BootTimeAvg, dna2.BootTimeAvg)+1)
	bootSim = math.Max(0, math.Min(1, bootSim))
	result.Reasons = append(result.Reasons, fmt.Sprintf("Boot time: %.1f%% similar", bootSim*100))

	// Clock frequency similarity (weight: 25%)
	clockSim := 1.0 - math.Abs(dna1.ClockFreq-dna2.ClockFreq)/math.Max(dna1.ClockFreq, dna2.ClockFreq)
	clockSim = math.Max(0, math.Min(1, clockSim))
	result.Reasons = append(result.Reasons, fmt.Sprintf("Clock: %.1f%% similar", clockSim*100))

	// Flash wear similarity (weight: 20%)
	flashSim := 1.0 - math.Abs(float64(dna1.FlashWear)-float64(dna2.FlashWear))/100
	flashSim = math.Max(0, math.Min(1, flashSim))
	result.Reasons = append(result.Reasons, fmt.Sprintf("Flash wear: %.1f%% similar", flashSim*100))

	// MCU UUID similarity (weight: 15%) — exact match
	mcuSim := 1.0
	if dna1.MCUUID != dna2.MCUUID {
		mcuSim = 0
	}
	result.Reasons = append(result.Reasons, fmt.Sprintf("MCU UUID: %v", mcuSim > 0.5))

	// Bootloader signature similarity (weight: 10%)
	bootSimSig := 1.0
	if dna1.BootloaderSig != dna2.BootloaderSig {
		bootSimSig = 0
	}
	result.Reasons = append(result.Reasons, fmt.Sprintf("Bootloader: %v", bootSimSig > 0.5))

	// Weighted similarity
	result.Similarity = bootSim*0.30 + clockSim*0.25 + flashSim*0.20 + mcuSim*0.15 + bootSimSig*0.10
	result.Similarity = math.Round(result.Similarity*100) / 100
	result.Match = result.Similarity >= threshold

	return result
}

// computeFingerprint generates a SHA256 fingerprint from DNA fields.
func computeFingerprint(dna DeviceDNA) string {
	h := sha256.New()
	h.Write([]byte(dna.MCUUID))
	h.Write([]byte(fmt.Sprintf("%.2f", dna.BootTimeAvg)))
	h.Write([]byte(fmt.Sprintf("%.2f", dna.ClockFreq)))
	h.Write([]byte(fmt.Sprintf("%d", dna.FlashWear)))
	h.Write([]byte(dna.BootloaderSig))
	return hex.EncodeToString(h.Sum(nil))
}

// meanStdDev computes mean and standard deviation of a float64 slice.
func meanStdDev(data []float64) (mean, stddev float64) {
	if len(data) == 0 {
		return 0, 0
	}
	sum := 0.0
	for _, v := range data {
		sum += v
	}
	mean = sum / float64(len(data))

	varSum := 0.0
	for _, v := range data {
		diff := v - mean
		varSum += diff * diff
	}
	stddev = math.Sqrt(varSum / float64(len(data)))
	return mean, stddev
}
